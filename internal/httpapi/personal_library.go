package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
	"github.com/sounddock/sounddock/internal/minilib"
)

func (s *Server) myPersonalLibrary(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	o, err := minilib.EnsureOwner(r.Context(), s.Pool, u.ID, s.discordUserID(r))
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	s.writePersonalLibrary(w, r, o, viewerID(u), false)
}

func viewerID(u *auth.User) uuid.UUID {
	if u == nil {
		return uuid.Nil
	}
	return u.ID
}

func viewerAdmin(u *auth.User) bool {
	return auth.HasPerm(u, "admin")
}

func (s *Server) userPersonalLibrary(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid user id")
		return
	}
	viewer := currentUser(r)
	o, err := minilib.OwnerByUser(r.Context(), s.Pool, id)
	if err != nil {
		var vis string
		if qerr := s.Pool.QueryRow(r.Context(), `SELECT coalesce(personal_library_visibility,'private') FROM users WHERE id=$1 AND disabled=false`, id).Scan(&vis); qerr != nil {
			writeErr(w, 404, "not_found", "library not found")
			return
		}
		allowed := (viewer != nil && viewer.ID == id) || auth.HasPerm(viewer, "admin") || vis == "public"
		if !allowed {
			writeErr(w, 404, "not_found", "library not found")
			return
		}
		o, err = minilib.EnsureOwner(r.Context(), s.Pool, id, "")
		if err != nil || o.ID == uuid.Nil {
			writeErr(w, 404, "not_found", "library not found")
			return
		}
		o.Visibility = vis
	}
	if !minilib.CanSee(viewerID(viewer), viewerAdmin(viewer), o) {
		writeErr(w, 404, "not_found", "library not found")
		return
	}
	s.writePersonalLibrary(w, r, o, viewerID(viewer), minilib.Inspecting(viewerID(viewer), viewerAdmin(viewer), o))
}

func (s *Server) adminUserPersonalLibrary(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid user id")
		return
	}
	o, err := minilib.OwnerByUser(r.Context(), s.Pool, id)
	if err != nil {
		o, err = minilib.EnsureOwner(r.Context(), s.Pool, id, "")
		if err != nil || o.ID == uuid.Nil {
			writeErr(w, 404, "not_found", "library not found")
			return
		}
	}
	s.writePersonalLibrary(w, r, o, viewerID(currentUser(r)), o.UserID != currentUser(r).ID)
}

func (s *Server) adminDiscordPersonalLibrary(w http.ResponseWriter, r *http.Request) {
	did := strings.TrimSpace(chi.URLParam(r, "discordID"))
	if did == "" {
		writeErr(w, 400, "invalid", "discord user id required")
		return
	}
	o, err := minilib.OwnerByDiscord(r.Context(), s.Pool, did)
	if err != nil {
		writeErr(w, 404, "not_found", "library not found")
		return
	}
	s.writePersonalLibrary(w, r, o, viewerID(currentUser(r)), true)
}

func (s *Server) userPublicProfile(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid user id")
		return
	}
	var username, display, vis string
	if err := s.Pool.QueryRow(r.Context(), `
		SELECT username, display_name, coalesce(personal_library_visibility,'private')
		FROM users WHERE id=$1 AND disabled=false`, id).Scan(&username, &display, &vis); err != nil {
		writeErr(w, 404, "not_found", "user not found")
		return
	}
	o, _ := minilib.OwnerByUser(r.Context(), s.Pool, id)
	if o.ID != uuid.Nil {
		vis = o.Visibility
	}
	viewer := currentUser(r)
	canSee := minilib.CanSee(viewerID(viewer), viewerAdmin(viewer), o)
	if o.ID == uuid.Nil {
		canSee = viewer != nil && (viewer.ID == id || auth.HasPerm(viewer, "admin") || vis == "public")
	}
	out := map[string]any{
		"id":                           id,
		"display_name":                 display,
		"username":                     username,
		"personal_library_visibility":  vis,
		"personal_library_visible":     canSee,
		"personal_library_track_count": 0,
	}
	if canSee && o.ID != uuid.Nil {
		var n int
		_ = s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM personal_library_entries WHERE owner_id=$1`, o.ID).Scan(&n)
		out["personal_library_track_count"] = n
	}
	writeJSON(w, 200, out)
}

func (s *Server) writePersonalLibrary(w http.ResponseWriter, r *http.Request, o minilib.Owner, viewer uuid.UUID, inspecting bool) {
	limit := trackPageLimit(r.URL.Query().Get("limit"))
	rows, err := s.Pool.Query(r.Context(), `
		SELECT t.id, t.title, t.duration_ms, t.track_number, t.disc_number, t.year, t.explicit, t.album_id, t.library_id,
		       coalesce(al.title,''), e.first_requested_at, e.last_requested_at, e.request_count, `+listenArtistSQL+`
		FROM personal_library_entries e
		JOIN tracks t ON t.id = e.track_id
		LEFT JOIN albums al ON al.id = t.album_id
		WHERE e.owner_id=$1
		  AND `+trackPlayablePred+`
		ORDER BY e.last_requested_at DESC, t.id DESC
		LIMIT $2`, o.ID, limit)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	items := scanMaps(rows, "id", "title", "duration_ms", "track_number", "disc_number", "year", "explicit", "album_id", "library_id", "album", "first_requested_at", "last_requested_at", "request_count", "artist")
	if items == nil {
		items = []map[string]any{}
	}
	owner := map[string]any{
		"visibility": o.Visibility,
	}
	if o.UserID != uuid.Nil {
		owner["user_id"] = o.UserID
		var name string
		_ = s.Pool.QueryRow(r.Context(), `SELECT coalesce(nullif(display_name,''), username) FROM users WHERE id=$1`, o.UserID).Scan(&name)
		owner["display_name"] = name
	} else if o.DiscordUserID != "" {
		var name string
		_ = s.Pool.QueryRow(r.Context(), `
			SELECT coalesce(nullif(provider_username,''), '')
			FROM user_identities WHERE provider='discord' AND provider_user_id=$1`, o.DiscordUserID).Scan(&name)
		if name == "" {
			name = "Discord listener"
		}
		owner["display_name"] = name
	}
	if o.DiscordUserID != "" && (inspecting || o.UserID == viewer) {
		owner["discord_user_id"] = o.DiscordUserID
	}
	writeJSON(w, 200, map[string]any{
		"owner":      owner,
		"items":      items,
		"inspecting": inspecting,
	})
}
