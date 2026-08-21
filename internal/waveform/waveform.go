package waveform

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/storage"
	"github.com/sounddock/sounddock/internal/transcode"
)

const (
	JobName        = "waveform.generate"
	SettingEnabled = "waveform_enabled"
	peakBuckets    = 256
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

func (s *Service) Handler() jobs.Handler {
	return func(ctx context.Context, job jobs.Job) error {
		if !Enabled(ctx, s.pool) {
			return nil
		}
		var p Payload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return err
		}
		if p.TrackID == uuid.Nil {
			return nil
		}
		var existing []byte
		_ = s.pool.QueryRow(ctx, `SELECT waveform_peaks FROM tracks WHERE id=$1`, p.TrackID).Scan(&existing)
		if len(existing) > 2 && string(existing) != "null" {
			return nil
		}
		local, cleanup, err := s.localFile(ctx, p)
		if err != nil || local == "" {
			return nil
		}
		if cleanup != nil {
			defer cleanup()
		}
		peaks, err := Generate(ctx, local, peakBuckets)
		if err != nil || len(peaks) == 0 {
			return nil
		}
		body, _ := json.Marshal(map[string]any{"version": 1, "buckets": len(peaks), "peaks": peaks})
		_, _ = s.pool.Exec(ctx, `UPDATE tracks SET waveform_peaks=$2, updated_at=now() WHERE id=$1`, p.TrackID, body)
		return nil
	}
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
	tmp, err := os.CreateTemp("", "sd-wave-*")
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

func Generate(ctx context.Context, src string, buckets int) ([]int, error) {
	if src == "" || !transcode.FFmpegAvailable() {
		return nil, nil
	}
	if buckets < 8 {
		buckets = peakBuckets
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error",
		"-i", src, "-ac", "1", "-ar", "8000", "-f", "s16le", "-c:a", "pcm_s16le", "pipe:1")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return DownsamplePeaks(out, buckets), nil
}

func DownsamplePeaks(pcm []byte, buckets int) []int {
	if buckets < 1 {
		buckets = peakBuckets
	}
	n := len(pcm) / 2
	if n == 0 {
		return nil
	}
	samples := make([]int16, n)
	for i := 0; i < n; i++ {
		samples[i] = int16(binary.LittleEndian.Uint16(pcm[i*2:]))
	}
	out := make([]int, buckets)
	for i := 0; i < buckets; i++ {
		start := i * n / buckets
		end := (i + 1) * n / buckets
		if end <= start {
			end = start + 1
		}
		if end > n {
			end = n
		}
		var maxAbs float64
		for _, s := range samples[start:end] {
			v := math.Abs(float64(s))
			if v > maxAbs {
				maxAbs = v
			}
		}
		out[i] = int(math.Round(maxAbs / 32767 * 255))
		if out[i] > 255 {
			out[i] = 255
		}
	}
	return out
}

func JobTypeOK(name string) bool {
	return strings.TrimSpace(name) == JobName
}
