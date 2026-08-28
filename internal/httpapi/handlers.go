package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
	"github.com/sounddock/sounddock/internal/ingest"
	"github.com/sounddock/sounddock/internal/opensubsonic"
	"github.com/sounddock/sounddock/internal/scan"
	"github.com/sounddock/sounddock/internal/search"
	"github.com/sounddock/sounddock/internal/stream"
)

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("sd_session"); err == nil {
		u, sid, err := s.Auth.SessionUser(r.Context(), c.Value)
		if err == nil {
			_ = s.Auth.DeleteSession(r.Context(), sid)
			_ = u
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "sd_session", Path: "/", MaxAge: -1})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) logoutAll(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	_ = s.Auth.DeleteUserSessions(r.Context(), u.ID)
	http.SetCookie(w, &http.Cookie{Name: "sd_session", Path: "/", MaxAge: -1})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, currentUser(r))
}

func (s *Server) patchMe(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var body struct {
		DisplayName      *string  `json:"display_name"`
		ReplayGainMode   *string  `json:"replaygain_mode"`
		CrossfadeSeconds *int     `json:"crossfade_seconds"`
		TargetLUFS       *float64 `json:"target_lufs"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	rg, xf, lufs := u.ReplayGainMode, u.CrossfadeSeconds, u.TargetLUFS
	if body.ReplayGainMode != nil {
		rg = *body.ReplayGainMode
	}
	if body.CrossfadeSeconds != nil {
		xf = *body.CrossfadeSeconds
	}
	if body.TargetLUFS != nil {
		lufs = *body.TargetLUFS
	}
	_ = s.Auth.UpdatePrefs(r.Context(), u.ID, rg, xf, lufs)
	if body.DisplayName != nil {
		_, _ = s.Pool.Exec(r.Context(), `UPDATE users SET display_name=$1 WHERE id=$2`, *body.DisplayName, u.ID)
	}
	nu, _ := s.Auth.GetUser(r.Context(), u.ID)
	writeJSON(w, 200, nu)
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := decodeJSON(r, &body); err != nil || body.New == "" {
		writeErr(w, 400, "invalid", "current and new password required")
		return
	}
	if err := s.Auth.ChangePassword(r.Context(), currentUser(r).ID, body.Current, body.New); err != nil {
		writeErr(w, 400, "password", "could not change password")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) mySessions(w http.ResponseWriter, r *http.Request) {
	list, _ := s.Auth.ListSessions(r.Context(), currentUser(r).ID)
	writeJSON(w, 200, list)
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	_ = s.Auth.DeleteSession(r.Context(), id)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) identities(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.Pool.Query(r.Context(), `SELECT provider, provider_user_id, provider_username, linked_at FROM user_identities WHERE user_id=$1`, currentUser(r).ID)
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var p, pid, pun string
		var at time.Time
		_ = rows.Scan(&p, &pid, &pun, &at)
		out = append(out, map[string]any{"provider": p, "provider_user_id": pid, "username": pun, "linked_at": at})
	}
	if out == nil {
		out = []map[string]any{}
	}
	writeJSON(w, 200, out)
}

func (s *Server) startDiscordLink(w http.ResponseWriter, r *http.Request) {
	tok, _ := cryptox.RandomToken(24)
	hash := cryptox.HashToken(tok)
	_, _ = s.Pool.Exec(r.Context(), `INSERT INTO identity_link_challenges (token_hash, provider, user_id, expires_at) VALUES ($1,'discord',$2,now()+interval '10 minutes')`, hash, currentUser(r).ID)
	writeJSON(w, 200, map[string]string{"url": s.absURL(r) + "/link/discord?challenge=" + tok, "challenge": tok})
}

func (s *Server) confirmDiscordLink(w http.ResponseWriter, r *http.Request) {
	var body struct{ Challenge, ProviderUserID, Username string }
	_ = decodeJSON(r, &body)
	hash := cryptox.HashToken(body.Challenge)
	var uid uuid.UUID
	err := s.Pool.QueryRow(r.Context(), `SELECT user_id FROM identity_link_challenges WHERE token_hash=$1 AND consumed_at IS NULL AND expires_at>now()`, hash).Scan(&uid)
	if err != nil {
		writeErr(w, 400, "invalid", "expired or invalid challenge")
		return
	}
	_, _ = s.Pool.Exec(r.Context(), `INSERT INTO user_identities (user_id, provider, provider_user_id, provider_username) VALUES ($1,'discord',$2,$3) ON CONFLICT (provider, provider_user_id) DO UPDATE SET user_id=EXCLUDED.user_id`, uid, body.ProviderUserID, body.Username)
	_, _ = s.Pool.Exec(r.Context(), `UPDATE identity_link_challenges SET consumed_at=now() WHERE token_hash=$1`, hash)
	s.Audit.Event(r.Context(), &uid, "identity.link", "discord", r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	q := r.URL.Query().Get("q")
	typ := r.URL.Query().Get("type")
	var types []string
	if typ != "" {
		types = strings.Split(typ, ",")
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	hits, err := s.Search.Search(search.WithUser(r.Context(), u.ID), q, types, s.libraryIDs(r.Context(), u), limit)
	if err != nil {
		writeErr(w, 500, "search", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		item := map[string]any{
			"type": h.Type, "id": h.ID, "title": h.Title, "artist": h.Artist, "album": h.Album,
			"duration_ms": h.Duration, "codec": h.Codec, "explicit": h.Explicit, "year": h.Year, "score": h.Score,
			"artwork_url": "/api/v1/tracks/" + h.ID.String() + "/artwork?size=thumb",
		}
		if h.Type == "track" {
			item["stream_url"] = "/api/v1/tracks/" + h.ID.String() + "/stream?token=" + stream.Sign(s.SignKey, h.ID, 15*time.Minute, "original")
			item["qualities"] = []string{"original", "high", "medium", "low"}
		}
		out = append(out, item)
	}
	writeJSON(w, 200, map[string]any{"query": q, "results": out})
}

func (s *Server) listTracks(w http.ResponseWriter, r *http.Request) {
	libs := s.libraryIDs(r.Context(), currentUser(r))
	rows, err := s.Pool.Query(r.Context(), `
		SELECT t.id, t.title, t.duration_ms, t.track_number, t.disc_number, t.year, t.explicit, t.album_id, t.library_id,
		       coalesce(al.title,''), t.created_at,
		       coalesce((SELECT string_agg(ar.name, ', ' ORDER BY ta.position)
		         FROM track_artists ta JOIN artists ar ON ar.id=ta.artist_id
		         WHERE ta.track_id=t.id AND ta.role='primary'),'')
		FROM tracks t LEFT JOIN albums al ON al.id=t.album_id
		WHERE t.library_id = ANY($1) ORDER BY t.created_at DESC LIMIT 10000`, libs)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "title", "duration_ms", "track_number", "disc_number", "year", "explicit", "album_id", "library_id", "album", "created_at", "artist"))
}

func (s *Server) getTrack(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	m, err := s.trackDTO(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "not_found", "track not found")
		return
	}
	writeJSON(w, 200, m)
}

func (s *Server) trackDTO(ctx context.Context, id uuid.UUID) (map[string]any, error) {
	var title string
	var dur, tn, dn int
	var year *int
	var expl *bool
	var albumID, libID uuid.UUID
	err := s.Pool.QueryRow(ctx, `SELECT title, duration_ms, track_number, disc_number, year, explicit, album_id, library_id FROM tracks WHERE id=$1`, id).
		Scan(&title, &dur, &tn, &dn, &year, &expl, &albumID, &libID)
	if err != nil {
		return nil, err
	}
	artists := s.namedArtists(ctx, id)
	return map[string]any{
		"id": id, "title": title, "duration_ms": dur, "track_number": tn, "disc_number": dn, "year": year,
		"explicit": expl, "album_id": albumID, "library_id": libID, "artists": artists,
		"artwork_url": "/api/v1/tracks/" + id.String() + "/artwork?size=page",
		"stream_url":  "/api/v1/tracks/" + id.String() + "/stream",
	}, nil
}

func (s *Server) namedArtists(ctx context.Context, trackID uuid.UUID) []map[string]any {
	rows, err := s.Pool.Query(ctx, `SELECT a.id, a.name, ta.role, ta.position FROM track_artists ta JOIN artists a ON a.id=ta.artist_id WHERE ta.track_id=$1 ORDER BY ta.position`, trackID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var name, role string
		var pos int
		_ = rows.Scan(&id, &name, &role, &pos)
		out = append(out, map[string]any{"id": id, "name": name, "role": role, "position": pos})
	}
	return out
}

func (s *Server) patchTrack(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	if !s.requireTrackLibraryWrite(w, r, id) {
		return
	}
	if !currentUser(r).IsAdmin {
		writeErr(w, 403, "forbidden", "admin or editor required")
		return
	}
	var body map[string]any
	_ = decodeJSON(r, &body)
	if t, ok := body["title"].(string); ok {
		_, _ = s.Pool.Exec(r.Context(), `UPDATE tracks SET title=$2, updated_at=now() WHERE id=$1`, id, t)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) bulkTracks(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs         []uuid.UUID `json:"ids"`
		Genre       *string     `json:"genre"`
		Year        *int        `json:"year"`
		Delete      bool        `json:"delete"`
		All         bool        `json:"all"`
		LibraryID   uuid.UUID   `json:"library_id"`
		DeleteFiles bool        `json:"delete_files"`
	}
	_ = decodeJSON(r, &body)
	if !currentUser(r).IsAdmin {
		writeErr(w, 403, "forbidden", "admin required")
		return
	}
	if body.LibraryID != uuid.Nil && !s.requireLibraryWrite(w, r, body.LibraryID) {
		return
	}
	for _, id := range body.IDs {
		if !s.requireTrackLibraryWrite(w, r, id) {
			return
		}
	}
	if body.Delete || body.All || r.Method == http.MethodDelete {
		s.deleteTracks(w, r, body.IDs, body.All, body.LibraryID, body.DeleteFiles)
		return
	}
	for _, id := range body.IDs {
		if body.Genre != nil {
			_, _ = s.Pool.Exec(r.Context(), `UPDATE tracks SET genre_text=$2 WHERE id=$1`, id, *body.Genre)
		}
		if body.Year != nil {
			_, _ = s.Pool.Exec(r.Context(), `UPDATE tracks SET year=$2 WHERE id=$1`, id, *body.Year)
		}
	}
	writeJSON(w, 200, map[string]int{"updated": len(body.IDs)})
}

func (s *Server) deleteTracks(w http.ResponseWriter, r *http.Request, ids []uuid.UUID, all bool, lib uuid.UUID, deleteFiles bool) {
	ctx := r.Context()
	if !all && len(ids) == 0 {
		writeJSON(w, 200, map[string]any{"deleted": 0, "skipped": []any{}})
		return
	}
	n, skipped, err := s.deleteTrackIDs(ctx, ids, all, lib, deleteFiles)
	if err != nil {
		writeErr(w, 400, "delete", err.Error())
		return
	}
	s.Audit.Event(ctx, &currentUser(r).ID, "tracks.delete", "", r.RemoteAddr, nil)
	writeJSON(w, 200, map[string]any{"deleted": n, "deleted_files": deleteFiles, "skipped": skipped})
}

func (s *Server) serveArtwork(w http.ResponseWriter, r *http.Request, ownerType string, ownerID uuid.UUID) {
	size := r.URL.Query().Get("size")
	if size == "" {
		size = "card"
	}
	var key string
	err := s.Pool.QueryRow(r.Context(), `
		SELECT coalesce(d.storage_key, a.storage_key) FROM artwork_assets a
		LEFT JOIN artwork_derivatives d ON d.artwork_id=a.id AND d.size_name=$3
		WHERE a.owner_type=$1 AND a.owner_id=$2
		ORDER BY a.created_at DESC LIMIT 1`, ownerType, ownerID, size).Scan(&key)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p, err := s.Art.File(key)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, p)
}

func (s *Server) trackArtwork(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	var albumID uuid.UUID
	if err := s.Pool.QueryRow(r.Context(), `SELECT album_id FROM tracks WHERE id=$1`, id).Scan(&albumID); err != nil {
		http.NotFound(w, r)
		return
	}
	s.serveArtwork(w, r, "album", albumID)
}

func (s *Server) albumArtwork(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	s.serveArtwork(w, r, "album", id)
}

func (s *Server) artistArtwork(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	s.serveArtwork(w, r, "artist", id)
}

func (s *Server) playlistArtwork(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	s.serveArtwork(w, r, "playlist", id)
}

func (s *Server) listAlbums(w http.ResponseWriter, r *http.Request) {
	libs := s.libraryIDs(r.Context(), currentUser(r))
	rows, err := s.Pool.Query(r.Context(), `
		SELECT a.id, a.title, a.year, a.edition_title, a.disc_count, a.is_compilation,
		       coalesce(string_agg(DISTINCT ar.name, ', ') FILTER (WHERE ar.name IS NOT NULL), '')
		FROM albums a
		LEFT JOIN album_artists aa ON aa.album_id=a.id
		LEFT JOIN artists ar ON ar.id=aa.artist_id
		WHERE a.library_id = ANY($1) OR a.library_id IS NULL
		GROUP BY a.id
		ORDER BY a.title LIMIT 200`, libs)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "title", "year", "edition_title", "disc_count", "is_compilation", "artist"))
}

func (s *Server) getAlbum(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	var title, edition string
	var year *int
	var discs int
	var comp bool
	if err := s.Pool.QueryRow(r.Context(), `SELECT title, year, edition_title, disc_count, is_compilation FROM albums WHERE id=$1`, id).
		Scan(&title, &year, &edition, &discs, &comp); err != nil {
		writeErr(w, 404, "not_found", "album not found")
		return
	}
	rows, _ := s.Pool.Query(r.Context(), `SELECT id, title, disc_number, track_number, duration_ms FROM tracks WHERE album_id=$1 ORDER BY disc_number, track_number`, id)
	defer rows.Close()
	tracks := scanMaps(rows, "id", "title", "disc_number", "track_number", "duration_ms")
	discsMap := map[int][]map[string]any{}
	for _, t := range tracks {
		d, _ := t["disc_number"].(int32)
		di := int(d)
		if v, ok := t["disc_number"].(int); ok {
			di = v
		}
		discsMap[di] = append(discsMap[di], t)
	}
	var artist string
	_ = s.Pool.QueryRow(r.Context(), `
		SELECT coalesce(string_agg(ar.name, ', '), '') FROM album_artists aa
		JOIN artists ar ON ar.id=aa.artist_id WHERE aa.album_id=$1`, id).Scan(&artist)
	writeJSON(w, 200, map[string]any{
		"id": id, "title": title, "year": year, "edition_title": edition, "disc_count": discs,
		"is_compilation": comp, "artist": artist, "tracks": tracks, "discs": discsMap,
		"artwork_url": "/api/v1/albums/" + id.String() + "/artwork?size=page",
	})
}

func (s *Server) patchAlbum(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	var body struct{ Title, Edition *string }
	_ = decodeJSON(r, &body)
	if body.Title != nil {
		_, _ = s.Pool.Exec(r.Context(), `UPDATE albums SET title=$2 WHERE id=$1`, id, *body.Title)
	}
	if body.Edition != nil {
		_, _ = s.Pool.Exec(r.Context(), `UPDATE albums SET edition_title=$2 WHERE id=$1`, id, *body.Edition)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) mergeAlbums(w http.ResponseWriter, r *http.Request) {
	var body struct{ Into, From uuid.UUID }
	_ = decodeJSON(r, &body)
	if !currentUser(r).IsAdmin {
		writeErr(w, 403, "forbidden", "admin required")
		return
	}
	_, _ = s.Pool.Exec(r.Context(), `UPDATE tracks SET album_id=$1 WHERE album_id=$2`, body.Into, body.From)
	_, _ = s.Pool.Exec(r.Context(), `DELETE FROM albums WHERE id=$1`, body.From)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) listArtists(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `SELECT id, name FROM artists ORDER BY name LIMIT 500`)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "name"))
}

func (s *Server) getArtist(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	var name string
	if err := s.Pool.QueryRow(r.Context(), `SELECT name FROM artists WHERE id=$1`, id).Scan(&name); err != nil {
		writeErr(w, 404, "not_found", "artist not found")
		return
	}
	rows, _ := s.Pool.Query(r.Context(), `SELECT DISTINCT a.id, a.title, a.year, a.is_compilation FROM albums a JOIN album_artists aa ON aa.album_id=a.id WHERE aa.artist_id=$1 ORDER BY a.year DESC NULLS LAST`, id)
	albums := scanMaps(rows, "id", "title", "year", "is_compilation")
	rows.Close()
	trows, _ := s.Pool.Query(r.Context(), `
		SELECT t.id, t.title, t.duration_ms, coalesce(al.title,'')
		FROM tracks t JOIN track_artists ta ON ta.track_id=t.id
		LEFT JOIN albums al ON al.id=t.album_id
		WHERE ta.artist_id=$1 ORDER BY t.created_at DESC LIMIT 20`, id)
	defer trows.Close()
	writeJSON(w, 200, map[string]any{
		"id": id, "name": name, "albums": albums,
		"tracks":      scanMaps(trows, "id", "title", "duration_ms", "album"),
		"artwork_url": "/api/v1/artists/" + id.String() + "/artwork?size=page",
	})
}

func (s *Server) mergeArtists(w http.ResponseWriter, r *http.Request) {
	var body struct{ Into, From uuid.UUID }
	_ = decodeJSON(r, &body)
	_, _ = s.Pool.Exec(r.Context(), `UPDATE track_artists SET artist_id=$1 WHERE artist_id=$2`, body.Into, body.From)
	_, _ = s.Pool.Exec(r.Context(), `UPDATE album_artists SET artist_id=$1 WHERE artist_id=$2`, body.Into, body.From)
	_, _ = s.Pool.Exec(r.Context(), `DELETE FROM artists WHERE id=$1`, body.From)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) listGenres(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.Pool.Query(r.Context(), `SELECT DISTINCT genre_text FROM tracks WHERE genre_text<>'' ORDER BY 1`)
	defer rows.Close()
	var g []string
	for rows.Next() {
		var x string
		_ = rows.Scan(&x)
		g = append(g, x)
	}
	writeJSON(w, 200, g)
}

func (s *Server) listLibraries(w http.ResponseWriter, r *http.Request) {
	ids := s.libraryIDs(r.Context(), currentUser(r))
	rows, err := s.Pool.Query(r.Context(), `
		SELECT l.id, l.name, l.kind, l.read_only, l.organisation_mode, l.is_default, sp.type,
		       (SELECT count(*) FROM tracks t WHERE t.library_id=l.id)
		FROM libraries l
		LEFT JOIN storage_providers sp ON sp.id=l.storage_provider_id
		WHERE l.id = ANY($1) OR $2`, ids, currentUser(r).IsAdmin)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "name", "kind", "read_only", "organisation_mode", "is_default", "storage_type", "track_count"))
}

func (s *Server) importURL(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if !auth.HasPerm(u, "library.import_url") && !auth.HasPerm(u, "library.upload") {
		writeErr(w, 403, "forbidden", "url import not permitted")
		return
	}
	var body ingest.URLPayload
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	list := body.URLs()
	if len(list) == 0 {
		writeErr(w, 400, "invalid", "url is required")
		return
	}
	if len(list) > 200 {
		writeErr(w, 400, "invalid", "at most 200 URLs per import")
		return
	}
	libID, err := s.resolveLibraryID(r.Context(), body.LibraryID)
	if err != nil {
		writeErr(w, 500, "library", "could not create a default library")
		return
	}
	if !s.requireLibraryWrite(w, r, libID) {
		return
	}
	body.LibraryID = libID
	body.URL = ""
	body.Extra = list
	id, err := s.Jobs.Enqueue(r.Context(), "ingest.url", body)
	if err != nil {
		s.writeJobErr(w, err)
		return
	}
	writeJSON(w, 202, map[string]any{"job_id": id, "count": len(list)})
}

func (s *Server) importJobs(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `
		SELECT id, type, status, progress, last_error, created_at,
			COALESCE(jsonb_array_length(payload->'urls'), 0)
		FROM jobs WHERE type='ingest.url' ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "id", "type", "status", "progress", "last_error", "created_at", "count"))
}

func (s *Server) createUpload(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if !auth.HasPerm(u, "library.upload") {
		writeErr(w, 403, "forbidden", "upload not permitted")
		return
	}
	var body struct {
		LibraryID uuid.UUID `json:"library_id"`
		Filename  string    `json:"filename"`
		Size      int64     `json:"size"`
	}
	_ = decodeJSON(r, &body)
	if !scan.IsUploadName(body.Filename) {
		writeErr(w, 400, "invalid", "unsupported audio type")
		return
	}
	libID, err := s.resolveLibraryID(r.Context(), body.LibraryID)
	if err != nil {
		writeErr(w, 500, "library", "could not create a default library")
		return
	}
	if !s.requireLibraryWrite(w, r, libID) {
		return
	}
	if err := s.CheckQuota(r.Context(), u.ID, libID, body.Size); err != nil {
		writeErr(w, 403, "quota", err.Error())
		return
	}
	id, _, err := s.Ingest.CreateUpload(r.Context(), u.ID, libID, body.Filename, body.Size)
	if err != nil {
		writeErr(w, 500, "upload", err.Error())
		return
	}
	w.Header().Set("Location", "/api/v1/uploads/"+id.String())
	w.Header().Set("Tus-Resumable", "1.0.0")
	writeJSON(w, 201, map[string]any{"id": id})
}

func (s *Server) patchUpload(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	off, _ := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
	n, err := s.Ingest.PatchUpload(r.Context(), id, off, io.LimitReader(r.Body, 200<<20))
	if err != nil {
		writeErr(w, 400, "upload", err.Error())
		return
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(n, 10))
	writeJSON(w, 200, map[string]int64{"offset": n})
}

func (s *Server) completeUpload(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	if s.Pool != nil {
		var libID uuid.UUID
		if err := s.Pool.QueryRow(r.Context(), `SELECT library_id FROM upload_sessions WHERE id=$1`, id).Scan(&libID); err == nil {
			if !s.requireLibraryWrite(w, r, libID) {
				return
			}
		}
	}
	var body struct {
		Scan *bool `json:"scan"`
	}
	_ = decodeJSON(r, &body)
	doScan := true
	if body.Scan != nil {
		doScan = *body.Scan
	}
	if err := s.Ingest.FinishUpload(r.Context(), id, s.ProviderFor, doScan); err != nil {
		if errors.Is(err, ingest.ErrZipQueued) {
			jid, qerr := s.Jobs.Enqueue(r.Context(), "ingest.zip", ingest.ZipPayload{SessionID: id})
			if qerr != nil {
				s.writeJobErr(w, qerr)
				return
			}
			writeJSON(w, 202, map[string]any{"ok": true, "zip": true, "job_id": jid})
			return
		}
		writeErr(w, 500, "upload", err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) finalizeUploads(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if !auth.HasPerm(u, "library.upload") {
		writeErr(w, 403, "forbidden", "upload not permitted")
		return
	}
	libID, err := s.resolveLibraryID(r.Context(), uuid.Nil)
	if err != nil {
		writeErr(w, 500, "library", "could not create a default library")
		return
	}
	if !s.requireLibraryWrite(w, r, libID) {
		return
	}
	id, err := s.Jobs.Enqueue(r.Context(), "library.scan", scan.Payload{LibraryID: libID, Kind: "upload"})
	if err != nil {
		s.writeJobErr(w, err)
		return
	}
	writeJSON(w, 202, map[string]any{"ok": true, "job_id": id})
}

func (s *Server) duplicates(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.Pool.Query(r.Context(), `
		SELECT content_hash, array_agg(storage_key), count(*) FROM track_files
		WHERE content_hash IS NOT NULL GROUP BY content_hash HAVING count(*)>1 LIMIT 100`)
	defer rows.Close()
	writeJSON(w, 200, scanMaps(rows, "hash", "keys", "count"))
}

func shaHex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func (s *Server) openSubsonic(w http.ResponseWriter, r *http.Request) {
	if !s.Cfg.OpenSubsonic {
		http.NotFound(w, r)
		return
	}
	(&opensubsonic.Router{Pool: s.Pool}).ServeHTTP(w, r)
}
