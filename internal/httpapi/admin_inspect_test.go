package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
)

func TestInspectRoutesRegistered(t *testing.T) {
	h := (&Server{}).Router()
	r, ok := h.(*chi.Mux)
	if !ok {
		t.Fatalf("router type %T", h)
	}
	got := map[string]bool{}
	if err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got[method+" "+route] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{
		"GET /api/v1/admin/inspect",
		"GET /api/v1/admin/errors",
		"GET /api/v1/admin/playback/sessions",
		"GET /api/v1/admin/playback/sessions/{id}",
		"GET /api/v1/admin/users/{id}/playback",
		"GET /api/v1/admin/discord/runtime",
		"GET /api/v1/admin/acquisition",
		"GET /api/v1/admin/media/holds",
		"GET /api/v1/admin/scans/errors",
		"GET /api/v1/admin/webhooks/deliveries",
		"GET /api/v1/admin/external/errors",
	} {
		if !got[n] {
			t.Fatalf("unregistered %s", n)
		}
	}
}

func TestAdminInspectWithoutPool(t *testing.T) {
	s := &Server{}
	admin := &auth.User{ID: uuid.New(), Username: "admin", IsAdmin: true}
	req := authedJSON(admin, http.MethodGet, "/api/v1/admin/inspect", nil)
	rec := httptest.NewRecorder()
	s.adminInspect(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["counts"] == nil || body["playback"] == nil || body["discord"] == nil || body["errors"] == nil {
		t.Fatalf("missing sections %v", body)
	}
}

func TestAdminInspectDB(t *testing.T) {
	s, pool := wave1HTTPServer(t)
	ctx := context.Background()
	u := seedQueueUser(t, pool, "inspect-did-"+uuid.NewString()[:8])
	admin := &auth.User{ID: u.ID, Username: u.Username, IsAdmin: true}

	sid, err := s.Play.WebSession(ctx, u.ID, "inspect-tab")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE playback_sessions SET status='playing', output_pref='discord', renderer_kind='discord' WHERE id=$1`, sid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO jobs (type, status, last_error, pool) VALUES ('inspect.fail','failed','pq: boom','maintenance')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO discord_playback_errors (guild_id, error_class, message) VALUES ('g1','ffmpeg','no rows in result set')`); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.adminInspect(rec, authedJSON(admin, http.MethodGet, "/api/v1/admin/inspect", nil))
	if rec.Code != 200 {
		t.Fatalf("inspect %d %s", rec.Code, rec.Body.String())
	}
	dump := decodeMap(t, rec)
	counts, _ := dump["counts"].(map[string]any)
	if n, _ := counts["playback_sessions"].(float64); n < 1 {
		t.Fatalf("counts %v", counts)
	}
	playbackSec, _ := dump["playback"].(map[string]any)
	sessions, _ := playbackSec["sessions"].([]any)
	found := false
	for _, raw := range sessions {
		m, _ := raw.(map[string]any)
		if m["id"] == sid.String() {
			found = true
			if m["status"] != "playing" {
				t.Fatalf("session %v", m)
			}
		}
	}
	if !found {
		t.Fatalf("session %s missing from %v", sid, sessions)
	}

	erec := httptest.NewRecorder()
	s.adminInspectErrors(erec, authedJSON(admin, http.MethodGet, "/api/v1/admin/errors", nil))
	if erec.Code != 200 {
		t.Fatalf("errors %d %s", erec.Code, erec.Body.String())
	}
	errBody := decodeMap(t, erec)
	items, _ := errBody["items"].([]any)
	if len(items) < 2 {
		t.Fatalf("want job+discord errors, got %v", errBody)
	}

	srec := httptest.NewRecorder()
	sreq := authedJSON(admin, http.MethodGet, "/api/v1/admin/playback/sessions/"+sid.String(), nil)
	sreq = sreq.WithContext(context.WithValue(sreq.Context(), chi.RouteCtxKey, routeCtx("id", sid.String())))
	s.adminPlaybackSession(srec, sreq)
	if srec.Code != 200 {
		t.Fatalf("session %d %s", srec.Code, srec.Body.String())
	}
	one := decodeMap(t, srec)
	if one["id"] != sid.String() {
		t.Fatalf("id %v", one["id"])
	}

	urec := httptest.NewRecorder()
	req := authedJSON(admin, http.MethodGet, "/api/v1/admin/users/"+u.ID.String()+"/playback", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx("id", u.ID.String())))
	s.adminUserPlayback(urec, req)
	if urec.Code != 200 {
		t.Fatalf("user playback %d %s", urec.Code, urec.Body.String())
	}
	up := decodeMap(t, urec)
	user, _ := up["user"].(map[string]any)
	if user["id"] != u.ID.String() {
		t.Fatalf("user %v", up)
	}
}

func TestAdminPlaybackSessionMissing(t *testing.T) {
	s := &Server{}
	admin := &auth.User{ID: uuid.New(), IsAdmin: true}
	req := authedJSON(admin, http.MethodGet, "/api/v1/admin/playback/sessions/"+uuid.NewString(), nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx("id", uuid.NewString())))
	rec := httptest.NewRecorder()
	s.adminPlaybackSession(rec, req)
	if rec.Code != 404 {
		t.Fatalf("status %d", rec.Code)
	}
}

func routeCtx(key, val string) *chi.Context {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return rctx
}

func TestWebSessionAllowsIntegrationClientID(t *testing.T) {
	s, pool := wave1HTTPServer(t)
	clientID := uuid.New()
	// Integration keys use api_clients.id as User.ID. That UUID is not in users.
	sid, err := s.Play.WebSession(context.Background(), clientID, "api")
	if err != nil {
		t.Fatalf("integration WebSession: %v", err)
	}
	if sid == uuid.Nil {
		t.Fatal("nil session")
	}
	var uid *uuid.UUID
	if err := pool.QueryRow(context.Background(), `SELECT user_id FROM playback_sessions WHERE id=$1`, sid).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if uid != nil {
		t.Fatalf("integration session must not set user_id, got %s", uid)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_sessions WHERE id=$1`, sid)
	})
}

func TestInspectErrorsSourceFilter(t *testing.T) {
	s, pool := wave1HTTPServer(t)
	admin := &auth.User{ID: uuid.New(), IsAdmin: true}
	if _, err := pool.Exec(context.Background(), `INSERT INTO jobs (type, status, last_error, pool) VALUES ('x','failed','job-only','maintenance')`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.adminInspectErrors(rec, authedJSON(admin, http.MethodGet, "/api/v1/admin/errors?source=job&q=job-only", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	items, _ := body["items"].([]any)
	if len(items) == 0 {
		t.Fatal("expected job error")
	}
	first, _ := items[0].(map[string]any)
	if first["source"] != "job" {
		t.Fatalf("source %v", first["source"])
	}
}
