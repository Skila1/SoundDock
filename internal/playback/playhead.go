package playback

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// checkpointMaxDriftMS is how far a progress write may jump from the stored
// position. A user seek is a larger jump; ignoring those writes stops a
// Discord streamer from overwriting the seek with the pre-seek elapsed time.
const checkpointMaxDriftMS = 8000

// CheckpointPlayhead writes position_ms, checkpoint_at, and increments playhead_sequence.
// It does not bump state_revision. instanceID must match playback_instance_id.
// Writes that jump more than checkpointMaxDriftMS are ignored so a seek wins.
// Sequence 1 (fresh instance) may jump so a slow first ffmpeg packet still lands.
func (e *Engine) CheckpointPlayhead(ctx context.Context, sessionID, instanceID uuid.UUID, positionMS int) error {
	if positionMS < 0 {
		positionMS = 0
	}
	tag, err := e.pool.Exec(ctx, `
		UPDATE playback_sessions
		SET position_ms=$3, checkpoint_at=now(), playhead_sequence=playhead_sequence+1, updated_at=now()
		WHERE id=$1 AND playback_instance_id=$2
		  AND (playhead_sequence <= 1 OR abs(position_ms - $3) <= $4)`, sessionID, instanceID, positionMS, checkpointMaxDriftMS)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		e.notifySession(ctx, sessionID, "session.playhead")
		return nil
	}
	var found uuid.UUID
	err = e.pool.QueryRow(ctx, `SELECT playback_instance_id FROM playback_sessions WHERE id=$1`, sessionID).Scan(&found)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInstanceMismatch
		}
		return err
	}
	if found != instanceID {
		return ErrInstanceMismatch
	}
	return nil
}

const sqlNewInstance = `playback_instance_id=gen_random_uuid(), playhead_sequence=1, position_ms=0, checkpoint_at=now()`

// sqlStampPlayhead is the seek/pause checkpoint triple. Resume must not use it.
const sqlStampPlayhead = `checkpoint_at=now(), playhead_sequence=playhead_sequence+1`

// sqlEmptySession matches replace-empty: stopped, no instance, sequence 0.
const sqlEmptySession = `current_index=0, current_track_id=NULL, status='stopped', position_ms=0, playback_instance_id=NULL, playhead_sequence=0, duration_ms=0`
