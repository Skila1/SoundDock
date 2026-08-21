package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
	"github.com/sounddock/sounddock/internal/ingest"
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
	rows, _ := s.Pool.Query(r.Context(), `SELECT id, type, status, progress, last_error, created_at FROM jobs ORDER BY created_at DESC LIMIT 100`)
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "type", "status", "progress", "last_error", "created_at"))
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	_ = s.Jobs.RequestCancel(r.Context(), id)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.Pool.Query(r.Context(), `
		SELECT u.id, u.username, u.display_name, u.email, u.disabled, u.created_at,
			i.provider_user_id, i.provider_username
		FROM users u
		LEFT JOIN user_identities i ON i.user_id = u.id AND i.provider = 'discord'
		ORDER BY u.created_at`)
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "username", "display_name", "email", "disabled", "created_at", "discord_id", "discord_username"))
}

func (s *Server) adminPatchUser(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	var body struct{ Disabled *bool }
	_ = decodeJSON(r, &body)
	if body.Disabled != nil {
		_, _ = s.Pool.Exec(r.Context(), `UPDATE users SET disabled=$2 WHERE id=$1`, id, *body.Disabled)
		s.Audit.Event(r.Context(), &currentUser(r).ID, "user.disable", id.String(), r.RemoteAddr, nil)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) adminStorage(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.Pool.Query(r.Context(), `SELECT id, name, type FROM storage_providers`)
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "name", "type"))
}

func (s *Server) adminCreateStorage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string          `json:"name"`
		Type   string          `json:"type"`
		Config json.RawMessage `json:"config"`
	}
	_ = decodeJSON(r, &body)
	enc := body.Config
	if s.Box != nil && len(body.Config) > 0 {
		if b, err := s.Box.Encrypt(body.Config); err == nil {
			enc = b
		}
	}
	if body.Type == "local" || body.Type == "managed" {
		var c struct {
			Root string `json:"root"`
		}
		_ = json.Unmarshal(body.Config, &c)
		if c.Root == "" {
			c.Root = s.Cfg.ManagedDir
		}
		if s.Box != nil {
			enc, _ = s.Box.Encrypt([]byte(c.Root))
		} else {
			enc = []byte(c.Root)
		}
	}
	var id uuid.UUID
	err := s.Pool.QueryRow(r.Context(), `INSERT INTO storage_providers (name, type, config_enc) VALUES ($1,$2,$3) RETURNING id`, body.Name, body.Type, enc).Scan(&id)
	if err != nil {
		writeErr(w, 400, "storage", err.Error())
		return
	}
	s.Audit.Event(r.Context(), &currentUser(r).ID, "storage.create", body.Name, r.RemoteAddr, nil)
	writeJSON(w, 201, map[string]any{"id": id})
}

func (s *Server) adminCreateLibrary(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name, Kind, Prefix, Org string
		StorageID               uuid.UUID `json:"storage_id"`
		ReadOnly                bool      `json:"read_only"`
	}
	_ = decodeJSON(r, &body)
	if body.Org == "" {
		body.Org = "virtual"
	}
	if body.Kind == "" {
		body.Kind = "music"
	}
	var id uuid.UUID
	err := s.Pool.QueryRow(r.Context(), `INSERT INTO libraries (name, kind, storage_provider_id, root_prefix, read_only, organisation_mode) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		body.Name, body.Kind, body.StorageID, body.Prefix, body.ReadOnly, body.Org).Scan(&id)
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
		writeErr(w, 500, "job", err.Error())
		return
	}
	writeJSON(w, 202, map[string]any{"job_id": jid})
}

func (s *Server) adminMigrate(w http.ResponseWriter, r *http.Request) {
	src, _ := uuid.Parse(chi.URLParam(r, "id"))
	var body struct {
		Dest   uuid.UUID `json:"dest_library_id"`
		Mode   string    `json:"mode"`
		Dedupe bool      `json:"dedupe"`
	}
	_ = decodeJSON(r, &body)
	jid, err := s.Jobs.Enqueue(r.Context(), "library.migrate", ingest.MigratePayload{Source: src, Dest: body.Dest, Mode: body.Mode, Dedupe: body.Dedupe})
	if err != nil {
		writeErr(w, 500, "job", err.Error())
		return
	}
	writeJSON(w, 202, map[string]any{"job_id": jid})
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
		(SELECT ok FROM backup_verifications v WHERE v.backup_id=b.id ORDER BY created_at DESC LIMIT 1) AS verified
		FROM backups b ORDER BY created_at DESC LIMIT 50`)
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "path", "size_bytes", "status", "created_at", "verified"))
}

func (s *Server) adminBackup(w http.ResponseWriter, r *http.Request) {
	id, err := s.Backup.Run(r.Context())
	if err != nil {
		writeErr(w, 500, "backup", err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"id": id})
}

func (s *Server) adminAudit(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.Pool.Query(r.Context(), `SELECT id, action, target, ip, created_at FROM audit_events ORDER BY created_at DESC LIMIT 200`)
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "action", "target", "ip", "created_at"))
}

func (s *Server) adminRetention(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.Pool.Query(r.Context(), `SELECT key, days FROM retention_policies`)
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "key", "days"))
}

func (s *Server) adminPutRetention(w http.ResponseWriter, r *http.Request) {
	var body map[string]int
	_ = decodeJSON(r, &body)
	for k, d := range body {
		_, _ = s.Pool.Exec(r.Context(), `INSERT INTO retention_policies (key, days) VALUES ($1,$2) ON CONFLICT (key) DO UPDATE SET days=EXCLUDED.days`, k, d)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
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
	rows, _ := s.Pool.Query(r.Context(), `SELECT id, name, scopes, last_used_at, created_at FROM api_clients WHERE disabled=false`)
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "name", "scopes", "last_used_at", "created_at"))
}

func (s *Server) adminCreateIntegration(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	_ = decodeJSON(r, &body)
	secret, _ := cryptox.RandomToken(32)
	plain := "sd_" + secret
	hash := cryptox.HashToken(plain)
	var id uuid.UUID
	_ = s.Pool.QueryRow(r.Context(), `INSERT INTO api_clients (name, scopes) VALUES ($1,$2) RETURNING id`, body.Name, body.Scopes).Scan(&id)
	_, _ = s.Pool.Exec(r.Context(), `INSERT INTO api_client_keys (client_id, prefix, secret_hash) VALUES ($1,$2,$3)`, id, plain[:10], hash)
	s.Audit.Event(r.Context(), &currentUser(r).ID, "integration.create", body.Name, r.RemoteAddr, nil)
	writeJSON(w, 201, map[string]any{"id": id, "secret": plain, "note": "shown once"})
}

func (s *Server) adminRevokeIntegration(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	_, _ = s.Pool.Exec(r.Context(), `UPDATE api_client_keys SET revoked_at=now() WHERE client_id=$1`, id)
	s.Audit.Event(r.Context(), &currentUser(r).ID, "integration.revoke", id.String(), r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) adminMetadata(w http.ResponseWriter, r *http.Request) {
	var enabled bool
	_ = s.Pool.QueryRow(r.Context(), `SELECT (value)::boolean FROM server_settings WHERE key='metadata_external_enabled'`).Scan(&enabled)
	writeJSON(w, 200, map[string]any{"external_enabled": enabled, "providers": []string{"musicbrainz", "coverartarchive"}})
}

func (s *Server) adminPutMetadata(w http.ResponseWriter, r *http.Request) {
	var body struct {
		External bool `json:"external_enabled"`
	}
	_ = decodeJSON(r, &body)
	_, _ = s.Pool.Exec(r.Context(), `INSERT INTO server_settings (key, value) VALUES ('metadata_external_enabled', to_jsonb($1::bool)) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, body.External)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) adminLogs(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.Pool.Query(r.Context(), `SELECT type, last_error, updated_at FROM jobs WHERE last_error IS NOT NULL ORDER BY updated_at DESC LIMIT 50`)
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "type", "error", "at"))
}

func (s *Server) discordGet(w http.ResponseWriter, r *http.Request) {
	var enabled bool
	var appID, status, clientID *string
	var tok, sec []byte
	_ = s.Pool.QueryRow(r.Context(), `SELECT enabled, application_id, last_gateway_status, bot_token_enc, client_id, client_secret_enc FROM discord_settings WHERE id=1`).Scan(&enabled, &appID, &status, &tok, &clientID, &sec)
	reg, _ := auth.LoadDiscordRegistration(r.Context(), s.Pool)
	oauth := auth.LoadDiscordOAuth(r.Context(), s.Pool, s.Box)
	writeJSON(w, 200, map[string]any{
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
	})
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
	_, _ = s.Pool.Exec(r.Context(), `UPDATE discord_voice_runtime SET connected=false, last_disconnect_reason='admin' WHERE guild_id=$1`, id)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) discordSessions(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.Pool.Query(r.Context(), `SELECT guild_id, voice_channel_id, connected, last_disconnect_reason FROM discord_voice_runtime`)
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "guild_id", "voice_channel_id", "connected", "reason"))
}

func (s *Server) discordLogs(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.Pool.Query(r.Context(), `SELECT guild_id, error_class, message, created_at FROM discord_playback_errors ORDER BY created_at DESC LIMIT 100`)
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "guild_id", "error_class", "message", "created_at"))
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
	if !update.SocketOK() {
		writeErr(w, 503, "no_socket", "Docker socket is not available. Re-run the installer so SoundDock can pull images.")
		return
	}
	u := currentUser(r)
	who := "admin"
	if u != nil && u.Username != "" {
		who = u.Username
	}
	id, err := s.Jobs.Enqueue(r.Context(), "app.update.apply", map[string]any{"by": who})
	if err != nil {
		writeErr(w, 500, "job", "could not start update")
		return
	}
	var uid *uuid.UUID
	if u != nil {
		uid = &u.ID
	}
	s.Audit.Event(r.Context(), uid, "updates.apply", id.String(), r.RemoteAddr, nil)
	writeJSON(w, 202, map[string]any{"ok": true, "job_id": id, "message": "Update started. The app will restart when the new image is ready."})
}

var _ = storage.ErrNotFound
