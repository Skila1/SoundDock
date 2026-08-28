package httpapi

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/lyrics"
)

func (s *Server) getTrackLyrics(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid", "id")
		return
	}
	if !s.requireTrackLibrary(w, r, id, "read") {
		return
	}
	ctx := r.Context()
	meta, ok := s.lyricsMeta(ctx, id)
	if !ok {
		writeErr(w, http.StatusNotFound, "not_found", "track not found")
		return
	}
	res := lyrics.New(s.Pool, s.Log).WithLocalDir(filepath.Join(s.Cfg.DataDir, "lyrics")).GetLyrics(ctx, meta)
	out := map[string]any{
		"body":   res.Body,
		"timed":  res.Timed,
		"source": res.Source,
	}
	if res.Timed {
		if lines := lyrics.ParseLines(res.Body); len(lines) > 0 {
			out["lines"] = lines
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) lyricsMeta(ctx context.Context, id uuid.UUID) (lyrics.Meta, bool) {
	meta := lyrics.Meta{TrackID: id}
	if s == nil || s.Pool == nil {
		return meta, false
	}
	var title, album, artist string
	var dur int
	err := s.Pool.QueryRow(ctx, `
		SELECT t.title, t.duration_ms, coalesce(al.title, ''),
		       coalesce((
		         SELECT string_agg(a.name, ', ' ORDER BY ta.position)
		         FROM track_artists ta JOIN artists a ON a.id=ta.artist_id
		         WHERE ta.track_id=t.id
		       ), '')
		FROM tracks t
		LEFT JOIN albums al ON al.id=t.album_id
		WHERE t.id=$1`, id).Scan(&title, &dur, &album, &artist)
	if err != nil {
		return meta, false
	}
	return lyrics.Meta{
		Title:      strings.TrimSpace(title),
		Artist:     strings.TrimSpace(artist),
		Album:      strings.TrimSpace(album),
		DurationMS: dur,
		TrackID:    id,
	}, true
}

func (s *Server) adminGetLyrics(w http.ResponseWriter, r *http.Request) {
	lyrics.EnsurePerm(r.Context(), s.Pool)
	writeJSON(w, http.StatusOK, lyrics.LoadConfig(r.Context(), s.Pool))
}

func (s *Server) adminPutLyrics(w http.ResponseWriter, r *http.Request) {
	lyrics.EnsurePerm(r.Context(), s.Pool)
	var body lyrics.Config
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid", "invalid body")
		return
	}
	if body.ExternalEnabled || body.Enabled {
		body.ExternalEnabled = true
		body.Enabled = true
		canon, err := lyrics.NormalizeProviderURL(body.ProviderURL)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid", err.Error())
			return
		}
		if canon == "" {
			writeErr(w, http.StatusBadRequest, "invalid", "provider_url is required when the external provider is on")
			return
		}
		body.ProviderURL = canon
	} else {
		body.ExternalEnabled = false
		body.Enabled = false
		body.ProviderURL = ""
	}
	if err := lyrics.StoreConfig(r.Context(), s.Pool, body); err != nil {
		writeErr(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, lyrics.LoadConfig(r.Context(), s.Pool))
}
