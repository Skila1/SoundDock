package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/radio"
)

func (s *Server) createAlbum(w http.ResponseWriter, r *http.Request) {
	if !s.requireMetaEditor(w, r) {
		return
	}
	var body struct {
		Title     string      `json:"title"`
		Year      *int        `json:"year"`
		LibraryID uuid.UUID   `json:"library_id"`
		Artist    string      `json:"artist"`
		TrackIDs  []uuid.UUID `json:"track_ids"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	title := strings.TrimSpace(body.Title)
	if title == "" {
		writeErr(w, 400, "invalid", "title required")
		return
	}
	libID := body.LibraryID
	if libID == uuid.Nil && len(body.TrackIDs) > 0 {
		_ = s.Pool.QueryRow(r.Context(), `SELECT library_id FROM tracks WHERE id=$1`, body.TrackIDs[0]).Scan(&libID)
	}
	if libID == uuid.Nil {
		var err error
		libID, err = s.resolveLibraryID(r.Context(), uuid.Nil)
		if err != nil {
			writeErr(w, 400, "invalid", "library_id required")
			return
		}
	}
	if !s.requireLibraryWrite(w, r, libID) {
		return
	}
	for _, id := range body.TrackIDs {
		if !s.requireTrackLibraryWrite(w, r, id) {
			return
		}
	}
	var albumID uuid.UUID
	err := s.Pool.QueryRow(r.Context(), `
		INSERT INTO albums (title, year, library_id) VALUES ($1,$2,$3) RETURNING id`,
		title, body.Year, libID).Scan(&albumID)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if names := strings.TrimSpace(body.Artist); names != "" {
		s.replaceAlbumArtists(r.Context(), albumID, names)
	}
	if len(body.TrackIDs) > 0 {
		_, _ = s.Pool.Exec(r.Context(), `UPDATE tracks SET album_id=$1, updated_at=now() WHERE id = ANY($2)`, albumID, body.TrackIDs)
	}
	writeJSON(w, 201, map[string]any{"id": albumID, "title": title, "library_id": libID, "ok": true})
}

func (s *Server) deleteAlbum(w http.ResponseWriter, r *http.Request) {
	if !s.requireMetaEditor(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "id")
		return
	}
	if !s.requireAlbumLibrary(w, r, id, "write") {
		return
	}
	_, err = s.Pool.Exec(r.Context(), `DELETE FROM albums WHERE id=$1`, id)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) replaceAlbumArtists(ctx context.Context, albumID uuid.UUID, names string) {
	_, _ = s.Pool.Exec(ctx, `DELETE FROM album_artists WHERE album_id=$1`, albumID)
	pos := 0
	for _, raw := range strings.Split(names, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		var aid uuid.UUID
		err := s.Pool.QueryRow(ctx, `SELECT id FROM artists WHERE lower(name)=lower($1) LIMIT 1`, name).Scan(&aid)
		if err != nil {
			_ = s.Pool.QueryRow(ctx, `INSERT INTO artists (name) VALUES ($1) RETURNING id`, name).Scan(&aid)
		}
		if aid != uuid.Nil {
			_, _ = s.Pool.Exec(ctx, `
				INSERT INTO album_artists (album_id, artist_id, role, position)
				VALUES ($1,$2,'album_artist',$3) ON CONFLICT DO NOTHING`, albumID, aid, pos)
			pos++
		}
	}
}

func (s *Server) syncTrackGenres(ctx context.Context, trackID uuid.UUID, genreText string) {
	tokens := radio.GenreTokens(genreText, nil)
	_, _ = s.Pool.Exec(ctx, `DELETE FROM track_genres WHERE track_id=$1`, trackID)
	for _, name := range tokens {
		var gid uuid.UUID
		err := s.Pool.QueryRow(ctx, `SELECT id FROM genres WHERE lower(name)=lower($1)`, name).Scan(&gid)
		if err != nil {
			_ = s.Pool.QueryRow(ctx, `INSERT INTO genres (name) VALUES ($1) ON CONFLICT (name) DO UPDATE SET name=EXCLUDED.name RETURNING id`, name).Scan(&gid)
		}
		if gid != uuid.Nil {
			_, _ = s.Pool.Exec(ctx, `INSERT INTO track_genres (track_id, genre_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, trackID, gid)
		}
	}
}
