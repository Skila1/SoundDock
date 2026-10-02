package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
	"github.com/sounddock/sounddock/internal/oplog"
)

type Bus struct {
	pool *pgxpool.Pool
	box  *cryptox.Box
	log  *slog.Logger
}

func New(pool *pgxpool.Pool, box *cryptox.Box, log *slog.Logger) *Bus {
	return &Bus{pool: pool, box: box, log: log}
}

func (b *Bus) Emit(ctx context.Context, event string, payload map[string]any) {
	if b == nil {
		return
	}
	recordEvent(ctx, event, payload)
	body, _ := json.Marshal(map[string]any{"event": event, "payload": payload, "ts": time.Now().UTC()})
	rows, err := b.pool.Query(ctx, `SELECT id, url, secret_enc FROM webhook_endpoints WHERE enabled AND $1 = ANY(events)`, event)
	if err != nil {
		return
	}
	defer rows.Close()
	type ep struct {
		id  string
		url string
		sec []byte
	}
	var list []ep
	for rows.Next() {
		var e ep
		if err := rows.Scan(&e.id, &e.url, &e.sec); err == nil {
			list = append(list, e)
		}
	}
	for _, e := range list {
		go b.deliver(e.id, e.url, e.sec, event, body)
	}
}

func (b *Bus) deliver(id, url string, secEnc []byte, event string, body []byte) {
	secret := secEnc
	if b.box != nil && len(secEnc) > 0 {
		if p, err := b.box.Decrypt(secEnc); err == nil {
			secret = p
		}
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-SoundDock-Event", event)
	req.Header.Set("X-SoundDock-Signature", "sha256="+sig)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	status := "failed"
	errStr := ""
	if err != nil {
		errStr = err.Error()
	} else {
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			status = "ok"
		} else {
			errStr = resp.Status
		}
	}
	if status != "ok" {
		oplog.Emit(context.Background(), oplog.Entry{
			Level:    "warn",
			Category: "integrations",
			Action:   "webhook.delivery_failed",
			Message:  "Webhook delivery failed for " + event,
			Error:    errStr,
			Result:   oplog.ResultFailure,
			Details:  map[string]any{"endpoint_id": id, "event": event, "host": hostOf(url)},
		})
	}
	sum := sha256.Sum256(body)
	_, _ = b.pool.Exec(context.Background(), `INSERT INTO webhook_deliveries (endpoint_id, event, status, attempts, payload_hash, last_error) VALUES ($1,$2,$3,1,$4,$5)`,
		id, event, status, hex.EncodeToString(sum[:]), errStr)
}

// recordEvent mirrors domain events into the activity log, whether or not
// any webhook endpoint subscribes to them.
func recordEvent(ctx context.Context, event string, payload map[string]any) {
	if event == "track.added" {
		// One per scanned file; the scan completion entry summarises them.
		return
	}
	category := "app"
	switch {
	case strings.HasPrefix(event, "playback."):
		category = "playback"
	case strings.HasPrefix(event, "playlist."):
		category = "playlists"
	case strings.HasPrefix(event, "external."):
		category = "integrations"
	case strings.HasPrefix(event, "library."):
		category = "library"
	}
	e := oplog.Entry{
		Level:    "info",
		Category: category,
		Action:   event,
		Message:  eventLabel(event),
		Result:   oplog.ResultSuccess,
		Details:  map[string]any{},
	}
	for k, v := range payload {
		e.Details[k] = v
	}
	if id, ok := uuidField(payload, "track_id"); ok {
		e.TrackID = &id
	}
	if id, ok := uuidField(payload, "job_id"); ok {
		e.JobID = &id
	}
	if id, ok := uuidField(payload, "library_id"); ok {
		e.LibraryID = &id
	}
	oplog.Emit(ctx, e)
}

func eventLabel(event string) string {
	switch event {
	case "playback.started":
		return "Playback started"
	case "playback.finished":
		return "Playback stopped"
	case "playlist.created":
		return "Playlist created"
	case "library.scan.completed":
		return "Library scan completed"
	case "external.provider.connected":
		return "Playlist provider connected"
	case "external.provider.disconnected":
		return "Playlist provider disconnected"
	case "external.playlist.imported":
		return "Playlist import queued"
	case "external.playlist.sync.completed":
		return "Playlist sync completed"
	case "external.track.matched":
		return "Playlist track matched"
	}
	return strings.ReplaceAll(event, ".", " ")
}

func uuidField(m map[string]any, k string) (uuid.UUID, bool) {
	switch v := m[k].(type) {
	case uuid.UUID:
		return v, v != uuid.Nil
	case *uuid.UUID:
		if v != nil {
			return *v, *v != uuid.Nil
		}
	case string:
		if id, err := uuid.Parse(v); err == nil {
			return id, true
		}
	}
	return uuid.Nil, false
}

func hostOf(raw string) string {
	raw = strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
	if i := strings.IndexAny(raw, "/?#"); i >= 0 {
		raw = raw[:i]
	}
	if i := strings.LastIndex(raw, "@"); i >= 0 {
		raw = raw[i+1:]
	}
	return raw
}
