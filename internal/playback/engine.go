package playback

import (
	"context"
	"math"
	"math/rand"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
		SELECT id, kind, owner_key, volume, repeat_mode, shuffle, crossfade_seconds, replaygain_mode,
			current_index, current_track_id, position_ms, status, shuffle_mode, stop_after_current, device_id
		FROM playback_sessions WHERE id=$1`, sid)
	var id uuid.UUID
	var kind, owner, repeat, rg, status, shuffleMode string
	var vol float64
	var shuffle, stopAfter bool
	var xf, idx, pos int
	var cur *uuid.UUID
	var deviceID *string
	if err := row.Scan(&id, &kind, &owner, &vol, &repeat, &shuffle, &xf, &rg, &idx, &cur, &pos, &status, &shuffleMode, &stopAfter, &deviceID); err != nil {
		return nil, err
	}
	items, _ := e.Queue(ctx, sid)
	return map[string]any{
		"id": id, "kind": kind, "owner_key": owner, "volume": vol, "repeat": repeat, "shuffle": shuffle,
		"crossfade_seconds": xf, "replaygain_mode": rg, "current_index": idx, "current_track_id": cur,
		"position_ms": pos, "status": status, "items": items,
		"shuffle_mode": shuffleMode, "stop_after_current": stopAfter, "device_id": deviceID,
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
	if start < 0 || start >= len(tracks) {
		start = 0
	}
	var cur any
	if start < len(tracks) {
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
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if next {
		var cur int
		if err := tx.QueryRow(ctx, `SELECT current_index FROM playback_sessions WHERE id=$1`, sid).Scan(&cur); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE playback_queue_items SET position=position+$1 WHERE session_id=$2 AND position>$3`, len(tracks), sid, cur); err != nil {
			return err
		}
		for i, t := range tracks {
			if _, err := tx.Exec(ctx, `INSERT INTO playback_queue_items (session_id, position, track_id) VALUES ($1,$2,$3)`, sid, cur+1+i, t); err != nil {
				return err
			}
		}
		return tx.Commit(ctx)
	}
	var max int
	if err := tx.QueryRow(ctx, `SELECT coalesce(max(position),-1) FROM playback_queue_items WHERE session_id=$1`, sid).Scan(&max); err != nil {
		return err
	}
	for i, t := range tracks {
		if _, err := tx.Exec(ctx, `INSERT INTO playback_queue_items (session_id, position, track_id) VALUES ($1,$2,$3)`, sid, max+1+i, t); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (e *Engine) SetPosition(ctx context.Context, sid uuid.UUID, ms int) error {
	if ms < 0 {
		ms = 0
	}
	_, err := e.pool.Exec(ctx, `UPDATE playback_sessions SET position_ms=$2, updated_at=now() WHERE id=$1`, sid, ms)
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
