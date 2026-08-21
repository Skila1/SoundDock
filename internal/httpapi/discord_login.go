package httpapi

import (
	"net/http"
	"net/url"
	"time"

	"github.com/sounddock/sounddock/internal/auth"
	"github.com/sounddock/sounddock/internal/external"
)

func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"needed":            false,
		"discord_only":      true,
		"discord_configured": s.Cfg.DiscordClientID != "" && s.Cfg.DiscordClientSecret != "",
	})
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	writeErr(w, 410, "discord_only", "SoundDock uses Discord sign-in. Set SOUNDDOCK_ADMIN_DISCORD_ID in .env.")
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	writeErr(w, 400, "discord_only", "Use Discord to sign in")
}

func (s *Server) discordLogin(w http.ResponseWriter, r *http.Request) {
	if s.Cfg.DiscordClientID == "" || s.Cfg.DiscordClientSecret == "" {
		writeErr(w, 503, "not_configured", "Set SOUNDDOCK_DISCORD_CLIENT_ID and SOUNDDOCK_DISCORD_CLIENT_SECRET")
		return
	}
	state := external.NewState()
	ver, ch := external.PKCE()
	if err := auth.StoreLoginState(r.Context(), s.Pool, s.Box, state, ver); err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	redir := auth.DiscordCallbackURL(s.Cfg.PublicURL)
	http.Redirect(w, r, auth.DiscordAuthURL(s.Cfg.DiscordClientID, redir, state, ch), http.StatusFound)
}

func (s *Server) discordLoginCallback(w http.ResponseWriter, r *http.Request) {
	fail := func(msg string) {
		http.Redirect(w, r, s.Cfg.PublicURL+"/?error="+url.QueryEscape(msg), http.StatusFound)
	}
	if r.URL.Query().Get("error") != "" || r.URL.Query().Get("code") == "" {
		fail("oauth_denied")
		return
	}
	state := r.URL.Query().Get("state")
	ver, err := auth.TakeLoginState(r.Context(), s.Pool, s.Box, state)
	if err != nil {
		fail("invalid_state")
		return
	}
	redir := auth.DiscordCallbackURL(s.Cfg.PublicURL)
	prof, err := auth.ExchangeDiscordCode(r.Context(), s.Cfg.DiscordClientID, s.Cfg.DiscordClientSecret, redir, r.URL.Query().Get("code"), ver)
	if err != nil {
		fail("token_exchange")
		return
	}
	u, err := s.Auth.UpsertDiscordUser(r.Context(), prof, s.Cfg.AdminDiscordIDs)
	if err != nil {
		fail("account")
		return
	}
	tok, sess, err := s.Auth.CreateSession(r.Context(), u.ID, r.UserAgent(), r.RemoteAddr, 30*24*time.Hour)
	if err != nil {
		fail("session")
		return
	}
	s.setSessionCookie(w, tok, sess.ExpiresAt)
	s.Audit.Event(r.Context(), &u.ID, "login.discord", prof.ID, r.RemoteAddr, nil)
	http.Redirect(w, r, s.Cfg.PublicURL+"/", http.StatusFound)
}
