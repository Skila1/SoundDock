package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/radio"
)

type playlistACL struct {
	Owner   uuid.UUID
	Name    string
	Desc    string
	Folder  string
	Collab  bool
	Public  bool
	IsOwner bool
	CanSee  bool
	CanEdit bool
}

func (s *Server) playlistACL(ctx context.Context, id, userID uuid.UUID) (playlistACL, error) {
	var a playlistACL
	err := s.Pool.QueryRow(ctx, `SELECT user_id, name, description, folder, collaborative, public FROM playlists WHERE id=$1`, id).
		Scan(&a.Owner, &a.Name, &a.Desc, &a.Folder, &a.Collab, &a.Public)
	if err != nil {
		return a, err
	}
	a.IsOwner = a.Owner == userID
	collab := false
	if !a.IsOwner {
		var n int
		_ = s.Pool.QueryRow(ctx, `SELECT 1 FROM playlist_collaborators WHERE playlist_id=$1 AND user_id=$2`, id, userID).Scan(&n)
		collab = n == 1
	}
	a.CanSee = a.IsOwner || a.Public || collab
	a.CanEdit = a.IsOwner || (a.Collab && collab)
	return a, nil
}

func (s *Server) requirePlaylistSee(w http.ResponseWriter, r *http.Request, id uuid.UUID) (playlistACL, bool) {
	a, err := s.playlistACL(r.Context(), id, currentUser(r).ID)
	if err != nil || !a.CanSee {
		writeErr(w, 404, "not_found", "playlist not found")
		return a, false
	}
	return a, true
}

func (s *Server) requirePlaylistEdit(w http.ResponseWriter, r *http.Request, id uuid.UUID) (playlistACL, bool) {
	a, ok := s.requirePlaylistSee(w, r, id)
	if !ok {
		return a, false
	}
	if !a.CanEdit {
		writeErr(w, 403, "forbidden", "not allowed to edit this playlist")
		return a, false
	}
	return a, true
}

func (s *Server) requirePlaylistOwner(w http.ResponseWriter, r *http.Request, id uuid.UUID) (playlistACL, bool) {
	a, ok := s.requirePlaylistSee(w, r, id)
	if !ok {
		return a, false
	}
	if !a.IsOwner {
		writeErr(w, 403, "forbidden", "owner required")
		return a, false
	}
	return a, true
}

func (s *Server) listPlaylists(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	folder := r.URL.Query().Get("folder")
	q := `
		SELECT p.id, p.name, p.description, p.collaborative, p.public, p.folder, p.created_at,
			ep.provider, ep.sync_mode, ep.last_sync_status, ep.external_playlist_id,
			EXISTS(SELECT 1 FROM smart_playlist_rules s WHERE s.playlist_id=p.id)
		FROM playlists p
		LEFT JOIN external_playlists ep ON ep.sounddock_playlist_id = p.id
		WHERE (p.user_id=$1 OR p.public=true OR p.id IN (SELECT playlist_id FROM playlist_collaborators WHERE user_id=$1))`
	args := []any{u.ID}
	if folder != "" {
		q += ` AND p.folder=$2`
		args = append(args, folder)
	}
	q += ` ORDER BY p.folder, p.updated_at DESC`
	rows, err := s.Pool.Query(r.Context(), q, args...)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "name", "description", "collaborative", "public", "folder", "created_at", "provider", "sync_mode", "last_sync_status", "external_id", "is_smart"))
}

func (s *Server) listPlaylistFolders(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	rows, err := s.Pool.Query(r.Context(), `
		SELECT folder, count(*) FROM playlists
		WHERE user_id=$1 OR public=true OR id IN (SELECT playlist_id FROM playlist_collaborators WHERE user_id=$1)
		GROUP BY folder ORDER BY folder`, u.ID)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var name string
		var n int
		if err := rows.Scan(&name, &n); err != nil {
			continue
		}
		out = append(out, map[string]any{"name": name, "count": n})
	}
	if out == nil {
		out = []map[string]any{}
	}
	writeJSON(w, 200, out)
}

func (s *Server) createPlaylist(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name          string          `json:"name"`
		Description   string          `json:"description"`
		Folder        string          `json:"folder"`
		Public        bool            `json:"public"`
		Collaborative bool            `json:"collaborative"`
		Smart         json.RawMessage `json:"smart"`
	}
	_ = decodeJSON(r, &body)
	if body.Name == "" {
		body.Name = "New playlist"
	}
	var id uuid.UUID
	err := s.Pool.QueryRow(r.Context(), `
		INSERT INTO playlists (user_id, name, description, folder, public, collaborative)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		currentUser(r).ID, body.Name, body.Description, body.Folder, body.Public, body.Collaborative).Scan(&id)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if len(body.Smart) > 0 && string(body.Smart) != "null" {
		rules, err := radio.ParseRules(body.Smart)
		if err != nil {
			writeErr(w, 400, "invalid", "smart rules: "+err.Error())
			return
		}
		b, _ := json.Marshal(rules)
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO smart_playlist_rules (playlist_id, rules) VALUES ($1,$2::jsonb)
			ON CONFLICT (playlist_id) DO UPDATE SET rules=EXCLUDED.rules, updated_at=now()`, id, b)
		if s.Jobs != nil {
			_, _ = s.Jobs.Enqueue(r.Context(), "smart_playlist.refresh", radio.SmartPayload{PlaylistID: id})
		}
	}
	if s.Hooks != nil {
		s.Hooks.Emit(r.Context(), "playlist.created", map[string]any{"id": id})
	}
	writeJSON(w, 201, map[string]any{"id": id, "name": body.Name, "folder": body.Folder})
}

func (s *Server) getPlaylist(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	a, ok := s.requirePlaylistSee(w, r, id)
	if !ok {
		return
	}
	rows, _ := s.Pool.Query(r.Context(), `SELECT e.id, e.position, t.id, t.title FROM playlist_entries e JOIN tracks t ON t.id=e.track_id WHERE e.playlist_id=$1 ORDER BY e.position`, id)
	defer rows.Close()
	tracks := scanMaps(rows, "entry_id", "position", "track_id", "title")
	out := map[string]any{
		"id": id, "name": a.Name, "description": a.Desc, "folder": a.Folder,
		"collaborative": a.Collab, "public": a.Public, "user_id": a.Owner,
		"is_owner": a.IsOwner, "can_edit": a.CanEdit, "tracks": tracks,
	}
	var prov, mode, status, extID string
	var last *time.Time
	var matched, unmatched int
	err := s.Pool.QueryRow(r.Context(), `
		SELECT provider, sync_mode, last_sync_status, last_sync_at, external_playlist_id,
			(SELECT count(*) FROM external_playlist_items i WHERE i.external_playlist_id=e.id AND i.mapped_track_id IS NOT NULL),
			(SELECT count(*) FROM external_playlist_items i WHERE i.external_playlist_id=e.id AND i.mapped_track_id IS NULL AND NOT i.ignored)
		FROM external_playlists e WHERE sounddock_playlist_id=$1`, id).Scan(&prov, &mode, &status, &last, &extID, &matched, &unmatched)
	if err == nil {
		out["external"] = map[string]any{
			"provider": prov, "sync_mode": mode, "status": status, "last_sync_at": last,
			"external_id": extID, "matched": matched, "unmatched": unmatched,
		}
	}
	var raw []byte
	var interval int
	if err := s.Pool.QueryRow(r.Context(), `SELECT rules, refresh_interval_seconds FROM smart_playlist_rules WHERE playlist_id=$1`, id).Scan(&raw, &interval); err == nil {
		var rules any
		_ = json.Unmarshal(raw, &rules)
		out["smart"] = map[string]any{"rules": rules, "refresh_interval_seconds": interval}
		out["is_smart"] = true
	}
	var snaps int
	_ = s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM playlist_snapshots WHERE playlist_id=$1`, id).Scan(&snaps)
	out["snapshot_count"] = snaps
	writeJSON(w, 200, out)
}

func (s *Server) updatePlaylist(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	a, ok := s.requirePlaylistOwner(w, r, id)
	if !ok {
		return
	}
	var body struct {
		Name          *string `json:"name"`
		Description   *string `json:"description"`
		Folder        *string `json:"folder"`
		Public        *bool   `json:"public"`
		Collaborative *bool   `json:"collaborative"`
	}
	_ = decodeJSON(r, &body)
	name, desc, folder := a.Name, a.Desc, a.Folder
	pub, collab := a.Public, a.Collab
	if body.Name != nil {
		name = *body.Name
	}
	if body.Description != nil {
		desc = *body.Description
	}
	if body.Folder != nil {
		folder = *body.Folder
	}
	if body.Public != nil {
		pub = *body.Public
	}
	if body.Collaborative != nil {
		collab = *body.Collaborative
	}
	_, _ = s.Pool.Exec(r.Context(), `UPDATE playlists SET name=$2, description=$3, folder=$4, public=$5, collaborative=$6, updated_at=now() WHERE id=$1`,
		id, name, desc, folder, pub, collab)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) deletePlaylist(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	_, _ = s.Pool.Exec(r.Context(), `DELETE FROM playlists WHERE id=$1 AND user_id=$2`, id, currentUser(r).ID)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) addPlaylistTracks(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	if _, ok := s.requirePlaylistEdit(w, r, id); !ok {
		return
	}
	var body struct {
		TrackIDs []uuid.UUID `json:"track_ids"`
	}
	_ = decodeJSON(r, &body)
	var max int
	_ = s.Pool.QueryRow(r.Context(), `SELECT coalesce(max(position),-1) FROM playlist_entries WHERE playlist_id=$1`, id).Scan(&max)
	for i, t := range body.TrackIDs {
		_, _ = s.Pool.Exec(r.Context(), `INSERT INTO playlist_entries (playlist_id, track_id, position, added_by) VALUES ($1,$2,$3,$4)`, id, t, max+1+i, currentUser(r).ID)
	}
	writeJSON(w, 200, map[string]int{"added": len(body.TrackIDs)})
}

func (s *Server) removePlaylistTrack(w http.ResponseWriter, r *http.Request) {
	pid, _ := uuid.Parse(chi.URLParam(r, "id"))
	if _, ok := s.requirePlaylistEdit(w, r, pid); !ok {
		return
	}
	eid, _ := uuid.Parse(chi.URLParam(r, "entryID"))
	_, _ = s.Pool.Exec(r.Context(), `DELETE FROM playlist_entries WHERE playlist_id=$1 AND id=$2`, pid, eid)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) reorderPlaylist(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	if _, ok := s.requirePlaylistEdit(w, r, id); !ok {
		return
	}
	var body struct {
		Order []uuid.UUID `json:"order"`
	}
	_ = decodeJSON(r, &body)
	for i, eid := range body.Order {
		_, _ = s.Pool.Exec(r.Context(), `UPDATE playlist_entries SET position=$3 WHERE playlist_id=$1 AND id=$2`, id, eid, i)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) exportM3U(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	if _, ok := s.requirePlaylistSee(w, r, id); !ok {
		return
	}
	rows, _ := s.Pool.Query(r.Context(), `
		SELECT coalesce(ar.name,''), t.title, t.duration_ms
		FROM playlist_entries e JOIN tracks t ON t.id=e.track_id
		LEFT JOIN track_artists ta ON ta.track_id=t.id AND ta.role='primary'
		LEFT JOIN artists ar ON ar.id=ta.artist_id
		WHERE e.playlist_id=$1 ORDER BY e.position`, id)
	defer rows.Close()
	w.Header().Set("Content-Type", "audio/x-mpegurl")
	w.Write([]byte("#EXTM3U\n"))
	for rows.Next() {
		var artist, title string
		var dur int
		_ = rows.Scan(&artist, &title, &dur)
		w.Write([]byte("#EXTINF:" + itoa(dur/1000) + "," + artist + " - " + title + "\n" + title + "\n"))
	}
}

func (s *Server) importM3U(w http.ResponseWriter, r *http.Request) {
	b, _ := ioReadAll(r)
	var id uuid.UUID
	_ = s.Pool.QueryRow(r.Context(), `INSERT INTO playlists (user_id, name) VALUES ($1,'Imported M3U') RETURNING id`, currentUser(r).ID).Scan(&id)
	pos := 0
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var tid uuid.UUID
		err := s.Pool.QueryRow(r.Context(), `SELECT id FROM tracks WHERE title ILIKE $1 LIMIT 1`, "%"+baseName(line)+"%").Scan(&tid)
		if err == nil {
			_, _ = s.Pool.Exec(r.Context(), `INSERT INTO playlist_entries (playlist_id, track_id, position) VALUES ($1,$2,$3)`, id, tid, pos)
			pos++
		}
	}
	writeJSON(w, 201, map[string]any{"id": id, "imported": pos})
}

func (s *Server) createPlaylistInvite(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	if _, ok := s.requirePlaylistOwner(w, r, id); !ok {
		return
	}
	tok, exp, err := radio.SignInvite(s.SignKey, id, 0)
	if err != nil {
		writeErr(w, 500, "invite", err.Error())
		return
	}
	_, _ = s.Pool.Exec(r.Context(), `UPDATE playlists SET collaborative=true, updated_at=now() WHERE id=$1`, id)
	path := "/playlists/invite?token=" + url.QueryEscape(tok)
	writeJSON(w, 200, map[string]any{
		"token": tok, "expires_at": exp, "path": path,
		"url": s.absURL(r) + path,
	})
}

func (s *Server) previewPlaylistInvite(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("token")
	pid, err := radio.VerifyInvite(s.SignKey, tok)
	if err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	var name string
	var owner uuid.UUID
	if err := s.Pool.QueryRow(r.Context(), `SELECT name, user_id FROM playlists WHERE id=$1`, pid).Scan(&name, &owner); err != nil {
		writeErr(w, 404, "not_found", "playlist not found")
		return
	}
	writeJSON(w, 200, map[string]any{"playlist_id": pid, "name": name, "user_id": owner})
}

func (s *Server) acceptPlaylistInvite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	_ = decodeJSON(r, &body)
	if body.Token == "" {
		body.Token = r.URL.Query().Get("token")
	}
	pid, err := radio.VerifyInvite(s.SignKey, body.Token)
	if err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	u := currentUser(r)
	_, err = s.Pool.Exec(r.Context(), `INSERT INTO playlist_collaborators (playlist_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, pid, u.ID)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	_, _ = s.Pool.Exec(r.Context(), `UPDATE playlists SET collaborative=true, updated_at=now() WHERE id=$1`, pid)
	writeJSON(w, 200, map[string]any{"ok": true, "playlist_id": pid})
}

func (s *Server) listPlaylistCollaborators(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	if _, ok := s.requirePlaylistSee(w, r, id); !ok {
		return
	}
	rows, err := s.Pool.Query(r.Context(), `
		SELECT u.id, u.username, u.display_name
		FROM playlist_collaborators c JOIN users u ON u.id=c.user_id
		WHERE c.playlist_id=$1 ORDER BY u.username`, id)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "username", "display_name"))
}

func (s *Server) removePlaylistCollaborator(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	if _, ok := s.requirePlaylistOwner(w, r, id); !ok {
		return
	}
	uid, _ := uuid.Parse(chi.URLParam(r, "userID"))
	_, _ = s.Pool.Exec(r.Context(), `DELETE FROM playlist_collaborators WHERE playlist_id=$1 AND user_id=$2`, id, uid)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) listPlaylistSnapshots(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	if _, ok := s.requirePlaylistSee(w, r, id); !ok {
		return
	}
	rows, err := s.Pool.Query(r.Context(), `
		SELECT id, created_at, created_by, jsonb_array_length(entries) FROM playlist_snapshots
		WHERE playlist_id=$1 ORDER BY created_at DESC LIMIT 50`, id)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "created_at", "created_by", "entry_count"))
}

func (s *Server) createPlaylistSnapshot(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	if _, ok := s.requirePlaylistEdit(w, r, id); !ok {
		return
	}
	sid, err := radio.New(s.Pool).CaptureSnapshot(r.Context(), id, currentUser(r).ID)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"id": sid})
}

func (s *Server) getPlaylistSnapshot(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	if _, ok := s.requirePlaylistSee(w, r, id); !ok {
		return
	}
	sid, _ := uuid.Parse(chi.URLParam(r, "sid"))
	var created time.Time
	var by *uuid.UUID
	var raw []byte
	err := s.Pool.QueryRow(r.Context(), `SELECT created_at, created_by, entries FROM playlist_snapshots WHERE id=$1 AND playlist_id=$2`, sid, id).
		Scan(&created, &by, &raw)
	if err != nil {
		writeErr(w, 404, "not_found", "snapshot not found")
		return
	}
	var entries any
	_ = json.Unmarshal(raw, &entries)
	writeJSON(w, 200, map[string]any{"id": sid, "playlist_id": id, "created_at": created, "created_by": by, "entries": entries})
}

func (s *Server) restorePlaylistSnapshot(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	if _, ok := s.requirePlaylistEdit(w, r, id); !ok {
		return
	}
	sid, _ := uuid.Parse(chi.URLParam(r, "sid"))
	if err := radio.New(s.Pool).RestoreSnapshot(r.Context(), id, sid, currentUser(r).ID); err != nil {
		writeErr(w, 400, "restore", err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) deletePlaylistSnapshot(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	if _, ok := s.requirePlaylistOwner(w, r, id); !ok {
		return
	}
	sid, _ := uuid.Parse(chi.URLParam(r, "sid"))
	_, _ = s.Pool.Exec(r.Context(), `DELETE FROM playlist_snapshots WHERE id=$1 AND playlist_id=$2`, sid, id)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) getSmartPlaylist(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	if _, ok := s.requirePlaylistSee(w, r, id); !ok {
		return
	}
	var raw []byte
	var interval int
	var updated time.Time
	err := s.Pool.QueryRow(r.Context(), `SELECT rules, refresh_interval_seconds, updated_at FROM smart_playlist_rules WHERE playlist_id=$1`, id).
		Scan(&raw, &interval, &updated)
	if err != nil {
		writeJSON(w, 200, map[string]any{"playlist_id": id, "rules": nil})
		return
	}
	var rules any
	_ = json.Unmarshal(raw, &rules)
	writeJSON(w, 200, map[string]any{"playlist_id": id, "rules": rules, "refresh_interval_seconds": interval, "updated_at": updated})
}

func (s *Server) putSmartPlaylist(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	if _, ok := s.requirePlaylistOwner(w, r, id); !ok {
		return
	}
	var body struct {
		Rules                  json.RawMessage `json:"rules"`
		RefreshIntervalSeconds *int            `json:"refresh_interval_seconds"`
	}
	_ = decodeJSON(r, &body)
	raw := body.Rules
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	rules, err := radio.ParseRules(raw)
	if err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	interval := 86400
	if body.RefreshIntervalSeconds != nil && *body.RefreshIntervalSeconds > 0 {
		interval = *body.RefreshIntervalSeconds
	}
	b, _ := json.Marshal(rules)
	_, err = s.Pool.Exec(r.Context(), `
		INSERT INTO smart_playlist_rules (playlist_id, rules, refresh_interval_seconds)
		VALUES ($1,$2::jsonb,$3)
		ON CONFLICT (playlist_id) DO UPDATE SET rules=EXCLUDED.rules, refresh_interval_seconds=EXCLUDED.refresh_interval_seconds, updated_at=now()`,
		id, b, interval)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if s.Jobs != nil {
		_, _ = s.Jobs.Enqueue(r.Context(), "smart_playlist.refresh", radio.SmartPayload{PlaylistID: id})
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) refreshSmartPlaylist(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	if _, ok := s.requirePlaylistOwner(w, r, id); !ok {
		return
	}
	jid, err := s.Jobs.Enqueue(r.Context(), "smart_playlist.refresh", radio.SmartPayload{PlaylistID: id})
	if err != nil {
		s.writeJobErr(w, err)
		return
	}
	writeJSON(w, 202, map[string]any{"job_id": jid, "ok": true})
}

func (s *Server) playlistSyncDiff(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	if _, ok := s.requirePlaylistSee(w, r, id); !ok {
		return
	}
	var epid uuid.UUID
	var prov, mode, status, lerr string
	var last *time.Time
	err := s.Pool.QueryRow(r.Context(), `
		SELECT id, provider, sync_mode, last_sync_status, coalesce(last_error,''), last_sync_at
		FROM external_playlists WHERE sounddock_playlist_id=$1`, id).Scan(&epid, &prov, &mode, &status, &lerr, &last)
	if err != nil {
		writeJSON(w, 200, map[string]any{"items": []any{}, "matched": 0, "unmatched": 0})
		return
	}
	rows, err := s.Pool.Query(r.Context(), `
		SELECT i.id, i.position, i.title, i.artists, i.album, i.isrc, i.match_status, i.match_confidence, i.mapped_track_id, i.ignored
		FROM external_playlist_items i
		WHERE i.external_playlist_id=$1
		ORDER BY i.position`, epid)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	items := scanMaps(rows, "id", "position", "title", "artists", "album", "isrc", "match_status", "match_confidence", "mapped_track_id", "ignored")
	var matched, unmatched, ignored int
	for _, it := range items {
		if b, _ := it["ignored"].(bool); b {
			ignored++
			continue
		}
		if it["mapped_track_id"] != nil {
			matched++
		} else {
			unmatched++
		}
	}
	writeJSON(w, 200, map[string]any{
		"provider": prov, "sync_mode": mode, "status": status, "error": lerr, "last_sync_at": last,
		"matched": matched, "unmatched": unmatched, "ignored": ignored, "items": items,
	})
}

func itoa(n int) string { return strings.TrimPrefix(strings.Replace(jsonNumber(n), `"`, "", -1), "") }

func jsonNumber(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func ioReadAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(io.LimitReader(r.Body, 2<<20))
}

func baseName(s string) string {
	s = strings.ReplaceAll(s, "\\", "/")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, "."); i > 0 {
		s = s[:i]
	}
	return s
}
