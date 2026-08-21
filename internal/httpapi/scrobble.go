package httpapi

import (
	"net/http"

	"github.com/sounddock/sounddock/internal/scrobble"
)

func (s *Server) scrobbleSvc() *scrobble.Service {
	return scrobble.New(s.Pool, s.Box, s.Search)
}

func (s *Server) getScrobble(w http.ResponseWriter, r *http.Request) {
	a, _ := s.scrobbleSvc().Account(r.Context(), currentUser(r).ID)
	writeJSON(w, 200, map[string]any{
		"lastfm_username":        a.LastFMUsername,
		"lastfm_connected":       a.LastFMConnected,
		"listenbrainz_username":  a.ListenBrainzUsername,
		"listenbrainz_connected": a.ListenBrainzConnected,
		"presence_enabled":       a.PresenceEnabled,
		"lastfm_configured":      osLastFMConfigured(s, r),
	})
}

func osLastFMConfigured(s *Server, r *http.Request) bool {
	key, secret := s.scrobbleSvc().LastFMConfigured(r.Context())
	return key && secret
}

func (s *Server) putScrobble(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LastFMUsername         *string `json:"lastfm_username"`
		LastFMPassword         *string `json:"lastfm_password"`
		LastFMDisconnect       bool    `json:"lastfm_disconnect"`
		ListenBrainzToken      *string `json:"listenbrainz_token"`
		ListenBrainzUsername   *string `json:"listenbrainz_username"`
		ListenBrainzDisconnect bool    `json:"listenbrainz_disconnect"`
		PresenceEnabled        *bool   `json:"presence_enabled"`
		LastFMAPIKey           *string `json:"lastfm_api_key"`
		LastFMAPISecret        *string `json:"lastfm_api_secret"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	svc := s.scrobbleSvc()
	u := currentUser(r)
	if body.LastFMAPIKey != nil && body.LastFMAPISecret != nil && u.IsAdmin {
		if err := svc.SaveLastFMKeys(r.Context(), *body.LastFMAPIKey, *body.LastFMAPISecret); err != nil {
			writeErr(w, 500, "db", err.Error())
			return
		}
	}
	if body.LastFMDisconnect {
		_ = svc.DisconnectLastFM(r.Context(), u.ID)
	} else if body.LastFMUsername != nil && body.LastFMPassword != nil && *body.LastFMPassword != "" {
		if err := svc.ConnectLastFM(r.Context(), u.ID, *body.LastFMUsername, *body.LastFMPassword); err != nil {
			writeErr(w, 400, "lastfm", err.Error())
			return
		}
	}
	if body.ListenBrainzDisconnect {
		_ = svc.DisconnectListenBrainz(r.Context(), u.ID)
	} else if body.ListenBrainzToken != nil && *body.ListenBrainzToken != "" {
		uname := ""
		if body.ListenBrainzUsername != nil {
			uname = *body.ListenBrainzUsername
		}
		if err := svc.ConnectListenBrainz(r.Context(), u.ID, *body.ListenBrainzToken, uname); err != nil {
			writeErr(w, 400, "listenbrainz", err.Error())
			return
		}
	}
	if body.PresenceEnabled != nil {
		_ = svc.SetPresence(r.Context(), u.ID, *body.PresenceEnabled)
	}
	s.getScrobble(w, r)
}

func (s *Server) importScrobbleHistory(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Provider string `json:"provider"`
	}
	_ = decodeJSON(r, &body)
	res, err := s.scrobbleSvc().ImportHistory(r.Context(), currentUser(r).ID, body.Provider)
	if err != nil {
		writeErr(w, 400, "import", err.Error())
		return
	}
	writeJSON(w, 200, res)
}
