package playback

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DropTracks removes tracks from every session queue and repairs the playhead.
func (e *Engine) DropTracks(ctx context.Context, tracks []uuid.UUID) error {
	if e == nil || e.pool == nil || len(tracks) == 0 {
		return nil
	}
	rows, err := e.pool.Query(ctx, `
		SELECT session_id FROM playback_queue_items WHERE track_id = ANY($1)
		UNION
		SELECT id FROM playback_sessions WHERE current_track_id = ANY($1)`, tracks)
	if err != nil {
		return err
	}
	var sids []uuid.UUID
	for rows.Next() {
		var sid uuid.UUID
		if rows.Scan(&sid) == nil && sid != uuid.Nil {
			sids = append(sids, sid)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, sid := range sids {
		if err := e.dropTracksFromSession(ctx, sid, tracks); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) dropTracksFromSession(ctx context.Context, sid uuid.UUID, tracks []uuid.UUID) error {
	unlock := e.lockSessions(sid)
	defer unlock()
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := lockSessionRow(ctx, tx, sid); err != nil {
		return err
	}
	var cur int
	var curTrack *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT current_index, current_track_id FROM playback_sessions WHERE id=$1`, sid).Scan(&cur, &curTrack); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM playback_queue_items WHERE session_id=$1 AND track_id = ANY($2)`, sid, tracks); err != nil {
		return err
	}
	ids, err := listQueueItemIDs(ctx, tx, sid)
	if err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := tx.Exec(ctx, `UPDATE playback_queue_items SET position=$2 WHERE id=$1`, id, i); err != nil {
			return err
		}
	}
	if len(ids) == 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE playback_sessions
			SET current_index=0, current_track_id=NULL, status='stopped', position_ms=0, updated_at=now()
			WHERE id=$1`, sid); err != nil {
			return err
		}
		if err := bumpRevision(ctx, tx, sid); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	keep := uuid.Nil
	if curTrack != nil {
		keep = *curTrack
		for _, gone := range tracks {
			if gone == keep {
				keep = uuid.Nil
				break
			}
		}
	}
	newIdx := cur
	newTrack := uuid.Nil
	if keep != uuid.Nil {
		err := tx.QueryRow(ctx, `SELECT position, track_id FROM playback_queue_items WHERE session_id=$1 AND track_id=$2 ORDER BY position LIMIT 1`, sid, keep).Scan(&newIdx, &newTrack)
		if err != nil {
			keep = uuid.Nil
		}
	}
	if keep == uuid.Nil {
		if newIdx >= len(ids) {
			newIdx = len(ids) - 1
		}
		if newIdx < 0 {
			newIdx = 0
		}
		if err := tx.QueryRow(ctx, `SELECT track_id FROM playback_queue_items WHERE session_id=$1 AND position=$2`, sid, newIdx).Scan(&newTrack); err != nil {
			if err == pgx.ErrNoRows {
				if _, err := tx.Exec(ctx, `
					UPDATE playback_sessions
					SET current_index=0, current_track_id=NULL, status='stopped', position_ms=0, updated_at=now()
					WHERE id=$1`, sid); err != nil {
					return err
				}
				if err := bumpRevision(ctx, tx, sid); err != nil {
					return err
				}
				return tx.Commit(ctx)
			}
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE playback_sessions SET current_index=$2, current_track_id=$3, updated_at=now() WHERE id=$1`, sid, newIdx, newTrack); err != nil {
		return err
	}
	if err := bumpRevision(ctx, tx, sid); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func listQueueItemIDs(ctx context.Context, q db, sid uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `SELECT id FROM playback_queue_items WHERE session_id=$1 ORDER BY position`, sid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
