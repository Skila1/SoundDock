package httpapi

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/transcode"
	"github.com/sounddock/sounddock/internal/version"
)

const (
	settingMaintenance  = "maintenance"
	settingAnnouncement = "announcement"
	settingQuotas       = "quotas"
)

type quotaUserCap struct {
	UserID   string `json:"user_id"`
	MaxBytes int64  `json:"max_bytes"`
}

type quotaLibraryCap struct {
	LibraryID string `json:"library_id"`
	MaxBytes  int64  `json:"max_bytes"`
}

type quotaSettings struct {
	DefaultUserBytes    int64             `json:"default_user_bytes"`
	DefaultLibraryBytes int64             `json:"default_library_bytes"`
	Users               []quotaUserCap    `json:"users"`
	Libraries           []quotaLibraryCap `json:"libraries"`
}

func (s *Server) settingJSON(ctx context.Context, key string, dest any) {
	var raw []byte
	if err := s.Pool.QueryRow(ctx, `SELECT value FROM server_settings WHERE key=$1`, key).Scan(&raw); err != nil || len(raw) == 0 {
		return
	}
	_ = json.Unmarshal(raw, dest)
}

func (s *Server) putSetting(ctx context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `
		INSERT INTO server_settings (key, value) VALUES ($1, $2::jsonb)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, key, b)
	return err
}

func (s *Server) maintenanceEnabled(ctx context.Context) bool {
	var on bool
	s.settingJSON(ctx, settingMaintenance, &on)
	return on
}

func fingerprintToolStatus() string {
	if _, err := exec.LookPath("fpcalc"); err == nil {
		return "available"
	}
	return "missing"
}

func pathPrefixed(p, prefix string) bool {
	return p == prefix || strings.HasPrefix(p, prefix+"/")
}

// maintenanceMutationAllowed reports whether a request may proceed while
// server_settings.maintenance is true. Safe methods always pass. Mutating
// playback, stream, login, and the maintenance toggle itself also pass.
func maintenanceMutationAllowed(method, path string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	p := path
	if i := strings.Index(p, "?"); i >= 0 {
		p = p[:i]
	}
	switch {
	case p == "/healthz", p == "/readyz":
		return true
	case pathPrefixed(p, "/api/v1/me/queue"):
		return true
	case pathPrefixed(p, "/api/v1/me/listen"):
		return true
	case pathPrefixed(p, "/api/v1/me/scrobble"):
		return true
	case pathPrefixed(p, "/api/v1/me/discord"):
		return true
	case pathPrefixed(p, "/api/v1/me/party"):
		return true
	case pathPrefixed(p, "/api/v1/listen"):
		return true
	case pathPrefixed(p, "/api/v1/scrobble"), pathPrefixed(p, "/api/v1/scrobbles"):
		return true
	case pathPrefixed(p, "/api/v1/stream-tokens"):
		return true
	case strings.Contains(p, "/stream"):
		return true
	case pathPrefixed(p, "/api/v1/me/devices"):
		return true
	case p == "/api/v1/me/sessions" && strings.ToUpper(method) != http.MethodDelete:
		return true
	case pathPrefixed(p, "/api/v1/admin/maintenance"):
		return true
	case p == "/api/v1/auth/login", p == "/api/v1/auth/logout":
		return true
	default:
		return false
	}
}

// MaintenanceGuard returns 503 for user/admin mutations while maintenance is on.
// Playback writes, scrobbles, stream, and /healthz are never blocked.
func (s *Server) MaintenanceGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if maintenanceMutationAllowed(r.Method, r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if s.Pool == nil || !s.maintenanceEnabled(r.Context()) {
			next.ServeHTTP(w, r)
			return
		}
		writeErr(w, http.StatusServiceUnavailable, "maintenance", "SoundDock is in maintenance mode")
	})
}

func (s *Server) adminHealthDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pg := s.Pool != nil && s.Pool.Ping(ctx) == nil
	streams := 0
	if s.Slots != nil {
		streams = s.Slots.Active()
	}
	writeJSON(w, 200, map[string]any{
		"postgres":               pg,
		"ffmpeg":                 transcode.FFmpegAvailable(),
		"ffprobe":                transcode.FFProbeAvailable(),
		"fingerprint":            fingerprintToolStatus(),
		"worker":                 !s.Draining,
		"draining":               s.Draining,
		"active_streams":         streams,
		"version":                version.Version,
		"maintenance":            s.maintenanceEnabled(ctx),
		"redis_configured":       s.Cfg.RedisURL != "",
		"meilisearch_configured": s.Cfg.MeiliURL != "",
		"worker_pools":           s.workerPoolHealth(ctx),
	})
}

func (s *Server) workerPoolHealth(ctx context.Context) []map[string]any {
	if s.Jobs == nil {
		return []map[string]any{}
	}
	var out []map[string]any
	for _, p := range s.Jobs.Status(ctx) {
		out = append(out, map[string]any{
			"id": p.ID, "name": p.Name, "enabled": p.Enabled,
			"active_workers": p.Live.ActiveWorkers, "busy": p.Live.Busy,
			"queue_depth": p.Live.QueueDepth, "reserved": p.Reserved,
		})
	}
	return out
}

func (s *Server) publicAnnouncement(w http.ResponseWriter, r *http.Request) {
	var text string
	s.settingJSON(r.Context(), settingAnnouncement, &text)
	writeJSON(w, 200, map[string]any{
		"announcement": text,
		"maintenance":  s.maintenanceEnabled(r.Context()),
	})
}

func (s *Server) adminAnnouncementGet(w http.ResponseWriter, r *http.Request) {
	s.publicAnnouncement(w, r)
}

func (s *Server) adminAnnouncementPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Announcement string `json:"announcement"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	if err := s.putSetting(r.Context(), settingAnnouncement, body.Announcement); err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	s.Audit.Event(r.Context(), &currentUser(r).ID, "announcement.update", "", r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]any{"ok": true, "announcement": body.Announcement})
}

func (s *Server) adminMaintenanceGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"maintenance": s.maintenanceEnabled(r.Context())})
}

func (s *Server) adminMaintenancePut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Maintenance bool `json:"maintenance"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	if err := s.putSetting(r.Context(), settingMaintenance, body.Maintenance); err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	s.Audit.Event(r.Context(), &currentUser(r).ID, "maintenance.update", fmt.Sprintf("%v", body.Maintenance), r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]any{"ok": true, "maintenance": body.Maintenance})
}

func (s *Server) loadQuotas(ctx context.Context) quotaSettings {
	q := quotaSettings{Users: []quotaUserCap{}, Libraries: []quotaLibraryCap{}}
	s.settingJSON(ctx, settingQuotas, &q)
	if q.Users == nil {
		q.Users = []quotaUserCap{}
	}
	if q.Libraries == nil {
		q.Libraries = []quotaLibraryCap{}
	}
	return q
}

func (s *Server) adminQuotasGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := s.loadQuotas(ctx)
	libUsage := map[string]int64{}
	if rows, err := s.Pool.Query(ctx, `SELECT library_id::text, COALESCE(SUM(size_bytes),0) FROM track_files GROUP BY library_id`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var id string
			var n int64
			if rows.Scan(&id, &n) == nil {
				libUsage[id] = n
			}
		}
	}
	userUsage := map[string]int64{}
	if rows, err := s.Pool.Query(ctx, `SELECT user_id::text, COALESCE(SUM(size_bytes),0) FROM upload_sessions GROUP BY user_id`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var id string
			var n int64
			if rows.Scan(&id, &n) == nil {
				userUsage[id] = n
			}
		}
	}
	writeJSON(w, 200, map[string]any{
		"default_user_bytes":    q.DefaultUserBytes,
		"default_library_bytes": q.DefaultLibraryBytes,
		"users":                 q.Users,
		"libraries":             q.Libraries,
		"library_usage":         libUsage,
		"user_usage":            userUsage,
	})
}

func (s *Server) adminQuotasPut(w http.ResponseWriter, r *http.Request) {
	var body quotaSettings
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	if body.Users == nil {
		body.Users = []quotaUserCap{}
	}
	if body.Libraries == nil {
		body.Libraries = []quotaLibraryCap{}
	}
	if err := s.putSetting(r.Context(), settingQuotas, body); err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	s.Audit.Event(r.Context(), &currentUser(r).ID, "quotas.update", "", r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// CheckQuota returns an error when extra bytes would exceed a configured cap.
// 0-byte caps are unlimited. Integrator/upload paths should call this before writes.
func (s *Server) CheckQuota(ctx context.Context, userID, libraryID uuid.UUID, extra int64) error {
	q := s.loadQuotas(ctx)
	if libraryID != uuid.Nil {
		var used int64
		_ = s.Pool.QueryRow(ctx, `SELECT COALESCE(SUM(size_bytes),0) FROM track_files WHERE library_id=$1`, libraryID).Scan(&used)
		capBytes := q.DefaultLibraryBytes
		for _, row := range q.Libraries {
			if row.LibraryID == libraryID.String() {
				capBytes = row.MaxBytes
				break
			}
		}
		if capBytes > 0 && used+extra > capBytes {
			return fmt.Errorf("library storage quota exceeded")
		}
	}
	if userID != uuid.Nil {
		var used int64
		_ = s.Pool.QueryRow(ctx, `SELECT COALESCE(SUM(size_bytes),0) FROM upload_sessions WHERE user_id=$1`, userID).Scan(&used)
		capBytes := q.DefaultUserBytes
		for _, row := range q.Users {
			if row.UserID == userID.String() {
				capBytes = row.MaxBytes
				break
			}
		}
		if capBytes > 0 && used+extra > capBytes {
			return fmt.Errorf("user storage quota exceeded")
		}
	}
	return nil
}

func (s *Server) adminLibraryGrantsGet(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid library id")
		return
	}
	rows, err := s.Pool.Query(r.Context(), `
		SELECT g.id::text, g.library_id::text, g.user_id::text, g.role_id::text, g.actions,
			u.username, r.name
		FROM library_grants g
		LEFT JOIN users u ON u.id = g.user_id
		LEFT JOIN roles r ON r.id = g.role_id
		WHERE g.library_id=$1
		ORDER BY r.name NULLS LAST, u.username NULLS LAST`, id)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var gid, libID string
		var userID, roleID, username, roleName *string
		var actions []string
		if err := rows.Scan(&gid, &libID, &userID, &roleID, &actions, &username, &roleName); err != nil {
			continue
		}
		kind := "user"
		if roleID != nil && *roleID != "" {
			kind = "role"
		}
		out = append(out, map[string]any{
			"id":         gid,
			"library_id": libID,
			"user_id":    userID,
			"role_id":    roleID,
			"username":   username,
			"role":       roleName,
			"actions":    actions,
			"kind":       kind,
		})
	}
	writeJSON(w, 200, out)
}

func (s *Server) adminLibraryGrantAdd(w http.ResponseWriter, r *http.Request) {
	libID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid library id")
		return
	}
	var body struct {
		UserID  uuid.UUID `json:"user_id"`
		Actions []string  `json:"actions"`
	}
	if err := decodeJSON(r, &body); err != nil || body.UserID == uuid.Nil {
		writeErr(w, 400, "invalid", "user_id is required")
		return
	}
	actions := body.Actions
	if len(actions) == 0 {
		actions = []string{"read", "stream"}
	}
	var existing uuid.UUID
	err = s.Pool.QueryRow(r.Context(), `SELECT id FROM library_grants WHERE library_id=$1 AND user_id=$2 LIMIT 1`, libID, body.UserID).Scan(&existing)
	if err == nil && existing != uuid.Nil {
		_, err = s.Pool.Exec(r.Context(), `UPDATE library_grants SET actions=$2 WHERE id=$1 AND user_id IS NOT NULL`, existing, actions)
	} else {
		err = s.Pool.QueryRow(r.Context(), `
			INSERT INTO library_grants (library_id, user_id, actions) VALUES ($1,$2,$3) RETURNING id`,
			libID, body.UserID, actions).Scan(&existing)
	}
	if err != nil {
		writeErr(w, 400, "grant", err.Error())
		return
	}
	s.Audit.Event(r.Context(), &currentUser(r).ID, "library.grant.user", libID.String()+":"+body.UserID.String(), r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]any{"id": existing, "ok": true})
}

func (s *Server) adminLibraryGrantDelete(w http.ResponseWriter, r *http.Request) {
	libID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid library id")
		return
	}
	gid, err := uuid.Parse(chi.URLParam(r, "grantID"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid grant id")
		return
	}
	tag, err := s.Pool.Exec(r.Context(), `DELETE FROM library_grants WHERE id=$1 AND library_id=$2 AND user_id IS NOT NULL`, gid, libID)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, 409, "role_grant", "role grants cannot be removed here; per-user grants are additive")
		return
	}
	s.Audit.Event(r.Context(), &currentUser(r).ID, "library.grant.delete", gid.String(), r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) adminBackupPreview(w http.ResponseWriter, r *http.Request) {
	if s.Backup == nil {
		writeErr(w, 500, "backup", "backup service unavailable")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid backup id")
		return
	}
	p, err := s.Backup.Preview(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "not_found", err.Error())
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) adminBackupRestore(w http.ResponseWriter, r *http.Request) {
	if s.Backup == nil {
		writeErr(w, 500, "backup", "backup service unavailable")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid backup id")
		return
	}
	var body struct {
		Confirm bool `json:"confirm"`
	}
	_ = decodeJSON(r, &body)
	if !body.Confirm {
		writeErr(w, 400, "confirm", "restore requires confirm=true")
		return
	}
	if err := s.Backup.Restore(r.Context(), id); err != nil {
		writeErr(w, 500, "restore", err.Error())
		return
	}
	s.Audit.Event(r.Context(), &currentUser(r).ID, "backup.restore", id.String(), r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]any{"ok": true, "id": id})
}

func (s *Server) adminDiagnostics(w http.ResponseWriter, r *http.Request) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	pg := s.Pool != nil && s.Pool.Ping(r.Context()) == nil
	var dbSize string
	if s.Pool != nil {
		_ = s.Pool.QueryRow(r.Context(), `SELECT pg_size_pretty(pg_database_size(current_database()))`).Scan(&dbSize)
	}
	failed := 0
	if s.Pool != nil {
		_ = s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM jobs WHERE status='failed'`).Scan(&failed)
	}
	lastBackup := map[string]any{}
	if s.Pool != nil {
		var id, path, status *string
		var created *time.Time
		_ = s.Pool.QueryRow(r.Context(), `SELECT id::text, path, status, created_at FROM backups ORDER BY created_at DESC LIMIT 1`).Scan(&id, &path, &status, &created)
		if id != nil {
			lastBackup["id"] = *id
			lastBackup["path"] = path
			lastBackup["status"] = status
			if created != nil {
				lastBackup["created_at"] = created.UTC().Format(time.RFC3339)
			}
		}
	}
	streams := 0
	if s.Slots != nil {
		streams = s.Slots.Active()
	}
	writeJSON(w, 200, map[string]any{
		"go": map[string]any{
			"version":    runtime.Version(),
			"goroutines": runtime.NumGoroutine(),
			"cpus":       runtime.NumCPU(),
			"heap_alloc": mem.HeapAlloc,
			"sys":        mem.Sys,
		},
		"dirs": map[string]any{
			"data":    dirUsage(s.Cfg.DataDir),
			"cache":   dirUsage(s.Cfg.CacheDir),
			"backup":  dirUsage(s.Cfg.BackupDir),
			"managed": dirUsage(s.Cfg.ManagedDir),
		},
		"binaries": map[string]any{
			"ffmpeg":  transcode.FFmpegAvailable(),
			"ffprobe": transcode.FFProbeAvailable(),
			"fpcalc":  fingerprintToolStatus(),
			"pg_dump": lookPathOK("pg_dump"),
			"psql":    lookPathOK("psql"),
		},
		"postgres":       pg,
		"database_size":  dbSize,
		"failed_jobs":    failed,
		"last_backup":    lastBackup,
		"active_streams": streams,
		"worker":         !s.Draining,
		"fingerprint":    fingerprintToolStatus(),
		"maintenance":    s.maintenanceEnabled(r.Context()),
	})
}

func lookPathOK(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func dirUsage(root string) map[string]any {
	out := map[string]any{"path": root, "bytes": int64(0), "files": 0, "ok": false}
	if root == "" {
		return out
	}
	if _, err := os.Stat(root); err != nil {
		out["error"] = err.Error()
		return out
	}
	var bytes int64
	files := 0
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		bytes += info.Size()
		files++
		if files >= 8000 {
			return filepath.SkipAll
		}
		return nil
	})
	out["bytes"] = bytes
	out["files"] = files
	out["ok"] = true
	return out
}

func (s *Server) adminDemoGet(w http.ResponseWriter, r *http.Request) {
	var id uuid.UUID
	err := s.Pool.QueryRow(r.Context(), `SELECT id FROM libraries WHERE lower(name)='demo' LIMIT 1`).Scan(&id)
	if err != nil {
		writeJSON(w, 200, map[string]any{"seeded": false, "library_id": nil, "track_count": 0})
		return
	}
	var tracks int
	_ = s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM tracks WHERE library_id=$1`, id).Scan(&tracks)
	writeJSON(w, 200, map[string]any{
		"seeded":      true,
		"library_id":  id,
		"track_count": tracks,
	})
}

func (s *Server) adminDemoSeed(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var existing uuid.UUID
	if err := s.Pool.QueryRow(ctx, `SELECT id FROM libraries WHERE lower(name)='demo' LIMIT 1`).Scan(&existing); err == nil && existing != uuid.Nil {
		writeJSON(w, 200, map[string]any{"ok": true, "already_seeded": true, "library_id": existing})
		return
	}
	libID, err := s.seedDemoLibrary(ctx)
	if err != nil {
		writeErr(w, 500, "demo", err.Error())
		return
	}
	s.Audit.Event(ctx, &currentUser(r).ID, "demo.seed", libID.String(), r.RemoteAddr, nil)
	writeJSON(w, 201, map[string]any{"ok": true, "already_seeded": false, "library_id": libID})
}

func (s *Server) adminDemoUnseed(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var id uuid.UUID
	if err := s.Pool.QueryRow(ctx, `SELECT id FROM libraries WHERE lower(name)='demo' LIMIT 1`).Scan(&id); err != nil {
		writeJSON(w, 200, map[string]any{"ok": true, "removed": false})
		return
	}
	_, _ = s.Pool.Exec(ctx, `DELETE FROM tracks WHERE library_id=$1`, id)
	_, _ = s.Pool.Exec(ctx, `DELETE FROM albums WHERE library_id=$1`, id)
	_, _ = s.Pool.Exec(ctx, `DELETE FROM library_grants WHERE library_id=$1`, id)
	_, _ = s.Pool.Exec(ctx, `DELETE FROM libraries WHERE id=$1`, id)
	_, _ = s.Pool.Exec(ctx, `DELETE FROM artists WHERE name='SoundDock Demo' AND NOT EXISTS (SELECT 1 FROM track_artists ta WHERE ta.artist_id=artists.id)`)
	if root, err := s.storageRoot(ctx, id); err == nil && root != "" {
		_ = os.RemoveAll(filepath.Join(root, "demo"))
	}
	_ = os.RemoveAll(filepath.Join(s.Cfg.ManagedDir, "demo"))
	s.Audit.Event(ctx, &currentUser(r).ID, "demo.unseed", id.String(), r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]any{"ok": true, "removed": true})
}

func (s *Server) seedDemoLibrary(ctx context.Context) (uuid.UUID, error) {
	sid, err := s.ensureManagedStorage(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	var libID uuid.UUID
	if err := s.Pool.QueryRow(ctx, `
		INSERT INTO libraries (name, kind, storage_provider_id, root_prefix, read_only, organisation_mode)
		VALUES ('Demo', 'music', $1, '', false, 'virtual') RETURNING id`, sid).Scan(&libID); err != nil {
		return uuid.Nil, err
	}
	_, _ = s.Pool.Exec(ctx, `INSERT INTO library_grants (library_id, role_id, actions)
		SELECT $1, id, ARRAY['read','stream','write','admin'] FROM roles WHERE name='Administrator'`, libID)
	_, _ = s.Pool.Exec(ctx, `INSERT INTO library_grants (library_id, role_id, actions)
		SELECT $1, id, ARRAY['read','stream'] FROM roles WHERE name='User'`, libID)

	var artistID, albumID uuid.UUID
	if err := s.Pool.QueryRow(ctx, `INSERT INTO artists (name, sort_name) VALUES ('SoundDock Demo', 'SoundDock Demo') RETURNING id`).Scan(&artistID); err != nil {
		return uuid.Nil, err
	}
	if err := s.Pool.QueryRow(ctx, `INSERT INTO albums (title, year, library_id) VALUES ('Tones', 2026, $1) RETURNING id`, libID).Scan(&albumID); err != nil {
		return uuid.Nil, err
	}
	_, _ = s.Pool.Exec(ctx, `INSERT INTO album_artists (album_id, artist_id, role, position) VALUES ($1,$2,'album_artist',0)`, albumID, artistID)

	root, err := s.providerRoot(ctx, sid)
	if err != nil {
		return uuid.Nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, "demo"), 0o755); err != nil {
		return uuid.Nil, err
	}
	tones := []struct {
		title string
		hz    float64
		file  string
	}{
		{"A4", 440, "a4.wav"},
		{"C5", 523.25, "c5.wav"},
		{"E5", 659.25, "e5.wav"},
	}
	for i, t := range tones {
		key := filepath.ToSlash(filepath.Join("demo", t.file))
		abs := filepath.Join(root, filepath.FromSlash(key))
		if err := writeSineWAV(abs, t.hz, 400*time.Millisecond); err != nil {
			return uuid.Nil, err
		}
		st, err := os.Stat(abs)
		if err != nil {
			return uuid.Nil, err
		}
		var trackID uuid.UUID
		if err := s.Pool.QueryRow(ctx, `
			INSERT INTO tracks (library_id, album_id, title, duration_ms, track_number, disc_number)
			VALUES ($1,$2,$3,$4,$5,1) RETURNING id`, libID, albumID, t.title, 400, i+1).Scan(&trackID); err != nil {
			return uuid.Nil, err
		}
		_, _ = s.Pool.Exec(ctx, `INSERT INTO track_artists (track_id, artist_id, role, position) VALUES ($1,$2,'primary',0)`, trackID, artistID)
		_, _ = s.Pool.Exec(ctx, `
			INSERT INTO track_files (track_id, library_id, storage_key, size_bytes, codec, container, sample_rate, channels, bit_depth, quality)
			VALUES ($1,$2,$3,$4,'pcm','wav',22050,1,16,'original')`, trackID, libID, key, st.Size())
	}
	return libID, nil
}

func (s *Server) storageRoot(ctx context.Context, libraryID uuid.UUID) (string, error) {
	var sid uuid.UUID
	if err := s.Pool.QueryRow(ctx, `SELECT storage_provider_id FROM libraries WHERE id=$1`, libraryID).Scan(&sid); err != nil {
		return "", err
	}
	return s.providerRoot(ctx, sid)
}

func (s *Server) providerRoot(ctx context.Context, sid uuid.UUID) (string, error) {
	var cfg []byte
	if err := s.Pool.QueryRow(ctx, `SELECT config_enc FROM storage_providers WHERE id=$1`, sid).Scan(&cfg); err != nil {
		return "", err
	}
	plain := cfg
	if s.Box != nil && len(cfg) > 0 {
		if p, e := s.Box.Decrypt(cfg); e == nil {
			plain = p
		}
	}
	root := strings.TrimSpace(string(plain))
	if root == "" {
		root = s.Cfg.ManagedDir
	}
	return root, nil
}

func (s *Server) ensureManagedStorage(ctx context.Context) (uuid.UUID, error) {
	var sid uuid.UUID
	err := s.Pool.QueryRow(ctx, `
		SELECT id FROM storage_providers
		WHERE type IN ('managed', 'local')
		ORDER BY CASE WHEN type='managed' THEN 0 ELSE 1 END, created_at
		LIMIT 1`).Scan(&sid)
	if err == nil {
		return sid, nil
	}
	root := s.Cfg.ManagedDir
	enc := []byte(root)
	if s.Box != nil {
		if b, e := s.Box.Encrypt([]byte(root)); e == nil {
			enc = b
		}
	}
	err = s.Pool.QueryRow(ctx, `INSERT INTO storage_providers (name, type, config_enc) VALUES ('Managed', 'managed', $1) RETURNING id`, enc).Scan(&sid)
	return sid, err
}

func writeSineWAV(path string, freq float64, dur time.Duration) error {
	const rate = 22050
	n := int(dur.Seconds() * rate)
	if n < 1 {
		n = rate / 4
	}
	data := make([]byte, n*2)
	for i := 0; i < n; i++ {
		v := math.Sin(2 * math.Pi * freq * float64(i) / rate)
		sample := int16(v * 0.35 * 32767)
		binary.LittleEndian.PutUint16(data[i*2:], uint16(sample))
	}
	hdr := make([]byte, 44)
	copy(hdr[0:], []byte("RIFF"))
	binary.LittleEndian.PutUint32(hdr[4:], uint32(36+len(data)))
	copy(hdr[8:], []byte("WAVE"))
	copy(hdr[12:], []byte("fmt "))
	binary.LittleEndian.PutUint32(hdr[16:], 16)
	binary.LittleEndian.PutUint16(hdr[20:], 1)
	binary.LittleEndian.PutUint16(hdr[22:], 1)
	binary.LittleEndian.PutUint32(hdr[24:], rate)
	binary.LittleEndian.PutUint32(hdr[28:], rate*2)
	binary.LittleEndian.PutUint16(hdr[32:], 2)
	binary.LittleEndian.PutUint16(hdr[34:], 16)
	copy(hdr[36:], []byte("data"))
	binary.LittleEndian.PutUint32(hdr[40:], uint32(len(data)))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(hdr, data...), 0o644)
}
