package httpapi

import (
	"net/http"
	"strings"
)

func (s *Server) absURL(r *http.Request) string {
	if s.Cfg.PublicURL != "" {
		return strings.TrimRight(s.Cfg.PublicURL, "/")
	}
	proto := "http"
	if r.TLS != nil {
		proto = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		proto = strings.TrimSpace(strings.Split(p, ",")[0])
	}
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = strings.TrimSpace(strings.Split(h, ",")[0])
	}
	return proto + "://" + host
}

func (s *Server) cookieSecureFor(r *http.Request) bool {
	if s.Cfg.UseSecureCookie {
		return true
	}
	return strings.HasPrefix(strings.ToLower(s.absURL(r)), "https://")
}
