package httpapi

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/stream"
)

func (s *Server) streamTokens(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TrackID uuid.UUID `json:"track_id"`
		Quality string    `json:"quality"`
	}
	_ = decodeJSON(r, &body)
	tok := stream.Sign(s.SignKey, body.TrackID, 15*time.Minute, body.Quality)
	writeJSON(w, 200, map[string]string{"token": tok, "url": "/api/v1/tracks/" + body.TrackID.String() + "/stream?token=" + tok})
}

func (s *Server) streamTrack(w http.ResponseWriter, r *http.Request) {
	id, _ := uuid.Parse(chi.URLParam(r, "id"))
	tok := r.URL.Query().Get("token")
	offlineTok := r.URL.Query().Get("offline_token")
	quality := r.URL.Query().Get("quality")
	if quality == "" {
		quality = "original"
	}
	authed := false
	if offlineTok != "" {
		claims, err := s.VerifyOfflineToken(r.Context(), offlineTok)
		if err != nil || claims.TrackID != id {
			writeErr(w, 401, "unauthorized", "offline token required")
			return
		}
		authed = true
	}
	if !authed && tok != "" {
		t, err := stream.Verify(s.SignKey, tok)
		if err == nil && t.TrackID == id {
			authed = true
			if t.Quality != "" {
				quality = t.Quality
			}
		}
	}
	if !authed {
		if c, err := r.Cookie("sd_session"); err == nil {
			if _, _, err := s.Auth.SessionUser(r.Context(), c.Value); err == nil {
				authed = true
			}
		}
		if b := bearer(r); b != "" {
			if _, err := s.apiKeyUser(r.Context(), b); err == nil {
				authed = true
			} else if _, _, err := s.Auth.SessionUser(r.Context(), b); err == nil {
				authed = true
			}
		}
	}
	if !authed {
		writeErr(w, 401, "unauthorized", "stream token required")
		return
	}
	quality = s.CapStreamQuality(r, quality)
	if !s.AcquireStreamSlot(r) {
		writeErr(w, 429, "stream_limit", "too many concurrent streams")
		return
	}
	defer s.ReleaseStreamSlot(r)

	var fileID, libID uuid.UUID
	var key, codec string
	err := s.Pool.QueryRow(r.Context(), `SELECT id, library_id, storage_key, codec FROM track_files WHERE track_id=$1 AND quality='original' AND deleted_at IS NULL LIMIT 1`, id).Scan(&fileID, &libID, &key, &codec)
	if err != nil {
		writeErr(w, 404, "not_found", "media missing")
		return
	}
	prov, _, _, err := s.ProviderFor(r.Context(), libID)
	if err != nil {
		writeErr(w, 500, "storage", err.Error())
		return
	}
	if quality != "original" && s.TX != nil {
		if src, ok := prov.(interface{ Root() string }); ok {
			p := strings.TrimRight(src.Root(), "/\\") + "/" + strings.ReplaceAll(key, "/", string('/'))
			if cached, err := s.TX.TranscodeToCache(r.Context(), fileID, p, quality); err == nil {
				http.ServeFile(w, r, cached)
				return
			}
		}
	}
	rc, info, err := prov.Open(r.Context(), key)
	if err != nil {
		writeErr(w, 404, "not_found", "file missing")
		return
	}
	defer rc.Close()
	mod := time.Now()
	if info != nil && !info.ModTime.IsZero() {
		mod = info.ModTime
	}
	rs, ok := rc.(io.ReadSeeker)
	if !ok {
		w.Header().Set("Content-Type", mimeFor(codec, key))
		io.Copy(w, rc)
		return
	}
	w.Header().Set("Content-Type", mimeFor(codec, key))
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, key, mod, rs)
}

func mimeFor(codec, key string) string {
	switch {
	case strings.Contains(codec, "mpeg") || strings.HasSuffix(key, ".mp3"):
		return "audio/mpeg"
	case strings.Contains(codec, "flac") || strings.HasSuffix(key, ".flac"):
		return "audio/flac"
	case strings.HasSuffix(key, ".ogg") || strings.HasSuffix(key, ".opus"):
		return "audio/ogg"
	case strings.HasSuffix(key, ".wav"):
		return "audio/wav"
	case strings.HasSuffix(key, ".m4a") || strings.HasSuffix(key, ".aac"):
		return "audio/mp4"
	default:
		return "application/octet-stream"
	}
}
