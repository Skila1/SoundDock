package playback

import (
	"context"

	"github.com/google/uuid"
)

// CheckpointPlayhead writes position_ms, checkpoint_at, and increments playhead_sequence.
// It does not bump state_revision. instanceID must match playback_instance_id.
func (e *Engine) CheckpointPlayhead(ctx context.Context, sessionID, instanceID uuid.UUID, positionMS int) error {
	if positionMS < 0 {
		positionMS = 0
	}
	tag, err := e.pool.Exec(ctx, `
		UPDATE playback_sessions
		SET position_ms=$3, checkpoint_at=now(), playhead_sequence=playhead_sequence+1, updated_at=now()
		WHERE id=$1 AND playback_instance_id=$2`, sessionID, instanceID, positionMS)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrInstanceMismatch
	}
	return nil
}

const sqlNewInstance = `playback_instance_id=gen_random_uuid(), playhead_sequence=1, position_ms=0, checkpoint_at=now()`
