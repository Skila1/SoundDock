package httpapi

import (
	"net/http"
	"strings"
)

func firstCSV(v string) string {
	return strings.TrimSpace(strings.Split(v, ",")[0])
}

func (s *Server) absURL(r *http.Request) string {
	if s.Cfg.PublicURL != "" {
		return strings.TrimRight(s.Cfg.PublicURL, "/")
	}
	proto := "http"
	if r.TLS != nil {
		proto = "https"
	}
	if p := firstCSV(r.Header.Get("X-Forwarded-Proto")); p != "" {
		proto = p
	} else if strings.Contains(strings.ToLower(r.Header.Get("CF-Visitor")), "https") {
		proto = "https"
	} else if r.Header.Get("CF-Ray") != "" {
		proto = "https"
	}
	host := r.Host
	if h := firstCSV(r.Header.Get("X-Forwarded-Host")); h != "" {
		host = h
	}
	return proto + "://" + host
}

func (s *Server) cookieSecureFor(r *http.Request) bool {
	if s.Cfg.UseSecureCookie {
		return true
	}
	return strings.HasPrefix(strings.ToLower(s.absURL(r)), "https://")
}
