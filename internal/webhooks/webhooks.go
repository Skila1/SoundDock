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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
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
	sum := sha256.Sum256(body)
	_, _ = b.pool.Exec(context.Background(), `INSERT INTO webhook_deliveries (endpoint_id, event, status, attempts, payload_hash, last_error) VALUES ($1,$2,$3,1,$4,$5)`,
		id, event, status, hex.EncodeToString(sum[:]), errStr)
}
