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
	rows, err := s.Pool.Query(r.Context(), `SELECT track_id, played_at, duration_ms, source FROM listen_history WHERE user_id=$1 ORDER BY played_at DESC LIMIT 200`, currentUser(r).ID)
	if err != nil {
		writeJSON(w, 200, []any{})
		return
	}
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "track_id", "played_at", "duration_ms", "source"))
}

const homeTrackSQL = `
		SELECT t.id, t.title, t.duration_ms, t.album_id, coalesce(al.title,''),
			coalesce((SELECT string_agg(ar.name, ', ' ORDER BY ta.position)
				FROM track_artists ta JOIN artists ar ON ar.id=ta.artist_id
				WHERE ta.track_id=t.id AND ta.role='primary'),'')
		FROM tracks t LEFT JOIN albums al ON al.id=t.album_id `

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	libs := s.libraryIDs(r.Context(), u)
	recent, err := s.Pool.Query(r.Context(), homeTrackSQL+` WHERE t.library_id = ANY($1) ORDER BY t.created_at DESC LIMIT 48`, libs)
	if err != nil {
		writeErr(w, 500, "home", err.Error())
		return
	}
	defer recent.Close()
	played, err := s.Pool.Query(r.Context(), `
		SELECT t.id, t.title, t.duration_ms, t.album_id, coalesce(al.title,''),
			coalesce((SELECT string_agg(ar.name, ', ' ORDER BY ta.position)
				FROM track_artists ta JOIN artists ar ON ar.id=ta.artist_id
				WHERE ta.track_id=t.id AND ta.role='primary'),''), pc.count
		FROM play_counts pc JOIN tracks t ON t.id=pc.track_id
		LEFT JOIN albums al ON al.id=t.album_id
		WHERE pc.user_id=$1 AND pc.count > 0
		ORDER BY pc.count DESC, pc.last_played_at DESC NULLS LAST
		LIMIT 48`, u.ID)
	if err != nil {
		writeErr(w, 500, "home", err.Error())
		return
	}
	defer played.Close()
	cont := s.homeContinue(r, u)
	writeJSON(w, 200, map[string]any{
		"recently_added": scanMaps(recent, "id", "title", "duration_ms", "album_id", "album", "artist"),
		"most_played":    scanMaps(played, "id", "title", "duration_ms", "album_id", "album", "artist", "count"),
		"continue":       cont,
	})
}

func (s *Server) homeContinue(r *http.Request, u *auth.User) []map[string]any {
	seen := map[uuid.UUID]bool{}
	var out []map[string]any
	sess, err := s.Pool.Query(r.Context(), `
		SELECT t.id, t.title, t.duration_ms, t.album_id, coalesce(al.title,''),
			coalesce((SELECT string_agg(ar.name, ', ' ORDER BY ta.position)
				FROM track_artists ta JOIN artists ar ON ar.id=ta.artist_id
				WHERE ta.track_id=t.id AND ta.role='primary'),''),
			s.position_ms
		FROM playback_sessions s
		JOIN tracks t ON t.id=s.current_track_id
		LEFT JOIN albums al ON al.id=t.album_id
		WHERE s.user_id=$1 AND s.kind='web_device'
			AND s.status IN ('playing','paused')
			AND s.current_track_id IS NOT NULL AND s.position_ms > 0
		ORDER BY s.updated_at DESC
		LIMIT 24`, u.ID)
	if err == nil {
		defer sess.Close()
		for _, row := range scanMaps(sess, "id", "title", "duration_ms", "album_id", "album", "artist", "position_ms") {
			if id, err := uuid.Parse(asString(row["id"])); err == nil {
				seen[id] = true
			}
			out = append(out, row)
		}
	}
	hist, err := s.Pool.Query(r.Context(), `
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
	if err == nil {
		defer hist.Close()
		for _, row := range scanMaps(hist, "id", "title", "duration_ms", "album_id", "album", "artist") {
			id, err := uuid.Parse(asString(row["id"]))
			if err == nil && seen[id] {
				continue
			}
			if err == nil {
				seen[id] = true
			}
			out = append(out, row)
			if len(out) >= 48 {
				break
			}
		}
	}
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
	if rows, err := s.Pool.Query(r.Context(), `SELECT track_id, played_at, duration_ms, source FROM listen_history WHERE user_id=$1 ORDER BY played_at DESC LIMIT 5000`, u.ID); err == nil {
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
