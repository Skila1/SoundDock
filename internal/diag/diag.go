package diag

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/backup"
	"github.com/sounddock/sounddock/internal/config"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/lyrics"
	"github.com/sounddock/sounddock/internal/storage"
	"github.com/sounddock/sounddock/internal/transcode"
	"github.com/sounddock/sounddock/internal/update"
)

type Status string

const (
	Pass Status = "PASS"
	Warn Status = "WARN"
	Fail Status = "FAIL"
	Skip Status = "SKIP"
)

type Check struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail"`
	OK     bool   `json:"ok"`
}

type Report struct {
	Checks []Check `json:"checks"`
	Failed int     `json:"failed"`
	Warned int     `json:"warned"`
}

type Deps struct {
	Pool     *pgxpool.Pool
	Cfg      config.Config
	Backup   *backup.Service
	Jobs     *jobs.Runner
	Draining bool
	Streams  int
	Client   *http.Client
	Now      time.Time
}

func Run(ctx context.Context, d Deps) Report {
	if d.Client == nil {
		d.Client = &http.Client{Timeout: 4 * time.Second}
	}
	if d.Now.IsZero() {
		d.Now = time.Now()
	}
	var r Report
	add := func(c Check) {
		c.OK = c.Status == Pass || c.Status == Skip
		r.Checks = append(r.Checks, c)
		switch c.Status {
		case Fail:
			r.Failed++
		case Warn:
			r.Warned++
		}
	}

	add(probePostgres(ctx, d.Pool))
	add(probeBinary("ffmpeg", "FFmpeg", transcode.FFmpegAvailable(), true, "Transcoding binary ran.", "FFmpeg is missing. Transcodes and some imports will fail."))
	add(probeBinary("ffprobe", "FFprobe", transcode.FFProbeAvailable(), true, "Probe binary ran.", "FFprobe is missing."))
	fpOK := lookPath("fpcalc")
	add(probeBinary("fpcalc", "fpcalc", fpOK, false, "Fingerprint tool ran.", "fpcalc is missing. Acoustic fingerprints are skipped."))
	add(probeBinary("pg_dump", "pg_dump", lookPath("pg_dump"), true, "pg_dump is on PATH and can dump Postgres.", "pg_dump is missing. Logical backups cannot run."))
	add(probeBinary("psql", "psql", lookPath("psql"), false, "psql is on PATH.", "psql is missing. Restore will fail."))

	if d.Draining {
		add(Check{ID: "worker", Name: "Job runner", Status: Fail, Detail: "Instance is draining."})
	} else {
		add(Check{ID: "worker", Name: "Job runner", Status: Pass, Detail: "Workers are accepting jobs."})
	}

	helper, sock := update.HelperOK(), update.SocketOK()
	switch {
	case helper:
		add(Check{ID: "update", Name: "In-app updates", Status: Pass, Detail: "Host helper is present and the update directory is writable."})
	case sock:
		add(Check{ID: "update", Name: "In-app updates", Status: Warn, Detail: "Host helper is missing. Docker socket is opted in via SD_ALLOW_DOCKER_SOCK."})
	default:
		add(Check{ID: "update", Name: "In-app updates", Status: Fail, Detail: "No host helper and Docker socket is not opted in. Update now cannot run."})
	}

	if d.Jobs != nil {
		for _, p := range d.Jobs.Status(ctx) {
			id := "pool-" + string(p.ID)
			if !p.Enabled {
				if p.Reserved {
					add(Check{ID: id, Name: p.Name + " pool", Status: Fail, Detail: p.Name + " is turned off but is reserved."})
				} else {
					add(Check{ID: id, Name: p.Name + " pool", Status: Skip, Detail: p.Name + " is turned off."})
				}
				continue
			}
			if p.Live.ActiveWorkers == 0 && p.Live.QueueDepth > 0 && !p.Reserved {
				add(Check{ID: id, Name: p.Name + " pool", Status: Fail, Detail: fmt.Sprintf("%s has a queue of %d and no workers.", p.Name, p.Live.QueueDepth)})
				continue
			}
			add(Check{ID: id, Name: p.Name + " pool", Status: Pass, Detail: fmt.Sprintf("%s: %d workers, %d busy, queue %d", p.Name, p.Live.ActiveWorkers, p.Live.Busy, p.Live.QueueDepth)})
		}
	}

	for _, dir := range []struct{ id, name, path string }{
		{"disk-data", "Data volume", d.Cfg.DataDir},
		{"disk-managed", "Managed media", d.Cfg.ManagedDir},
		{"disk-backup", "Backup folder", d.Cfg.BackupDir},
	} {
		add(probeDisk(dir.id, dir.name, dir.path))
	}

	ly := lyrics.LoadConfig(ctx, d.Pool)
	if ly.LocalEnabled {
		add(Check{ID: "lyrics-local", Name: "Local lyrics", Status: Pass, Detail: "Embedded tags and data/lyrics files are used."})
	} else {
		add(Check{ID: "lyrics-local", Name: "Local lyrics", Status: Skip, Detail: "Local lyrics are off."})
	}
	if ly.ExternalEnabled && ly.ProviderURL != "" {
		add(probeHTTP(ctx, d.Client, "lyrics-external", "External lyrics", strings.TrimRight(ly.ProviderURL, "/")+"/api/search?track_name=probe&artist_name=sounddock", true))
	} else {
		add(Check{ID: "lyrics-external", Name: "External lyrics", Status: Skip, Detail: "LRCLIB is off."})
	}

	if d.Backup != nil {
		bst := d.Backup.LoadSettings(ctx)
		if bst.LocalEnabled {
			add(probeWritableDir("backup-local", "Local backups", d.Cfg.BackupDir))
		} else {
			add(Check{ID: "backup-local", Name: "Local backups", Status: Skip, Detail: "Local backup copies are off."})
		}
		if bst.R2Enabled {
			add(probeR2(ctx, d.Backup, bst))
		} else {
			add(Check{ID: "backup-r2", Name: "R2 backups", Status: Skip, Detail: "R2 destination is off."})
		}
	}

	add(probeDiscord(ctx, d.Pool, d.Client))
	for _, c := range probeProviders(ctx, d.Pool) {
		add(c)
	}
	add(Check{ID: "streams", Name: "Playback slots", Status: Pass, Detail: fmt.Sprintf("%d active streams", d.Streams)})
	add(probeRedis(ctx, d.Cfg.RedisURL))
	add(probeMeili(ctx, d.Client, d.Cfg.MeiliURL, d.Cfg.MeiliKey))
	return r
}

func probePostgres(ctx context.Context, pool *pgxpool.Pool) Check {
	c := Check{ID: "postgres", Name: "Postgres"}
	if pool == nil {
		c.Status = Fail
		c.Detail = "Database pool is not configured."
		return c
	}
	if err := pool.Ping(ctx); err != nil {
		c.Status = Fail
		c.Detail = "Database ping failed."
		return c
	}
	c.Status = Pass
	c.Detail = "Database accepted a ping."
	return c
}

func probeBinary(id, name string, ok, required bool, yes, no string) Check {
	if ok {
		return Check{ID: id, Name: name, Status: Pass, Detail: yes}
	}
	if required {
		return Check{ID: id, Name: name, Status: Fail, Detail: no}
	}
	return Check{ID: id, Name: name, Status: Warn, Detail: no}
}

func probeDisk(id, name, path string) Check {
	c := Check{ID: id, Name: name}
	if strings.TrimSpace(path) == "" {
		c.Status = Fail
		c.Detail = "Path is not configured."
		return c
	}
	if _, err := os.Stat(path); err != nil {
		c.Status = Fail
		c.Detail = "Path is not reachable."
		return c
	}
	total, free, err := storage.DiskSpace(path)
	if err != nil {
		c.Status = Fail
		c.Detail = "Could not measure free space on " + path
		return c
	}
	c.Detail = fmt.Sprintf("%s free of %s on %s", bytesLabel(free), bytesLabel(total), path)
	switch {
	case free < 1<<30:
		c.Status = Fail
	case free < 5<<30:
		c.Status = Warn
	default:
		c.Status = Pass
	}
	return c
}

func probeWritableDir(id, name, path string) Check {
	c := Check{ID: id, Name: name}
	if strings.TrimSpace(path) == "" {
		c.Status = Fail
		c.Detail = "Backup folder is not configured."
		return c
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		c.Status = Fail
		c.Detail = "Backup folder is not writable."
		return c
	}
	probe := filepath.Join(path, ".diag-write")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		c.Status = Fail
		c.Detail = "Could not write a probe file in the backup folder."
		return c
	}
	_ = os.Remove(probe)
	c.Status = Pass
	c.Detail = "Wrote a probe file in " + path
	return c
}

func probeHTTP(ctx context.Context, client *http.Client, id, name, rawURL string, optional bool) Check {
	c := Check{ID: id, Name: name}
	if strings.TrimSpace(rawURL) == "" {
		c.Status = Skip
		c.Detail = name + " is not configured."
		return c
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		c.Status = Fail
		c.Detail = "Could not build probe request."
		return c
	}
	res, err := client.Do(req)
	if err != nil {
		if optional {
			c.Status = Warn
			c.Detail = "Probe failed: endpoint did not respond."
			return c
		}
		c.Status = Fail
		c.Detail = "Probe failed: endpoint did not respond."
		return c
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	if res.StatusCode >= 500 {
		c.Status = Fail
		c.Detail = fmt.Sprintf("Probe returned HTTP %d.", res.StatusCode)
		return c
	}
	c.Status = Pass
	c.Detail = fmt.Sprintf("Probe returned HTTP %d.", res.StatusCode)
	return c
}

func probeRedis(ctx context.Context, rawURL string) Check {
	c := Check{ID: "redis", Name: "Redis"}
	if strings.TrimSpace(rawURL) == "" {
		c.Status = Skip
		c.Detail = "Redis is not configured."
		return c
	}
	addr := redisAddr(rawURL)
	if addr == "" {
		c.Status = Fail
		c.Detail = "Redis URL is not a usable address."
		return c
	}
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		c.Status = Fail
		c.Detail = "Redis is configured but the address did not accept a TCP connection."
		return c
	}
	_ = conn.Close()
	c.Status = Pass
	c.Detail = "Redis accepted a TCP connection."
	return c
}

func redisAddr(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "redis://")
	raw = strings.TrimPrefix(raw, "rediss://")
	if i := strings.Index(raw, "@"); i >= 0 {
		raw = raw[i+1:]
	}
	if i := strings.Index(raw, "/"); i >= 0 {
		raw = raw[:i]
	}
	if raw != "" && !strings.Contains(raw, ":") {
		raw += ":6379"
	}
	return raw
}

func probeMeili(ctx context.Context, client *http.Client, rawURL, key string) Check {
	c := Check{ID: "meili", Name: "Meilisearch"}
	if strings.TrimSpace(rawURL) == "" {
		c.Status = Skip
		c.Detail = "Meilisearch is not configured."
		return c
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(rawURL, "/")+"/health", nil)
	if err != nil {
		c.Status = Fail
		c.Detail = "Could not build Meilisearch probe."
		return c
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	res, err := client.Do(req)
	if err != nil {
		c.Status = Fail
		c.Detail = "Meilisearch is configured but /health did not respond."
		return c
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 2048))
	if res.StatusCode >= 300 {
		c.Status = Fail
		c.Detail = fmt.Sprintf("Meilisearch /health returned HTTP %d.", res.StatusCode)
		return c
	}
	c.Status = Pass
	c.Detail = "Meilisearch /health succeeded."
	return c
}

func probeR2(ctx context.Context, svc *backup.Service, st backup.Settings) Check {
	c := Check{ID: "backup-r2", Name: "R2 backups"}
	if st.Bucket == "" || st.Endpoint == "" {
		c.Status = Fail
		c.Detail = "R2 is on but endpoint or bucket is missing."
		return c
	}
	if _, err := svc.ListRemote(ctx, st); err != nil {
		c.Status = Fail
		c.Detail = "R2 list probe failed."
		return c
	}
	c.Status = Pass
	c.Detail = "Listed objects on the R2 destination."
	return c
}

func probeDiscord(ctx context.Context, pool *pgxpool.Pool, client *http.Client) Check {
	c := Check{ID: "discord", Name: "Discord"}
	if pool == nil {
		c.Status = Skip
		c.Detail = "Database is unavailable."
		return c
	}
	var enabled bool
	var tok []byte
	var gateway string
	if err := pool.QueryRow(ctx, `SELECT enabled, bot_token_enc, COALESCE(last_gateway_status,'') FROM discord_settings WHERE id=1`).Scan(&enabled, &tok, &gateway); err != nil {
		c.Status = Skip
		c.Detail = "Discord settings are not available."
		return c
	}
	if !enabled {
		c.Status = Skip
		c.Detail = "Bot is off."
		return c
	}
	if len(tok) == 0 {
		c.Status = Fail
		c.Detail = "Bot is enabled but no token is stored."
		return c
	}
	if !strings.EqualFold(gateway, "connected") {
		c.Status = Fail
		c.Detail = "Bot is enabled but the gateway is not connected."
		return c
	}
	c.Status = Pass
	c.Detail = "Gateway reported connected."
	_ = client
	return c
}

func probeProviders(ctx context.Context, pool *pgxpool.Pool) []Check {
	if pool == nil {
		return nil
	}
	rows, err := pool.Query(ctx, `SELECT provider, enabled FROM external_provider_settings ORDER BY provider`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Check
	for rows.Next() {
		var prov string
		var on bool
		if rows.Scan(&prov, &on) != nil {
			continue
		}
		name := titleWords(strings.ReplaceAll(prov, "_", " "))
		id := "provider-" + prov
		if !on {
			out = append(out, Check{ID: id, Name: name, Status: Skip, Detail: "Provider is off."})
			continue
		}
		out = append(out, Check{ID: id, Name: name, Status: Warn, Detail: "Provider is enabled. Diagnostics do not treat a stored setting as a live probe."})
	}
	return out
}

func titleWords(s string) string {
	parts := strings.Fields(s)
	for i, p := range parts {
		if p == "" {
			continue
		}
		r := []rune(strings.ToLower(p))
		if r[0] >= 'a' && r[0] <= 'z' {
			r[0] = r[0] - 32
		}
		parts[i] = string(r)
	}
	return strings.Join(parts, " ")
}

func lookPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func bytesLabel(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	if n < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(n)/1024/1024)
	}
	return fmt.Sprintf("%.1f GB", float64(n)/1024/1024/1024)
}

// ProbeHTTP is exported for tests that assert a failed probe is FAIL.
func ProbeHTTP(ctx context.Context, client *http.Client, rawURL string) Check {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	return probeHTTP(ctx, client, "probe", "Probe", rawURL, false)
}
