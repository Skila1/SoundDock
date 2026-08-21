package playback

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (e *Engine) Control(ctx context.Context, sid uuid.UUID, action string, extra map[string]any) error {
	m := e.lock(sid.String())
	m.Lock()
	defer m.Unlock()
	if extra == nil {
		extra = map[string]any{}
	}
	if v, ok := extraBool(extra, "stop_after_current"); ok {
		if _, err := e.pool.Exec(ctx, `UPDATE playback_sessions SET stop_after_current=$2, updated_at=now() WHERE id=$1`, sid, v); err != nil {
			return err
		}
	}
	if mode := extraString(extra, "shuffle_mode"); mode != "" {
		switch mode {
		case "smart", "random", "album":
			if _, err := e.pool.Exec(ctx, `UPDATE playback_sessions SET shuffle_mode=$2, updated_at=now() WHERE id=$1`, sid, mode); err != nil {
				return err
			}
		}
	}
	if ms, ok := extraInt(extra, "position_ms"); ok && action != "seek" && action != "stop" {
		if _, err := e.pool.Exec(ctx, `UPDATE playback_sessions SET position_ms=$2, updated_at=now() WHERE id=$1`, sid, ms); err != nil {
			return err
		}
	}
	ended, _ := extraBool(extra, "ended")
	switch action {
	case "pause":
		_, err := e.pool.Exec(ctx, `UPDATE playback_sessions SET status='paused', updated_at=now() WHERE id=$1`, sid)
		return err
	case "resume":
		_, err := e.pool.Exec(ctx, `UPDATE playback_sessions SET status='playing', updated_at=now() WHERE id=$1`, sid)
		return err
	case "stop":
		_, err := e.pool.Exec(ctx, `UPDATE playback_sessions SET status='stopped', position_ms=0, stop_after_current=false, updated_at=now() WHERE id=$1`, sid)
		return err
	case "clear":
		if _, err := e.pool.Exec(ctx, `DELETE FROM playback_queue_items WHERE session_id=$1`, sid); err != nil {
			return err
		}
		_, err := e.pool.Exec(ctx, `UPDATE playback_sessions SET current_track_id=NULL, current_index=0, status='stopped', position_ms=0, updated_at=now() WHERE id=$1`, sid)
		return err
	case "shuffle":
		_, err := e.pool.Exec(ctx, `UPDATE playback_sessions SET shuffle = NOT shuffle, updated_at=now() WHERE id=$1`, sid)
		return err
	case "repeat":
		mode := extraString(extra, "mode")
		if mode == "" {
			mode = "queue"
		}
		switch mode {
		case "off", "queue", "one":
		default:
			return fmt.Errorf("invalid repeat mode")
		}
		_, err := e.pool.Exec(ctx, `UPDATE playback_sessions SET repeat_mode=$2, updated_at=now() WHERE id=$1`, sid, mode)
		return err
	case "volume":
		vol, ok := extra["volume"].(float64)
		if !ok {
			if i, ok := extraInt(extra, "volume"); ok {
				vol = float64(i)
			}
		}
		if vol < 0 {
			vol = 0
		}
		if vol > 1 {
			vol = 1
		}
		_, err := e.pool.Exec(ctx, `UPDATE playback_sessions SET volume=$2, updated_at=now() WHERE id=$1`, sid, vol)
		return err
	case "seek":
		ms, _ := extraInt(extra, "position_ms")
		if ms < 0 {
			ms = 0
		}
		_, err := e.pool.Exec(ctx, `UPDATE playback_sessions SET position_ms=$2, updated_at=now() WHERE id=$1`, sid, ms)
		return err
	case "skip", "next":
		return e.move(ctx, sid, 1, ended)
	case "previous":
		return e.move(ctx, sid, -1, false)
	case "remove":
		pos, _ := extraInt(extra, "position")
		return e.removeAt(ctx, sid, pos)
	case "reorder":
		from, okFrom := extraInt(extra, "from")
		to, okTo := extraInt(extra, "to")
		if !okFrom || !okTo {
			return fmt.Errorf("reorder requires from and to")
		}
		return e.reorder(ctx, sid, from, to)
	default:
		return fmt.Errorf("unknown action")
	}
}

func (e *Engine) move(ctx context.Context, sid uuid.UUID, delta int, ended bool) error {
	var idx int
	var repeat, mode string
	var shuffle, stopAfter bool
	if err := e.pool.QueryRow(ctx, `
		SELECT current_index, repeat_mode, shuffle, shuffle_mode, stop_after_current
		FROM playback_sessions WHERE id=$1`, sid).Scan(&idx, &repeat, &shuffle, &mode, &stopAfter); err != nil {
		return err
	}
	if stopAfter && delta > 0 {
		_, err := e.pool.Exec(ctx, `UPDATE playback_sessions SET status='stopped', stop_after_current=false, updated_at=now() WHERE id=$1`, sid)
		return err
	}
	items, err := e.queueMeta(ctx, sid)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	next, stop := nextIndex(items, idx, delta, repeat, mode, shuffle, ended, e.intn)
	if stop {
		_, err = e.pool.Exec(ctx, `UPDATE playback_sessions SET status='stopped', stop_after_current=false, updated_at=now() WHERE id=$1`, sid)
		return err
	}
	tid := items[next].TrackID
	_, err = e.pool.Exec(ctx, `UPDATE playback_sessions SET current_index=$2, current_track_id=$3, position_ms=0, status='playing', updated_at=now() WHERE id=$1`, sid, next, tid)
	return err
}

func (e *Engine) queueMeta(ctx context.Context, sid uuid.UUID) ([]queueMeta, error) {
	rows, err := e.pool.Query(ctx, `
		SELECT q.position, q.track_id,
			coalesce(t.album_id, '00000000-0000-0000-0000-000000000000'),
			coalesce(t.disc_number, 1), coalesce(t.track_number, 0),
			coalesce((
				SELECT ta.artist_id FROM track_artists ta
				WHERE ta.track_id=q.track_id AND ta.role='primary'
				ORDER BY ta.position LIMIT 1
			), '00000000-0000-0000-0000-000000000000')
		FROM playback_queue_items q
		LEFT JOIN tracks t ON t.id=q.track_id
		WHERE q.session_id=$1
		ORDER BY q.position`, sid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []queueMeta
	for rows.Next() {
		var it queueMeta
		if err := rows.Scan(&it.Position, &it.TrackID, &it.AlbumID, &it.Disc, &it.TrackNo, &it.ArtistID); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (e *Engine) removeAt(ctx context.Context, sid uuid.UUID, pos int) error {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var cur int
	if err := tx.QueryRow(ctx, `SELECT current_index FROM playback_sessions WHERE id=$1`, sid).Scan(&cur); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM playback_queue_items WHERE session_id=$1 AND position=$2`, sid, pos)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE playback_queue_items SET position=position-1 WHERE session_id=$1 AND position>$2`, sid, pos); err != nil {
		return err
	}
	newIdx := cur
	if pos < cur {
		newIdx = cur - 1
	}
	if newIdx < 0 {
		newIdx = 0
	}
	var tid uuid.UUID
	err = tx.QueryRow(ctx, `SELECT track_id FROM playback_queue_items WHERE session_id=$1 AND position=$2`, sid, newIdx).Scan(&tid)
	if err == pgx.ErrNoRows {
		_, err = tx.Exec(ctx, `UPDATE playback_sessions SET current_index=0, current_track_id=NULL, status='stopped', position_ms=0, updated_at=now() WHERE id=$1`, sid)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE playback_sessions SET current_index=$2, current_track_id=$3, updated_at=now() WHERE id=$1`, sid, newIdx, tid)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (e *Engine) reorder(ctx context.Context, sid uuid.UUID, from, to int) error {
	items, err := e.Queue(ctx, sid)
	if err != nil {
		return err
	}
	n := len(items)
	if from < 0 || from >= n || to < 0 || to >= n {
		return fmt.Errorf("invalid reorder")
	}
	if from == to {
		return nil
	}
	type row struct {
		id    uuid.UUID
		track uuid.UUID
	}
	rows := make([]row, n)
	for i, it := range items {
		rows[i] = row{id: it["id"].(uuid.UUID), track: it["track_id"].(uuid.UUID)}
	}
	moved := rows[from]
	rest := append(append([]row{}, rows[:from]...), rows[from+1:]...)
	movedRows := append(append([]row{}, rest[:to]...), append([]row{moved}, rest[to:]...)...)

	var cur int
	if err := e.pool.QueryRow(ctx, `SELECT current_index FROM playback_sessions WHERE id=$1`, sid).Scan(&cur); err != nil {
		return err
	}
	newIdx := cur
	if cur == from {
		newIdx = to
	} else if from < cur && to >= cur {
		newIdx = cur - 1
	} else if from > cur && to <= cur {
		newIdx = cur + 1
	}

	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for i, r := range movedRows {
		if _, err := tx.Exec(ctx, `UPDATE playback_queue_items SET position=$2 WHERE id=$1`, r.id, i); err != nil {
			return err
		}
	}
	var tid uuid.UUID
	if newIdx >= 0 && newIdx < len(movedRows) {
		tid = movedRows[newIdx].track
	}
	if _, err := tx.Exec(ctx, `UPDATE playback_sessions SET current_index=$2, current_track_id=$3, updated_at=now() WHERE id=$1`, sid, newIdx, tid); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
