package httpapi

import (
	"net/http"
	"net/url"
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
	host := r.Host
	if s.trustedPeer(r) {
		if p := firstCSV(r.Header.Get("X-Forwarded-Proto")); p != "" {
			proto = p
		} else if strings.Contains(strings.ToLower(r.Header.Get("CF-Visitor")), "https") {
			proto = "https"
		} else if r.Header.Get("CF-Ray") != "" {
			proto = "https"
		}
		if h := firstCSV(r.Header.Get("X-Forwarded-Host")); h != "" {
			host = h
		}
	}
	return proto + "://" + host
}

func (s *Server) cookieSecureFor(r *http.Request) bool {
	if s.Cfg.UseSecureCookie {
		return true
	}
	return strings.HasPrefix(strings.ToLower(s.absURL(r)), "https://")
}

func (s *Server) originAllowed(r *http.Request, origin string) bool {
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	allowedHost := strings.TrimSpace(strings.ToLower(u.Host))
	if allowedHost == strings.TrimSpace(strings.ToLower(r.Host)) {
		return true
	}
	for _, allowed := range s.Cfg.CORSAllowedOrigins() {
		parsed, err := url.Parse(allowed)
		if err == nil && parsed.Host != "" && strings.EqualFold(parsed.Host, allowedHost) {
			return true
		}
	}
	return false
}
