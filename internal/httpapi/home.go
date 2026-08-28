package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
)

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
	rows, err := s.Pool.Query(r.Context(), `SELECT entity_type, entity_id, created_at FROM favourites WHERE user_id=$1 ORDER BY created_at DESC`, currentUser(r).ID)
	if err != nil {
		writeJSON(w, 200, []any{})
		return
	}
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "type", "id", "created_at"))
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	events := s.listenReaderEvents(r.Context())
	rows, err := s.Pool.Query(r.Context(), historyListSQL(events, 200), currentUser(r).ID)
	if err != nil {
		writeJSON(w, 200, []any{})
		return
	}
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "track_id", "played_at", "duration_ms", "source"))
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	writeJSON(w, 200, map[string]any{
		"continue":       s.homeContinue(r, u),
		"recently_added": s.homeRecentlyAdded(r, u),
		"most_played":    s.homeMostPlayed(r, u),
	})
}

func homeRecentlyAddedSQL() string {
	return `SELECT t.id, t.title, t.duration_ms, t.album_id, coalesce(al.title,'') AS album, ` + listenArtistSQL + ` AS artist
		FROM tracks t
		LEFT JOIN albums al ON al.id=t.album_id
		WHERE t.library_id = ANY($1)
		  AND ` + trackPlayablePred + `
		ORDER BY t.created_at DESC
		LIMIT 15`
}

func homeMostPlayedSQL() string {
	return `SELECT t.id, t.title, t.duration_ms, t.album_id, coalesce(al.title,'') AS album, ` + listenArtistSQL + ` AS artist, pc.count
		FROM play_counts pc
		JOIN tracks t ON t.id = pc.track_id
		LEFT JOIN albums al ON al.id=t.album_id
		WHERE pc.user_id=$1 AND pc.count > 0 AND t.library_id = ANY($2)
		  AND ` + trackPlayablePred + `
		ORDER BY pc.count DESC, pc.last_played_at DESC NULLS LAST
		LIMIT 15`
}

func (s *Server) homeContinue(r *http.Request, u *auth.User) []map[string]any {
	events := s.listenReaderEvents(r.Context())
	hist, err := s.Pool.Query(r.Context(), homeContinueSQL(events), u.ID)
	if err != nil {
		return []map[string]any{}
	}
	defer hist.Close()
	out := scanMaps(hist, "id", "title", "duration_ms", "album_id", "album", "artist")
	if out == nil {
		out = []map[string]any{}
	}
	return out
}

func (s *Server) homeRecentlyAdded(r *http.Request, u *auth.User) []map[string]any {
	libs := s.libraryIDs(r.Context(), u)
	rows, err := s.Pool.Query(r.Context(), homeRecentlyAddedSQL(), libs)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	out := scanMaps(rows, "id", "title", "duration_ms", "album_id", "album", "artist")
	if out == nil {
		out = []map[string]any{}
	}
	return out
}

func (s *Server) homeMostPlayed(r *http.Request, u *auth.User) []map[string]any {
	libs := s.libraryIDs(r.Context(), u)
	rows, err := s.Pool.Query(r.Context(), homeMostPlayedSQL(), u.ID, libs)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	out := scanMaps(rows, "id", "title", "duration_ms", "album_id", "album", "artist", "count")
	if out == nil {
		out = []map[string]any{}
	}
	return out
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case uuid.UUID:
		return t.String()
	default:
		return ""
	}
}

func (s *Server) exportMe(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	favs := []map[string]any{}
	if rows, err := s.Pool.Query(r.Context(), `SELECT entity_type, entity_id, created_at FROM favourites WHERE user_id=$1 ORDER BY created_at DESC`, u.ID); err == nil {
		defer rows.Close()
		favs = scanMaps(rows, "type", "id", "created_at")
	}
	hist := []map[string]any{}
	if rows, err := s.Pool.Query(r.Context(), historyListSQL(s.listenReaderEvents(r.Context()), 5000), u.ID); err == nil {
		defer rows.Close()
		hist = scanMaps(rows, "track_id", "played_at", "duration_ms", "source")
	}
	pls := []map[string]any{}
	if rows, err := s.Pool.Query(r.Context(), `SELECT id, name, description, public, collaborative FROM playlists WHERE user_id=$1 ORDER BY name`, u.ID); err == nil {
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			var name, desc string
			var pub, collab bool
			if err := rows.Scan(&id, &name, &desc, &pub, &collab); err != nil {
				continue
			}
			entries := []map[string]any{}
			erows, eerr := s.Pool.Query(r.Context(), `SELECT track_id, position FROM playlist_entries WHERE playlist_id=$1 ORDER BY position`, id)
			if eerr == nil {
				entries = scanMaps(erows, "track_id", "position")
				erows.Close()
			}
			pls = append(pls, map[string]any{
				"id": id, "name": name, "description": desc, "public": pub, "collaborative": collab, "entries": entries,
			})
		}
	}
	writeJSON(w, 200, map[string]any{"user": u.Username, "exported_at": time.Now(), "favourites": favs, "history": hist, "playlists": pls})
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
