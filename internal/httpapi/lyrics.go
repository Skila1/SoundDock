package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/lyrics"
	"github.com/sounddock/sounddock/internal/whisper"
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
	if gen := s.lyricsGenerateState(ctx, r, id, res); gen != nil {
		out["generate"] = gen
	}
	writeJSON(w, http.StatusOK, out)
}

// lyricsGenerateState tells the player whether it may offer "Generate
// lyrics" for this track, and how the latest attempt went. Nil hides it.
func (s *Server) lyricsGenerateState(ctx context.Context, r *http.Request, id uuid.UUID, res lyrics.Result) map[string]any {
	if s.Whisper == nil || !canEditMeta(currentUser(r)) {
		return nil
	}
	if !lyrics.LoadConfig(ctx, s.Pool).GenerateEnabled {
		return nil
	}
	protected := res.Source == lyrics.SourceManual || res.Source == lyrics.SourceUser || s.fieldLocked(ctx, "track", id, "lyrics")
	out := map[string]any{"available": !protected, "status": "idle"}
	var status string
	var lastErr *string
	err := s.Pool.QueryRow(ctx, `
		SELECT status, last_error FROM jobs
		WHERE type=$1 AND payload->>'track_id'=$2
		  AND (status IN ('queued','running','retry') OR updated_at > now() - interval '1 day')
		ORDER BY created_at DESC LIMIT 1`, lyrics.GenerateJob, id.String()).Scan(&status, &lastErr)
	if err == nil {
		switch status {
		case "queued", "retry":
			out["status"] = "queued"
		case "running":
			out["status"] = "running"
		case "failed":
			out["status"] = "failed"
			if lastErr != nil {
				out["error"] = *lastErr
			}
		}
	}
	return out
}

// generateTrackLyrics queues a whisper.cpp transcription of the track.
// Admin only, like other lyrics edits; hand-edited lyrics are never replaced.
func (s *Server) generateTrackLyrics(w http.ResponseWriter, r *http.Request) {
	if !s.requireMetaEditor(w, r) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid", "id")
		return
	}
	if !s.requireTrackLibraryWrite(w, r, id) {
		return
	}
	ctx := r.Context()
	if s.Whisper == nil {
		writeErr(w, http.StatusServiceUnavailable, "not_configured", "lyrics generation is not set up: start the whisper container and set SD_WHISPER_URL")
		return
	}
	if !lyrics.LoadConfig(ctx, s.Pool).GenerateEnabled {
		writeErr(w, http.StatusConflict, "disabled", "lyrics generation is turned off in Admin > Media Settings")
		return
	}
	if _, ok := s.lyricsMeta(ctx, id); !ok {
		writeErr(w, http.StatusNotFound, "not_found", "track not found")
		return
	}
	if s.fieldLocked(ctx, "track", id, "lyrics") {
		writeErr(w, http.StatusLocked, "locked", "lyrics are locked for this track")
		return
	}
	var n int
	if s.Pool.QueryRow(ctx, `SELECT 1 FROM lyrics WHERE track_id=$1 AND source IN ('manual','user') LIMIT 1`, id).Scan(&n) == nil {
		writeErr(w, http.StatusConflict, "protected", "this track has hand-edited lyrics, which are kept")
		return
	}
	if err := s.Whisper.Health(ctx); err != nil {
		msg := "whisper.cpp is not reachable"
		if errors.Is(err, whisper.ErrLoading) {
			msg = "whisper.cpp is still loading its model, try again in a minute"
		}
		writeErr(w, http.StatusServiceUnavailable, "unavailable", msg)
		return
	}
	jobID, err := s.Jobs.EnqueueCoalesced(ctx, lyrics.GenerateJob, lyrics.GenerateJob+"|"+id.String(), lyrics.GeneratePayload{TrackID: id})
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "queue", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID, "status": "queued"})
}

// adminLyricsGenerator reports whether the whisper.cpp container answers.
func (s *Server) adminLyricsGenerator(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"configured": s.Whisper != nil, "status": "not_configured"}
	if s.Whisper != nil {
		out["url"] = s.Whisper.URL()
		switch err := s.Whisper.Health(r.Context()); {
		case err == nil:
			out["status"] = "ok"
		case errors.Is(err, whisper.ErrLoading):
			out["status"] = "loading"
		default:
			out["status"] = "unreachable"
			out["error"] = err.Error()
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
	var raw map[string]json.RawMessage
	if err := decodeJSON(r, &raw); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid", "invalid body")
		return
	}
	var body lyrics.Config
	b, _ := json.Marshal(raw)
	if err := json.Unmarshal(b, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid", "invalid body")
		return
	}
	// Older clients only send the lookup fields; keep the generator settings.
	current := lyrics.LoadConfig(r.Context(), s.Pool)
	if _, ok := raw["generate_enabled"]; !ok {
		body.GenerateEnabled = current.GenerateEnabled
	}
	if _, ok := raw["generate_language"]; !ok {
		body.GenerateLanguage = current.GenerateLanguage
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
