package scan

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/transcode"
)

type libOpts struct {
	OrgMode       string
	AllowPhysical bool
	ReadOnly      bool
	Template      string
	KeepOriginal  bool
	Preset        string
	ScanMode      string
}

func StoreOptsFor(ctx context.Context, pool *pgxpool.Pool, lib uuid.UUID) transcode.StoreOpts {
	o := transcode.StoreOpts{Preset: transcode.PresetStandard}
	if pool == nil {
		return o
	}
	var v bool
	if err := pool.QueryRow(ctx, `SELECT (value)::boolean FROM server_settings WHERE key=$1`, "keep_original."+lib.String()).Scan(&v); err == nil {
		o.KeepOriginal = v
	} else if err := pool.QueryRow(ctx, `SELECT (value)::boolean FROM server_settings WHERE key='keep_original'`).Scan(&v); err == nil {
		o.KeepOriginal = v
	}
	var preset string
	if err := pool.QueryRow(ctx, `SELECT value #>> '{}' FROM server_settings WHERE key='compression_preset'`).Scan(&preset); err == nil {
		o.Preset = transcode.NormalizePreset(preset)
	}
	return o
}

func (s *Scanner) libraryOpts(ctx context.Context, lib uuid.UUID) libOpts {
	o := libOpts{OrgMode: "virtual", Preset: transcode.PresetStandard, ScanMode: "incremental"}
	if s.pool == nil {
		return o
	}
	_ = s.pool.QueryRow(ctx, `
		SELECT organisation_mode, allow_physical_reorganise, read_only, naming_template, scan_mode
		FROM libraries WHERE id=$1`, lib).Scan(&o.OrgMode, &o.AllowPhysical, &o.ReadOnly, &o.Template, &o.ScanMode)
	so := StoreOptsFor(ctx, s.pool, lib)
	o.KeepOriginal = so.KeepOriginal
	o.Preset = so.Preset
	return o
}

func (s *Scanner) boolSetting(ctx context.Context, key string, def bool) bool {
	if s.pool == nil {
		return def
	}
	var v bool
	err := s.pool.QueryRow(ctx, `SELECT (value)::boolean FROM server_settings WHERE key=$1`, key).Scan(&v)
	if err != nil {
		return def
	}
	return v
}

func (s *Scanner) enqueueOnce(ctx context.Context, typ string, payload any) {
	if s.pool == nil {
		return
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	var id uuid.UUID
	err = s.pool.QueryRow(ctx, `
		SELECT id FROM jobs WHERE type=$1 AND payload=$2::jsonb AND status IN ('queued','running','retry') LIMIT 1`, typ, b).Scan(&id)
	if err == nil {
		return
	}
	_, _ = s.pool.Exec(ctx, `INSERT INTO jobs (type, payload) VALUES ($1,$2)`, typ, b)
}
