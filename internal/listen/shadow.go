package listen

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Checkpoint struct {
	TrackID            uuid.UUID
	PositionMS         int
	DurationMS         int
	Source             string
	Kind               string
	StopAfter          bool
	PlaybackInstanceID uuid.UUID
	PlayheadSequence   int64
	ClientID           string
	DeviceID           string
	Status             string
	PlaybackRate       float64
	RendererKind       string
	RendererID         string
	AudioListener      *bool
	At                 time.Time
}

func ApplyShadow(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, cp Checkpoint) error {
	if pool == nil || userID == uuid.Nil || cp.PlaybackInstanceID == uuid.Nil || cp.TrackID == uuid.Nil {
		return nil
	}
	src, ok := NormalizeSource(cp.Source)
	if !ok || src == "import" {
		return nil
	}
	cp.Source = src
	if err := EnsureEventsSchema(ctx, pool); err != nil {
		return nil
	}
	now := cp.At
	if now.IsZero() {
		now = time.Now()
	}
	st, err := loadFSM(ctx, pool, cp.PlaybackInstanceID, userID)
	if err != nil {
		return nil
	}
	if st.TrackID != uuid.Nil && st.TrackID != cp.TrackID {
		st = FSM{TrackID: cp.TrackID, StartedAt: now}
	}
	if st.TrackID == uuid.Nil {
		st.TrackID = cp.TrackID
	}
	if st.StartedAt.IsZero() {
		st.StartedAt = now
	}

	if cp.Kind == "skip" {
		if cp.StopAfter {
			return saveFSM(ctx, pool, cp.PlaybackInstanceID, userID, st)
		}
		if !st.Qualified && st.AccumulatedMS >= int64(ThresholdMs(cp.DurationMS)) {
			st.Qualified = true
			_ = insertEvent(ctx, pool, userID, cp, st, "qualify")
		}
		if !st.Skipped {
			st.Skipped = true
			_ = insertEvent(ctx, pool, userID, cp, st, "skip")
		}
		return saveFSM(ctx, pool, cp.PlaybackInstanceID, userID, st)
	}

	if !IsAudioListener(cp.Kind, cp.RendererKind, cp.RendererID, cp.ClientID, cp.DeviceID, cp.AudioListener) {
		return nil
	}

	_, next, drop := Credit(st, cp.PositionMS, cp.PlayheadSequence, cp.Status, cp.PlaybackRate, now)
	if drop {
		return nil
	}
	st = next

	out := outputOf(cp.RendererKind)
	if out != "" {
		_ = syncOutputSegment(ctx, pool, cp.PlaybackInstanceID, userID, out, now)
		st.LastOutput = out
	}

	if !st.Qualified && st.AccumulatedMS >= int64(ThresholdMs(cp.DurationMS)) {
		st.Qualified = true
		_ = insertEvent(ctx, pool, userID, cp, st, "qualify")
	}
	return saveFSM(ctx, pool, cp.PlaybackInstanceID, userID, st)
}

func loadFSM(ctx context.Context, pool *pgxpool.Pool, instanceID, userID uuid.UUID) (FSM, error) {
	var st FSM
	var lastAt *time.Time
	var lastOut *string
	err := pool.QueryRow(ctx, `
		SELECT track_id, accumulated_ms, last_position_ms, last_playhead_sequence,
			last_checkpoint_at, qualified, skipped, last_output, started_at
		FROM listen_instance_state WHERE playback_instance_id=$1 AND user_id=$2`,
		instanceID, userID).Scan(
		&st.TrackID, &st.AccumulatedMS, &st.LastPositionMS, &st.LastSequence,
		&lastAt, &st.Qualified, &st.Skipped, &lastOut, &st.StartedAt)
	if err == pgx.ErrNoRows {
		return FSM{}, nil
	}
	if err != nil {
		return FSM{}, err
	}
	if lastAt != nil {
		st.LastCheckpoint = *lastAt
	}
	if lastOut != nil {
		st.LastOutput = *lastOut
	}
	return st, nil
}

func saveFSM(ctx context.Context, pool *pgxpool.Pool, instanceID, userID uuid.UUID, st FSM) error {
	var lastAt *time.Time
	if !st.LastCheckpoint.IsZero() {
		t := st.LastCheckpoint
		lastAt = &t
	}
	var lastOut *string
	if st.LastOutput != "" {
		o := st.LastOutput
		lastOut = &o
	}
	started := st.StartedAt
	if started.IsZero() {
		started = time.Now()
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO listen_instance_state (
			playback_instance_id, user_id, track_id, accumulated_ms, last_position_ms,
			last_playhead_sequence, last_checkpoint_at, qualified, skipped, last_output, started_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (playback_instance_id, user_id) DO UPDATE SET
			track_id=EXCLUDED.track_id,
			accumulated_ms=EXCLUDED.accumulated_ms,
			last_position_ms=EXCLUDED.last_position_ms,
			last_playhead_sequence=EXCLUDED.last_playhead_sequence,
			last_checkpoint_at=EXCLUDED.last_checkpoint_at,
			qualified=EXCLUDED.qualified,
			skipped=EXCLUDED.skipped,
			last_output=EXCLUDED.last_output`,
		instanceID, userID, st.TrackID, st.AccumulatedMS, st.LastPositionMS,
		st.LastSequence, lastAt, st.Qualified, st.Skipped, lastOut, started)
	return err
}

func insertEvent(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, cp Checkpoint, st FSM, kind string) error {
	qualified := st.Qualified
	skipped := kind == "skip" || st.Skipped
	if kind == "qualify" {
		qualified = true
		skipped = false
	}
	started := st.StartedAt
	if started.IsZero() {
		started = time.Now()
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO listen_events (
			playback_instance_id, user_id, track_id, kind,
			accumulated_listened_ms, listened_ms, track_duration_ms,
			qualified_play, skipped, legacy_backfill, source, started_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,false,$10,$11)
		ON CONFLICT DO NOTHING`,
		cp.PlaybackInstanceID, userID, cp.TrackID, kind,
		st.AccumulatedMS, intOrNil(st.AccumulatedMS), cp.DurationMS,
		qualified, skipped, cp.Source, started)
	return err
}

func syncOutputSegment(ctx context.Context, pool *pgxpool.Pool, instanceID, userID uuid.UUID, output string, now time.Time) error {
	if output != "browser" && output != "discord" {
		return nil
	}
	var open string
	err := pool.QueryRow(ctx, `
		SELECT output FROM listen_output_segments
		WHERE playback_instance_id=$1 AND user_id=$2 AND ended_at IS NULL
		ORDER BY started_at DESC LIMIT 1`, instanceID, userID).Scan(&open)
	if err == nil && open == output {
		return nil
	}
	if err == nil {
		_, _ = pool.Exec(ctx, `
			UPDATE listen_output_segments SET ended_at=$3
			WHERE playback_instance_id=$1 AND user_id=$2 AND ended_at IS NULL`,
			instanceID, userID, now)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO listen_output_segments (playback_instance_id, user_id, output, started_at)
		VALUES ($1,$2,$3,$4)`, instanceID, userID, output, now)
	return err
}

func intOrNil(n int64) *int {
	if n < 0 {
		return nil
	}
	if n > int64(math.MaxInt32) {
		v := math.MaxInt32
		return &v
	}
	v := int(n)
	return &v
}
