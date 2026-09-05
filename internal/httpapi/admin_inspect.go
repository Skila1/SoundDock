package httpapi

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/oplog"
)

// Admin inspect dumps. An admin API key can read live playback, Discord bind
// state, and every error table — /me/queue only sees the caller's session.

func inspectLimit(r *http.Request, def, max int) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if n <= 0 {
		n = def
	}
	if n > max {
		n = max
	}
	return n
}

func inspectQuery(r *http.Request, key string) string {
	return strings.TrimSpace(r.URL.Query().Get(key))
}

func inspectParseUUID(s string) uuid.UUID {
	id, err := uuid.Parse(strings.TrimSpace(s))
	if err != nil {
		return uuid.Nil
	}
	return id
}

func inspectRedact(s string) string {
	return oplog.Redact(strings.TrimSpace(s))
}

func emptyList() []map[string]any {
	return []map[string]any{}
}

func (s *Server) adminInspect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limit := inspectLimit(r, 40, 200)
	window := 48 * time.Hour
	disc := s.inspectDiscord(ctx, limit)
	if errs, ok := disc["playback_errors"].([]map[string]any); ok {
		disc["playback_errors"] = inspectRecent(errs, window)
	}
	writeJSON(w, 200, map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"error_window": "48h",
		"endpoints":    adminInspectEndpoints(),
		"counts":       s.inspectCounts(ctx),
		"playback":     map[string]any{"sessions": s.listPlaybackSessions(ctx, playbackInspectFilter{Limit: limit})},
		"discord":      disc,
		"errors":       map[string]any{"items": inspectRecent(s.listInspectErrors(ctx, inspectErrorFilter{Limit: limit}), window)},
		"jobs":         map[string]any{"failed": inspectRecent(s.listFailedJobs(ctx, limit), window)},
		"acquisition": map[string]any{
			"intents": s.listAcquisitionIntents(ctx, acquisitionInspectFilter{FailedOnly: true, Limit: limit}),
			"holds":   s.listMediaHolds(ctx, limit),
		},
		"scans":    map[string]any{"file_errors": s.listScanFileErrors(ctx, limit)},
		"external": s.inspectExternal(ctx, limit),
		"webhooks": map[string]any{"deliveries": s.listWebhookDeliveries(ctx, limit)},
	})
}

func adminInspectEndpoints() []map[string]string {
	return []map[string]string{
		{"method": "GET", "path": "/api/v1/admin/inspect", "description": "One-shot dump of every live section and recent error"},
		{"method": "GET", "path": "/api/v1/admin/errors", "description": "Unified errors from every table. ?source=&q=&limit="},
		{"method": "GET", "path": "/api/v1/admin/playback/sessions", "description": "All playback sessions + queues + Discord bind. ?kind=&user_id=&status=&limit="},
		{"method": "GET", "path": "/api/v1/admin/playback/sessions/{id}", "description": "One session: queue, bind, holds, receipts, party"},
		{"method": "GET", "path": "/api/v1/admin/users/{id}/playback", "description": "That user's web sessions, attached Discord session, voice, recent acquire/errors"},
		{"method": "GET", "path": "/api/v1/admin/discord/runtime", "description": "Full Discord settings, runtime, voice members, playback errors"},
		{"method": "GET", "path": "/api/v1/admin/acquisition", "description": "Acquisition intents. ?status=&user_id=&session_id=&limit="},
		{"method": "GET", "path": "/api/v1/admin/media/holds", "description": "Active and recent media holds"},
		{"method": "GET", "path": "/api/v1/admin/scans/errors", "description": "Per-file scan errors"},
		{"method": "GET", "path": "/api/v1/admin/webhooks/deliveries", "description": "Webhook delivery attempts and last_error"},
		{"method": "GET", "path": "/api/v1/admin/external/errors", "description": "Provider account, playlist, and sync errors"},
	}
}

func (s *Server) adminInspectErrors(w http.ResponseWriter, r *http.Request) {
	items := s.listInspectErrors(r.Context(), inspectErrorFilter{
		Source: inspectQuery(r, "source"),
		Q:      inspectQuery(r, "q"),
		Limit:  inspectLimit(r, 100, 500),
	})
	writeJSON(w, 200, map[string]any{
		"items":  items,
		"limit":  inspectLimit(r, 100, 500),
		"source": inspectQuery(r, "source"),
	})
}

func (s *Server) adminPlaybackSessions(w http.ResponseWriter, r *http.Request) {
	items := s.listPlaybackSessions(r.Context(), playbackInspectFilter{
		Kind:   inspectQuery(r, "kind"),
		UserID: inspectParseUUID(inspectQuery(r, "user_id")),
		Status: inspectQuery(r, "status"),
		Limit:  inspectLimit(r, 100, 500),
	})
	writeJSON(w, 200, map[string]any{"sessions": items, "count": len(items)})
}

func (s *Server) adminPlaybackSession(w http.ResponseWriter, r *http.Request) {
	id := inspectParseUUID(chi.URLParam(r, "id"))
	if id == uuid.Nil {
		writeErr(w, 400, "invalid", "session id required")
		return
	}
	items := s.listPlaybackSessions(r.Context(), playbackInspectFilter{SessionID: id, Limit: 1})
	if len(items) == 0 {
		writeErr(w, 404, "not_found", "playback session not found")
		return
	}
	sess := items[0]
	sess["receipts"] = s.listSessionReceipts(r.Context(), id, 20)
	sess["party"] = s.listSessionParty(r.Context(), id)
	sess["holds"] = s.listSessionHolds(r.Context(), id)
	writeJSON(w, 200, sess)
}

func (s *Server) adminUserPlayback(w http.ResponseWriter, r *http.Request) {
	uid := inspectParseUUID(chi.URLParam(r, "id"))
	if uid == uuid.Nil {
		writeErr(w, 400, "invalid", "user id required")
		return
	}
	ctx := r.Context()
	var username, display, discordID, discordName *string
	if s.Pool != nil {
		_ = s.Pool.QueryRow(ctx, `
			SELECT u.username, u.display_name, i.provider_user_id,
				(SELECT v.display_name FROM discord_user_voice v WHERE v.discord_user_id=i.provider_user_id ORDER BY v.updated_at DESC LIMIT 1)
			FROM users u
			LEFT JOIN user_identities i ON i.user_id=u.id AND i.provider='discord'
			WHERE u.id=$1`, uid).Scan(&username, &display, &discordID, &discordName)
	}
	if username == nil {
		writeErr(w, 404, "not_found", "user not found")
		return
	}
	sessions := s.listPlaybackSessions(ctx, playbackInspectFilter{UserID: uid, Limit: 50})
	voice := emptyList()
	did := ""
	if discordID != nil {
		did = strings.TrimSpace(*discordID)
	}
	if did != "" {
		voice = s.listDiscordVoiceForUser(ctx, did)
		bound := s.listPlaybackSessions(ctx, playbackInspectFilter{DiscordUserID: did, Limit: 20})
		seen := map[string]bool{}
		for _, sess := range sessions {
			seen[asString(sess["id"])] = true
		}
		for _, sess := range bound {
			if !seen[asString(sess["id"])] {
				sessions = append(sessions, sess)
			}
		}
	}
	writeJSON(w, 200, map[string]any{
		"user": map[string]any{
			"id":               uid.String(),
			"username":         derefStr(username),
			"display_name":     derefStr(display),
			"discord_id":       did,
			"discord_username": derefStr(discordName),
		},
		"voice":       voice,
		"sessions":    sessions,
		"acquisition": s.listAcquisitionIntents(ctx, acquisitionInspectFilter{UserID: uid, Limit: 40}),
		"errors":      s.listInspectErrors(ctx, inspectErrorFilter{UserID: uid, DiscordUserID: did, Limit: 40}),
	})
}

func (s *Server) adminDiscordRuntime(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.inspectDiscord(r.Context(), inspectLimit(r, 100, 500)))
}

func (s *Server) adminAcquisitionInspect(w http.ResponseWriter, r *http.Request) {
	status := inspectQuery(r, "status")
	failedOnly := status == ""
	items := s.listAcquisitionIntents(r.Context(), acquisitionInspectFilter{
		Status:     status,
		UserID:     inspectParseUUID(inspectQuery(r, "user_id")),
		SessionID:  inspectParseUUID(inspectQuery(r, "session_id")),
		FailedOnly: failedOnly,
		Limit:      inspectLimit(r, 100, 500),
	})
	writeJSON(w, 200, map[string]any{"intents": items, "holds": s.listMediaHolds(r.Context(), inspectLimit(r, 100, 500))})
}

func (s *Server) adminMediaHolds(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"holds": s.listMediaHolds(r.Context(), inspectLimit(r, 200, 500))})
}

func (s *Server) adminScanErrors(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"items": s.listScanFileErrors(r.Context(), inspectLimit(r, 100, 500))})
}

func (s *Server) adminWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"deliveries": s.listWebhookDeliveries(r.Context(), inspectLimit(r, 100, 500))})
}

func (s *Server) adminExternalErrors(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.inspectExternal(r.Context(), inspectLimit(r, 100, 500)))
}

type playbackInspectFilter struct {
	Kind          string
	UserID        uuid.UUID
	SessionID     uuid.UUID
	Status        string
	DiscordUserID string
	Limit         int
}

func (s *Server) listPlaybackSessions(ctx context.Context, f playbackInspectFilter) []map[string]any {
	if s == nil || s.Pool == nil {
		return emptyList()
	}
	if f.Limit <= 0 {
		f.Limit = 100
	}
	q := `
		SELECT s.id, s.kind, s.owner_key, s.user_id, u.username, u.display_name,
			s.device_id, s.last_device, s.status, s.output_pref, s.current_index, s.current_track_id,
			coalesce(t.title,''), coalesce(t.acquisition,''), coalesce(t.acquisition_ref,''),
			s.position_ms, s.duration_ms, s.checkpoint_at, s.playback_rate, s.playhead_sequence,
			s.state_revision, s.renderer_kind, s.renderer_id, s.renderer_generation, s.renderer_heartbeat_at,
			s.volume, s.muted, s.repeat_mode, s.shuffle, s.autoplay, s.updated_at, s.playback_instance_id,
			EXISTS(SELECT 1 FROM track_files tf WHERE tf.track_id=s.current_track_id) AS current_has_file,
			r.guild_id, r.voice_channel_id, r.connected, r.last_disconnect_reason, r.binding_revision
		FROM playback_sessions s
		LEFT JOIN users u ON u.id = s.user_id
		LEFT JOIN tracks t ON t.id = s.current_track_id
		LEFT JOIN discord_voice_runtime r ON r.session_id = s.id
		WHERE ($1='' OR s.kind=$1)
		  AND ($2::uuid IS NULL OR s.user_id=$2)
		  AND ($3::uuid IS NULL OR s.id=$3)
		  AND ($4='' OR s.status=$4)
		  AND ($5='' OR EXISTS (
			SELECT 1 FROM discord_user_voice v
			WHERE v.discord_user_id=$5 AND v.guild_id=r.guild_id AND v.channel_id=r.voice_channel_id
		  ))
		ORDER BY s.updated_at DESC NULLS LAST
		LIMIT $6`
	var userArg, sessArg any
	if f.UserID != uuid.Nil {
		userArg = f.UserID
	}
	if f.SessionID != uuid.Nil {
		sessArg = f.SessionID
	}
	rows, err := s.Pool.Query(ctx, q, f.Kind, userArg, sessArg, f.Status, f.DiscordUserID, f.Limit)
	if err != nil {
		return emptyList()
	}
	defer rows.Close()
	out := emptyList()
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		var kind, owner, status, outputPref, rendererKind, repeat string
		var title, acq, acqRef string
		var userID *uuid.UUID
		var username, display, deviceID, lastDevice, rendererID *string
		var cur *uuid.UUID
		var instanceID *uuid.UUID
		var idx, pos, duration int
		var rate, vol float64
		var seq, stateRev, rendererGen int64
		var muted, shuffle, autoplay, hasFile bool
		var checkpoint, heartbeat, updated *time.Time
		var guildID, channelID, disconnect *string
		var connected *bool
		var bindRev *int64
		if err := rows.Scan(&id, &kind, &owner, &userID, &username, &display, &deviceID, &lastDevice, &status, &outputPref,
			&idx, &cur, &title, &acq, &acqRef, &pos, &duration, &checkpoint, &rate, &seq, &stateRev,
			&rendererKind, &rendererID, &rendererGen, &heartbeat, &vol, &muted, &repeat, &shuffle, &autoplay,
			&updated, &instanceID, &hasFile, &guildID, &channelID, &connected, &disconnect, &bindRev); err != nil {
			continue
		}
		m := map[string]any{
			"id": id.String(), "kind": kind, "owner_key": owner, "status": status, "output_pref": outputPref,
			"current_index": idx, "position_ms": pos, "duration_ms": duration, "playback_rate": rate,
			"playhead_sequence": seq, "state_revision": stateRev, "renderer_kind": rendererKind,
			"renderer_generation": rendererGen, "volume": vol, "muted": muted, "repeat": repeat,
			"shuffle": shuffle, "autoplay": autoplay, "current_has_file": hasFile, "items": emptyList(),
		}
		if userID != nil {
			m["user_id"] = userID.String()
		}
		if username != nil {
			m["username"] = *username
		}
		if display != nil && *display != "" {
			m["display_name"] = *display
		}
		if deviceID != nil {
			m["device_id"] = *deviceID
		}
		if lastDevice != nil {
			m["last_device"] = *lastDevice
		}
		if cur != nil {
			m["current_track_id"] = cur.String()
		}
		if title != "" {
			m["current_title"] = title
		}
		if acq != "" {
			m["current_acquisition"] = acq
		}
		if acqRef != "" {
			m["current_acquisition_ref"] = acqRef
		}
		if rendererID != nil {
			m["renderer_id"] = *rendererID
		}
		if instanceID != nil {
			m["playback_instance_id"] = instanceID.String()
		}
		if checkpoint != nil {
			m["checkpoint_at"] = checkpoint.UTC().Format(time.RFC3339Nano)
		}
		if heartbeat != nil {
			m["renderer_heartbeat_at"] = heartbeat.UTC().Format(time.RFC3339Nano)
		}
		if updated != nil {
			m["updated_at"] = updated.UTC().Format(time.RFC3339Nano)
		}
		if guildID != nil || connected != nil {
			bind := map[string]any{}
			if guildID != nil {
				bind["guild_id"] = *guildID
			}
			if channelID != nil {
				bind["voice_channel_id"] = *channelID
			}
			if connected != nil {
				bind["connected"] = *connected
			}
			if disconnect != nil {
				bind["last_disconnect_reason"] = *disconnect
			}
			if bindRev != nil {
				bind["binding_revision"] = *bindRev
			}
			m["discord"] = bind
		}
		out = append(out, m)
		ids = append(ids, id)
	}
	s.attachSessionQueues(ctx, out, ids)
	return out
}

func (s *Server) attachSessionQueues(ctx context.Context, sessions []map[string]any, ids []uuid.UUID) {
	if s.Play == nil || len(ids) == 0 {
		return
	}
	byID := map[string]int{}
	for i, sess := range sessions {
		byID[asString(sess["id"])] = i
	}
	for _, id := range ids {
		items, err := s.Play.Queue(ctx, id)
		if err != nil || items == nil {
			continue
		}
		if i, ok := byID[id.String()]; ok {
			sessions[i]["items"] = items
			sessions[i]["queue_len"] = len(items)
		}
	}
}

func (s *Server) inspectCounts(ctx context.Context) map[string]any {
	out := map[string]any{
		"playback_sessions": 0, "playing": 0, "discord_connected": 0,
		"failed_jobs": 0, "media_holds": 0, "acquisition_failed": 0,
		"discord_playback_errors": 0, "scan_file_errors": 0,
	}
	if s == nil || s.Pool == nil {
		return out
	}
	queries := []struct{ key, sql string }{
		{"playback_sessions", `SELECT count(*) FROM playback_sessions`},
		{"playing", `SELECT count(*) FROM playback_sessions WHERE status='playing'`},
		{"discord_connected", `SELECT count(*) FROM discord_voice_runtime WHERE connected`},
		{"failed_jobs", `SELECT count(*) FROM jobs WHERE status='failed'`},
		{"media_holds", `SELECT count(*) FROM media_holds WHERE lease_until > now()`},
		{"acquisition_failed", `SELECT count(*) FROM acquisition_intents WHERE status IN ('failed','cancelled','stale')`},
		{"discord_playback_errors", `SELECT count(*) FROM discord_playback_errors`},
		{"scan_file_errors", `SELECT count(*) FROM scan_file_errors`},
	}
	for _, q := range queries {
		var n int
		_ = s.Pool.QueryRow(ctx, q.sql).Scan(&n)
		out[q.key] = n
	}
	return out
}

func (s *Server) inspectDiscord(ctx context.Context, limit int) map[string]any {
	settings := map[string]any{}
	if s != nil && s.Pool != nil {
		var enabled bool
		var gateway, commands string
		var lastErr, appID *string
		_ = s.Pool.QueryRow(ctx, `
			SELECT enabled, last_gateway_status, command_registration_status, last_error_redacted, application_id
			FROM discord_settings WHERE id=1`).Scan(&enabled, &gateway, &commands, &lastErr, &appID)
		settings["enabled"] = enabled
		settings["gateway"] = gateway
		settings["commands"] = commands
		if appID != nil {
			settings["application_id"] = *appID
		}
		if lastErr != nil && *lastErr != "" {
			settings["last_error"] = inspectRedact(*lastErr)
		}
	}
	return map[string]any{
		"settings":        settings,
		"runtime":         s.listDiscordRuntime(ctx),
		"voice":           s.listDiscordVoice(ctx, limit),
		"playback_errors": s.listDiscordPlaybackErrors(ctx, limit),
	}
}

func (s *Server) listDiscordRuntime(ctx context.Context) []map[string]any {
	if s == nil || s.Pool == nil {
		return emptyList()
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT r.guild_id, g.name, r.voice_channel_id, r.session_id, r.connected, r.last_disconnect_reason, r.binding_revision,
			s.status, s.current_track_id, s.position_ms, s.renderer_kind, s.output_pref
		FROM discord_voice_runtime r
		LEFT JOIN discord_guilds g ON g.id=r.guild_id
		LEFT JOIN playback_sessions s ON s.id=r.session_id`)
	if err != nil {
		return emptyList()
	}
	defer rows.Close()
	out := scanMaps(rows, "guild_id", "guild_name", "voice_channel_id", "session_id", "connected", "last_disconnect_reason", "binding_revision", "status", "current_track_id", "position_ms", "renderer_kind", "output_pref")
	for _, m := range out {
		m["reason"] = m["last_disconnect_reason"]
	}
	return out
}

func (s *Server) listDiscordVoice(ctx context.Context, limit int) []map[string]any {
	if s == nil || s.Pool == nil {
		return emptyList()
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT v.discord_user_id, v.guild_id, v.channel_id, v.display_name, v.updated_at,
			u.id, u.username, u.display_name
		FROM discord_user_voice v
		LEFT JOIN user_identities i ON i.provider='discord' AND i.provider_user_id=v.discord_user_id
		LEFT JOIN users u ON u.id=i.user_id
		ORDER BY v.updated_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return emptyList()
	}
	defer rows.Close()
	return scanMaps(rows, "discord_user_id", "guild_id", "channel_id", "display_name", "updated_at", "user_id", "username", "user_display_name")
}

func (s *Server) listDiscordVoiceForUser(ctx context.Context, discordID string) []map[string]any {
	if s == nil || s.Pool == nil || discordID == "" {
		return emptyList()
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT discord_user_id, guild_id, channel_id, display_name, updated_at
		FROM discord_user_voice WHERE discord_user_id=$1 ORDER BY updated_at DESC`, discordID)
	if err != nil {
		return emptyList()
	}
	defer rows.Close()
	return scanMaps(rows, "discord_user_id", "guild_id", "channel_id", "display_name", "updated_at")
}

func (s *Server) listDiscordPlaybackErrors(ctx context.Context, limit int) []map[string]any {
	if s == nil || s.Pool == nil {
		return emptyList()
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT e.id, e.guild_id, e.track_id, e.error_class, e.message, e.created_at, t.title
		FROM discord_playback_errors e
		LEFT JOIN tracks t ON t.id=e.track_id
		ORDER BY e.created_at DESC LIMIT $1`, limit)
	if err != nil {
		return emptyList()
	}
	defer rows.Close()
	out := emptyList()
	for _, m := range scanMaps(rows, "id", "guild_id", "track_id", "error_class", "message", "created_at", "title") {
		if msg, ok := m["message"].(string); ok {
			m["message"] = inspectRedact(msg)
		}
		out = append(out, m)
	}
	return out
}

type acquisitionInspectFilter struct {
	Status     string
	UserID     uuid.UUID
	SessionID  uuid.UUID
	FailedOnly bool
	Limit      int
}

func (s *Server) listAcquisitionIntents(ctx context.Context, f acquisitionInspectFilter) []map[string]any {
	if s == nil || s.Pool == nil {
		return emptyList()
	}
	if f.Limit <= 0 {
		f.Limit = 100
	}
	var userArg, sessArg any
	if f.UserID != uuid.Nil {
		userArg = f.UserID
	}
	if f.SessionID != uuid.Nil {
		sessArg = f.SessionID
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT a.id, a.job_id, a.user_id, u.username, a.session_id, a.track_id, a.intent, a.source_ref, a.provider,
			a.dest_library_id, a.status, a.error, a.created_at, a.updated_at, j.last_error, j.status
		FROM acquisition_intents a
		LEFT JOIN users u ON u.id=a.user_id
		LEFT JOIN jobs j ON j.id=a.job_id
		WHERE ($1='' OR a.status=$1)
		  AND (NOT $2 OR a.status IN ('failed','cancelled','stale'))
		  AND ($3::uuid IS NULL OR a.user_id=$3)
		  AND ($4::uuid IS NULL OR a.session_id=$4)
		ORDER BY a.updated_at DESC
		LIMIT $5`, f.Status, f.FailedOnly, userArg, sessArg, f.Limit)
	if err != nil {
		return emptyList()
	}
	defer rows.Close()
	out := emptyList()
	for _, m := range scanMaps(rows, "id", "job_id", "user_id", "username", "session_id", "track_id", "intent", "source_ref", "provider", "dest_library_id", "status", "error", "created_at", "updated_at", "job_last_error", "job_status") {
		if msg, ok := m["error"].(string); ok {
			m["error"] = inspectRedact(msg)
		}
		if msg, ok := m["job_last_error"].(string); ok {
			m["job_last_error"] = inspectRedact(msg)
		}
		out = append(out, m)
	}
	return out
}

func (s *Server) listMediaHolds(ctx context.Context, limit int) []map[string]any {
	if s == nil || s.Pool == nil {
		return emptyList()
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT h.id, h.track_id, t.title, h.kind, h.holder_id, h.instance_id, h.lease_until, h.created_at, h.heartbeat_at,
			(h.lease_until > now()) AS active
		FROM media_holds h
		LEFT JOIN tracks t ON t.id=h.track_id
		ORDER BY h.heartbeat_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return emptyList()
	}
	defer rows.Close()
	return scanMaps(rows, "id", "track_id", "title", "kind", "holder_id", "instance_id", "lease_until", "created_at", "heartbeat_at", "active")
}

func (s *Server) listSessionHolds(ctx context.Context, sid uuid.UUID) []map[string]any {
	if s == nil || s.Pool == nil {
		return emptyList()
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT h.id, h.track_id, h.kind, h.holder_id, h.instance_id, h.lease_until, h.heartbeat_at, (h.lease_until > now()) AS active
		FROM media_holds h
		JOIN playback_sessions s ON s.current_track_id=h.track_id
		WHERE s.id=$1
		ORDER BY h.heartbeat_at DESC`, sid)
	if err != nil {
		return emptyList()
	}
	defer rows.Close()
	return scanMaps(rows, "id", "track_id", "kind", "holder_id", "instance_id", "lease_until", "heartbeat_at", "active")
}

func (s *Server) listSessionReceipts(ctx context.Context, sid uuid.UUID, limit int) []map[string]any {
	if s == nil || s.Pool == nil {
		return emptyList()
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT command_id, action, result_status, result_code, resulting_revision, created_at
		FROM playback_command_receipts
		WHERE session_id=$1 ORDER BY created_at DESC LIMIT $2`, sid, limit)
	if err != nil {
		return emptyList()
	}
	defer rows.Close()
	return scanMaps(rows, "command_id", "action", "result_status", "result_code", "resulting_revision", "created_at")
}

func (s *Server) listSessionParty(ctx context.Context, sid uuid.UUID) map[string]any {
	out := map[string]any{"members": emptyList(), "votes": emptyList()}
	if s == nil || s.Pool == nil {
		return out
	}
	mem, err := s.Pool.Query(ctx, `
		SELECT user_id, role, joined_at FROM party_members WHERE session_id=$1 ORDER BY joined_at`, sid)
	if err == nil {
		defer mem.Close()
		out["members"] = scanMaps(mem, "user_id", "role", "joined_at")
	}
	votes, err := s.Pool.Query(ctx, `
		SELECT user_id, track_id, created_at FROM party_votes WHERE session_id=$1 ORDER BY created_at DESC LIMIT 40`, sid)
	if err == nil {
		defer votes.Close()
		out["votes"] = scanMaps(votes, "user_id", "track_id", "created_at")
	}
	return out
}

func (s *Server) listFailedJobs(ctx context.Context, limit int) []map[string]any {
	if s == nil || s.Pool == nil {
		return emptyList()
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT id, type, pool, status, progress, attempts, last_error, created_at, started_at, finished_at
		FROM jobs WHERE status IN ('failed','cancelled') AND coalesce(last_error,'') <> ''
		ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return emptyList()
	}
	defer rows.Close()
	out := emptyList()
	for _, m := range scanMaps(rows, "id", "type", "pool", "status", "progress", "attempts", "last_error", "created_at", "started_at", "finished_at") {
		if msg, ok := m["last_error"].(string); ok {
			m["last_error"] = inspectRedact(msg)
		}
		out = append(out, m)
	}
	return out
}

func (s *Server) listScanFileErrors(ctx context.Context, limit int) []map[string]any {
	if s == nil || s.Pool == nil {
		return emptyList()
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT e.id, e.scan_run_id, e.storage_key, e.error, e.created_at, sr.library_id, sr.kind
		FROM scan_file_errors e
		LEFT JOIN scan_runs sr ON sr.id=e.scan_run_id
		ORDER BY e.created_at DESC LIMIT $1`, limit)
	if err != nil {
		return emptyList()
	}
	defer rows.Close()
	out := emptyList()
	for _, m := range scanMaps(rows, "id", "scan_run_id", "storage_key", "error", "created_at", "library_id", "kind") {
		if msg, ok := m["error"].(string); ok {
			m["error"] = inspectRedact(msg)
		}
		out = append(out, m)
	}
	return out
}

func (s *Server) listWebhookDeliveries(ctx context.Context, limit int) []map[string]any {
	if s == nil || s.Pool == nil {
		return emptyList()
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT d.id, d.endpoint_id, e.url, d.event, d.status, d.attempts, d.last_error, d.created_at
		FROM webhook_deliveries d
		LEFT JOIN webhook_endpoints e ON e.id=d.endpoint_id
		ORDER BY d.created_at DESC LIMIT $1`, limit)
	if err != nil {
		return emptyList()
	}
	defer rows.Close()
	out := emptyList()
	for _, m := range scanMaps(rows, "id", "endpoint_id", "url", "event", "status", "attempts", "last_error", "created_at") {
		if msg, ok := m["last_error"].(string); ok {
			m["last_error"] = inspectRedact(msg)
		}
		out = append(out, m)
	}
	return out
}

func (s *Server) inspectExternal(ctx context.Context, limit int) map[string]any {
	return map[string]any{
		"accounts":    s.listExternalAccountErrors(ctx, limit),
		"playlists":   s.listExternalPlaylistErrors(ctx, limit),
		"sync_runs":   s.listExternalSyncRuns(ctx, limit),
		"sync_errors": s.listExternalSyncErrors(ctx, limit),
	}
}

func (s *Server) listExternalAccountErrors(ctx context.Context, limit int) []map[string]any {
	if s == nil || s.Pool == nil {
		return emptyList()
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT a.id, a.user_id, u.username, a.provider, a.display_name, a.status, a.last_error, a.last_successful_sync_at, a.connected_at
		FROM external_provider_accounts a
		LEFT JOIN users u ON u.id=a.user_id
		WHERE coalesce(a.last_error,'') <> '' OR a.status IN ('error','expired','revoked')
		ORDER BY a.connected_at DESC LIMIT $1`, limit)
	if err != nil {
		return emptyList()
	}
	defer rows.Close()
	out := emptyList()
	for _, m := range scanMaps(rows, "id", "user_id", "username", "provider", "display_name", "status", "last_error", "last_successful_sync_at", "connected_at") {
		if msg, ok := m["last_error"].(string); ok {
			m["last_error"] = inspectRedact(msg)
		}
		out = append(out, m)
	}
	return out
}

func (s *Server) listExternalPlaylistErrors(ctx context.Context, limit int) []map[string]any {
	if s == nil || s.Pool == nil {
		return emptyList()
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT p.id, p.user_id, u.username, p.provider, p.name, p.last_sync_status, p.last_error, p.last_sync_at, p.next_sync_at
		FROM external_playlists p
		LEFT JOIN users u ON u.id=p.user_id
		WHERE coalesce(p.last_error,'') <> '' OR p.last_sync_status IN ('error','failed')
		ORDER BY coalesce(p.last_sync_attempt_at, p.last_sync_at) DESC NULLS LAST LIMIT $1`, limit)
	if err != nil {
		return emptyList()
	}
	defer rows.Close()
	out := emptyList()
	for _, m := range scanMaps(rows, "id", "user_id", "username", "provider", "name", "last_sync_status", "last_error", "last_sync_at", "next_sync_at") {
		if msg, ok := m["last_error"].(string); ok {
			m["last_error"] = inspectRedact(msg)
		}
		out = append(out, m)
	}
	return out
}

func (s *Server) listExternalSyncRuns(ctx context.Context, limit int) []map[string]any {
	if s == nil || s.Pool == nil {
		return emptyList()
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT id, external_playlist_id, job_id, started_at, finished_at, unmatched_count, error
		FROM external_sync_runs
		WHERE coalesce(error,'') <> ''
		ORDER BY started_at DESC LIMIT $1`, limit)
	if err != nil {
		return emptyList()
	}
	defer rows.Close()
	out := emptyList()
	for _, m := range scanMaps(rows, "id", "external_playlist_id", "job_id", "started_at", "finished_at", "unmatched_count", "error") {
		if msg, ok := m["error"].(string); ok {
			m["error"] = inspectRedact(msg)
		}
		out = append(out, m)
	}
	return out
}

func (s *Server) listExternalSyncErrors(ctx context.Context, limit int) []map[string]any {
	if s == nil || s.Pool == nil {
		return emptyList()
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT id, run_id, item_id, error_class, message, created_at
		FROM external_sync_errors ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return emptyList()
	}
	defer rows.Close()
	out := emptyList()
	for _, m := range scanMaps(rows, "id", "run_id", "item_id", "error_class", "message", "created_at") {
		if msg, ok := m["message"].(string); ok {
			m["message"] = inspectRedact(msg)
		}
		out = append(out, m)
	}
	return out
}

type inspectErrorFilter struct {
	Source        string
	Q             string
	UserID        uuid.UUID
	DiscordUserID string
	Limit         int
}

func (s *Server) listInspectErrors(ctx context.Context, f inspectErrorFilter) []map[string]any {
	if f.Limit <= 0 {
		f.Limit = 100
	}
	want := strings.ToLower(strings.TrimSpace(f.Source))
	q := strings.ToLower(f.Q)
	var items []map[string]any
	add := func(src string, rows []map[string]any) {
		if want != "" && want != src {
			return
		}
		for _, row := range rows {
			row["source"] = src
			if q != "" && !inspectErrorMatch(row, q) {
				continue
			}
			items = append(items, row)
		}
	}
	if s != nil && s.Pool != nil {
		add("oplog", s.collectOplogErrors(ctx, f.Limit))
		add("job", s.collectJobErrors(ctx, f.Limit))
		add("job_attempt", s.collectJobAttemptErrors(ctx, f.Limit))
		add("discord_playback", s.collectDiscordPlaybackAsErrors(ctx, f.Limit, f.DiscordUserID))
		add("discord_gateway", s.collectDiscordGatewayError(ctx))
		add("acquisition", s.collectAcquisitionErrors(ctx, f.Limit, f.UserID))
		add("scan_file", s.collectScanErrors(ctx, f.Limit))
		add("webhook", s.collectWebhookErrors(ctx, f.Limit))
		add("external_account", s.collectExternalAccountAsErrors(ctx, f.Limit, f.UserID))
		add("external_playlist", s.collectExternalPlaylistAsErrors(ctx, f.Limit, f.UserID))
		add("external_sync", s.collectExternalSyncAsErrors(ctx, f.Limit))
	}
	sort.Slice(items, func(i, j int) bool {
		return asTime(items[i]["at"]).After(asTime(items[j]["at"]))
	})
	if len(items) > f.Limit {
		items = items[:f.Limit]
	}
	if items == nil {
		items = emptyList()
	}
	return items
}

func inspectErrorMatch(row map[string]any, q string) bool {
	for _, k := range []string{"message", "error", "source", "class", "type", "category"} {
		if strings.Contains(strings.ToLower(asString(row[k])), q) {
			return true
		}
	}
	return false
}

func inspectErr(at time.Time, msg, class string, extra map[string]any) map[string]any {
	m := map[string]any{
		"at":      at.UTC().Format(time.RFC3339Nano),
		"level":   "error",
		"message": inspectRedact(msg),
	}
	if class != "" {
		m["class"] = class
	}
	for k, v := range extra {
		if v != nil {
			m[k] = v
		}
	}
	return m
}

func (s *Server) collectOplogErrors(ctx context.Context, limit int) []map[string]any {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, created_at, level, category, message, job_id, track_id, actor_id
		FROM operational_logs WHERE level IN ('warn','error')
		ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var at time.Time
		var level, cat, msg string
		var job, track, actor *uuid.UUID
		if err := rows.Scan(&id, &at, &level, &cat, &msg, &job, &track, &actor); err != nil {
			continue
		}
		m := inspectErr(at, msg, cat, map[string]any{"id": id.String(), "level": level, "category": cat})
		if job != nil {
			m["job_id"] = job.String()
		}
		if track != nil {
			m["track_id"] = track.String()
		}
		if actor != nil {
			m["actor_id"] = actor.String()
		}
		out = append(out, m)
	}
	return out
}

func (s *Server) collectJobErrors(ctx context.Context, limit int) []map[string]any {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, type, status, last_error, created_at, finished_at
		FROM jobs WHERE coalesce(last_error,'') <> ''
		ORDER BY coalesce(finished_at, updated_at, created_at) DESC LIMIT $1`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var typ, status, msg string
		var created time.Time
		var finished *time.Time
		if err := rows.Scan(&id, &typ, &status, &msg, &created, &finished); err != nil {
			continue
		}
		at := created
		if finished != nil {
			at = *finished
		}
		out = append(out, inspectErr(at, msg, typ, map[string]any{"id": id.String(), "job_id": id.String(), "type": typ, "status": status}))
	}
	return out
}

func (s *Server) collectJobAttemptErrors(ctx context.Context, limit int) []map[string]any {
	rows, err := s.Pool.Query(ctx, `
		SELECT a.id, a.job_id, a.attempt, a.error, coalesce(a.finished_at, a.started_at), j.type
		FROM job_attempts a LEFT JOIN jobs j ON j.id=a.job_id
		WHERE coalesce(a.error,'') <> ''
		ORDER BY coalesce(a.finished_at, a.started_at) DESC LIMIT $1`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, job uuid.UUID
		var attempt int
		var msg string
		var at time.Time
		var typ *string
		if err := rows.Scan(&id, &job, &attempt, &msg, &at, &typ); err != nil {
			continue
		}
		extra := map[string]any{"id": id.String(), "job_id": job.String(), "attempt": attempt}
		if typ != nil {
			extra["type"] = *typ
		}
		out = append(out, inspectErr(at, msg, "job_attempt", extra))
	}
	return out
}

func (s *Server) collectDiscordPlaybackAsErrors(ctx context.Context, limit int, discordUserID string) []map[string]any {
	_ = discordUserID
	rows, err := s.Pool.Query(ctx, `
		SELECT id, guild_id, track_id, error_class, message, created_at
		FROM discord_playback_errors ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var guild, class, msg string
		var track *uuid.UUID
		var at time.Time
		if err := rows.Scan(&id, &guild, &track, &class, &msg, &at); err != nil {
			continue
		}
		extra := map[string]any{"id": id.String(), "guild_id": guild}
		if track != nil {
			extra["track_id"] = track.String()
		}
		out = append(out, inspectErr(at, msg, class, extra))
	}
	return out
}

func (s *Server) collectDiscordGatewayError(ctx context.Context) []map[string]any {
	var msg *string
	var updated time.Time
	if err := s.Pool.QueryRow(ctx, `SELECT last_error_redacted, updated_at FROM discord_settings WHERE id=1`).Scan(&msg, &updated); err != nil || msg == nil || strings.TrimSpace(*msg) == "" {
		return nil
	}
	return []map[string]any{inspectErr(updated, *msg, "gateway", nil)}
}

func (s *Server) collectAcquisitionErrors(ctx context.Context, limit int, userID uuid.UUID) []map[string]any {
	var userArg any
	if userID != uuid.Nil {
		userArg = userID
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT id, user_id, session_id, track_id, status, error, updated_at, source_ref
		FROM acquisition_intents
		WHERE coalesce(error,'') <> '' AND ($1::uuid IS NULL OR user_id=$1)
		ORDER BY updated_at DESC LIMIT $2`, userArg, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var uid uuid.UUID
		var sid, tid *uuid.UUID
		var status, msg, ref string
		var at time.Time
		if err := rows.Scan(&id, &uid, &sid, &tid, &status, &msg, &at, &ref); err != nil {
			continue
		}
		extra := map[string]any{"id": id.String(), "user_id": uid.String(), "status": status, "source_ref": ref}
		if sid != nil {
			extra["session_id"] = sid.String()
		}
		if tid != nil {
			extra["track_id"] = tid.String()
		}
		out = append(out, inspectErr(at, msg, "acquisition", extra))
	}
	return out
}

func (s *Server) collectScanErrors(ctx context.Context, limit int) []map[string]any {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, scan_run_id, storage_key, error, created_at FROM scan_file_errors
		ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, run uuid.UUID
		var key, msg string
		var at time.Time
		if err := rows.Scan(&id, &run, &key, &msg, &at); err != nil {
			continue
		}
		out = append(out, inspectErr(at, msg, "scan_file", map[string]any{"id": id.String(), "scan_run_id": run.String(), "storage_key": key}))
	}
	return out
}

func (s *Server) collectWebhookErrors(ctx context.Context, limit int) []map[string]any {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, endpoint_id, event, last_error, created_at FROM webhook_deliveries
		WHERE coalesce(last_error,'') <> '' ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, ep uuid.UUID
		var event, msg string
		var at time.Time
		if err := rows.Scan(&id, &ep, &event, &msg, &at); err != nil {
			continue
		}
		out = append(out, inspectErr(at, msg, event, map[string]any{"id": id.String(), "endpoint_id": ep.String()}))
	}
	return out
}

func (s *Server) collectExternalAccountAsErrors(ctx context.Context, limit int, userID uuid.UUID) []map[string]any {
	var userArg any
	if userID != uuid.Nil {
		userArg = userID
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT id, user_id, provider, last_error, connected_at FROM external_provider_accounts
		WHERE coalesce(last_error,'') <> '' AND ($1::uuid IS NULL OR user_id=$1)
		ORDER BY connected_at DESC LIMIT $2`, userArg, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var uid *uuid.UUID
		var provider, msg string
		var at time.Time
		if err := rows.Scan(&id, &uid, &provider, &msg, &at); err != nil {
			continue
		}
		extra := map[string]any{"id": id.String(), "provider": provider}
		if uid != nil {
			extra["user_id"] = uid.String()
		}
		out = append(out, inspectErr(at, msg, provider, extra))
	}
	return out
}

func (s *Server) collectExternalPlaylistAsErrors(ctx context.Context, limit int, userID uuid.UUID) []map[string]any {
	var userArg any
	if userID != uuid.Nil {
		userArg = userID
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT id, user_id, provider, name, last_error, coalesce(last_sync_attempt_at, last_sync_at, now())
		FROM external_playlists
		WHERE coalesce(last_error,'') <> '' AND ($1::uuid IS NULL OR user_id=$1)
		ORDER BY coalesce(last_sync_attempt_at, last_sync_at) DESC NULLS LAST LIMIT $2`, userArg, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, uid uuid.UUID
		var provider, name, msg string
		var at time.Time
		if err := rows.Scan(&id, &uid, &provider, &name, &msg, &at); err != nil {
			continue
		}
		out = append(out, inspectErr(at, msg, provider, map[string]any{"id": id.String(), "user_id": uid.String(), "name": name}))
	}
	return out
}

func (s *Server) collectExternalSyncAsErrors(ctx context.Context, limit int) []map[string]any {
	var out []map[string]any
	rows, err := s.Pool.Query(ctx, `
		SELECT id, run_id, error_class, message, created_at FROM external_sync_errors
		ORDER BY created_at DESC LIMIT $1`, limit)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			var run *uuid.UUID
			var class, msg string
			var at time.Time
			if err := rows.Scan(&id, &run, &class, &msg, &at); err != nil {
				continue
			}
			extra := map[string]any{"id": id.String()}
			if run != nil {
				extra["run_id"] = run.String()
			}
			out = append(out, inspectErr(at, msg, class, extra))
		}
	}
	runs, err := s.Pool.Query(ctx, `
		SELECT id, external_playlist_id, error, started_at FROM external_sync_runs
		WHERE coalesce(error,'') <> '' ORDER BY started_at DESC LIMIT $1`, limit)
	if err != nil {
		return out
	}
	defer runs.Close()
	for runs.Next() {
		var id uuid.UUID
		var playlist *uuid.UUID
		var msg string
		var at time.Time
		if err := runs.Scan(&id, &playlist, &msg, &at); err != nil {
			continue
		}
		extra := map[string]any{"id": id.String()}
		if playlist != nil {
			extra["external_playlist_id"] = playlist.String()
		}
		out = append(out, inspectErr(at, msg, "sync_run", extra))
	}
	return out
}

func inspectRecent(items []map[string]any, maxAge time.Duration) []map[string]any {
	if len(items) == 0 {
		return emptyList()
	}
	cut := time.Now().Add(-maxAge)
	out := emptyList()
	for _, m := range items {
		t := asTime(m["at"])
		if t.IsZero() {
			t = asTime(m["created_at"])
		}
		if t.IsZero() || !t.Before(cut) {
			out = append(out, m)
		}
	}
	return out
}

func asTime(v any) time.Time {
	switch t := v.(type) {
	case time.Time:
		return t
	case string:
		if tm, err := time.Parse(time.RFC3339Nano, t); err == nil {
			return tm
		}
		if tm, err := time.Parse(time.RFC3339, t); err == nil {
			return tm
		}
	}
	return time.Time{}
}
