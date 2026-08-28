package httpapi

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
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
	var offlineUser uuid.UUID
	if offlineTok != "" {
		claims, err := s.VerifyOfflineToken(r.Context(), offlineTok)
		if err != nil || claims.TrackID != id {
			writeErr(w, 401, "unauthorized", "offline token required")
			return
		}
		authed = true
		offlineUser = claims.UserID
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
			if s.Auth != nil {
				if _, _, err := s.Auth.SessionUser(r.Context(), c.Value); err == nil {
					authed = true
				}
			}
		}
		if b := bearer(r); b != "" {
			if _, err := s.apiKeyUser(r.Context(), b); err == nil {
				authed = true
			} else if s.Auth != nil {
				if _, _, err := s.Auth.SessionUser(r.Context(), b); err == nil {
					authed = true
				}
			}
		}
	}
	if !authed {
		writeErr(w, 401, "unauthorized", "stream token required")
		return
	}
	quality = s.CapStreamQuality(r, quality)

	var libID uuid.UUID
	err := s.Pool.QueryRow(r.Context(), `SELECT library_id FROM tracks WHERE id=$1`, id).Scan(&libID)
	if err != nil {
		s.writeStreamMediaUnavailable(w, r, id)
		return
	}

	if u := s.streamPrincipal(r, offlineUser); u != nil {
		if !auth.HasPerm(u, "tracks.stream") {
			writeErr(w, http.StatusForbidden, "forbidden", "tracks.stream not permitted")
			return
		}
		if !s.userHasLibraryAction(r.Context(), u, libID, "stream") {
			writeErr(w, http.StatusForbidden, "library_grant", "library stream not granted")
			return
		}
	}

	var fileID uuid.UUID
	var key, codec string
	err = s.Pool.QueryRow(r.Context(), `SELECT id, library_id, storage_key, codec FROM track_files WHERE track_id=$1 AND quality='original' AND deleted_at IS NULL LIMIT 1`, id).Scan(&fileID, &libID, &key, &codec)
	if err != nil {
		s.writeStreamMediaUnavailable(w, r, id)
		return
	}
	if !s.AcquireStreamSlot(r) {
		writeErr(w, 429, "stream_limit", "too many concurrent streams")
		return
	}
	defer s.ReleaseStreamSlot(r)
	releaseBusy := s.MediaBusy.Hold(id)
	defer releaseBusy()
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
		s.writeStreamMediaUnavailable(w, r, id)
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

// streamPrincipal is the user whose library_grants apply to this stream GET.
// Cookie / API-key / bearer session users are always enforced. HMAC-only
// (HTMLAudio ?token= with no cookie) has no cheap user join and stays valid
// for that track id. Offline tokens carry user_id so they are enforced.
func (s *Server) streamPrincipal(r *http.Request, offlineUser uuid.UUID) *auth.User {
	if u := currentUser(r); u != nil && !u.Disabled {
		return u
	}
	if s.Auth != nil {
		if c, err := r.Cookie("sd_session"); err == nil && c.Value != "" {
			if u, _, err := s.Auth.SessionUser(r.Context(), c.Value); err == nil && u != nil && !u.Disabled {
				return u
			}
		}
	}
	if b := bearer(r); b != "" {
		if isAPIToken(b) && s.Pool != nil {
			if u, err := s.apiKeyUser(r.Context(), b); err == nil && u != nil && !u.Disabled {
				return u
			}
		} else if s.Auth != nil {
			if u, _, err := s.Auth.SessionUser(r.Context(), b); err == nil && u != nil && !u.Disabled {
				return u
			}
		}
	}
	if offlineUser != uuid.Nil {
		if s.Auth != nil {
			if u, err := s.Auth.GetUser(r.Context(), offlineUser); err == nil && u != nil && !u.Disabled {
				return u
			}
		}
		return &auth.User{ID: offlineUser, Permissions: []string{"tracks.stream"}}
	}
	return nil
}

// writeStreamMediaUnavailable maps a missing original to 409 (managed
// youtube/scapex stub) or 404 media_unavailable_external (NAS/local hole).
// Does not start ScapeX.
func (s *Server) writeStreamMediaUnavailable(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	acq, found := s.lookupTrackAcquisition(r.Context(), id)
	if !found {
		writeErr(w, http.StatusNotFound, "not_found", "media missing")
		return
	}
	status, code, msg := streamMissingCodes(acq)
	writeErr(w, status, code, msg)
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
