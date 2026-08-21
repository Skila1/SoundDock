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
	oauth := auth.LoadDiscordOAuth(r.Context(), s.Pool, s.Box)
	writeJSON(w, 200, map[string]any{
		"needed":             needed,
		"discord_enabled":    oauth.LoginEnabled,
		"discord_configured": oauth.Ready(),
	})
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
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
	s.setSessionCookie(w, r, tok, sess.ExpiresAt)
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
	s.setSessionCookie(w, r, tok, sess.ExpiresAt)
	s.Audit.Event(r.Context(), &u.ID, "login", u.Username, r.RemoteAddr, nil)
	writeJSON(w, 200, u)
}

func (s *Server) discordLogin(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("code") != "" || r.URL.Query().Get("error") != "" {
		s.discordLoginCallback(w, r)
		return
	}
	oauth := auth.LoadDiscordOAuth(r.Context(), s.Pool, s.Box)
	if !oauth.Ready() {
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
	base := s.absURL(r)
	http.Redirect(w, r, auth.DiscordAuthURL(oauth.ClientID, auth.DiscordCallbackURL(base), state, ch, auth.DiscordLoginScope(reg)), http.StatusFound)
}

func (s *Server) discordLoginCallback(w http.ResponseWriter, r *http.Request) {
	fail := func(msg string) {
		http.Redirect(w, r, "/?error="+url.QueryEscape(msg), http.StatusFound)
	}
	oauth := auth.LoadDiscordOAuth(r.Context(), s.Pool, s.Box)
	if !oauth.Ready() {
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
		if s.Log != nil {
			s.Log.Warn("discord oauth invalid state", "err", err)
		}
		fail("invalid_state")
		return
	}
	if ver == "" {
		if s.Log != nil {
			s.Log.Warn("discord oauth missing pkce verifier")
		}
		fail("invalid_state")
		return
	}
	redir := s.absURL(r) + r.URL.Path
	ex, err := auth.ExchangeDiscordCode(r.Context(), oauth.ClientID, oauth.Secret, redir, r.URL.Query().Get("code"), ver)
	if err != nil {
		if s.Log != nil {
			s.Log.Warn("discord oauth token exchange failed", "err", err, "redirect_uri", redir)
		}
		fail("token_exchange")
		return
	}
	prof := ex.Profile
	exists, _ := auth.DiscordUserExists(r.Context(), s.Pool, prof.ID)
	admins, _ := s.Auth.AdministratorCount(r.Context())
	stored := auth.LoadAdminDiscordIDs(r.Context(), s.Pool)
	if !exists && admins > 0 && !auth.IsAdminDiscordID(prof.ID, stored) {
		reg, _ := auth.LoadDiscordRegistration(r.Context(), s.Pool)
		if err := auth.CheckDiscordRegistration(r.Context(), ex.AccessToken, reg); err != nil {
			if s.Log != nil {
				s.Log.Warn("discord oauth registration denied", "err", err, "discord_id", prof.ID)
			}
			fail(err.Error())
			return
		}
	}
	u, err := s.Auth.UpsertDiscordUser(r.Context(), prof)
	if err != nil {
		fail("account")
		return
	}
	tok, sess, err := s.Auth.CreateSession(r.Context(), u.ID, r.UserAgent(), r.RemoteAddr, 30*24*time.Hour)
	if err != nil {
		fail("session")
		return
	}
	s.setSessionCookie(w, r, tok, sess.ExpiresAt)
	s.Audit.Event(r.Context(), &u.ID, "login.discord", prof.ID, r.RemoteAddr, nil)
	http.Redirect(w, r, "/", http.StatusFound)
}
