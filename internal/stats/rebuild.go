package stats

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// ReaderKey is the server_settings key for the active recap source.
	ReaderKey = "listen_reader"
	// ReaderHistory reads listen_history (default when the key is missing).
	ReaderHistory = "history"
	// ReaderEvents reads listen_events only (qualified_play for plays).
	ReaderEvents = "events"
)

// Result is the outcome of a stats.rebuild cutover.
type Result struct {
	PlayCountRows int64  `json:"play_counts_rows"`
	Reader        string `json:"listen_reader"`
}

// ReaderIsEvents reports whether recap readers should query listen_events.
// A missing key, empty value, or anything other than "events" is history.
func ReaderIsEvents(ctx context.Context, pool *pgxpool.Pool) bool {
	if pool == nil {
		return false
	}
	var v string
	err := pool.QueryRow(ctx, `SELECT value #>> '{}' FROM server_settings WHERE key=$1`, ReaderKey).Scan(&v)
	if err != nil || v != ReaderEvents {
		return false
	}
	return true
}

// SetReader stores "events" or "history" in server_settings.listen_reader.
func SetReader(ctx context.Context, pool *pgxpool.Pool, mode string) error {
	if mode != ReaderEvents && mode != ReaderHistory {
		return fmt.Errorf("invalid listen reader %q", mode)
	}
	if pool == nil {
		return fmt.Errorf("listen reader: no database")
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO server_settings (key, value) VALUES ($1, to_jsonb($2::text))
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, ReaderKey, mode)
	return err
}

// CutoverToEvents rebuilds play_counts from listen_events, then flips
// listen_reader to "events" in the same transaction so readers never see a
// half-swap. listen_history rows are not deleted.
func CutoverToEvents(ctx context.Context, pool *pgxpool.Pool) (Result, error) {
	if pool == nil {
		return Result{}, fmt.Errorf("stats.rebuild: no database")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `LOCK TABLE play_counts IN EXCLUSIVE MODE`); err != nil {
		return Result{}, err
	}

	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE play_counts_rebuild (
			user_id UUID NOT NULL,
			track_id UUID NOT NULL,
			count INT NOT NULL DEFAULT 0,
			skip_count INT NOT NULL DEFAULT 0,
			last_played_at TIMESTAMPTZ,
			PRIMARY KEY (user_id, track_id)
		) ON COMMIT DROP`); err != nil {
		return Result{}, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO play_counts_rebuild (user_id, track_id, count, skip_count, last_played_at)
		SELECT user_id, track_id,
			(count(*) FILTER (WHERE kind = 'qualify'))::int,
			(count(*) FILTER (WHERE kind = 'skip'))::int,
			max(started_at) FILTER (WHERE kind = 'qualify')
		FROM listen_events
		GROUP BY user_id, track_id`); err != nil {
		return Result{}, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO play_counts (user_id, track_id, count, skip_count, last_played_at)
		SELECT user_id, track_id, count, skip_count, last_played_at
		FROM play_counts_rebuild
		ON CONFLICT (user_id, track_id) DO UPDATE SET
			count = EXCLUDED.count,
			skip_count = EXCLUDED.skip_count,
			last_played_at = EXCLUDED.last_played_at`); err != nil {
		return Result{}, err
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM play_counts pc
		WHERE NOT EXISTS (
			SELECT 1 FROM play_counts_rebuild r
			WHERE r.user_id = pc.user_id AND r.track_id = pc.track_id
		)`); err != nil {
		return Result{}, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO server_settings (key, value) VALUES ($1, to_jsonb($2::text))
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, ReaderKey, ReaderEvents); err != nil {
		return Result{}, err
	}

	var n int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM play_counts`).Scan(&n); err != nil {
		return Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return Result{PlayCountRows: n, Reader: ReaderEvents}, nil
}
