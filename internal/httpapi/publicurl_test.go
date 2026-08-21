package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sounddock/sounddock/internal/config"
)

func TestAbsURLCloudflare(t *testing.T) {
	s := &Server{Cfg: config.Config{}}
	r := httptest.NewRequest(http.MethodGet, "http://localhost:8080/api/v1/auth/discord", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "bot.nxsrp.com")
	r.Host = "localhost:8080"
	if got := s.absURL(r); got != "https://bot.nxsrp.com" {
		t.Fatalf("got %s", got)
	}
	if !s.cookieSecureFor(r) {
		t.Fatal("expected secure cookie behind https tunnel")
	}
}

func TestAbsURLCFRay(t *testing.T) {
	s := &Server{Cfg: config.Config{}}
	r := httptest.NewRequest(http.MethodGet, "http://localhost:8080/", nil)
	r.Host = "bot.nxsrp.com"
	r.Header.Set("CF-Ray", "abc")
	if got := s.absURL(r); got != "https://bot.nxsrp.com" {
		t.Fatalf("got %s", got)
	}
}
