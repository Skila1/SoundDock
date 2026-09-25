package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/sounddock/sounddock/internal/config"
)

func TestAbsURLCloudflare(t *testing.T) {
	s := &Server{Cfg: config.Config{TrustedProxies: []string{"127.0.0.1/32", "::1/128"}}}
	r := httptest.NewRequest(http.MethodGet, "http://localhost:8080/api/v1/auth/discord", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "bot.nxsrp.com")
	r.Host = "localhost:8080"
	r = r.WithContext(context.WithValue(r.Context(), peerKey{}, "127.0.0.1:1234"))
	if got := s.absURL(r); got != "https://bot.nxsrp.com" {
		t.Fatalf("got %s", got)
	}
	if !s.cookieSecureFor(r) {
		t.Fatal("expected secure cookie behind https tunnel")
	}
}

func TestAbsURLIgnoresUntrustedForwardedHost(t *testing.T) {
	s := &Server{Cfg: config.Config{TrustedProxies: []string{"127.0.0.1/32"}}}
	r := httptest.NewRequest(http.MethodGet, "http://localhost:8080/", nil)
	r.RemoteAddr = "203.0.113.9:9"
	r.Host = "localhost:8080"
	r.Header.Set("X-Forwarded-Host", "evil.example")
	r.Header.Set("X-Forwarded-Proto", "https")
	r = r.WithContext(context.WithValue(r.Context(), peerKey{}, "203.0.113.9:9"))
	if got := s.absURL(r); got != "http://localhost:8080" {
		t.Fatalf("got %s", got)
	}
}

func TestAbsURLPublicURLWins(t *testing.T) {
	s := &Server{Cfg: config.Config{PublicURL: "https://app.example.test"}}
	r := httptest.NewRequest(http.MethodGet, "http://localhost:8080/", nil)
	r.Header.Set("X-Forwarded-Host", "evil.example")
	if got := s.absURL(r); got != "https://app.example.test" {
		t.Fatalf("got %s", got)
	}
}

func TestCORSOriginAllowlist(t *testing.T) {
	s := &Server{Cfg: config.Config{PublicURL: "https://app.example.test"}}
	if !s.corsOriginOK(nil, "http://localhost:5173") {
		t.Fatal("vite")
	}
	if !s.corsOriginOK(nil, "https://app.example.test") {
		t.Fatal("public url")
	}
	if s.corsOriginOK(nil, "https://evil.example") {
		t.Fatal("random origin")
	}
}

func TestAbsURLCFRay(t *testing.T) {
	s := &Server{Cfg: config.Config{TrustedProxies: []string{"127.0.0.1/32"}}}
	r := httptest.NewRequest(http.MethodGet, "http://localhost:8080/", nil)
	r.RemoteAddr = "127.0.0.1:9"
	r.Host = "bot.nxsrp.com"
	r.Header.Set("CF-Ray", "abc")
	r = r.WithContext(context.WithValue(r.Context(), peerKey{}, "127.0.0.1:9"))
	if got := s.absURL(r); got != "https://bot.nxsrp.com" {
		t.Fatalf("got %s", got)
	}
}

func TestSPADoesNotServeAPI(t *testing.T) {
	s := &Server{Web: fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/discord/callback?code=x&state=y", nil)
	s.spa().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<html>") {
		t.Fatal("served spa html for api callback")
	}
}

func TestCSRFMiddlewareRejectsMissingToken(t *testing.T) {
	s := &Server{Cfg: config.Config{AllowedOrigins: []string{"https://app.example.com"}, PublicURL: "https://app.example.com"}}
	h := s.csrfProtection(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "https://app.example.com/api/v1/me", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.AddCookie(&http.Cookie{Name: "sd_session", Value: "session-token"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "csrf") {
		t.Fatalf("expected CSRF error response, got %s", rec.Body.String())
	}
}

func TestCSRFMiddlewareAcceptsSameOriginToken(t *testing.T) {
	s := &Server{Cfg: config.Config{AllowedOrigins: []string{"https://app.example.com"}, PublicURL: "https://app.example.com"}}
	h := s.csrfProtection(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "https://app.example.com/api/v1/me", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("X-CSRF-Token", "abc123")
	req.AddCookie(&http.Cookie{Name: "sd_session", Value: "session-token"})
	req.AddCookie(&http.Cookie{Name: "sd_csrf", Value: "abc123"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected ok, got %d: %s", rec.Code, rec.Body.String())
	}
}
