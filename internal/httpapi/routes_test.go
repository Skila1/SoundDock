package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
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
		"POST /api/v1/me/queue/renderer/acquire",
		"POST /api/v1/me/queue/heartbeat",
		"GET /api/v1/me/queue/sse",
		"GET /api/v1/me/queue/events",
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
		"GET /api/v1/tracks/{id}/playability",
		"GET /api/v1/tracks/{id}/lyrics",
		"GET /api/v1/tracks/{id}/metadata",
		"GET /api/v1/admin/lyrics",
		"PUT /api/v1/admin/lyrics",
		"GET /api/v1/me/tokens",
		"GET /api/v1/admin/health/detail",
		"GET /api/v1/admin/maintenance",
		"GET /api/v1/admin/stream-policy",
		"GET /api/v1/admin/acquisition-policy",
		"PUT /api/v1/admin/acquisition-policy",
		"GET /api/v1/admin/library/health",
		"GET /api/v1/admin/roles",
		"GET /api/v1/admin/workers",
		"GET /api/v1/admin/backups/settings",
		"PUT /api/v1/admin/backups/settings",
		"POST /api/v1/admin/backups/passphrase",
		"GET /api/v1/admin/backups/reminder",
		"POST /api/v1/admin/backups/reminder/dismiss",
		"GET /api/v1/admin/backups/restore-requirements",
		"GET /api/v1/admin/backups/remote",
		"POST /api/v1/admin/backups/import-remote",
		"GET /api/v1/setup/backups/settings",
		"PUT /api/v1/setup/backups/settings",
		"GET /api/v1/setup/backups/remote",
		"POST /api/v1/setup/backups/import-remote",
		"PATCH /api/v1/admin/storage/{id}",
		"DELETE /api/v1/admin/storage/{id}",
		"GET /api/v1/admin/listen-compare",
		"GET /api/v1/admin/stats/rebuild",
		"POST /api/v1/admin/stats/rebuild",
		"PUT /api/v1/admin/workers",
		"POST /api/v1/admin/jobs/{id}/retry",
		"POST /api/v1/admin/libraries/{id}/merge",
		"POST /api/v1/tracks/{id}/replace-source",
		"GET /api/v1/admin/duplicate-review",
		"POST /api/v1/admin/duplicate-review/{id}/merge",
		"POST /api/v1/admin/duplicate-review/{id}/ignore",
		"DELETE /api/v1/admin/libraries/{id}",
		"DELETE /api/v1/tracks/bulk",
		"GET /api/v1/admin/libraries/{id}/grants",
		"POST /api/v1/admin/libraries/{id}/grants",
		"PATCH /api/v1/admin/libraries/{id}/grants/{grantID}",
		"DELETE /api/v1/admin/libraries/{id}/grants/{grantID}",
		"GET /api/v1/admin/library-grants-strict",
		"PUT /api/v1/admin/library-grants-strict",
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

func TestRequirePermForbiddenWithoutPerm(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "user"}
	called := false
	h := s.requirePerm("tracks.merge", func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})
	req := authedJSON(u, http.MethodPost, "/api/v1/admin/libraries/"+uuid.New().String()+"/merge", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if called {
		t.Fatal("next handler must not run")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "forbidden" {
		t.Fatalf("code %v", body["code"])
	}
}

func TestRequirePermAllowsNamedPermission(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "ops", Permissions: []string{"tracks.merge"}}
	h := s.requirePerm("tracks.merge", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	req := authedJSON(u, http.MethodPost, "/api/v1/admin/libraries/"+uuid.New().String()+"/merge", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("named perm should pass HasPerm, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestRequirePermAllowsAdminFlag(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "admin", IsAdmin: true}
	h := s.requirePerm("tracks.replace_source", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	req := authedJSON(u, http.MethodPost, "/api/v1/tracks/"+uuid.New().String()+"/replace-source", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("admin should pass HasPerm, got %d %s", rec.Code, rec.Body.String())
	}
}
