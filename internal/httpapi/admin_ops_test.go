package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestMaintenanceMutationAllowed(t *testing.T) {
	allow := []struct{ method, path string }{
		{http.MethodGet, "/healthz"},
		{http.MethodGet, "/api/v1/admin/users"},
		{http.MethodHead, "/api/v1/tracks"},
		{http.MethodPost, "/api/v1/me/queue"},
		{http.MethodPut, "/api/v1/me/queue"},
		{http.MethodPost, "/api/v1/me/queue/control"},
		{http.MethodPost, "/api/v1/me/listen"},
		{http.MethodPost, "/api/v1/me/scrobble"},
		{http.MethodPut, "/api/v1/me/scrobble"},
		{http.MethodPost, "/api/v1/me/discord/play"},
		{http.MethodPost, "/api/v1/me/party"},
		{http.MethodPost, "/api/v1/scrobble"},
		{http.MethodPost, "/api/v1/stream-tokens"},
		{http.MethodGet, "/api/v1/tracks/abc/stream"},
		{http.MethodPost, "/api/v1/tracks/abc/stream"},
		{http.MethodPut, "/api/v1/admin/maintenance"},
		{http.MethodPost, "/api/v1/auth/login"},
		{http.MethodPost, "/api/v1/auth/logout"},
		{http.MethodPatch, "/api/v1/me/devices/browser-1"},
	}
	for _, c := range allow {
		if !maintenanceMutationAllowed(c.method, c.path) {
			t.Fatalf("expected allow %s %s", c.method, c.path)
		}
	}
	deny := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/uploads"},
		{http.MethodPost, "/api/v1/admin/users"},
		{http.MethodPatch, "/api/v1/admin/users/1"},
		{http.MethodDelete, "/api/v1/admin/users/1"},
		{http.MethodPut, "/api/v1/admin/metadata"},
		{http.MethodPatch, "/api/v1/tracks/abc"},
		{http.MethodDelete, "/api/v1/playlists/abc"},
		{http.MethodPost, "/api/v1/admin/libraries"},
		{http.MethodPost, "/api/v1/admin/demo"},
		{http.MethodDelete, "/api/v1/me/sessions/abc"},
	}
	for _, c := range deny {
		if maintenanceMutationAllowed(c.method, c.path) {
			t.Fatalf("expected deny %s %s", c.method, c.path)
		}
	}
}

func TestMaintenanceGuardWithoutPool(t *testing.T) {
	s := &Server{}
	h := s.MaintenanceGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	rec := httptestRecorder(h, http.MethodPost, "/api/v1/uploads")
	if rec != 200 {
		t.Fatalf("nil pool should fail open, got %d", rec)
	}
}

func TestWriteSineWAV(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.wav")
	if err := writeSineWAV(p, 440, 0); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 44 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		t.Fatalf("bad wav header len=%d", len(b))
	}
}

func TestFingerprintToolStatus(t *testing.T) {
	st := fingerprintToolStatus()
	if st != "available" && st != "missing" {
		t.Fatalf("status=%s", st)
	}
}

func httptestRecorder(h http.Handler, method, path string) int {
	w := &statusWriter{code: 200}
	req, _ := http.NewRequest(method, path, nil)
	h.ServeHTTP(w, req)
	return w.code
}

type statusWriter struct {
	code int
	hdr  http.Header
}

func (s *statusWriter) Header() http.Header {
	if s.hdr == nil {
		s.hdr = http.Header{}
	}
	return s.hdr
}
func (s *statusWriter) Write(b []byte) (int, error) { return len(b), nil }
func (s *statusWriter) WriteHeader(status int)      { s.code = status }
