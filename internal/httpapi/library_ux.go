package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
)

func (s *Server) listPlaylists(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	rows, err := s.Pool.Query(r.Context(), `
		SELECT p.id, p.name, p.description, p.collaborative, p.public, p.created_at,
			ep.provider, ep.sync_mode, ep.last_sync_status
		FROM playlists p
		LEFT JOIN external_playlists ep ON ep.sounddock_playlist_id = p.id
		WHERE p.user_id=$1 OR p.public=true OR p.id IN (SELECT playlist_id FROM playlist_collaborators WHERE user_id=$1)
		ORDER BY p.updated_at DESC`, u.ID)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "name", "description", "collaborative", "public", "created_at", "provider", "sync_mode", "last_sync_status"))
}

func (s *Server) createPlaylist(w http.ResponseWriter, r *http.Request) {
	var body struct{ Name, Description string }
	_ = decodeJSON(r, &body)
	if body.Name == "" {
		body.Name = "New playlist"
	}
	var id uuid.UUID
	err := s.Pool.QueryRow(r.Context(), `INSERT INTO playlists (user_id, name, description) VALUES ($1,$2,$3) RETURNING id`, currentUser(r).ID, body.Name, body.Description).Scan(&id)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if s.Hooks != nil {
		s.Hooks.Emit(r.Context(), "playlist.created", map[string]any{"id": id})
	}
	writeJSON(w, 201, map[string]any{"id": id, "name": body.Name})
}

func (s *Server) getPlaylist(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	var name, desc string
	if err := s.Pool.QueryRow(r.Context(), `SELECT name, description FROM playlists WHERE id=$1`, id).Scan(&name, &desc); err != nil {
		writeErr(w, 404, "not_found", "playlist not found")
		return
	}
	rows, _ := s.Pool.Query(r.Context(), `SELECT e.id, e.position, t.id, t.title FROM playlist_entries e JOIN tracks t ON t.id=e.track_id WHERE e.playlist_id=$1 ORDER BY e.position`, id)
	defer rows.Close()
	tracks := scanMaps(rows, "entry_id", "position", "track_id", "title")
	out := map[string]any{"id": id, "name": name, "description": desc, "tracks": tracks}
	var prov, mode, status string
	var last *time.Time
	var matched, unmatched int
	err := s.Pool.QueryRow(r.Context(), `
		SELECT provider, sync_mode, last_sync_status, last_sync_at,
			(SELECT count(*) FROM external_playlist_items i WHERE i.external_playlist_id=e.id AND i.mapped_track_id IS NOT NULL),
			(SELECT count(*) FROM external_playlist_items i WHERE i.external_playlist_id=e.id AND i.mapped_track_id IS NULL AND NOT i.ignored)
		FROM external_playlists e WHERE sounddock_playlist_id=$1`, id).Scan(&prov, &mode, &status, &last, &matched, &unmatched)
	if err == nil {
		out["external"] = map[string]any{"provider": prov, "sync_mode": mode, "status": status, "last_sync_at": last, "matched": matched, "unmatched": unmatched}
	}
	writeJSON(w, 200, out)
}

func (s *Server) updatePlaylist(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	var body struct{ Name, Description *string }
	_ = decodeJSON(r, &body)
	if body.Name != nil {
		_, _ = s.Pool.Exec(r.Context(), `UPDATE playlists SET name=$2, updated_at=now() WHERE id=$1`, id, *body.Name)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) deletePlaylist(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	_, _ = s.Pool.Exec(r.Context(), `DELETE FROM playlists WHERE id=$1 AND user_id=$2`, id, currentUser(r).ID)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) addPlaylistTracks(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	var body struct{ TrackIDs []uuid.UUID `json:"track_ids"` }
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
	eid, _ := uuid.Parse(chi.URLParam(r, "entryID"))
	_, _ = s.Pool.Exec(r.Context(), `DELETE FROM playlist_entries WHERE playlist_id=$1 AND id=$2`, pid, eid)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) reorderPlaylist(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	var body struct{ Order []uuid.UUID `json:"order"` }
	_ = decodeJSON(r, &body)
	for i, eid := range body.Order {
		_, _ = s.Pool.Exec(r.Context(), `UPDATE playlist_entries SET position=$3 WHERE playlist_id=$1 AND id=$2`, id, eid, i)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) exportM3U(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
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

func (s *Server) setFavourite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Type string    `json:"type"`
		ID   uuid.UUID `json:"id"`
		On   bool      `json:"on"`
	}
	_ = decodeJSON(r, &body)
	if body.On {
		_, _ = s.Pool.Exec(r.Context(), `INSERT INTO favourites (user_id, entity_type, entity_id) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, currentUser(r).ID, body.Type, body.ID)
	} else {
		_, _ = s.Pool.Exec(r.Context(), `DELETE FROM favourites WHERE user_id=$1 AND entity_type=$2 AND entity_id=$3`, currentUser(r).ID, body.Type, body.ID)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) listFavourites(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.Pool.Query(r.Context(), `SELECT entity_type, entity_id, created_at FROM favourites WHERE user_id=$1 ORDER BY created_at DESC`, currentUser(r).ID)
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "type", "id", "created_at"))
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.Pool.Query(r.Context(), `SELECT track_id, played_at, duration_ms, source FROM listen_history WHERE user_id=$1 ORDER BY played_at DESC LIMIT 200`, currentUser(r).ID)
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "track_id", "played_at", "duration_ms", "source"))
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	libs := s.libraryIDs(r.Context(), u)
	const trackSQL = `
		SELECT t.id, t.title, t.duration_ms, t.album_id, coalesce(al.title,''),
			coalesce((SELECT string_agg(ar.name, ', ' ORDER BY ta.position)
				FROM track_artists ta JOIN artists ar ON ar.id=ta.artist_id
				WHERE ta.track_id=t.id AND ta.role='primary'),'')
		FROM tracks t LEFT JOIN albums al ON al.id=t.album_id `
	recent, _ := s.Pool.Query(r.Context(), trackSQL+` WHERE t.library_id = ANY($1) ORDER BY t.created_at DESC LIMIT 48`, libs)
	defer recent.Close()
	played, _ := s.Pool.Query(r.Context(), `
		SELECT t.id, t.title, t.duration_ms, t.album_id, coalesce(al.title,''),
			coalesce((SELECT string_agg(ar.name, ', ' ORDER BY ta.position)
				FROM track_artists ta JOIN artists ar ON ar.id=ta.artist_id
				WHERE ta.track_id=t.id AND ta.role='primary'),''), pc.count
		FROM play_counts pc JOIN tracks t ON t.id=pc.track_id
		LEFT JOIN albums al ON al.id=t.album_id
		WHERE pc.user_id=$1 ORDER BY pc.count DESC LIMIT 48`, u.ID)
	defer played.Close()
	cont, _ := s.Pool.Query(r.Context(), `
		SELECT id, title, duration_ms, album_id, album, artist FROM (
			SELECT DISTINCT ON (h.track_id) t.id, t.title, t.duration_ms, t.album_id, coalesce(al.title,'') AS album,
				coalesce((SELECT string_agg(ar.name, ', ' ORDER BY ta.position)
					FROM track_artists ta JOIN artists ar ON ar.id=ta.artist_id
					WHERE ta.track_id=t.id AND ta.role='primary'),'') AS artist, h.played_at
			FROM listen_history h
			JOIN tracks t ON t.id=h.track_id
			LEFT JOIN albums al ON al.id=t.album_id
			WHERE h.user_id=$1
			ORDER BY h.track_id, h.played_at DESC
		) x ORDER BY played_at DESC LIMIT 48`, u.ID)
	defer cont.Close()
	writeJSON(w, 200, map[string]any{
		"recently_added": scanMaps(recent, "id", "title", "duration_ms", "album_id", "album", "artist"),
		"most_played":    scanMaps(played, "id", "title", "duration_ms", "album_id", "album", "artist", "count"),
		"continue":       scanMaps(cont, "id", "title", "duration_ms", "album_id", "album", "artist"),
	})
}

func (s *Server) getQueue(w http.ResponseWriter, r *http.Request) {
	sid, err := s.Play.Session(r.Context(), "web_device", currentUser(r).ID.String(), &currentUser(r).ID)
	if err != nil {
		writeErr(w, 500, "queue", err.Error())
		return
	}
	q, _ := s.Play.Get(r.Context(), sid)
	writeJSON(w, 200, q)
}

func (s *Server) putQueue(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TrackIDs []uuid.UUID `json:"track_ids"`
		Start    int         `json:"start"`
	}
	_ = decodeJSON(r, &body)
	u := currentUser(r)
	sid, _ := s.Play.Session(r.Context(), "web_device", u.ID.String(), &u.ID)
	_ = s.Play.Replace(r.Context(), sid, body.TrackIDs, body.Start)
	if len(body.TrackIDs) > 0 && s.Hooks != nil {
		s.Hooks.Emit(r.Context(), "playback.started", map[string]any{"track_id": body.TrackIDs[body.Start]})
	}
	q, _ := s.Play.Get(r.Context(), sid)
	writeJSON(w, 200, q)
}

func (s *Server) queueAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TrackIDs []uuid.UUID `json:"track_ids"`
		Next     bool        `json:"next"`
	}
	_ = decodeJSON(r, &body)
	u := currentUser(r)
	sid, _ := s.Play.Session(r.Context(), "web_device", u.ID.String(), &u.ID)
	_ = s.Play.Add(r.Context(), sid, body.TrackIDs, body.Next)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) queueControl(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string         `json:"action"`
		Extra  map[string]any `json:"extra"`
	}
	_ = decodeJSON(r, &body)
	u := currentUser(r)
	sid, _ := s.Play.Session(r.Context(), "web_device", u.ID.String(), &u.ID)
	if err := s.Play.Control(r.Context(), sid, body.Action, body.Extra); err != nil {
		writeErr(w, 400, "control", err.Error())
		return
	}
	if body.Action == "stop" && s.Hooks != nil {
		s.Hooks.Emit(r.Context(), "playback.finished", map[string]any{"session": sid})
	}
	q, _ := s.Play.Get(r.Context(), sid)
	writeJSON(w, 200, q)
}

func (s *Server) exportMe(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	favs := []any{}
	hist := []any{}
	pls := []any{}
	writeJSON(w, 200, map[string]any{"user": u.Username, "exported_at": time.Now(), "favourites": favs, "history": hist, "playlists": pls})
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

func (s *Server) adminCreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct{ Username, Password, Role string }
	_ = decodeJSON(r, &body)
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		writeErr(w, 500, "hash", err.Error())
		return
	}
	var id uuid.UUID
	if err := s.Pool.QueryRow(r.Context(), `INSERT INTO users (username, password_hash, display_name) VALUES ($1,$2,$1) RETURNING id`, body.Username, hash).Scan(&id); err != nil {
		writeErr(w, 400, "user", err.Error())
		return
	}
	role := body.Role
	if role == "" {
		role = "User"
	}
	_, _ = s.Pool.Exec(r.Context(), `INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE name=$2`, id, role)
	s.Audit.Event(r.Context(), &currentUser(r).ID, "user.create", body.Username, r.RemoteAddr, nil)
	writeJSON(w, 201, map[string]any{"id": id})
}
