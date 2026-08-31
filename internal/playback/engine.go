package playback

import (
	"context"
	"math"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/minilib"
)

type requesterCtxKey struct{}
type originCtxKey struct{}

// Requester is the user (and optional Discord id) attributed on queue INSERTs.
type Requester struct {
	UserID        uuid.UUID
	DiscordUserID string
}

func WithRequester(ctx context.Context, userID uuid.UUID, discordID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, requesterCtxKey{}, Requester{UserID: userID, DiscordUserID: discordID})
}

func WithOrigin(ctx context.Context, origin string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, originCtxKey{}, origin)
}

func originFrom(ctx context.Context) string {
	if ctx != nil {
		if v, ok := ctx.Value(originCtxKey{}).(string); ok {
			switch v {
			case OriginUser, OriginAutoplay, OriginRadio:
				return v
			}
		}
	}
	return OriginUser
}

func requesterFrom(ctx context.Context) (userID any, discordID any) {
	if ctx == nil {
		return nil, nil
	}
	v, ok := ctx.Value(requesterCtxKey{}).(Requester)
	if !ok {
		return nil, nil
	}
	if v.UserID != uuid.Nil {
		userID = v.UserID
	}
	if s := strings.TrimSpace(v.DiscordUserID); s != "" {
		discordID = s
	}
	return userID, discordID
}

func insertQueueTrack(ctx context.Context, tx db, sid uuid.UUID, position int, trackID uuid.UUID) error {
	origin := originFrom(ctx)
	userID, discordID := requesterFrom(ctx)
	_, err := tx.Exec(ctx, `
		INSERT INTO playback_queue_items (session_id, position, track_id, origin, requested_by_user_id, requested_by_discord_user_id)
		VALUES ($1,$2,$3,$4,$5,$6)`, sid, position, trackID, origin, userID, discordID)
	if err != nil {
		return err
	}
	var uid uuid.UUID
	if id, ok := userID.(uuid.UUID); ok {
		uid = id
	}
	did := ""
	if s, ok := discordID.(string); ok {
		did = s
	}
	return minilib.Record(ctx, tx, origin, uid, did, []uuid.UUID{trackID})
}

type db interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Engine struct {
	pool *pgxpool.Pool
	mu   sync.Map
	rnd  *rand.Rand
	rndM sync.Mutex
}

func New(pool *pgxpool.Pool) *Engine { return &Engine{pool: pool} }

func (e *Engine) lock(key string) *sync.Mutex {
	v, _ := e.mu.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (e *Engine) lockSessions(ids ...uuid.UUID) func() {
	seen := map[string]struct{}{}
	var keys []string
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		k := id.String()
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var mus []*sync.Mutex
	for _, k := range keys {
		m := e.lock(k)
		m.Lock()
		mus = append(mus, m)
	}
	return func() {
		for i := len(mus) - 1; i >= 0; i-- {
			mus[i].Unlock()
		}
	}
}

func (e *Engine) intn(n int) int {
	if n <= 1 {
		return 0
	}
	e.rndM.Lock()
	defer e.rndM.Unlock()
	if e.rnd != nil {
		return e.rnd.Intn(n)
	}
	return rand.Intn(n)
}

func (e *Engine) Get(ctx context.Context, sid uuid.UUID) (map[string]any, error) {
	row := e.pool.QueryRow(ctx, `
		SELECT s.id, s.kind, s.owner_key, s.volume, s.repeat_mode, s.shuffle, s.crossfade_seconds, s.replaygain_mode,
			s.current_index, s.current_track_id, s.position_ms, s.status, s.shuffle_mode, s.stop_after_current, s.device_id,
			s.state_revision, s.playhead_sequence, s.playback_instance_id, s.muted, s.output_pref, s.autoplay,
			s.renderer_kind, s.renderer_id, s.renderer_generation, s.checkpoint_at, s.duration_ms, s.playback_rate,
			s.renderer_heartbeat_at,
			(SELECT r.binding_revision FROM discord_voice_runtime r WHERE r.session_id=s.id ORDER BY r.binding_revision DESC LIMIT 1)
		FROM playback_sessions s WHERE s.id=$1`, sid)
	var id uuid.UUID
	var kind, owner, repeat, rg, status, shuffleMode, outputPref, rendererKind string
	var vol, playbackRate float64
	var shuffle, stopAfter, muted, autoplay bool
	var xf, idx, pos, durationMS int
	var stateRev, playheadSeq, rendererGen int64
	var cur, instanceID *uuid.UUID
	var deviceID, rendererID *string
	var checkpointAt, heartbeatAt *time.Time
	var bindingRev *int64
	if err := row.Scan(&id, &kind, &owner, &vol, &repeat, &shuffle, &xf, &rg, &idx, &cur, &pos, &status, &shuffleMode, &stopAfter, &deviceID,
		&stateRev, &playheadSeq, &instanceID, &muted, &outputPref, &autoplay,
		&rendererKind, &rendererID, &rendererGen, &checkpointAt, &durationMS, &playbackRate, &heartbeatAt, &bindingRev); err != nil {
		return nil, err
	}
	items, _ := e.Queue(ctx, sid)
	return map[string]any{
		"id": id, "kind": kind, "owner_key": owner, "volume": vol, "repeat": repeat, "shuffle": shuffle,
		"crossfade_seconds": xf, "replaygain_mode": rg, "current_index": idx, "current_track_id": cur,
		"position_ms": pos, "status": status, "items": items,
		"shuffle_mode": shuffleMode, "stop_after_current": stopAfter, "device_id": deviceID,
		"state_revision": stateRev, "playhead_sequence": playheadSeq, "playback_instance_id": instanceID,
		"muted": muted, "output_pref": outputPref, "autoplay": autoplay,
		"renderer_kind": rendererKind, "renderer_id": rendererID, "renderer_generation": rendererGen,
		"checkpoint_at": checkpointAt, "duration_ms": durationMS, "playback_rate": playbackRate,
		"renderer_heartbeat_at": heartbeatAt, "binding_revision": bindingRev,
	}, nil
}

func (e *Engine) Queue(ctx context.Context, sid uuid.UUID) ([]map[string]any, error) {
	return e.queue(ctx, e.pool, sid)
}

func (e *Engine) queue(ctx context.Context, q db, sid uuid.UUID) ([]map[string]any, error) {
	rows, err := q.Query(ctx, `
		SELECT q.id, q.position, q.track_id, q.origin,
			q.requested_by_user_id, q.requested_by_discord_user_id, u.display_name,
			coalesce(t.title,''), coalesce(t.duration_ms,0), coalesce(t.acquisition_ref,''),
			coalesce((
				SELECT string_agg(ar.name, ', ' ORDER BY ta.position)
				FROM track_artists ta JOIN artists ar ON ar.id=ta.artist_id
				WHERE ta.track_id=t.id AND ta.role='primary'
			),'')
		FROM playback_queue_items q
		LEFT JOIN users u ON u.id = q.requested_by_user_id
		LEFT JOIN tracks t ON t.id = q.track_id
		WHERE q.session_id=$1
		ORDER BY q.position`, sid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var pos int
		var tid uuid.UUID
		var origin string
		var reqUser *uuid.UUID
		var reqDiscord, display *string
		var title, acqRef, artist string
		var duration int
		if err := rows.Scan(&id, &pos, &tid, &origin, &reqUser, &reqDiscord, &display, &title, &duration, &acqRef, &artist); err != nil {
			return nil, err
		}
		item := map[string]any{"id": id, "position": pos, "track_id": tid, "origin": origin}
		if rb := requestedByMap(reqUser, reqDiscord, display); rb != nil {
			item["requested_by"] = rb
		}
		if title != "" {
			item["title"] = title
		}
		if artist != "" {
			item["artist"] = artist
		}
		if duration > 0 {
			item["duration_ms"] = duration
		}
		if acqRef != "" {
			item["youtube_id"] = acqRef
		}
		out = append(out, item)
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, rows.Err()
}

func requestedByMap(userID *uuid.UUID, discordID, displayName *string) map[string]any {
	out := map[string]any{}
	if userID != nil && *userID != uuid.Nil {
		out["user_id"] = *userID
	}
	if discordID != nil && strings.TrimSpace(*discordID) != "" {
		out["discord_user_id"] = strings.TrimSpace(*discordID)
	}
	if displayName != nil && strings.TrimSpace(*displayName) != "" {
		out["display_name"] = strings.TrimSpace(*displayName)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (e *Engine) Replace(ctx context.Context, sid uuid.UUID, tracks []uuid.UUID, start int) error {
	m := e.lock(sid.String())
	m.Lock()
	defer m.Unlock()
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := lockSessionRow(ctx, tx, sid); err != nil {
		return err
	}
	if err := replaceQueueTx(ctx, tx, sid, tracks, start); err != nil {
		return err
	}
	if err := bumpRevision(ctx, tx, sid); err != nil {
		return err
	}
	return e.commitSession(ctx, tx, sid, "session.state")
}

func replaceQueueTx(ctx context.Context, tx db, sid uuid.UUID, tracks []uuid.UUID, start int) error {
	if _, err := tx.Exec(ctx, `DELETE FROM playback_queue_items WHERE session_id=$1`, sid); err != nil {
		return err
	}
	for i, t := range tracks {
		if err := insertQueueTrack(ctx, tx, sid, i, t); err != nil {
			return err
		}
	}
	if start < 0 || start >= len(tracks) {
		start = 0
	}
	var cur any
	if start < len(tracks) {
		cur = tracks[start]
	}
	var err error
	if len(tracks) == 0 {
		_, err = tx.Exec(ctx, `
			UPDATE playback_sessions
			SET `+sqlEmptySession+`, updated_at=now()
			WHERE id=$1`, sid)
	} else {
		_, err = tx.Exec(ctx, `
			UPDATE playback_sessions
			SET current_index=$2, current_track_id=$3, status='playing',
				`+sqlNewInstance+`,
				duration_ms=COALESCE((SELECT t.duration_ms FROM tracks t WHERE t.id=$3), 0),
				updated_at=now()
			WHERE id=$1`, sid, start, cur)
	}
	return err
}

func (e *Engine) Add(ctx context.Context, sid uuid.UUID, tracks []uuid.UUID, next bool) error {
	m := e.lock(sid.String())
	m.Lock()
	defer m.Unlock()
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := lockSessionRow(ctx, tx, sid); err != nil {
		return err
	}
	if err := addTracksTx(ctx, tx, sid, tracks, next); err != nil {
		return err
	}
	if err := bumpRevision(ctx, tx, sid); err != nil {
		return err
	}
	return e.commitSession(ctx, tx, sid, "session.state")
}

func addTracksTx(ctx context.Context, tx db, sid uuid.UUID, tracks []uuid.UUID, next bool) error {
	if len(tracks) == 0 {
		return nil
	}
	if next {
		var cur int
		if err := tx.QueryRow(ctx, `SELECT current_index FROM playback_sessions WHERE id=$1`, sid).Scan(&cur); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE playback_queue_items SET position=position+$1 WHERE session_id=$2 AND position>$3`, len(tracks), sid, cur); err != nil {
			return err
		}
		for i, t := range tracks {
			if err := insertQueueTrack(ctx, tx, sid, cur+1+i, t); err != nil {
				return err
			}
		}
		return nil
	}
	var max int
	if err := tx.QueryRow(ctx, `SELECT coalesce(max(position),-1) FROM playback_queue_items WHERE session_id=$1`, sid).Scan(&max); err != nil {
		return err
	}
	for i, t := range tracks {
		if err := insertQueueTrack(ctx, tx, sid, max+1+i, t); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) SetPosition(ctx context.Context, sid uuid.UUID, ms int) error {
	if ms < 0 {
		ms = 0
	}
	// Listen must not overwrite a renderer owner's playhead.
	_, err := e.pool.Exec(ctx, `
		UPDATE playback_sessions SET position_ms=$2, updated_at=now()
		WHERE id=$1 AND renderer_kind='none'`, sid, ms)
	return err
}

func ReplayGainMultiplier(mode string, trackGain, albumGain *float64, targetLUFS float64) float64 {
	if mode == "off" || mode == "" {
		return 1
	}
	var g *float64
	if mode == "album" && albumGain != nil {
		g = albumGain
	} else {
		g = trackGain
	}
	if g == nil {
		return 1
	}
	db := *g
	if targetLUFS != 0 {
		// keep as stored gain
	}
	return math.Pow(10, db/20)
}
