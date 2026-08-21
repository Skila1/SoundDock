package fingerprint

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/storage"
)

const (
	JobName        = "fingerprint.generate"
	SettingEnabled = "fingerprint_enabled"
	Available      = "available"
	Missing        = "missing"
)

type Payload struct {
	TrackID     uuid.UUID `json:"track_id"`
	TrackFileID uuid.UUID `json:"track_file_id"`
}

type Service struct {
	pool    *pgxpool.Pool
	getProv func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error)
}

func New(pool *pgxpool.Pool, getProv func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error)) *Service {
	return &Service{pool: pool, getProv: getProv}
}

func Availability() string {
	if _, err := exec.LookPath("fpcalc"); err != nil {
		return Missing
	}
	return Available
}

func Enabled(ctx context.Context, pool *pgxpool.Pool) bool {
	if pool == nil {
		return true
	}
	var v bool
	err := pool.QueryRow(ctx, `SELECT (value)::boolean FROM server_settings WHERE key=$1`, SettingEnabled).Scan(&v)
	if err != nil {
		return true
	}
	return v
}

func EnsureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return nil
	}
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS track_fingerprints (
			track_file_id UUID PRIMARY KEY REFERENCES track_files(id) ON DELETE CASCADE,
			track_id UUID NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
			fingerprint TEXT NOT NULL,
			duration_seconds DOUBLE PRECISION,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`)
	return err
}

func (s *Service) Handler() jobs.Handler {
	return func(ctx context.Context, job jobs.Job) error {
		if Availability() == Missing {
			return nil
		}
		if !Enabled(ctx, s.pool) {
			return nil
		}
		var p Payload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return err
		}
		if err := EnsureSchema(ctx, s.pool); err != nil {
			return err
		}
		var exists bool
		_ = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM track_fingerprints WHERE track_file_id=$1)`, p.TrackFileID).Scan(&exists)
		if exists {
			return nil
		}
		local, cleanup, err := s.localFile(ctx, p)
		if err != nil || local == "" {
			return nil
		}
		if cleanup != nil {
			defer cleanup()
		}
		fp, dur, err := Calc(ctx, local)
		if err != nil || fp == "" {
			return nil
		}
		_, _ = s.pool.Exec(ctx, `
			INSERT INTO track_fingerprints (track_file_id, track_id, fingerprint, duration_seconds)
			VALUES ($1,$2,$3,$4)
			ON CONFLICT (track_file_id) DO UPDATE SET fingerprint=EXCLUDED.fingerprint, duration_seconds=EXCLUDED.duration_seconds`,
			p.TrackFileID, p.TrackID, fp, dur)
		return nil
	}
}

func Calc(ctx context.Context, path string) (fingerprint string, duration float64, err error) {
	if Availability() == Missing || path == "" {
		return "", 0, nil
	}
	cmd := exec.CommandContext(ctx, "fpcalc", "-json", path)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return "", 0, nil
	}
	var raw struct {
		Duration    float64 `json:"duration"`
		Fingerprint string  `json:"fingerprint"`
	}
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		return "", 0, nil
	}
	return raw.Fingerprint, raw.Duration, nil
}

func (s *Service) localFile(ctx context.Context, p Payload) (string, func(), error) {
	var key string
	var lib uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT storage_key, library_id FROM track_files
		WHERE id=$1 AND deleted_at IS NULL`, p.TrackFileID).Scan(&key, &lib)
	if err != nil || s.getProv == nil {
		return "", nil, err
	}
	prov, _, _, err := s.getProv(ctx, lib)
	if err != nil {
		return "", nil, err
	}
	if fs, ok := prov.(storage.FFmpegSourcer); ok {
		src, err := fs.FFmpegSource(ctx, key)
		if err == nil && src.Path != "" {
			return src.Path, func() {
				if src.Close != nil {
					_ = src.Close()
				}
			}, nil
		}
		if src.Close != nil {
			_ = src.Close()
		}
	}
	rc, _, err := prov.Open(ctx, key)
	if err != nil {
		return "", nil, err
	}
	tmp, err := os.CreateTemp("", "sd-fp-*")
	if err != nil {
		rc.Close()
		return "", nil, err
	}
	name := tmp.Name()
	_, err = tmp.ReadFrom(rc)
	tmp.Close()
	rc.Close()
	if err != nil {
		os.Remove(name)
		return "", nil, err
	}
	return name, func() { os.Remove(name) }, nil
}

func JobTypeOK(name string) bool {
	return strings.TrimSpace(name) == JobName
}
