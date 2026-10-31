package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	discordx "github.com/sounddock/sounddock/internal/discord"
)

// Discord server management for admins: owner and bot details, playback and
// library scope per server, leave and ban, and each server's activity.

var serverPermissionNames = []struct {
	bit   int64
	key   string
	label string
}{
	{discordgo.PermissionAdministrator, "administrator", "Administrator"},
	{discordgo.PermissionViewChannel, "view_channels", "View channels"},
	{discordgo.PermissionSendMessages, "send_messages", "Send messages"},
	{discordgo.PermissionEmbedLinks, "embed_links", "Embed links"},
	{discordgo.PermissionVoiceConnect, "connect", "Connect to voice"},
	{discordgo.PermissionVoiceSpeak, "speak", "Speak in voice"},
	{discordgo.PermissionManageGuild, "manage_server", "Manage server"},
	{discordgo.PermissionManageChannels, "manage_channels", "Manage channels"},
	{discordgo.PermissionManageRoles, "manage_roles", "Manage roles"},
}

func describePermissions(bits int64) []map[string]any {
	// Administrator grants everything, whatever else the bits say.
	if bits&discordgo.PermissionAdministrator != 0 {
		bits = discordgo.PermissionAll
	}
	out := make([]map[string]any, 0, len(serverPermissionNames))
	for _, p := range serverPermissionNames {
		out = append(out, map[string]any{"key": p.key, "label": p.label, "granted": bits&p.bit == p.bit})
	}
	return out
}

const discordServerSelect = `
	SELECT g.id, g.name, g.icon, g.owner_id, g.owner_name, g.member_count, g.bot_permissions,
		g.info_refreshed_at, g.joined_at, g.left_at, g.banned_at, g.ban_reason,
		COALESCE(NULLIF(bu.display_name,''), bu.username, ''),
		g.enabled, g.queue_mode, g.library_mode, g.queue_limit, g.explicit_policy,
		COALESCE(r.connected, false), COALESCE(r.voice_channel_id, ''),
		COALESCE((SELECT array_agg(l.library_id::text ORDER BY l.library_id) FROM discord_guild_libraries l WHERE l.guild_id=g.id), '{}')
	FROM discord_guilds g
	LEFT JOIN discord_voice_runtime r ON r.guild_id=g.id
	LEFT JOIN users bu ON bu.id=g.banned_by`

type serverRow interface {
	Scan(dest ...any) error
}

func scanDiscordServer(row serverRow) (map[string]any, error) {
	var id, name, icon, ownerID, ownerName, banReason, bannedBy, queueMode, libMode, explicit, voiceCh string
	var members, queueLimit int
	var perms int64
	var refreshed, joined, left, banned *time.Time
	var enabled, inVoice bool
	var libs []string
	if err := row.Scan(&id, &name, &icon, &ownerID, &ownerName, &members, &perms, &refreshed, &joined, &left, &banned, &banReason,
		&bannedBy, &enabled, &queueMode, &libMode, &queueLimit, &explicit, &inVoice, &voiceCh, &libs); err != nil {
		return nil, err
	}
	status := "connected"
	switch {
	case banned != nil:
		status = "banned"
	case left != nil:
		status = "left"
	}
	iconURL := ""
	if icon != "" {
		iconURL = "https://cdn.discordapp.com/icons/" + id + "/" + icon + ".png?size=128"
	}
	return map[string]any{
		"id": id, "name": name, "icon_url": iconURL, "owner_id": ownerID, "owner_name": ownerName,
		"member_count": members, "bot_permissions": strconv.FormatInt(perms, 10),
		"bot_admin": perms&discordgo.PermissionAdministrator != 0, "permissions": describePermissions(perms),
		"info_refreshed_at": refreshed, "joined_at": joined, "left_at": left,
		"banned_at": banned, "ban_reason": banReason, "banned_by": bannedBy, "status": status,
		"enabled": enabled, "queue_mode": queueMode, "library_mode": libMode, "library_ids": libs,
		"queue_limit": queueLimit, "explicit_policy": explicit, "in_voice": inVoice && left == nil, "voice_channel_id": voiceCh,
	}, nil
}

func (s *Server) adminDiscordServers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), discordServerSelect+` ORDER BY g.banned_at IS NOT NULL, g.left_at IS NOT NULL, lower(g.name)`)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		m, err := scanDiscordServer(rows)
		if err != nil {
			writeErr(w, 500, "db", err.Error())
			return
		}
		out = append(out, m)
	}
	writeJSON(w, 200, map[string]any{"servers": out, "bot_online": discordx.Live().Online()})
}

func (s *Server) loadDiscordServer(ctx context.Context, id string) (map[string]any, error) {
	return scanDiscordServer(s.Pool.QueryRow(ctx, discordServerSelect+` WHERE g.id=$1`, id))
}

func (s *Server) adminDiscordServer(w http.ResponseWriter, r *http.Request) {
	m, err := s.loadDiscordServer(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 404, "not_found", "Discord server not found")
		return
	}
	m["bot_online"] = discordx.Live().Online()
	writeJSON(w, 200, m)
}

func (s *Server) auditServer(r *http.Request, action, guildID string, meta map[string]any) {
	if meta == nil {
		meta = map[string]any{}
	}
	meta["guild_id"] = guildID
	s.Audit.Event(r.Context(), &currentUser(r).ID, action, "discord_guild:"+guildID, r.RemoteAddr, meta)
}

func (s *Server) adminPatchDiscordServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Enabled        *bool     `json:"enabled"`
		QueueMode      *string   `json:"queue_mode"`
		LibraryMode    *string   `json:"library_mode"`
		LibraryIDs     *[]string `json:"library_ids"`
		QueueLimit     *int      `json:"queue_limit"`
		ExplicitPolicy *string   `json:"explicit_policy"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	if _, err := s.loadDiscordServer(r.Context(), id); err != nil {
		writeErr(w, 404, "not_found", "Discord server not found")
		return
	}
	changed := map[string]any{}
	tx, err := s.Pool.Begin(r.Context())
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer tx.Rollback(r.Context())
	set := func(col string, v any) error {
		_, err := tx.Exec(r.Context(), `UPDATE discord_guilds SET `+col+`=$2 WHERE id=$1`, id, v)
		changed[col] = v
		return err
	}
	var errs []error
	if body.Enabled != nil {
		errs = append(errs, set("enabled", *body.Enabled))
	}
	if body.QueueMode != nil {
		if *body.QueueMode != "shared" && *body.QueueMode != "personal" {
			writeErr(w, 400, "invalid", "queue_mode must be shared or personal")
			return
		}
		errs = append(errs, set("queue_mode", *body.QueueMode))
	}
	if body.LibraryMode != nil {
		if *body.LibraryMode != "all" && *body.LibraryMode != "selected" {
			writeErr(w, 400, "invalid", "library_mode must be all or selected")
			return
		}
		errs = append(errs, set("library_mode", *body.LibraryMode))
	}
	if body.QueueLimit != nil {
		if *body.QueueLimit < 1 || *body.QueueLimit > 10000 {
			writeErr(w, 400, "invalid", "queue_limit must be between 1 and 10000")
			return
		}
		errs = append(errs, set("queue_limit", *body.QueueLimit))
	}
	if body.ExplicitPolicy != nil {
		if *body.ExplicitPolicy != "allow" && *body.ExplicitPolicy != "deny" {
			writeErr(w, 400, "invalid", "explicit_policy must be allow or deny")
			return
		}
		errs = append(errs, set("explicit_policy", *body.ExplicitPolicy))
	}
	if body.LibraryIDs != nil {
		ids := make([]uuid.UUID, 0, len(*body.LibraryIDs))
		for _, raw := range *body.LibraryIDs {
			lid, err := uuid.Parse(raw)
			if err != nil {
				writeErr(w, 400, "invalid", "bad library id")
				return
			}
			ids = append(ids, lid)
		}
		_, err := tx.Exec(r.Context(), `DELETE FROM discord_guild_libraries WHERE guild_id=$1`, id)
		errs = append(errs, err)
		if len(ids) > 0 {
			_, err = tx.Exec(r.Context(), `
				INSERT INTO discord_guild_libraries (guild_id, library_id)
				SELECT $1, l.id FROM libraries l WHERE l.id = ANY($2)`, id, ids)
			errs = append(errs, err)
		}
		changed["library_ids"] = *body.LibraryIDs
	}
	for _, err := range errs {
		if err != nil {
			writeErr(w, 500, "db", err.Error())
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if len(changed) > 0 {
		s.auditServer(r, "discord.server.update", id, changed)
	}
	s.adminDiscordServer(w, r)
}

func (s *Server) adminRefreshDiscordServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	bot := discordx.Live()
	if !bot.Online() {
		writeErr(w, 409, "bot_offline", discordx.ErrBotOffline.Error())
		return
	}
	if _, err := bot.RefreshServer(r.Context(), id); err != nil {
		writeErr(w, 502, "discord", "Could not read the server from Discord: "+err.Error())
		return
	}
	s.adminDiscordServer(w, r)
}

func (s *Server) adminDisconnectDiscordServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if bot := discordx.Live(); bot != nil {
		_ = bot.LeaveGuild(r.Context(), id)
	}
	discordx.MarkVoiceDisconnected(r.Context(), s.Pool, id, "admin")
	s.auditServer(r, "discord.server.disconnect", id, nil)
	s.adminDiscordServer(w, r)
}

func (s *Server) adminLeaveDiscordServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	bot := discordx.Live()
	if !bot.Online() {
		writeErr(w, 409, "bot_offline", discordx.ErrBotOffline.Error())
		return
	}
	if err := bot.LeaveServer(r.Context(), id); err != nil {
		writeErr(w, 502, "discord", "Could not leave the server: "+err.Error())
		return
	}
	s.auditServer(r, "discord.server.leave", id, nil)
	s.adminDiscordServer(w, r)
}

func (s *Server) adminBanDiscordServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Reason string `json:"reason"`
	}
	_ = decodeJSON(r, &body)
	reason := strings.TrimSpace(body.Reason)
	if len(reason) > 500 {
		reason = reason[:500]
	}
	tag, err := s.Pool.Exec(r.Context(), `UPDATE discord_guilds SET banned_at=now(), banned_by=$2, ban_reason=$3 WHERE id=$1`, id, currentUser(r).ID, reason)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, 404, "not_found", "Discord server not found")
		return
	}
	meta := map[string]any{"reason": reason}
	// The ban stands even when the bot is offline; it leaves the next time it
	// sees the server.
	if bot := discordx.Live(); bot.Online() {
		if err := bot.LeaveServer(r.Context(), id); err != nil {
			meta["leave_error"] = err.Error()
		}
	}
	s.auditServer(r, "discord.server.ban", id, meta)
	s.adminDiscordServer(w, r)
}

func (s *Server) adminUnbanDiscordServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := s.Pool.Exec(r.Context(), `UPDATE discord_guilds SET banned_at=NULL, banned_by=NULL, ban_reason='' WHERE id=$1`, id); err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	s.auditServer(r, "discord.server.unban", id, nil)
	s.adminDiscordServer(w, r)
}

var serverAuditMessages = map[string]string{
	"discord.server.update":     "Changed server settings",
	"discord.server.disconnect": "Disconnected the bot from voice",
	"discord.server.leave":      "Made the bot leave the server",
	"discord.server.ban":        "Banned the server",
	"discord.server.unban":      "Unbanned the server",
}

// adminDiscordServerActivity merges admin actions (audit), bot events and
// slash commands (operational log) and playback errors for one server.
func (s *Server) adminDiscordServerActivity(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	before := time.Now().Add(time.Minute)
	if t := parseTimeParam(r.URL.Query().Get("before")); !t.IsZero() {
		before = t
	}
	ctx := r.Context()
	var out []map[string]any
	rows, err := s.Pool.Query(ctx, `
		SELECT e.id::text, e.created_at, e.action, e.meta, COALESCE(NULLIF(u.display_name,''), u.username, '')
		FROM audit_events e LEFT JOIN users u ON u.id=e.actor_user_id
		WHERE e.meta ? 'guild_id' AND e.meta->>'guild_id'=$1 AND e.created_at < $2
		ORDER BY e.created_at DESC LIMIT $3`, id, before, limit)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	for rows.Next() {
		var eid, action, actor string
		var at time.Time
		var meta []byte
		if rows.Scan(&eid, &at, &action, &meta, &actor) != nil {
			continue
		}
		var details map[string]any
		_ = json.Unmarshal(meta, &details)
		delete(details, "guild_id")
		msg := serverAuditMessages[action]
		if msg == "" {
			msg = action
		}
		out = append(out, map[string]any{"id": "a-" + eid, "at": at, "kind": "admin", "level": "info", "action": action,
			"message": msg, "actor": actor, "details": details})
	}
	rows.Close()
	rows, err = s.Pool.Query(ctx, `
		SELECT id::text, created_at, level, action, message, actor_name, details, COALESCE(details->>'error','')
		FROM operational_logs
		WHERE details ? 'guild_id' AND details->>'guild_id'=$1 AND created_at < $2
		ORDER BY created_at DESC LIMIT $3`, id, before, limit)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	for rows.Next() {
		var eid, level, action, msg, actor, errText string
		var at time.Time
		var raw []byte
		if rows.Scan(&eid, &at, &level, &action, &msg, &actor, &raw, &errText) != nil {
			continue
		}
		var details map[string]any
		_ = json.Unmarshal(raw, &details)
		delete(details, "guild_id")
		delete(details, "error")
		kind := "bot"
		if strings.HasPrefix(action, "discord.command.") {
			kind = "command"
		}
		out = append(out, map[string]any{"id": "o-" + eid, "at": at, "kind": kind, "level": level, "action": action,
			"message": msg, "actor": strings.TrimPrefix(actor, "discord:"), "details": details, "error": errText})
	}
	rows.Close()
	rows, err = s.Pool.Query(ctx, `
		SELECT e.id::text, e.created_at, e.error_class, e.message, COALESCE(t.title,'')
		FROM discord_playback_errors e LEFT JOIN tracks t ON t.id=e.track_id
		WHERE e.guild_id=$1 AND e.created_at < $2
		ORDER BY e.created_at DESC LIMIT $3`, id, before, limit)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	for rows.Next() {
		var eid, class, msg, title string
		var at time.Time
		if rows.Scan(&eid, &at, &class, &msg, &title) != nil {
			continue
		}
		text := "Playback error"
		if title != "" {
			text += ": " + title
		}
		out = append(out, map[string]any{"id": "e-" + eid, "at": at, "kind": "error", "level": "error", "action": "discord.playback." + class,
			"message": text, "error": msg})
	}
	rows.Close()
	sort.SliceStable(out, func(i, j int) bool { return out[i]["at"].(time.Time).After(out[j]["at"].(time.Time)) })
	next := ""
	if len(out) > limit {
		out = out[:limit]
	}
	if len(out) == limit {
		next = out[len(out)-1]["at"].(time.Time).Format(time.RFC3339Nano)
	}
	if out == nil {
		out = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"items": out, "next_before": next})
}

// serverTrackFilter drops library songs a restricted server may not play
// when sid is that server's bound queue. YouTube refs are kept: they are
// fetched for this request rather than browsed from a library.
func (s *Server) serverTrackFilter(ctx context.Context, sid uuid.UUID, refs []string, hints []queueTrackHint) ([]string, []queueTrackHint, int) {
	if len(refs) == 0 || sid == uuid.Nil {
		return refs, hints, 0
	}
	var guildID string
	if err := s.Pool.QueryRow(ctx, `SELECT guild_id FROM discord_voice_runtime WHERE session_id=$1 LIMIT 1`, sid).Scan(&guildID); err != nil {
		return refs, hints, 0
	}
	sc := discordx.LoadServerScope(ctx, s.Pool, guildID)
	if !sc.Restricted {
		return refs, hints, 0
	}
	var ids []uuid.UUID
	for _, ref := range refs {
		if id, err := uuid.Parse(strings.TrimSpace(ref)); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return refs, hints, 0
	}
	libOf := map[uuid.UUID]uuid.UUID{}
	rows, err := s.Pool.Query(ctx, `SELECT id, library_id FROM tracks WHERE id = ANY($1)`, ids)
	if err == nil {
		for rows.Next() {
			var id, lib uuid.UUID
			if rows.Scan(&id, &lib) == nil {
				libOf[id] = lib
			}
		}
		rows.Close()
	}
	dropped := map[string]bool{}
	kept := refs[:0:0]
	for _, ref := range refs {
		id, err := uuid.Parse(strings.TrimSpace(ref))
		if lib, ok := libOf[id]; err == nil && ok && !sc.Allows(lib) {
			dropped[ref] = true
			continue
		}
		kept = append(kept, ref)
	}
	if len(dropped) == 0 {
		return refs, hints, 0
	}
	var keptHints []queueTrackHint
	for _, h := range hints {
		if !dropped[h.ID] {
			keptHints = append(keptHints, h)
		}
	}
	return kept, keptHints, len(refs) - len(kept)
}

// writeServerSkipped answers when every song was outside the server's libraries.
func writeServerSkipped(w http.ResponseWriter) {
	writeErr(w, 403, "not_in_server", "These songs are not available in this Discord server")
}
