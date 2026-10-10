// Package archive hides songs nobody has played for a while. Archiving never
// deletes anything: the track, its files, playlist entries and personal
// library entries all stay. Archived songs are left out of listings until a
// search or play brings them back.
package archive

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/jobs"
)

const (
	SettingKey = "archive.policy"
	JobType    = "maintenance.archive"
)

// Policy is the admin-editable archive configuration.
type Policy struct {
	Enabled      bool       `json:"enabled"`
	Days         int        `json:"days"`
	LastRunAt    *time.Time `json:"last_run_at,omitempty"`
	LastArchived int        `json:"last_archived"`
}

func DefaultPolicy() Policy { return Policy{Enabled: true, Days: 14} }

func Normalize(p Policy) Policy {
	if p.Days < 1 {
		p.Days = 14
	}
	if p.Days > 3650 {
		p.Days = 3650
	}
	return p
}

func Load(ctx context.Context, pool *pgxpool.Pool) Policy {
	p := DefaultPolicy()
	var raw []byte
	if pool == nil || pool.QueryRow(ctx, `SELECT value FROM server_settings WHERE key=$1`, SettingKey).Scan(&raw) != nil || len(raw) == 0 {
		return p
	}
	_ = json.Unmarshal(raw, &p)
	return Normalize(p)
}

func Save(ctx context.Context, pool *pgxpool.Pool, p Policy) error {
	b, err := json.Marshal(Normalize(p))
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO server_settings (key, value) VALUES ($1, $2::jsonb)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, SettingKey, b)
	return err
}

// candidateSQL selects songs to archive: older than the cutoff, not played by
// anyone since, not kept forever, not anyone's favourite and not queued.
const candidateSQL = `
	SELECT t.id FROM tracks t
	WHERE t.archived_at IS NULL
	  AND coalesce(t.keep_forever, false) = false
	  AND t.created_at < $1
	  AND NOT EXISTS (SELECT 1 FROM listen_history h WHERE h.track_id=t.id AND h.played_at >= $1)
	  AND NOT EXISTS (SELECT 1 FROM play_counts pc WHERE pc.track_id=t.id AND pc.last_played_at >= $1)
	  AND NOT EXISTS (SELECT 1 FROM favourites f WHERE f.entity_type='track' AND f.entity_id=t.id)
	  AND NOT EXISTS (SELECT 1 FROM playback_queue_items qi WHERE qi.track_id=t.id)`

// Preview counts how many songs a run would archive right now.
func Preview(ctx context.Context, pool *pgxpool.Pool, days int) (int, error) {
	cutoff := time.Now().Add(-time.Duration(Normalize(Policy{Days: days}).Days) * 24 * time.Hour)
	var n int
	err := pool.QueryRow(ctx, `SELECT count(*) FROM (`+candidateSQL+`) c`, cutoff).Scan(&n)
	return n, err
}

// Run archives every current candidate and records the run.
func Run(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	p := Load(ctx, pool)
	cutoff := time.Now().Add(-time.Duration(p.Days) * 24 * time.Hour)
	tag, err := pool.Exec(ctx, `UPDATE tracks SET archived_at=now() WHERE id IN (`+candidateSQL+`)`, cutoff)
	if err != nil {
		return 0, err
	}
	n := int(tag.RowsAffected())
	now := time.Now()
	p.LastRunAt, p.LastArchived = &now, n
	_ = Save(ctx, pool, p)
	return n, nil
}

// Restore brings songs back into listings. An empty ids restores everything.
func Restore(ctx context.Context, pool *pgxpool.Pool, ids []uuid.UUID) (int, error) {
	if pool == nil {
		return 0, nil
	}
	if ids == nil {
		tag, err := pool.Exec(ctx, `UPDATE tracks SET archived_at=NULL WHERE archived_at IS NOT NULL`)
		return int(tag.RowsAffected()), err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	tag, err := pool.Exec(ctx, `UPDATE tracks SET archived_at=NULL WHERE id = ANY($1) AND archived_at IS NOT NULL`, ids)
	return int(tag.RowsAffected()), err
}

// Due reports whether the daily scheduled run should happen.
func Due(ctx context.Context, pool *pgxpool.Pool) bool {
	p := Load(ctx, pool)
	return p.Enabled && (p.LastRunAt == nil || time.Since(*p.LastRunAt) >= 24*time.Hour)
}

// Handler runs the archive job when the policy is enabled, or when an admin
// asked for a run explicitly.
func Handler(pool *pgxpool.Pool) jobs.Handler {
	return func(ctx context.Context, job jobs.Job) error {
		var payload struct {
			Manual bool `json:"manual"`
		}
		_ = json.Unmarshal(job.Payload, &payload)
		if !payload.Manual && !Load(ctx, pool).Enabled {
			return nil
		}
		_, err := Run(ctx, pool)
		return err
	}
}
