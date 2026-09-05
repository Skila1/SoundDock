package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
	discordx "github.com/sounddock/sounddock/internal/discord"
	"github.com/sounddock/sounddock/internal/ingest"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/minilib"
	"github.com/sounddock/sounddock/internal/oplog"
	"github.com/sounddock/sounddock/internal/scan"
	"github.com/sounddock/sounddock/internal/storage"
	"github.com/sounddock/sounddock/internal/transcode"
	"github.com/sounddock/sounddock/internal/update"
	"github.com/sounddock/sounddock/internal/version"
)

func (s *Server) adminOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	counts := map[string]int{}
	for _, q := range []struct{ k, sql string }{
		{"tracks", `SELECT count(*) FROM tracks`},
		{"albums", `SELECT count(*) FROM albums`},
		{"artists", `SELECT count(*) FROM artists`},
		{"users", `SELECT count(*) FROM users`},
		{"libraries", `SELECT count(*) FROM libraries`},
	} {
		var n int
		_ = s.Pool.QueryRow(ctx, q.sql).Scan(&n)
		counts[q.k] = n
	}
	writeJSON(w, 200, map[string]any{
		"counts": counts, "version": version.Version, "ffmpeg": transcode.FFmpegAvailable(),
		"active_streams": s.Slots.Active(), "draining": s.Draining,
	})
}

func (s *Server) adminHealth(w http.ResponseWriter, r *http.Request) {
	pg := s.Pool.Ping(r.Context()) == nil
	writeJSON(w, 200, map[string]any{
		"postgres": pg, "ffmpeg": transcode.FFmpegAvailable(), "ffprobe": transcode.FFProbeAvailable(),
		"worker": !s.Draining,
	})
}

func (s *Server) adminDatabase(w http.ResponseWriter, r *http.Request) {
	v, dirty := s.dbVersion(r.Context())
	var size string
	_ = s.Pool.QueryRow(r.Context(), `SELECT pg_size_pretty(pg_database_size(current_database()))`).Scan(&size)
	rows, _ := s.Pool.Query(r.Context(), `
		SELECT relname, pg_size_pretty(pg_total_relation_size(oid))
		FROM pg_class WHERE relkind='r' AND relnamespace='public'::regnamespace
		ORDER BY pg_total_relation_size(oid) DESC LIMIT 20`)
	defer rows.Close()
	writeJSON(w, 200, map[string]any{"migration_version": v, "dirty": dirty, "database_size": size, "tables": scanMaps(rows, "name", "size")})
}

func (s *Server) adminJobs(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.Pool.Query(r.Context(), `SELECT id, type, pool, status, progress, last_error, created_at FROM jobs ORDER BY created_at DESC LIMIT 100`)
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "type", "pool", "status", "progress", "last_error", "created_at"))
}

func (s *Server) adminScans(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `
		SELECT sr.id, sr.library_id, sr.job_id, sr.kind,
			sr.files_seen, sr.files_added, sr.files_failed, sr.files_total,
			sr.started_at, sr.finished_at,
			coalesce(j.status, CASE WHEN sr.finished_at IS NULL THEN 'running' ELSE 'completed' END),
			coalesce(j.progress, CASE WHEN sr.finished_at IS NULL THEN 0 ELSE 100 END),
			j.last_error
		FROM scan_runs sr
		LEFT JOIN jobs j ON j.id = sr.job_id
		WHERE sr.kind IN ('full', 'upload')
		  AND (sr.finished_at IS NULL OR sr.started_at > now() - interval '6 hours')
		ORDER BY sr.started_at DESC
		LIMIT 80`)
	if err != nil {
		writeErr(w, 500, "scan", err.Error())
		return
	}
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "library_id", "job_id", "kind", "files_seen", "files_added", "files_failed", "files_total", "started_at", "finished_at", "status", "progress", "last_error"))
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid job id")
		return
	}
	if s.Jobs == nil {
		writeErr(w, 503, "jobs", "worker runner is not available")
		return
	}
	if err := s.Jobs.RequestCancel(r.Context(), id); err != nil {
		if errors.Is(err, jobs.ErrNotCancellable) {
			writeErr(w, 409, "not_cancellable", "this job type or stage cannot be cancelled")
			return
		}
		s.writeJobErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.Pool.Query(r.Context(), adminUserSQL+" ORDER BY u.created_at")
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, adminUserCols...))
}

const adminUserSQL = `
		SELECT u.id::text, u.username, u.display_name, u.email, u.disabled, u.created_at,
			i.provider_user_id, i.provider_username,
			COALESCE((
				SELECT r.name FROM user_roles ur JOIN roles r ON r.id = ur.role_id
				WHERE ur.user_id = u.id
				ORDER BY CASE r.name WHEN 'Administrator' THEN 0 ELSE 1 END
				LIMIT 1
			), 'User'),
			COALESCE((
				SELECT array_agg(r.name ORDER BY CASE r.name WHEN 'Administrator' THEN 0 ELSE 1 END, r.name)
				FROM user_roles ur JOIN roles r ON r.id = ur.role_id
				WHERE ur.user_id = u.id
			), ARRAY[]::text[])
		FROM users u
		LEFT JOIN user_identities i ON i.user_id = u.id AND i.provider = 'discord'`

var adminUserCols = []string{"id", "username", "display_name", "email", "disabled", "created_at", "discord_id", "discord_username", "role", "roles"}

func (s *Server) adminGetUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid user id")
		return
	}
	rows, err := s.Pool.Query(r.Context(), adminUserSQL+" WHERE u.id=$1", id)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	out := scanMaps(rows, adminUserCols...)
	if len(out) == 0 {
		writeErr(w, 404, "not_found", "user not found")
		return
	}
	writeJSON(w, 200, out[0])
}

func (s *Server) isAdministrator(ctx context.Context, userID uuid.UUID) bool {
	var ok bool
	_ = s.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id
			WHERE ur.user_id=$1 AND r.name='Administrator'
		)`, userID).Scan(&ok)
	return ok
}

func (s *Server) isLastAdministrator(ctx context.Context, userID uuid.UUID) bool {
	if !s.isAdministrator(ctx, userID) {
		return false
	}
	n, _ := s.Auth.AdministratorCount(ctx)
	return n <= 1
}

func (s *Server) discordIDForUser(ctx context.Context, userID uuid.UUID) string {
	var id *string
	_ = s.Pool.QueryRow(ctx, `SELECT provider_user_id FROM user_identities WHERE user_id=$1 AND provider='discord'`, userID).Scan(&id)
	if id == nil {
		return ""
	}
	return *id
}

func (s *Server) adminPatchUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid user id")
		return
	}
	var exists bool
	if err := s.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)`, id).Scan(&exists); err != nil || !exists {
		writeErr(w, 404, "not_found", "user not found")
		return
	}
	var body struct {
		Disabled *bool       `json:"disabled"`
		Role     *string     `json:"role"`
		RoleIDs  []uuid.UUID `json:"role_ids"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	self := currentUser(r).ID == id
	if body.Disabled != nil {
		if self && *body.Disabled {
			writeErr(w, 400, "self", "you cannot disable your own account")
			return
		}
		if *body.Disabled && s.isLastAdministrator(r.Context(), id) {
			writeErr(w, 409, "last_admin", "cannot disable the last administrator")
			return
		}
	}
	if body.Role != nil {
		role := strings.TrimSpace(*body.Role)
		if role != "User" && role != "Administrator" {
			writeErr(w, 400, "invalid", "role must be User or Administrator")
			return
		}
		if role != "Administrator" && s.isLastAdministrator(r.Context(), id) {
			writeErr(w, 409, "last_admin", "cannot remove the last administrator")
			return
		}
	}
	if body.RoleIDs != nil {
		losingAdmin := true
		for _, rid := range body.RoleIDs {
			var n string
			_ = s.Pool.QueryRow(r.Context(), `SELECT name FROM roles WHERE id=$1`, rid).Scan(&n)
			if n == "Administrator" {
				losingAdmin = false
				break
			}
		}
		if losingAdmin && s.isLastAdministrator(r.Context(), id) {
			writeErr(w, 409, "last_admin", "cannot remove the last administrator")
			return
		}
	}
	if body.Disabled != nil {
		_, _ = s.Pool.Exec(r.Context(), `UPDATE users SET disabled=$2, updated_at=now() WHERE id=$1`, id, *body.Disabled)
		if *body.Disabled {
			_ = s.Auth.DeleteUserSessions(r.Context(), id)
			_, _ = s.Pool.Exec(r.Context(), `UPDATE personal_access_tokens SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, id)
		}
		s.Audit.Event(r.Context(), &currentUser(r).ID, "user.disable", id.String(), r.RemoteAddr, nil)
	}
	if body.RoleIDs != nil {
		_, _ = s.Pool.Exec(r.Context(), `DELETE FROM user_roles WHERE user_id=$1`, id)
		for _, rid := range body.RoleIDs {
			_, _ = s.Pool.Exec(r.Context(), `INSERT INTO user_roles (user_id, role_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, id, rid)
		}
		s.Audit.Event(r.Context(), &currentUser(r).ID, "user.roles", id.String(), r.RemoteAddr, nil)
	} else if body.Role != nil {
		// Merge built-in User vs Administrator only. Do not wipe custom groups.
		role := strings.TrimSpace(*body.Role)
		if role == "Administrator" {
			_, _ = s.Pool.Exec(r.Context(), `
				INSERT INTO user_roles (user_id, role_id)
				SELECT $1, id FROM roles WHERE name='Administrator'
				ON CONFLICT DO NOTHING`, id)
		} else {
			_, _ = s.Pool.Exec(r.Context(), `
				DELETE FROM user_roles
				WHERE user_id=$1 AND role_id IN (SELECT id FROM roles WHERE name='Administrator')`, id)
			_, _ = s.Pool.Exec(r.Context(), `
				INSERT INTO user_roles (user_id, role_id)
				SELECT $1, id FROM roles WHERE name='User'
				ON CONFLICT DO NOTHING`, id)
		}
		s.Audit.Event(r.Context(), &currentUser(r).ID, "user.role", id.String()+":"+role, r.RemoteAddr, nil)
	}
	s.adminGetUser(w, r)
}

func (s *Server) adminUnlinkDiscord(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid user id")
		return
	}
	did := s.discordIDForUser(r.Context(), id)
	tag, err := s.Pool.Exec(r.Context(), `DELETE FROM user_identities WHERE user_id=$1 AND provider='discord'`, id)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, 404, "not_found", "no Discord identity on this user")
		return
	}
	auth.RemoveAdminDiscordID(r.Context(), s.Pool, did)
	_ = minilib.DetachDiscord(r.Context(), s.Pool, id)
	s.Audit.Event(r.Context(), &currentUser(r).ID, "user.unlink_discord", id.String(), r.RemoteAddr, nil)
	s.adminGetUser(w, r)
}

func (s *Server) adminDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid user id")
		return
	}
	if currentUser(r).ID == id {
		writeErr(w, 400, "self", "you cannot delete your own account")
		return
	}
	var exists bool
	if err := s.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)`, id).Scan(&exists); err != nil || !exists {
		writeErr(w, 404, "not_found", "user not found")
		return
	}
	if s.isLastAdministrator(r.Context(), id) {
		writeErr(w, 409, "last_admin", "cannot delete the last administrator")
		return
	}
	did := s.discordIDForUser(r.Context(), id)
	if _, err := s.Pool.Exec(r.Context(), `DELETE FROM users WHERE id=$1`, id); err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	auth.RemoveAdminDiscordID(r.Context(), s.Pool, did)
	s.Audit.Event(r.Context(), &currentUser(r).ID, "user.delete", id.String(), r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) adminCreateLibrary(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name, Kind, Prefix, Org string
		StorageID               uuid.UUID `json:"storage_id"`
		ReadOnly                *bool     `json:"read_only"`
	}
	_ = decodeJSON(r, &body)
	if body.Org == "" {
		body.Org = "virtual"
	}
	if body.Kind == "" {
		body.Kind = "music"
	}
	readOnly := false
	if body.ReadOnly != nil {
		readOnly = *body.ReadOnly
	} else {
		var typ string
		_ = s.Pool.QueryRow(r.Context(), `SELECT type FROM storage_providers WHERE id=$1`, body.StorageID).Scan(&typ)
		if typ == "local" || typ == "s3" {
			readOnly = true
		}
	}
	var id uuid.UUID
	err := s.Pool.QueryRow(r.Context(), `INSERT INTO libraries (name, kind, storage_provider_id, root_prefix, read_only, organisation_mode, is_default)
		VALUES ($1,$2,$3,$4,$5,$6, NOT EXISTS (SELECT 1 FROM libraries WHERE is_default)) RETURNING id`,
		body.Name, body.Kind, body.StorageID, body.Prefix, readOnly, body.Org).Scan(&id)
	if err != nil {
		writeErr(w, 400, "library", err.Error())
		return
	}
	s.Audit.Event(r.Context(), &currentUser(r).ID, "library.create", body.Name, r.RemoteAddr, nil)
	_, _ = s.Pool.Exec(r.Context(), `INSERT INTO library_grants (library_id, role_id, actions)
		SELECT $1, id, ARRAY['read','stream','write','admin'] FROM roles WHERE name='Administrator'`, id)
	_, _ = s.Pool.Exec(r.Context(), `INSERT INTO library_grants (library_id, role_id, actions)
		SELECT $1, id, ARRAY['read','stream'] FROM roles WHERE name='User'`, id)
	writeJSON(w, 201, map[string]any{"id": id})
}

func (s *Server) adminPatchLibrary(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	var body struct {
		Name             *string
		OrganisationMode *string `json:"organisation_mode"`
	}
	_ = decodeJSON(r, &body)
	if body.Name != nil {
		_, _ = s.Pool.Exec(r.Context(), `UPDATE libraries SET name=$2 WHERE id=$1`, id, *body.Name)
	}
	if body.OrganisationMode != nil {
		_, _ = s.Pool.Exec(r.Context(), `UPDATE libraries SET organisation_mode=$2 WHERE id=$1`, id, *body.OrganisationMode)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) adminScan(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	jid, err := s.Jobs.Enqueue(r.Context(), "library.scan", scan.Payload{LibraryID: id, Kind: "full"})
	if err != nil {
		s.writeJobErr(w, err)
		return
	}
	_, _ = s.Pool.Exec(r.Context(), `INSERT INTO scan_runs (library_id, job_id, kind) VALUES ($1,$2,'full')`, id, jid)
	writeJSON(w, 202, map[string]any{"job_id": jid})
}

func (s *Server) adminMigrate(w http.ResponseWriter, r *http.Request) {
	src, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid library id")
		return
	}
	var body struct {
		Dest   uuid.UUID `json:"dest_library_id"`
		Mode   string    `json:"mode"`
		Dedupe bool      `json:"dedupe"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	if body.Dest == uuid.Nil {
		writeErr(w, 400, "invalid", "dest_library_id required")
		return
	}
	if body.Dest == src {
		writeErr(w, 400, "invalid", "destination library must differ from source")
		return
	}
	var srcExists, destExists bool
	_ = s.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM libraries WHERE id=$1)`, src).Scan(&srcExists)
	if !srcExists {
		writeErr(w, 404, "not_found", "source library not found")
		return
	}
	_ = s.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM libraries WHERE id=$1)`, body.Dest).Scan(&destExists)
	if !destExists {
		writeErr(w, 404, "not_found", "destination library not found")
		return
	}
	req, effective, reason := "copy", "copy", ""
	if s.Ingest != nil {
		req, effective, reason = s.Ingest.ResolveMigrateModes(r.Context(), body.Mode, src)
	} else if strings.EqualFold(strings.TrimSpace(body.Mode), "move") {
		req, effective, reason = "move", "copy", "source_not_managed"
	}
	jid, err := s.Jobs.Enqueue(r.Context(), "library.migrate", ingest.MigratePayload{
		Source: src, Dest: body.Dest, Mode: effective, RequestedMode: req, EffectiveMode: effective, Reason: reason, Dedupe: body.Dedupe,
	})
	if err != nil {
		s.writeJobErr(w, err)
		return
	}
	writeJSON(w, 202, map[string]any{
		"job_id": jid, "requested_mode": req, "effective_mode": effective, "reason": reason,
	})
}

func (s *Server) adminTranscode(w http.ResponseWriter, r *http.Request) {
	st, _ := s.TX.Stats(r.Context())
	writeJSON(w, 200, st)
}

func (s *Server) adminClearCache(w http.ResponseWriter, r *http.Request) {
	_ = s.TX.Clear(r.Context())
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) adminBackups(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.Pool.Query(r.Context(), `SELECT b.id, b.path, b.size_bytes, b.status, b.created_at,
		COALESCE(b.destination,'local'), COALESCE(b.kind,'sql'), COALESCE(b.remote_key,''),
		(SELECT ok FROM backup_verifications v WHERE v.backup_id=b.id ORDER BY created_at DESC LIMIT 1) AS verified
		FROM backups b ORDER BY created_at DESC LIMIT 50`)
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "path", "size_bytes", "status", "created_at", "destination", "kind", "remote_key", "verified"))
}

func (s *Server) adminBackup(w http.ResponseWriter, r *http.Request) {
	id, err := s.Backup.Run(r.Context())
	if err != nil {
		code := 500
		if strings.Contains(err.Error(), "passphrase") || strings.Contains(err.Error(), "pg_dump is missing") {
			code = 400
		}
		writeErr(w, code, "backup", err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"id": id})
}

func (s *Server) adminAudit(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.Pool.Query(r.Context(), `SELECT id, actor_user_id, action, target, ip, created_at FROM audit_events ORDER BY created_at DESC LIMIT 200`)
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "actor_user_id", "action", "target", "ip", "created_at"))
}

func (s *Server) adminWebhooks(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.Pool.Query(r.Context(), `SELECT id, url, events, enabled FROM webhook_endpoints`)
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "url", "events", "enabled"))
}

func (s *Server) adminCreateWebhook(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL    string   `json:"url"`
		Secret string   `json:"secret"`
		Events []string `json:"events"`
	}
	_ = decodeJSON(r, &body)
	enc, _ := s.Box.Encrypt([]byte(body.Secret))
	var id uuid.UUID
	err := s.Pool.QueryRow(r.Context(), `INSERT INTO webhook_endpoints (url, secret_enc, events) VALUES ($1,$2,$3) RETURNING id`, body.URL, enc, body.Events).Scan(&id)
	if err != nil {
		writeErr(w, 400, "webhook", err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"id": id})
}

func (s *Server) adminDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	_, _ = s.Pool.Exec(r.Context(), `DELETE FROM webhook_endpoints WHERE id=$1`, id)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) adminIntegrations(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.Pool.Query(r.Context(), `
		SELECT c.id, c.name, c.scopes, c.last_used_at, c.created_at,
			(SELECT k.prefix FROM api_client_keys k WHERE k.client_id=c.id AND k.revoked_at IS NULL ORDER BY k.created_at DESC LIMIT 1)
		FROM api_clients c WHERE c.disabled=false
		ORDER BY c.created_at DESC`)
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "name", "scopes", "last_used_at", "created_at", "prefix"))
}

func (s *Server) adminCreateIntegration(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeErr(w, 400, "invalid", "name required")
		return
	}
	scopes, err := normalizeAPIKeyScopes(body.Scopes)
	if err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	secret, err := cryptox.RandomToken(32)
	if err != nil {
		writeErr(w, 500, "token", err.Error())
		return
	}
	plain := "sd_" + secret
	hash := cryptox.HashToken(plain)
	name := strings.TrimSpace(body.Name)
	var id uuid.UUID
	err = s.Pool.QueryRow(r.Context(), `INSERT INTO api_clients (name, scopes) VALUES ($1,$2) RETURNING id`, name, scopes).Scan(&id)
	if err != nil {
		writeErr(w, 500, "db", "could not create API key")
		return
	}
	_, _ = s.Pool.Exec(r.Context(), `INSERT INTO api_client_keys (client_id, prefix, secret_hash) VALUES ($1,$2,$3)`, id, plain[:10], hash)
	s.Audit.Event(r.Context(), &currentUser(r).ID, "integration.create", name, r.RemoteAddr, nil)
	writeJSON(w, 201, map[string]any{"id": id, "name": name, "scopes": scopes, "prefix": plain[:10], "secret": plain, "note": "shown once"})
}

func (s *Server) adminRevokeIntegration(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	_, _ = s.Pool.Exec(r.Context(), `UPDATE api_client_keys SET revoked_at=now() WHERE client_id=$1`, id)
	_, _ = s.Pool.Exec(r.Context(), `UPDATE api_clients SET disabled=true WHERE id=$1`, id)
	s.Audit.Event(r.Context(), &currentUser(r).ID, "integration.revoke", id.String(), r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

const metadataRefreshJobType = scan.JobRefresh

type metadataRefreshJobView struct {
	ID         uuid.UUID      `json:"id"`
	Status     string         `json:"status"`
	Progress   int            `json:"progress"`
	LastError  *string        `json:"last_error"`
	CreatedAt  time.Time      `json:"created_at"`
	StartedAt  *time.Time     `json:"started_at"`
	FinishedAt *time.Time     `json:"finished_at"`
	Result     map[string]any `json:"result,omitempty"`
}

func metadataRefreshBusy(status string) bool {
	return status == "queued" || status == "running" || status == "retry"
}

func (s *Server) latestMetadataRefreshJob(ctx context.Context) *metadataRefreshJobView {
	if s.Pool == nil {
		return nil
	}
	var j metadataRefreshJobView
	var raw []byte
	err := s.Pool.QueryRow(ctx, `
		SELECT id, status, progress, last_error, created_at, started_at, finished_at, result
		FROM jobs WHERE type=$1
		ORDER BY created_at DESC LIMIT 1`, metadataRefreshJobType).
		Scan(&j.ID, &j.Status, &j.Progress, &j.LastError, &j.CreatedAt, &j.StartedAt, &j.FinishedAt, &raw)
	if err != nil {
		return nil
	}
	if len(raw) > 0 && string(raw) != "{}" && string(raw) != "null" {
		_ = json.Unmarshal(raw, &j.Result)
	}
	return &j
}

func (s *Server) activeMetadataRefreshJobID(ctx context.Context) (uuid.UUID, bool) {
	if s.Pool == nil {
		return uuid.Nil, false
	}
	var id uuid.UUID
	err := s.Pool.QueryRow(ctx, `
		SELECT id FROM jobs
		WHERE type=$1 AND status IN ('queued','running','retry')
		ORDER BY created_at LIMIT 1`, metadataRefreshJobType).Scan(&id)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

func (s *Server) adminMetadata(w http.ResponseWriter, r *http.Request) {
	var enabled bool
	if s.Pool != nil {
		_ = s.Pool.QueryRow(r.Context(), `SELECT (value)::boolean FROM server_settings WHERE key='metadata_external_enabled'`).Scan(&enabled)
	}
	var tracks int
	if s.Pool != nil {
		_ = s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM tracks`).Scan(&tracks)
	}
	job := s.latestMetadataRefreshJob(r.Context())
	busy := job != nil && metadataRefreshBusy(job.Status)
	writeJSON(w, 200, map[string]any{
		"external_enabled": enabled,
		"providers":        []string{"musicbrainz", "coverartarchive"},
		"track_count":      tracks,
		"busy":             busy,
		"job":              job,
	})
}

func (s *Server) adminPutMetadata(w http.ResponseWriter, r *http.Request) {
	var body struct {
		External bool `json:"external_enabled"`
	}
	_ = decodeJSON(r, &body)
	if s.Pool != nil {
		_, _ = s.Pool.Exec(r.Context(), `INSERT INTO server_settings (key, value) VALUES ('metadata_external_enabled', to_jsonb($1::bool)) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, body.External)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) enableMetadataExternal(ctx context.Context) {
	if s.Pool == nil {
		return
	}
	_, _ = s.Pool.Exec(ctx, `INSERT INTO server_settings (key, value) VALUES ('metadata_external_enabled', 'true'::jsonb) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`)
}

func (s *Server) adminRefreshMetadata(w http.ResponseWriter, r *http.Request) {
	if s.Jobs == nil {
		writeErr(w, 503, "jobs", "worker runner is not available")
		return
	}
	if id, ok := s.activeMetadataRefreshJobID(r.Context()); ok {
		writeJSON(w, 409, map[string]any{
			"code":    "refresh_in_progress",
			"message": "a library metadata refresh is already queued or running",
			"job_id":  id,
		})
		return
	}
	s.enableMetadataExternal(r.Context())
	payload := map[string]any{}
	if u := currentUser(r); u != nil {
		payload["actor_id"] = u.ID
	}
	jid, err := s.Jobs.Enqueue(r.Context(), metadataRefreshJobType, payload)
	if err != nil {
		s.writeJobErr(w, err)
		return
	}
	if u := currentUser(r); u != nil && s.Audit != nil {
		s.Audit.Event(r.Context(), &u.ID, "metadata.refresh", jid.String(), r.RemoteAddr, nil)
	}
	writeJSON(w, 202, map[string]any{"job_id": jid})
}

func (s *Server) adminLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	items, next, err := oplog.List(r.Context(), s.Pool, oplog.Filter{
		Level:    q.Get("level"),
		Category: q.Get("category"),
		Q:        q.Get("q"),
		Limit:    limit,
		Cursor:   q.Get("cursor"),
	})
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, e := range items {
		jobType := e.Type
		if jobType == "" && e.Category == "job" {
			jobType = e.Category
		}
		human := e.Message
		if e.Category == "job" || jobType != "" {
			human = explainJobError(jobType, e.Message)
		}
		out = append(out, map[string]any{
			"id":         e.ID,
			"type":       jobType,
			"category":   e.Category,
			"level":      e.Level,
			"error":      human,
			"detail":     e.Message,
			"message":    e.Message,
			"at":         e.CreatedAt,
			"created_at": e.CreatedAt,
			"summary":    jobTypeSummary(jobType),
			"job_id":     e.JobID,
		})
	}
	writeJSON(w, 200, map[string]any{
		"items":       out,
		"next_cursor": next,
		"limit":       limit,
	})
}

func jobTypeSummary(typ string) string {
	switch typ {
	case "external.playlist.import":
		return "Playlist import"
	case "external.playlist.tick":
		return "Playlist sync"
	case "scapex.fetch":
		return "Download"
	case "library.scan":
		return "Library scan"
	case "backup.run":
		return "Backup"
	case "app.update.apply":
		return "App update"
	default:
		if typ == "" {
			return "Job"
		}
		return typ
	}
}

func (s *Server) discordGet(w http.ResponseWriter, r *http.Request) {
	var enabled bool
	var appID, status, clientID *string
	var tok, sec []byte
	var lastErr, commands *string
	_ = s.Pool.QueryRow(r.Context(), `SELECT enabled, application_id, last_gateway_status, bot_token_enc, client_id, client_secret_enc, last_error_redacted, command_registration_status FROM discord_settings WHERE id=1`).Scan(&enabled, &appID, &status, &tok, &clientID, &sec, &lastErr, &commands)
	reg, _ := auth.LoadDiscordRegistration(r.Context(), s.Pool)
	oauth := auth.LoadDiscordOAuth(r.Context(), s.Pool, s.Box)
	out := map[string]any{
		"enabled":                    enabled,
		"application_id":             appID,
		"client_id":                  clientID,
		"gateway_status":             status,
		"token_configured":           len(tok) > 0,
		"secret_configured":          len(sec) > 0,
		"login_enabled":              oauth.LoginEnabled,
		"login_ready":                oauth.Ready(),
		"oauth_redirect":             auth.DiscordCallbackURL(s.absURL(r)),
		"admin_discord_ids":          auth.LoadAdminDiscordIDs(r.Context(), s.Pool),
		"registration_guild_enabled": reg.GuildEnabled,
		"registration_guild_id":      reg.GuildID,
		"registration_role_enabled":  reg.RoleEnabled,
		"registration_role_id":       reg.RoleID,
	}
	if commands != nil {
		out["command_registration_status"] = *commands
	}
	if lastErr != nil && *lastErr != "" {
		out["last_error"] = inspectRedact(*lastErr)
	}
	writeJSON(w, 200, out)
}

func (s *Server) discordPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled                  bool      `json:"enabled"`
		LoginEnabled             *bool     `json:"login_enabled"`
		Token                    *string   `json:"token"`
		ApplicationID            *string   `json:"application_id"`
		ClientID                 *string   `json:"client_id"`
		ClientSecret             *string   `json:"client_secret"`
		RegistrationGuildEnabled *bool     `json:"registration_guild_enabled"`
		RegistrationGuildID      *string   `json:"registration_guild_id"`
		RegistrationRoleEnabled  *bool     `json:"registration_role_enabled"`
		RegistrationRoleID       *string   `json:"registration_role_id"`
		AdminDiscordIDs          *[]string `json:"admin_discord_ids"`
	}
	_ = decodeJSON(r, &body)
	if body.Token != nil && *body.Token != "" && s.Box != nil {
		enc, _ := s.Box.Encrypt([]byte(*body.Token))
		_, _ = s.Pool.Exec(r.Context(), `UPDATE discord_settings SET enabled=$1, bot_token_enc=$2, application_id=coalesce($3, application_id), client_id=coalesce($4, client_id), updated_at=now() WHERE id=1`, body.Enabled, enc, body.ApplicationID, body.ClientID)
	} else {
		_, _ = s.Pool.Exec(r.Context(), `UPDATE discord_settings SET enabled=$1, application_id=coalesce($2, application_id), client_id=coalesce($3, client_id), updated_at=now() WHERE id=1`, body.Enabled, body.ApplicationID, body.ClientID)
	}
	if body.ClientSecret != nil && *body.ClientSecret != "" && s.Box != nil {
		enc, _ := s.Box.Encrypt([]byte(*body.ClientSecret))
		_, _ = s.Pool.Exec(r.Context(), `UPDATE discord_settings SET client_secret_enc=$1, updated_at=now() WHERE id=1`, enc)
	}
	if body.LoginEnabled != nil {
		_, _ = s.Pool.Exec(r.Context(), `UPDATE discord_settings SET login_enabled=$1, updated_at=now() WHERE id=1`, *body.LoginEnabled)
	}
	if body.RegistrationGuildEnabled != nil || body.RegistrationGuildID != nil || body.RegistrationRoleEnabled != nil || body.RegistrationRoleID != nil {
		cur, _ := auth.LoadDiscordRegistration(r.Context(), s.Pool)
		ge, gid, re, rid := cur.GuildEnabled, cur.GuildID, cur.RoleEnabled, cur.RoleID
		if body.RegistrationGuildEnabled != nil {
			ge = *body.RegistrationGuildEnabled
		}
		if body.RegistrationGuildID != nil {
			gid = strings.TrimSpace(*body.RegistrationGuildID)
		}
		if body.RegistrationRoleEnabled != nil {
			re = *body.RegistrationRoleEnabled
		}
		if body.RegistrationRoleID != nil {
			rid = strings.TrimSpace(*body.RegistrationRoleID)
		}
		_, _ = s.Pool.Exec(r.Context(), `UPDATE discord_settings SET registration_guild_enabled=$1, registration_guild_id=$2, registration_role_enabled=$3, registration_role_id=$4, updated_at=now() WHERE id=1`, ge, gid, re, rid)
	}
	if body.AdminDiscordIDs != nil {
		ids, err := auth.NormalizeAdminDiscordIDs(*body.AdminDiscordIDs)
		if err != nil {
			writeErr(w, 400, "invalid_id", "Discord user IDs must be numeric snowflakes")
			return
		}
		if err := auth.SaveAdminDiscordIDs(r.Context(), s.Pool, ids); err != nil {
			writeErr(w, 500, "db", "could not save Discord administrator IDs")
			return
		}
	}
	s.Audit.Event(r.Context(), &currentUser(r).ID, "discord.update", "", r.RemoteAddr, nil)
	s.discordGet(w, r)
}

func (s *Server) discordTest(w http.ResponseWriter, r *http.Request) {
	var enabled bool
	var tok []byte
	_ = s.Pool.QueryRow(r.Context(), `SELECT enabled, bot_token_enc FROM discord_settings WHERE id=1`).Scan(&enabled, &tok)
	writeJSON(w, 200, map[string]any{"ok": enabled && len(tok) > 0, "enabled": enabled, "token_configured": len(tok) > 0})
}

func (s *Server) discordInvite(w http.ResponseWriter, r *http.Request) {
	var appID *string
	_ = s.Pool.QueryRow(r.Context(), `SELECT coalesce(application_id, client_id) FROM discord_settings WHERE id=1`).Scan(&appID)
	if appID == nil || *appID == "" {
		writeErr(w, 400, "not_configured", "set application id first")
		return
	}
	perms := "36727808" // Connect, Speak, View Channel, Send Messages, Embed Links, Attach Files, Use App Commands-ish
	url := "https://discord.com/oauth2/authorize?client_id=" + *appID + "&scope=bot%20applications.commands&permissions=" + perms
	writeJSON(w, 200, map[string]any{
		"url":         url,
		"scopes":      []string{"bot", "applications.commands"},
		"permissions": []string{"Connect", "Speak", "View Channel", "Send Messages", "Embed Links", "Attach Files", "Use Application Commands"},
	})
}

func (s *Server) discordSync(w http.ResponseWriter, r *http.Request) {
	_, _ = s.Pool.Exec(r.Context(), `UPDATE discord_settings SET command_registration_status='pending', updated_at=now() WHERE id=1`)
	writeJSON(w, 202, map[string]string{"status": "pending"})
}

func (s *Server) discordStatus(w http.ResponseWriter, r *http.Request) {
	var st, cmd string
	var n int
	_ = s.Pool.QueryRow(r.Context(), `SELECT last_gateway_status, command_registration_status FROM discord_settings WHERE id=1`).Scan(&st, &cmd)
	_ = s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM discord_guilds`).Scan(&n)
	writeJSON(w, 200, map[string]any{"gateway": st, "commands": cmd, "guilds": n})
}

func (s *Server) discordGuilds(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.Pool.Query(r.Context(), `SELECT id, name, enabled, default_volume, queue_limit, explicit_policy FROM discord_guilds`)
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "name", "enabled", "default_volume", "queue_limit", "explicit_policy"))
}

func (s *Server) discordPatchGuild(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body map[string]any
	_ = decodeJSON(r, &body)
	if v, ok := body["enabled"].(bool); ok {
		_, _ = s.Pool.Exec(r.Context(), `UPDATE discord_guilds SET enabled=$2 WHERE id=$1`, id, v)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) discordDisconnect(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if bot := discordx.Live(); bot != nil {
		_ = bot.LeaveGuild(r.Context(), id)
	}
	discordx.MarkVoiceDisconnected(r.Context(), s.Pool, id, "admin")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) discordSessions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.listDiscordRuntime(r.Context()))
}

func (s *Server) discordLogs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.listDiscordPlaybackErrors(r.Context(), 100))
}

func (s *Server) adminUpdatesGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, update.Load(r.Context(), s.Pool))
}

func (s *Server) adminUpdatesPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AutoEnabled *bool `json:"auto_enabled"`
	}
	_ = decodeJSON(r, &body)
	if body.AutoEnabled != nil {
		if err := update.SetAuto(r.Context(), s.Pool, *body.AutoEnabled); err != nil {
			writeErr(w, 500, "db", "could not save update settings")
			return
		}
	}
	s.Audit.Event(r.Context(), &currentUser(r).ID, "updates.settings", "", r.RemoteAddr, nil)
	writeJSON(w, 200, update.Load(r.Context(), s.Pool))
}

func (s *Server) adminUpdatesCheck(w http.ResponseWriter, r *http.Request) {
	st, err := update.Check(r.Context(), s.Pool)
	if err != nil {
		writeJSON(w, 200, st)
		return
	}
	writeJSON(w, 200, st)
}

func (s *Server) adminUpdatesApply(w http.ResponseWriter, r *http.Request) {
	if !update.CanApply() {
		writeErr(w, 503, "no_helper", "The host update helper is not running. Re-run the installer so sounddock-update can pull images on the host.")
		return
	}
	u := currentUser(r)
	who := "admin"
	if u != nil && u.Username != "" {
		who = u.Username
	}
	started, err := update.BeginApply(r.Context(), s.Pool, who)
	if err != nil {
		writeErr(w, 500, "update", err.Error())
		return
	}
	if started {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			_ = update.RunApply(ctx, s.Pool, who)
		}()
	}
	var uid *uuid.UUID
	if u != nil {
		uid = &u.ID
	}
	s.Audit.Event(r.Context(), uid, "updates.apply", "", r.RemoteAddr, nil)
	writeJSON(w, 202, map[string]any{"ok": true, "message": "The host is pulling the new image. SoundDock stays up until the new container starts."})
}

var _ = storage.ErrNotFound
