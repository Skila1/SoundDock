package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestWave1RoutesRegistered(t *testing.T) {
	h := (&Server{}).Router()
	r, ok := h.(*chi.Mux)
	if !ok {
		t.Fatalf("router type %T", h)
	}
	need := []string{
		"POST /api/v1/me/listen",
		"GET /api/v1/search",
		"GET /api/v1/search/youtube",
		"GET /api/v1/me/queue",
		"PUT /api/v1/me/queue",
		"POST /api/v1/me/queue/add",
		"POST /api/v1/me/queue/control",
		"GET /api/v1/me/party",
		"POST /api/v1/me/party",
		"POST /api/v1/me/offline/tokens",
		"DELETE /api/v1/me/offline/tokens",
		"GET /api/v1/me/discord/voice-state",
		"POST /api/v1/me/discord/join",
		"POST /api/v1/me/discord/play",
		"GET /api/v1/me/scrobble",
		"PUT /api/v1/me/scrobble",
		"GET /api/v1/radio",
		"GET /api/v1/playlists/folders",
		"GET /api/v1/playlists/invite",
		"POST /api/v1/providers/{provider}/import-all",
		"POST /api/v1/playlists/{id}/items/{itemID}/youtube",
		"GET /api/v1/me/history",
		"GET /api/v1/me/never-played",
		"GET /api/v1/me/stats",
		"GET /api/v1/me/wrapped",
		"GET /api/v1/tracks/{id}/waveform",
		"GET /api/v1/tracks/{id}/metadata",
		"GET /api/v1/me/tokens",
		"GET /api/v1/admin/health/detail",
		"GET /api/v1/admin/maintenance",
		"GET /api/v1/admin/stream-policy",
		"GET /api/v1/admin/library/health",
		"GET /api/v1/admin/scans",
		"GET /healthz",
		"GET /api/v1/tracks/{id}/stream",
	}
	got := map[string]bool{}
	if err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got[method+" "+route] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var missing []string
	for _, n := range need {
		if !got[n] {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("unregistered routes:\n%s", strings.Join(missing, "\n"))
	}
}

func TestHealthzUnaffectedByMaintenanceGuard(t *testing.T) {
	h := (&Server{}).Router()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("healthz %d", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("body %q", rec.Body.String())
	}
}
