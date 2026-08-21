package httpapi

import (
	"net/http"
	"net/url"
	"time"

	"github.com/sounddock/sounddock/internal/auth"
	"github.com/sounddock/sounddock/internal/external"
)

func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	needed, _ := s.Auth.SetupNeeded(r.Context())
	if s.Cfg.DiscordLoginEnabled() {
		needed = false
	}
	writeJSON(w, 200, map[string]any{
		"needed":             needed,
		"discord_enabled":    s.Cfg.DiscordEnabled,
		"discord_configured": s.Cfg.DiscordLoginEnabled(),
	})
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if s.Cfg.DiscordLoginEnabled() {
		writeErr(w, 410, "discord_login", "This instance uses Discord sign-in. The first administrator is SD_ADMIN_DISCORD_ID.")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Email    string `json:"email"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Username == "" || body.Password == "" {
		writeErr(w, 400, "invalid", "username and password required")
		return
	}
	u, err := s.Auth.CreateAdmin(r.Context(), body.Username, body.Password, body.Email)
	if err != nil {
		if err == auth.ErrSetupComplete {
			writeErr(w, 409, "exists", "setup already complete")
			return
		}
		writeErr(w, 400, "setup", err.Error())
		return
	}
	tok, sess, err := s.Auth.CreateSession(r.Context(), u.ID, r.UserAgent(), r.RemoteAddr, 30*24*time.Hour)
	if err != nil {
		writeErr(w, 500, "session", err.Error())
		return
	}
	s.setSessionCookie(w, tok, sess.ExpiresAt)
	s.Audit.Event(r.Context(), &u.ID, "setup", u.Username, r.RemoteAddr, nil)
	writeJSON(w, 201, u)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	u, err := s.Auth.Authenticate(r.Context(), body.Username, body.Password)
	if err != nil {
		writeErr(w, 401, "auth", "invalid credentials")
		return
	}
	tok, sess, err := s.Auth.CreateSession(r.Context(), u.ID, r.UserAgent(), r.RemoteAddr, 30*24*time.Hour)
	if err != nil {
		writeErr(w, 500, "session", err.Error())
		return
	}
	s.setSessionCookie(w, tok, sess.ExpiresAt)
	s.Audit.Event(r.Context(), &u.ID, "login", u.Username, r.RemoteAddr, nil)
	writeJSON(w, 200, u)
}

func (s *Server) discordLogin(w http.ResponseWriter, r *http.Request) {
	if !s.Cfg.DiscordLoginEnabled() {
		writeErr(w, 503, "disabled", "Discord sign-in is off")
		return
	}
	reg, _ := auth.LoadDiscordRegistration(r.Context(), s.Pool)
	state := external.NewState()
	ver, ch := external.PKCE()
	if err := auth.StoreLoginState(r.Context(), s.Pool, s.Box, state, ver); err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	redir := auth.DiscordCallbackURL(s.Cfg.PublicURL)
	http.Redirect(w, r, auth.DiscordAuthURL(s.Cfg.DiscordClientID, redir, state, ch, auth.DiscordLoginScope(reg)), http.StatusFound)
}

func (s *Server) discordLoginCallback(w http.ResponseWriter, r *http.Request) {
	fail := func(msg string) {
		http.Redirect(w, r, s.Cfg.PublicURL+"/?error="+url.QueryEscape(msg), http.StatusFound)
	}
	if !s.Cfg.DiscordLoginEnabled() {
		fail("disabled")
		return
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
	oauth, err := auth.ExchangeDiscordCode(r.Context(), s.Cfg.DiscordClientID, s.Cfg.DiscordClientSecret, redir, r.URL.Query().Get("code"), ver)
	if err != nil {
		fail("token_exchange")
		return
	}
	prof := oauth.Profile
	exists, _ := auth.DiscordUserExists(r.Context(), s.Pool, prof.ID)
	if !exists && !auth.IsAdminDiscordID(prof.ID, s.Cfg.AdminDiscordIDs) {
		reg, _ := auth.LoadDiscordRegistration(r.Context(), s.Pool)
		if err := auth.CheckDiscordRegistration(r.Context(), oauth.AccessToken, reg); err != nil {
			fail(err.Error())
			return
		}
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
