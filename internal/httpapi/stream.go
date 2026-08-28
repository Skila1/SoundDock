package httpapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
	"github.com/sounddock/sounddock/internal/mediabusy"
	"github.com/sounddock/sounddock/internal/stream"
)

func (s *Server) streamTokens(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u == nil || rejectIfDisabled(w, u) {
		if u == nil {
			writeErr(w, 401, "unauthorized", "authentication required")
		}
		return
	}
	var body struct {
		TrackID uuid.UUID `json:"track_id"`
		Quality string    `json:"quality"`
	}
	_ = decodeJSON(r, &body)
	if body.TrackID == uuid.Nil {
		writeErr(w, 400, "invalid", "track_id required")
		return
	}
	var libID uuid.UUID
	if err := s.Pool.QueryRow(r.Context(), `SELECT library_id FROM tracks WHERE id=$1`, body.TrackID).Scan(&libID); err != nil {
		writeErr(w, 404, "not_found", "track not found")
		return
	}
	if !s.userHasLibraryAction(r.Context(), u, libID, "stream") {
		writeErr(w, http.StatusForbidden, "library_grant", "library stream not granted")
		return
	}
	tok := stream.Sign(s.SignKey, u.ID, body.TrackID, 15*time.Minute, body.Quality)
	if tok == "" {
		writeErr(w, 500, "token", "could not mint stream token")
		return
	}
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
	var offlineUser, tokenUser uuid.UUID
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
		if err != nil || t.TrackID != id {
			writeErr(w, 401, "unauthorized", "stream token required")
			return
		}
		authed = true
		tokenUser = t.UserID
		if t.Quality != "" {
			quality = t.Quality
		}
	}
	if !authed {
		if u := currentUser(r); u != nil {
			if rejectIfDisabled(w, u) {
				return
			}
			authed = true
		}
	}
	if !authed {
		if c, err := r.Cookie("sd_session"); err == nil && s.Auth != nil {
			if u, _, err := s.Auth.SessionUser(r.Context(), c.Value); err == nil {
				if rejectIfDisabled(w, u) {
					return
				}
				authed = true
			}
		}
		if !authed {
			if b := bearer(r); b != "" {
				if isAPIToken(b) {
					if u, err := s.apiKeyUser(r.Context(), b); err == nil {
						if rejectIfDisabled(w, u) {
							return
						}
						authed = true
					}
				} else if s.Auth != nil {
					if u, _, err := s.Auth.SessionUser(r.Context(), b); err == nil {
						if rejectIfDisabled(w, u) {
							return
						}
						authed = true
					}
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

	u := s.streamPrincipal(r, offlineUser, tokenUser)
	if tokenUser != uuid.Nil || offlineUser != uuid.Nil {
		if u == nil {
			writeErr(w, 403, "disabled", "account disabled")
			return
		}
		if rejectIfDisabled(w, u) {
			return
		}
	}
	if u != nil {
		if rejectIfDisabled(w, u) {
			return
		}
		if !auth.HasPerm(u, "tracks.stream") {
			writeErr(w, http.StatusForbidden, "forbidden", "tracks.stream not permitted")
			return
		}
		if !s.userHasLibraryAction(r.Context(), u, libID, "stream") {
			writeErr(w, http.StatusForbidden, "library_grant", "library stream not granted")
			return
		}
	} else {
		writeErr(w, 401, "unauthorized", "stream token required")
		return
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
	kind := mediabusy.KindHTTPStream
	if tokenUser != uuid.Nil {
		kind = mediabusy.KindHMACStream
	}
	releaseBusy := s.MediaBusy.Acquire(r.Context(), id, kind, mediabusy.NewHolder(kind))
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
// HMAC tokens embed user id (stream-v2). Disabled users are never a valid principal.
func (s *Server) streamPrincipal(r *http.Request, offlineUser, tokenUser uuid.UUID) *auth.User {
	if tokenUser != uuid.Nil {
		return s.loadStreamUser(r.Context(), tokenUser)
	}
	if u := currentUser(r); u != nil {
		if u.Disabled {
			return u
		}
		return u
	}
	if s.Auth != nil {
		if c, err := r.Cookie("sd_session"); err == nil && c.Value != "" {
			if u, _, err := s.Auth.SessionUser(r.Context(), c.Value); err == nil && u != nil {
				return u
			}
		}
	}
	if b := bearer(r); b != "" {
		if isAPIToken(b) && s.Pool != nil {
			if u, err := s.apiKeyUser(r.Context(), b); err == nil && u != nil {
				return u
			}
		} else if s.Auth != nil {
			if u, _, err := s.Auth.SessionUser(r.Context(), b); err == nil && u != nil {
				return u
			}
		}
	}
	if offlineUser != uuid.Nil {
		if u := s.loadStreamUser(r.Context(), offlineUser); u != nil {
			return u
		}
		return &auth.User{ID: offlineUser, Permissions: []string{"tracks.stream"}}
	}
	return nil
}

func (s *Server) loadStreamUser(ctx context.Context, id uuid.UUID) *auth.User {
	if s.Auth != nil {
		if u, err := s.Auth.GetUser(ctx, id); err == nil && u != nil {
			return u
		}
	}
	if s.Pool == nil || id == uuid.Nil {
		return nil
	}
	u := &auth.User{ID: id, Permissions: []string{"tracks.read", "tracks.stream"}}
	if err := s.Pool.QueryRow(ctx, `SELECT username, disabled FROM users WHERE id=$1`, id).Scan(&u.Username, &u.Disabled); err != nil {
		return nil
	}
	var admin bool
	_ = s.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM user_roles ur JOIN roles ro ON ro.id=ur.role_id
			WHERE ur.user_id=$1 AND ro.name='Administrator'
		)`, id).Scan(&admin)
	u.IsAdmin = admin
	return u
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
	state := ""
	if p, err := s.lookupPlayability(r.Context(), id); err == nil {
		state = p.State
	}
	status, code, msg := streamMissingCodes(acq, state)
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
