package playback

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Engine struct {
	pool *pgxpool.Pool
	mu   sync.Map
}

func New(pool *pgxpool.Pool) *Engine { return &Engine{pool: pool} }

func (e *Engine) lock(key string) *sync.Mutex {
	v, _ := e.mu.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (e *Engine) Session(ctx context.Context, kind, owner string, userID *uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := e.pool.QueryRow(ctx, `SELECT id FROM playback_sessions WHERE kind=$1 AND owner_key=$2`, kind, owner).Scan(&id)
	if err == nil {
		return id, nil
	}
	err = e.pool.QueryRow(ctx, `INSERT INTO playback_sessions (kind, owner_key, user_id) VALUES ($1,$2,$3) RETURNING id`, kind, owner, userID).Scan(&id)
	return id, err
}

func (e *Engine) Get(ctx context.Context, sid uuid.UUID) (map[string]any, error) {
	row := e.pool.QueryRow(ctx, `SELECT id, kind, owner_key, volume, repeat_mode, shuffle, crossfade_seconds, replaygain_mode, current_index, current_track_id, position_ms, status FROM playback_sessions WHERE id=$1`, sid)
	var id uuid.UUID
	var kind, owner, repeat, rg, status string
	var vol float64
	var shuffle bool
	var xf, idx, pos int
	var cur *uuid.UUID
	if err := row.Scan(&id, &kind, &owner, &vol, &repeat, &shuffle, &xf, &rg, &idx, &cur, &pos, &status); err != nil {
		return nil, err
	}
	items, _ := e.Queue(ctx, sid)
	return map[string]any{
		"id": id, "kind": kind, "owner_key": owner, "volume": vol, "repeat": repeat, "shuffle": shuffle,
		"crossfade_seconds": xf, "replaygain_mode": rg, "current_index": idx, "current_track_id": cur,
		"position_ms": pos, "status": status, "items": items,
	}, nil
}

func (e *Engine) Queue(ctx context.Context, sid uuid.UUID) ([]map[string]any, error) {
	rows, err := e.pool.Query(ctx, `SELECT id, position, track_id FROM playback_queue_items WHERE session_id=$1 ORDER BY position`, sid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var pos int
		var tid uuid.UUID
		if err := rows.Scan(&id, &pos, &tid); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "position": pos, "track_id": tid})
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, rows.Err()
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
	if _, err := tx.Exec(ctx, `DELETE FROM playback_queue_items WHERE session_id=$1`, sid); err != nil {
		return err
	}
	for i, t := range tracks {
		if _, err := tx.Exec(ctx, `INSERT INTO playback_queue_items (session_id, position, track_id) VALUES ($1,$2,$3)`, sid, i, t); err != nil {
			return err
		}
	}
	var cur any
	if start >= 0 && start < len(tracks) {
		cur = tracks[start]
	}
	_, err = tx.Exec(ctx, `UPDATE playback_sessions SET current_index=$2, current_track_id=$3, status='playing', position_ms=0, updated_at=now() WHERE id=$1`, sid, start, cur)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (e *Engine) Add(ctx context.Context, sid uuid.UUID, tracks []uuid.UUID, next bool) error {
	m := e.lock(sid.String())
	m.Lock()
	defer m.Unlock()
	var max int
	_ = e.pool.QueryRow(ctx, `SELECT coalesce(max(position),-1) FROM playback_queue_items WHERE session_id=$1`, sid).Scan(&max)
	if next {
		var cur int
		_ = e.pool.QueryRow(ctx, `SELECT current_index FROM playback_sessions WHERE id=$1`, sid).Scan(&cur)
		_, _ = e.pool.Exec(ctx, `UPDATE playback_queue_items SET position=position+$1 WHERE session_id=$2 AND position>$3`, len(tracks), sid, cur)
		for i, t := range tracks {
			_, _ = e.pool.Exec(ctx, `INSERT INTO playback_queue_items (session_id, position, track_id) VALUES ($1,$2,$3)`, sid, cur+1+i, t)
		}
		return nil
	}
	for i, t := range tracks {
		_, _ = e.pool.Exec(ctx, `INSERT INTO playback_queue_items (session_id, position, track_id) VALUES ($1,$2,$3)`, sid, max+1+i, t)
	}
	return nil
}

func (e *Engine) Control(ctx context.Context, sid uuid.UUID, action string, extra map[string]any) error {
	m := e.lock(sid.String())
	m.Lock()
	defer m.Unlock()
	switch action {
	case "pause":
		_, err := e.pool.Exec(ctx, `UPDATE playback_sessions SET status='paused', updated_at=now() WHERE id=$1`, sid)
		return err
	case "resume":
		_, err := e.pool.Exec(ctx, `UPDATE playback_sessions SET status='playing', updated_at=now() WHERE id=$1`, sid)
		return err
	case "stop":
		_, err := e.pool.Exec(ctx, `UPDATE playback_sessions SET status='stopped', position_ms=0, updated_at=now() WHERE id=$1`, sid)
		return err
	case "clear":
		_, _ = e.pool.Exec(ctx, `DELETE FROM playback_queue_items WHERE session_id=$1`, sid)
		_, err := e.pool.Exec(ctx, `UPDATE playback_sessions SET current_track_id=NULL, current_index=0, status='stopped', updated_at=now() WHERE id=$1`, sid)
		return err
	case "shuffle":
		_, err := e.pool.Exec(ctx, `UPDATE playback_sessions SET shuffle = NOT shuffle, updated_at=now() WHERE id=$1`, sid)
		return err
	case "repeat":
		mode, _ := extra["mode"].(string)
		if mode == "" {
			mode = "queue"
		}
		_, err := e.pool.Exec(ctx, `UPDATE playback_sessions SET repeat_mode=$2, updated_at=now() WHERE id=$1`, sid, mode)
		return err
	case "volume":
		vol, _ := extra["volume"].(float64)
		vol = math.Max(0, math.Min(1, vol))
		_, err := e.pool.Exec(ctx, `UPDATE playback_sessions SET volume=$2, updated_at=now() WHERE id=$1`, sid, vol)
		return err
	case "seek":
		ms, _ := extra["position_ms"].(float64)
		_, err := e.pool.Exec(ctx, `UPDATE playback_sessions SET position_ms=$2, updated_at=now() WHERE id=$1`, sid, int(ms))
		return err
	case "skip", "next":
		return e.move(ctx, sid, 1)
	case "previous":
		return e.move(ctx, sid, -1)
	case "remove":
		pos, _ := extra["position"].(float64)
		_, err := e.pool.Exec(ctx, `DELETE FROM playback_queue_items WHERE session_id=$1 AND position=$2`, sid, int(pos))
		return err
	default:
		return fmt.Errorf("unknown action")
	}
}

func (e *Engine) move(ctx context.Context, sid uuid.UUID, delta int) error {
	var idx int
	var repeat string
	var n int
	if err := e.pool.QueryRow(ctx, `SELECT current_index, repeat_mode FROM playback_sessions WHERE id=$1`, sid).Scan(&idx, &repeat); err != nil {
		return err
	}
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM playback_queue_items WHERE session_id=$1`, sid).Scan(&n)
	if n == 0 {
		return nil
	}
	next := idx + delta
	if next >= n {
		if repeat == "queue" {
			next = 0
		} else {
			next = n - 1
		}
	}
	if next < 0 {
		next = 0
	}
	var tid uuid.UUID
	err := e.pool.QueryRow(ctx, `SELECT track_id FROM playback_queue_items WHERE session_id=$1 AND position=$2`, sid, next).Scan(&tid)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	_, err = e.pool.Exec(ctx, `UPDATE playback_sessions SET current_index=$2, current_track_id=$3, position_ms=0, status='playing', updated_at=now() WHERE id=$1`, sid, next, tid)
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
	// ReplayGain is dB relative to 89 dB; apply as linear scale.
	db := *g
	if targetLUFS != 0 {
		// keep as stored gain
	}
	return math.Pow(10, db/20)
}
