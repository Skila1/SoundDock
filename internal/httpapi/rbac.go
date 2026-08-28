package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (s *Server) adminPermissions(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `SELECT name, description FROM permissions ORDER BY name`)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "name", "description"))
}

func (s *Server) adminRoles(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `
		SELECT r.id::text, r.name, r.description, r.is_system,
			(SELECT count(*) FROM user_roles ur WHERE ur.role_id=r.id),
			COALESCE((SELECT array_agg(p.name ORDER BY p.name) FROM role_permissions rp JOIN permissions p ON p.id=rp.permission_id WHERE rp.role_id=r.id), ARRAY[]::text[])
		FROM roles r ORDER BY r.is_system DESC, r.name`)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	list := scanMaps(rows, "id", "name", "description", "is_system", "member_count", "permissions")
	links, _ := s.roleDiscordLinks(r.Context(), uuid.Nil)
	for _, row := range list {
		id, _ := row["id"].(string)
		row["discord_links"] = links[id]
		if row["discord_links"] == nil {
			row["discord_links"] = []any{}
		}
	}
	writeJSON(w, 200, list)
}

func (s *Server) roleDiscordLinks(ctx context.Context, roleID uuid.UUID) (map[string][]map[string]any, error) {
	q := `SELECT role_id::text, guild_id, discord_role_id FROM role_discord_links`
	var args []any
	if roleID != uuid.Nil {
		q += ` WHERE role_id=$1`
		args = append(args, roleID)
	}
	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return map[string][]map[string]any{}, err
	}
	defer rows.Close()
	out := map[string][]map[string]any{}
	for rows.Next() {
		var rid, gid, did string
		if err := rows.Scan(&rid, &gid, &did); err != nil {
			continue
		}
		out[rid] = append(out[rid], map[string]any{"guild_id": gid, "discord_role_id": did})
	}
	return out, nil
}

func (s *Server) adminCreateRole(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeErr(w, 400, "invalid", "name required")
		return
	}
	if strings.EqualFold(name, "Administrator") || strings.EqualFold(name, "User") {
		writeErr(w, 400, "invalid", "that name is reserved")
		return
	}
	var id uuid.UUID
	err := s.Pool.QueryRow(r.Context(), `INSERT INTO roles (name, description, is_system) VALUES ($1,$2,false) RETURNING id`, name, strings.TrimSpace(body.Description)).Scan(&id)
	if err != nil {
		writeErr(w, 400, "role", err.Error())
		return
	}
	s.replaceRolePermissions(r.Context(), id, body.Permissions)
	s.Audit.Event(r.Context(), &currentUser(r).ID, "role.create", name, r.RemoteAddr, nil)
	writeJSON(w, 201, map[string]any{"id": id})
}

func (s *Server) adminPatchRole(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid role id")
		return
	}
	var name string
	var system bool
	if err := s.Pool.QueryRow(r.Context(), `SELECT name, is_system FROM roles WHERE id=$1`, id).Scan(&name, &system); err != nil {
		writeErr(w, 404, "not_found", "group not found")
		return
	}
	var body struct {
		Name        *string  `json:"name"`
		Description *string  `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	if body.Name != nil {
		n := strings.TrimSpace(*body.Name)
		if n == "" {
			writeErr(w, 400, "invalid", "name required")
			return
		}
		if system && n != name {
			writeErr(w, 400, "system", "built-in groups cannot be renamed")
			return
		}
		_, _ = s.Pool.Exec(r.Context(), `UPDATE roles SET name=$2 WHERE id=$1`, id, n)
	}
	if body.Description != nil {
		_, _ = s.Pool.Exec(r.Context(), `UPDATE roles SET description=$2 WHERE id=$1`, id, strings.TrimSpace(*body.Description))
	}
	if body.Permissions != nil {
		if name == "Administrator" {
			writeErr(w, 400, "system", "Administrator always has every permission")
			return
		}
		s.replaceRolePermissions(r.Context(), id, body.Permissions)
	}
	s.Audit.Event(r.Context(), &currentUser(r).ID, "role.patch", id.String(), r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) replaceRolePermissions(ctx context.Context, roleID uuid.UUID, names []string) {
	_, _ = s.Pool.Exec(ctx, `DELETE FROM role_permissions WHERE role_id=$1`, roleID)
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || name == "admin" {
			continue
		}
		_, _ = s.Pool.Exec(ctx, `
			INSERT INTO role_permissions (role_id, permission_id)
			SELECT $1, id FROM permissions WHERE name=$2
			ON CONFLICT DO NOTHING`, roleID, name)
	}
}

func (s *Server) adminDeleteRole(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid role id")
		return
	}
	var system bool
	if err := s.Pool.QueryRow(r.Context(), `SELECT is_system FROM roles WHERE id=$1`, id).Scan(&system); err != nil {
		writeErr(w, 404, "not_found", "group not found")
		return
	}
	if system {
		writeErr(w, 400, "system", "built-in groups cannot be deleted")
		return
	}
	if _, err := s.Pool.Exec(r.Context(), `DELETE FROM roles WHERE id=$1`, id); err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	s.Audit.Event(r.Context(), &currentUser(r).ID, "role.delete", id.String(), r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) adminRoleMembers(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid role id")
		return
	}
	var body struct {
		UserIDs []uuid.UUID `json:"user_ids"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	if len(body.UserIDs) == 0 {
		writeErr(w, 400, "invalid", "user_ids required")
		return
	}
	var roleName string
	_ = s.Pool.QueryRow(r.Context(), `SELECT name FROM roles WHERE id=$1`, id).Scan(&roleName)
	if r.Method == http.MethodDelete {
		for _, uid := range body.UserIDs {
			if roleName == "Administrator" && s.isLastAdministrator(r.Context(), uid) {
				writeErr(w, 409, "last_admin", "cannot remove the last administrator")
				return
			}
			_, _ = s.Pool.Exec(r.Context(), `DELETE FROM user_roles WHERE user_id=$1 AND role_id=$2`, uid, id)
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	for _, uid := range body.UserIDs {
		_, _ = s.Pool.Exec(r.Context(), `INSERT INTO user_roles (user_id, role_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, uid, id)
	}
	s.Audit.Event(r.Context(), &currentUser(r).ID, "role.members", id.String(), r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]any{"ok": true, "added": len(body.UserIDs)})
}

func (s *Server) adminRoleDiscord(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid role id")
		return
	}
	var exists bool
	if err := s.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM roles WHERE id=$1)`, id).Scan(&exists); err != nil || !exists {
		writeErr(w, 404, "not_found", "group not found")
		return
	}
	var body struct {
		Links []struct {
			GuildID       string `json:"guild_id"`
			DiscordRoleID string `json:"discord_role_id"`
		} `json:"links"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	_, _ = s.Pool.Exec(r.Context(), `DELETE FROM role_discord_links WHERE role_id=$1`, id)
	for _, l := range body.Links {
		gid := strings.TrimSpace(l.GuildID)
		rid := strings.TrimSpace(l.DiscordRoleID)
		if rid == "" {
			continue
		}
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO role_discord_links (role_id, guild_id, discord_role_id) VALUES ($1,$2,$3)
			ON CONFLICT DO NOTHING`, id, gid, rid)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) adminSyncDiscordRoles(w http.ResponseWriter, r *http.Request) {
	n, err := s.syncDiscordRoleMembership(r.Context())
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": true, "synced": 0, "discord": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "synced": n, "discord": true})
}

func (s *Server) discordBotToken(ctx context.Context) string {
	if s.Box == nil {
		return ""
	}
	var enc []byte
	_ = s.Pool.QueryRow(ctx, `SELECT bot_token_enc FROM discord_settings WHERE id=1`).Scan(&enc)
	if len(enc) == 0 {
		return ""
	}
	plain, err := s.Box.Decrypt(enc)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(plain))
}

func (s *Server) syncDiscordRoleMembership(ctx context.Context) (int, error) {
	token := s.discordBotToken(ctx)
	if token == "" {
		return 0, errString("Discord is not configured")
	}
	rows, err := s.Pool.Query(ctx, `SELECT role_id, guild_id, discord_role_id FROM role_discord_links`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type link struct {
		RoleID uuid.UUID
		Guild  string
		DRole  string
	}
	var links []link
	guilds := map[string]struct{}{}
	for rows.Next() {
		var l link
		if err := rows.Scan(&l.RoleID, &l.Guild, &l.DRole); err != nil {
			continue
		}
		if l.Guild == "" {
			continue
		}
		links = append(links, l)
		guilds[l.Guild] = struct{}{}
	}
	if len(links) == 0 {
		return 0, nil
	}
	users, err := s.Pool.Query(ctx, `SELECT user_id, provider_user_id FROM user_identities WHERE provider='discord'`)
	if err != nil {
		return 0, err
	}
	defer users.Close()
	type ident struct {
		UserID    uuid.UUID
		DiscordID string
	}
	var idents []ident
	for users.Next() {
		var it ident
		if users.Scan(&it.UserID, &it.DiscordID) == nil && it.DiscordID != "" {
			idents = append(idents, it)
		}
	}
	n := 0
	client := &http.Client{Timeout: 8 * time.Second}
	for _, it := range idents {
		for gid := range guilds {
			roles, ok := discordMemberRoles(ctx, client, token, gid, it.DiscordID)
			if !ok {
				continue
			}
			have := map[string]struct{}{}
			for _, rid := range roles {
				have[rid] = struct{}{}
			}
			for _, l := range links {
				if l.Guild != gid {
					continue
				}
				if _, ok := have[l.DRole]; !ok {
					continue
				}
				tag, err := s.Pool.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, it.UserID, l.RoleID)
				if err == nil && tag.RowsAffected() > 0 {
					n++
				}
			}
		}
	}
	return n, nil
}

func discordMemberRoles(ctx context.Context, client *http.Client, token, guildID, userID string) ([]string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://discord.com/api/v10/guilds/"+guildID+"/members/"+userID, nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("Authorization", "Bot "+token)
	res, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil, false
	}
	var body struct {
		Roles []string `json:"roles"`
	}
	if json.NewDecoder(res.Body).Decode(&body) != nil {
		return nil, false
	}
	return body.Roles, true
}
