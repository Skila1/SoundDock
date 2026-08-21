package transcode

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Profile struct {
	Name    string
	Codec   string
	Bitrate string
	Args    []string
}

var Profiles = map[string]Profile{
	"original": {Name: "original"},
	"high":     {Name: "high", Codec: "libmp3lame", Bitrate: "320k", Args: []string{"-f", "mp3"}},
	"medium":   {Name: "medium", Codec: "libmp3lame", Bitrate: "192k", Args: []string{"-f", "mp3"}},
	"low":      {Name: "low", Codec: "aac", Bitrate: "96k", Args: []string{"-f", "adts"}},
}

type Manager struct {
	pool     *pgxpool.Pool
	cacheDir string
	maxBytes int64
	sem      chan struct{}
	mu       sync.Mutex
}

func New(pool *pgxpool.Pool, cacheDir string, maxBytes int64, concurrency int) *Manager {
	if concurrency < 1 {
		concurrency = 2
	}
	if maxBytes <= 0 {
		maxBytes = 10 << 30
	}
	d := filepath.Join(cacheDir, "transcode")
	_ = os.MkdirAll(d, 0o755)
	return &Manager{pool: pool, cacheDir: d, maxBytes: maxBytes, sem: make(chan struct{}, concurrency)}
}

func (m *Manager) Acquire() { m.sem <- struct{}{} }
func (m *Manager) Release() { <-m.sem }

func (m *Manager) CachedPath(ctx context.Context, fileID uuid.UUID, profile string) (string, bool) {
	var key string
	err := m.pool.QueryRow(ctx, `SELECT storage_key FROM transcode_cache_entries WHERE track_file_id=$1 AND profile=$2`, fileID, profile).Scan(&key)
	if err != nil {
		return "", false
	}
	p := filepath.Join(m.cacheDir, filepath.Base(key))
	if _, err := os.Stat(p); err != nil {
		return "", false
	}
	_, _ = m.pool.Exec(ctx, `UPDATE transcode_cache_entries SET last_access=now() WHERE track_file_id=$1 AND profile=$2`, fileID, profile)
	return p, true
}

func (m *Manager) TranscodeToCache(ctx context.Context, fileID uuid.UUID, srcPath, profile string) (string, error) {
	pr, ok := Profiles[profile]
	if !ok || profile == "original" {
		return srcPath, nil
	}
	if p, hit := m.CachedPath(ctx, fileID, profile); hit {
		return p, nil
	}
	m.Acquire()
	defer m.Release()
	outName := fmt.Sprintf("%s.%s.mp3", fileID, profile)
	out := filepath.Join(m.cacheDir, outName)
	args := []string{"-y", "-i", srcPath, "-vn", "-c:a", pr.Codec, "-b:a", pr.Bitrate}
	args = append(args, pr.Args...)
	args = append(args, out)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if err := cmd.Run(); err != nil {
		return "", err
	}
	st, err := os.Stat(out)
	if err != nil {
		return "", err
	}
	_, _ = m.pool.Exec(ctx, `INSERT INTO transcode_cache_entries (profile, track_file_id, storage_key, size_bytes)
		VALUES ($1,$2,$3,$4) ON CONFLICT (profile, track_file_id) DO UPDATE SET size_bytes=EXCLUDED.size_bytes, last_access=now()`,
		profile, fileID, outName, st.Size())
	_ = m.Evict(ctx)
	return out, nil
}

func (m *Manager) Pipe(ctx context.Context, src string, profile string, w io.Writer) error {
	pr, ok := Profiles[profile]
	if !ok || profile == "original" {
		f, err := os.Open(src)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	}
	m.Acquire()
	defer m.Release()
	args := []string{"-i", src, "-vn", "-c:a", pr.Codec, "-b:a", pr.Bitrate}
	args = append(args, pr.Args...)
	args = append(args, "pipe:1")
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stdout = w
	return cmd.Run()
}

func (m *Manager) Stats(ctx context.Context) (map[string]any, error) {
	var n int64
	var bytes int64
	_ = m.pool.QueryRow(ctx, `SELECT COUNT(*), coalesce(SUM(size_bytes),0) FROM transcode_cache_entries`).Scan(&n, &bytes)
	return map[string]any{"entries": n, "bytes": bytes, "max_bytes": m.maxBytes}, nil
}

func (m *Manager) Clear(ctx context.Context) error {
	_, _ = m.pool.Exec(ctx, `DELETE FROM transcode_cache_entries`)
	return os.RemoveAll(m.cacheDir)
}

func (m *Manager) Evict(ctx context.Context) error {
	var used int64
	_ = m.pool.QueryRow(ctx, `SELECT coalesce(SUM(size_bytes),0) FROM transcode_cache_entries`).Scan(&used)
	for used > m.maxBytes {
		var id uuid.UUID
		var key string
		var sz int64
		err := m.pool.QueryRow(ctx, `SELECT id, storage_key, size_bytes FROM transcode_cache_entries ORDER BY last_access ASC LIMIT 1`).Scan(&id, &key, &sz)
		if err != nil {
			return nil
		}
		_ = os.Remove(filepath.Join(m.cacheDir, filepath.Base(key)))
		_, _ = m.pool.Exec(ctx, `DELETE FROM transcode_cache_entries WHERE id=$1`, id)
		used -= sz
	}
	_, _ = m.pool.Exec(ctx, `DELETE FROM transcode_cache_entries WHERE last_access < $1`, time.Now().Add(-30*24*time.Hour))
	return nil
}

func FFmpegAvailable() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

func FFProbeAvailable() bool {
	_, err := exec.LookPath("ffprobe")
	return err == nil
}
