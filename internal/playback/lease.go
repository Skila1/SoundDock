package playback

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// AcquireBrowserRenderer CAS-grants a browser renderer lease.
// expectedGeneration==0 is a user-gesture steal of none/browser (not discord).
// expectedGeneration!=0 requires a matching generation or an empty (none) holder.
func (e *Engine) AcquireBrowserRenderer(ctx context.Context, sessionID uuid.UUID, clientRendererID string, expectedGeneration int64) (int64, error) {
	unlock := e.lockSessions(sessionID)
	defer unlock()
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if err := lockSessionRow(ctx, tx, sessionID); err != nil {
		return 0, err
	}
	gen, err := casAcquireBrowser(ctx, tx, sessionID, clientRendererID, expectedGeneration, false)
	if err != nil {
		return 0, err
	}
	if err := bumpRevision(ctx, tx, sessionID); err != nil {
		return 0, err
	}
	return gen, tx.Commit(ctx)
}

// HeartbeatRenderer updates renderer_heartbeat_at only if the CAS identity matches.
// It does not bump state_revision or playhead_sequence.
func (e *Engine) HeartbeatRenderer(ctx context.Context, sessionID uuid.UUID, kind, rendererID string, generation int64) error {
	tag, err := e.pool.Exec(ctx, `
		UPDATE playback_sessions
		SET renderer_heartbeat_at=now(), updated_at=now()
		WHERE id=$1 AND renderer_kind=$2 AND renderer_id=$3 AND renderer_generation=$4`,
		sessionID, kind, rendererID, generation)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseConflict
	}
	return nil
}

// ReleaseRenderer CAS-releases a lease only if kind/id/generation still match.
func (e *Engine) ReleaseRenderer(ctx context.Context, sessionID uuid.UUID, kind, rendererID string, generation int64) error {
	unlock := e.lockSessions(sessionID)
	defer unlock()
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := lockSessionRow(ctx, tx, sessionID); err != nil {
		return err
	}
	ok, err := casRelease(ctx, tx, sessionID, kind, rendererID, generation)
	if err != nil {
		return err
	}
	if !ok {
		return ErrLeaseConflict
	}
	if err := bumpRevision(ctx, tx, sessionID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func casAcquireBrowser(ctx context.Context, q db, sessionID uuid.UUID, clientRendererID string, expectedGeneration int64, stealDiscord bool) (int64, error) {
	var gen int64
	var err error
	if stealDiscord {
		err = q.QueryRow(ctx, `
			UPDATE playback_sessions
			SET renderer_kind=$2, renderer_id=$3,
				renderer_generation=COALESCE(renderer_generation,0)+1,
				renderer_heartbeat_at=now(), updated_at=now()
			WHERE id=$1 AND (
				renderer_kind='none'
				OR renderer_kind='discord'
				OR renderer_kind='browser'
			)
			RETURNING renderer_generation`, sessionID, RendererBrowser, clientRendererID).Scan(&gen)
	} else if expectedGeneration == 0 {
		err = q.QueryRow(ctx, `
			UPDATE playback_sessions
			SET renderer_kind=$2, renderer_id=$3,
				renderer_generation=COALESCE(renderer_generation,0)+1,
				renderer_heartbeat_at=now(), updated_at=now()
			WHERE id=$1 AND renderer_kind IN ('none','browser')
			RETURNING renderer_generation`, sessionID, RendererBrowser, clientRendererID).Scan(&gen)
	} else {
		err = q.QueryRow(ctx, `
			UPDATE playback_sessions
			SET renderer_kind=$2, renderer_id=$3,
				renderer_generation=COALESCE(renderer_generation,0)+1,
				renderer_heartbeat_at=now(), updated_at=now()
			WHERE id=$1 AND (
				renderer_kind='none'
				OR (renderer_kind='browser' AND renderer_generation=$4)
			)
			RETURNING renderer_generation`, sessionID, RendererBrowser, clientRendererID, expectedGeneration).Scan(&gen)
	}
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, ErrLeaseConflict
		}
		return 0, err
	}
	return gen, nil
}

func casGrantDiscord(ctx context.Context, q db, sessionID uuid.UUID, rendererID string, generation int64) error {
	tag, err := q.Exec(ctx, `
		UPDATE playback_sessions
		SET renderer_kind=$2, renderer_id=$3, renderer_generation=$4,
			renderer_heartbeat_at=now(), output_pref=$5, updated_at=now()
		WHERE id=$1`, sessionID, RendererDiscord, rendererID, generation, OutputDiscord)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseConflict
	}
	return nil
}

func casReleaseDiscordIfHeld(ctx context.Context, q db, sessionID uuid.UUID, resetOutput bool) (bool, error) {
	outputSQL := ""
	if resetOutput {
		outputSQL = `, output_pref=CASE WHEN output_pref='discord' THEN 'browser' ELSE output_pref END`
	}
	tag, err := q.Exec(ctx, `
		UPDATE playback_sessions
		SET renderer_kind='none', renderer_id=NULL,
			renderer_generation=renderer_generation+1,
			renderer_heartbeat_at=NULL, updated_at=now()`+outputSQL+`
		WHERE id=$1 AND renderer_kind='discord'`, sessionID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	return true, nil
}

func casRelease(ctx context.Context, q db, sessionID uuid.UUID, kind, rendererID string, generation int64) (bool, error) {
	tag, err := q.Exec(ctx, `
		UPDATE playback_sessions
		SET renderer_kind='none', renderer_id=NULL,
			renderer_generation=renderer_generation+1,
			renderer_heartbeat_at=NULL, updated_at=now()
		WHERE id=$1 AND renderer_kind=$2 AND renderer_id=$3 AND renderer_generation=$4`,
		sessionID, kind, rendererID, generation)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func bumpRevision(ctx context.Context, q db, sessionID uuid.UUID) error {
	_, err := q.Exec(ctx, `UPDATE playback_sessions SET state_revision=state_revision+1, updated_at=now() WHERE id=$1`, sessionID)
	return err
}

func lockSessionRow(ctx context.Context, q db, sessionID uuid.UUID) error {
	var n int
	return q.QueryRow(ctx, `SELECT 1 FROM playback_sessions WHERE id=$1 FOR UPDATE`, sessionID).Scan(&n)
}
