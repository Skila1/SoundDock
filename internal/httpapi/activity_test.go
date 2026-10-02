package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/audit"
	"github.com/sounddock/sounddock/internal/auth"
	"github.com/sounddock/sounddock/internal/httpapi/ratelimit"
	"github.com/sounddock/sounddock/internal/oplog"
)

func TestShouldLogRequestPolicy(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		route  string
		status int
		want   bool
	}{
		{"mutation", "POST", "/api/v1/admin/users", "/api/v1/admin/users", 201, true},
		{"failed mutation", "DELETE", "/api/v1/playlists/x", "/api/v1/playlists/{id}", 404, true},
		{"successful read", "GET", "/api/v1/tracks", "/api/v1/tracks", 200, false},
		{"admin read", "GET", "/api/v1/admin/workers", "/api/v1/admin/workers", 200, false},
		{"read server error", "GET", "/api/v1/tracks", "/api/v1/tracks", 500, true},
		{"read forbidden", "GET", "/api/v1/admin/users", "/api/v1/admin/users", 403, true},
		{"read rate limited", "GET", "/api/v1/search", "/api/v1/search", 429, true},
		{"expired session poll", "GET", "/api/v1/me/queue", "/api/v1/me/queue", 401, false},
		{"export", "GET", "/api/v1/me/export", "/api/v1/me/export", 200, true},
		{"discord callback", "GET", "/api/v1/auth/discord/callback", "/api/v1/auth/discord/callback", 302, true},
		{"static asset", "GET", "/assets/index.js", "", 200, false},
		{"spa route", "GET", "/admin/activity", "", 200, false},
		{"healthz", "GET", "/healthz", "/healthz", 200, false},
		{"healthz down", "GET", "/healthz", "/healthz", 503, true},
		{"byte range", "GET", "/api/v1/tracks/1/stream", "/api/v1/tracks/{id}/stream", 206, false},
		{"stream missing", "GET", "/api/v1/tracks/1/stream", "/api/v1/tracks/{id}/stream", 404, false},
		{"stream crash", "GET", "/api/v1/tracks/1/stream", "/api/v1/tracks/{id}/stream", 500, true},
		{"artwork", "GET", "/api/v1/albums/1/artwork", "/api/v1/albums/{id}/artwork", 200, false},
		{"sse", "GET", "/api/v1/me/queue/sse", "/api/v1/me/queue/sse", 200, false},
		{"heartbeat", "POST", "/api/v1/me/queue/heartbeat", "/api/v1/me/queue/heartbeat", 200, false},
		{"heartbeat rejected", "POST", "/api/v1/me/queue/heartbeat", "/api/v1/me/queue/heartbeat", 409, true},
		{"listen progress", "POST", "/api/v1/me/listen", "/api/v1/me/listen", 204, false},
		{"upload chunk", "PATCH", "/api/v1/uploads/abc", "/api/v1/uploads/{id}", 204, false},
		{"upload complete", "POST", "/api/v1/uploads/abc/complete", "/api/v1/uploads/{id}/complete", 200, true},
		{"options", "OPTIONS", "/api/v1/admin/users", "", 204, false},
		{"subsonic ok", "GET", "/rest/stream.view", "/rest/*", 200, false},
		{"subsonic error", "GET", "/rest/stream.view", "/rest/*", 500, true},
	}
	for _, c := range cases {
		if got := shouldLogRequest(&oplog.Request{}, c.method, c.path, c.route, c.status); got != c.want {
			t.Errorf("%s: shouldLog=%v want %v", c.name, got, c.want)
		}
	}
	sup := &oplog.Request{}
	sup.Suppress()
	if shouldLogRequest(sup, "POST", "/api/v1/me/queue/control", "/api/v1/me/queue/control", 200) {
		t.Error("suppressed success should be skipped")
	}
	if !shouldLogRequest(sup, "POST", "/api/v1/me/queue/control", "/api/v1/me/queue/control", 400) {
		t.Error("suppressed failure should still be logged")
	}
	forced := &oplog.Request{}
	forced.Fail("oauth_denied")
	if !shouldLogRequest(forced, "GET", "/api/v1/tracks", "/api/v1/tracks", 302) {
		t.Error("failed request must be logged")
	}
}

func TestDescribeRouteAndCategories(t *testing.T) {
	if c, l := describeRoute("POST", "/api/v1/admin/users", "/api/v1/admin/users"); c != "access" || l != "Create user" {
		t.Fatalf("labelled route: %s %s", c, l)
	}
	for path, want := range map[string]string{
		"/api/v1/admin/integrations/discord/guilds/1": "discord",
		"/api/v1/admin/webhooks/x":                    "integrations",
		"/api/v1/admin/backups/settings":              "backup",
		"/api/v1/admin/libraries/1/scan":              "library",
		"/api/v1/admin/libraries/1/grants":            "access",
		"/api/v1/admin/metadata":                      "config",
		"/api/v1/me/queue/add":                        "playback",
		"/api/v1/playlists/1/tracks":                  "playlists",
		"/api/v1/tracks/bulk":                         "media",
		"/api/v1/auth/login":                          "auth",
		"/rest/ping.view":                             "subsonic",
	} {
		if got := routeCategory(path); got != want {
			t.Errorf("%s: %s want %s", path, got, want)
		}
	}
}

func TestSafeQueryRedactsCredentials(t *testing.T) {
	q := map[string][]string{"token": {"abc"}, "access_key": {"k"}, "code": {"oauthcode"}, "u": {"bob"}, "p": {"pw"}, "q": {"daft punk"}}
	out := safeQuery(q)
	raw, _ := json.Marshal(out)
	for _, leak := range []string{"abc", `"k"`, "oauthcode", "pw", "bob"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("leaked %s in %s", leak, raw)
		}
	}
	if out["q"] != "daft punk" {
		t.Fatalf("search term dropped: %v", out)
	}
}

// activityProbe installs a writer whose queue the test reads directly.
func activityProbe(t *testing.T) *oplog.Writer {
	t.Helper()
	w := oplog.NewWriter(nil, nil)
	prev := oplog.Default()
	oplog.SetDefault(w)
	t.Cleanup(func() { oplog.SetDefault(prev) })
	return w
}

func drainProbe(t *testing.T, w *oplog.Writer) []oplog.Entry {
	t.Helper()
	var out []oplog.Entry
	for {
		e, ok := oplog.TryNext(w)
		if !ok {
			return out
		}
		out = append(out, e)
	}
}

func activityRouter(s *Server, mount func(r chi.Router)) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(s.activityLog)
	mount(r)
	return r
}

func TestActivityMiddlewareRecordsMutationWithActorAndRequestID(t *testing.T) {
	w := activityProbe(t)
	s := &Server{}
	uid := uuid.New()
	h := activityRouter(s, func(r chi.Router) {
		r.Post("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
			noteActor(r, &auth.User{ID: uid, Username: "alice", DisplayName: "Alice"}, "session")
			writeJSON(w, 201, map[string]any{"id": "x", "password": "never-logged"})
		})
		r.Get("/api/v1/tracks", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, []int{}) })
	})
	req := httptest.NewRequest("POST", "/api/v1/admin/users?q=1&token=sekrit", strings.NewReader(`{"username":"bob","password":"hunter2"}`))
	req.RemoteAddr = "203.0.113.5:4444"
	req.Header.Set("Cookie", "sd_session=abc")
	req.Header.Set("Authorization", "Bearer sdp_secretsecretsecretsecret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("response should carry X-Request-Id")
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/tracks", nil))

	got := drainProbe(t, w)
	if len(got) != 1 {
		t.Fatalf("entries %d, want 1 (successful GET skipped): %+v", len(got), got)
	}
	e := got[0]
	if e.Category != "access" || e.Message != "Create user" || e.Result != oplog.ResultSuccess || e.Level != "info" {
		t.Fatalf("summary: %+v", e)
	}
	if e.ActorID == nil || *e.ActorID != uid || !strings.Contains(e.ActorName, "alice") {
		t.Fatalf("actor: %+v", e)
	}
	if e.IP != "203.0.113.5" || e.RequestID != rec.Header().Get("X-Request-Id") || e.Status == nil || *e.Status != 201 {
		t.Fatalf("request fields: %+v", e)
	}
	if e.Route != "/api/v1/admin/users" || e.Method != "POST" || e.DurationMs == nil {
		t.Fatalf("route fields: %+v", e)
	}
	raw, _ := json.Marshal(e)
	for _, leak := range []string{"hunter2", "never-logged", "sekrit", "sd_session=abc", "secretsecret"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("leaked %q in %s", leak, raw)
		}
	}
}

func TestActivityMiddlewareCapturesFailureReasonAndPanics(t *testing.T) {
	w := activityProbe(t)
	s := &Server{}
	h := activityRouter(s, func(r chi.Router) {
		r.Delete("/api/v1/admin/libraries/{id}", func(w http.ResponseWriter, r *http.Request) {
			writeErr(w, 409, "busy", "library is scanning")
		})
		r.Post("/api/v1/admin/backups", func(w http.ResponseWriter, r *http.Request) {
			panic("boom password=hunter2")
		})
	})
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("DELETE", "/api/v1/admin/libraries/abc", nil))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/admin/backups", nil))
	if rec.Code != 500 {
		t.Fatalf("panic should become 500, got %d", rec.Code)
	}
	got := drainProbe(t, w)
	if len(got) != 2 {
		t.Fatalf("entries %d: %+v", len(got), got)
	}
	fail := got[0]
	if fail.Level != "warn" || fail.Result != oplog.ResultFailure || fail.Error != "library is scanning" ||
		fail.Message != "Delete library failed: library is scanning" || fail.Details["code"] != "busy" {
		t.Fatalf("failure entry: %+v", fail)
	}
	if p, _ := fail.Details["params"].(map[string]any); p["id"] != "abc" {
		t.Fatalf("route params: %+v", fail.Details)
	}
	crash := got[1]
	if crash.Level != "error" || crash.Result != oplog.ResultFailure || !strings.Contains(crash.Error, "panic: boom") {
		t.Fatalf("panic entry: %+v", crash)
	}
	if _, ok := crash.Details["stack"]; !ok {
		t.Fatal("panic entry should keep a stack trace")
	}
	raw, _ := json.Marshal(oplog.RedactDetails(map[string]any{"e": crash.Error, "d": crash.Details}))
	if strings.Contains(string(raw), "hunter2") {
		t.Fatalf("panic text not redacted: %s", raw)
	}
}

func TestActivityMiddlewareDescribeAndSuppress(t *testing.T) {
	w := activityProbe(t)
	s := &Server{}
	h := activityRouter(s, func(r chi.Router) {
		r.Post("/api/v1/me/queue/control", func(w http.ResponseWriter, r *http.Request) {
			var body struct{ Action string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			describePlaybackControl(r, body.Action)
			writeJSON(w, 200, map[string]any{})
		})
	})
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/v1/me/queue/control", strings.NewReader(`{"Action":"volume"}`)))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/v1/me/queue/control", strings.NewReader(`{"Action":"next"}`)))
	got := drainProbe(t, w)
	if len(got) != 1 || got[0].Action != "playback.next" || got[0].Message != "Playback: next" || got[0].Category != "playback" {
		t.Fatalf("entries: %+v", got)
	}
}

func TestLoginFailureRecordsAttemptedUserNotPassword(t *testing.T) {
	pool := testPool(t)
	w := activityProbe(t)
	s := &Server{Pool: pool, Auth: auth.New(pool), Limit: ratelimit.New(), Audit: audit.New(pool)}
	h := s.Router()
	name := "nobody-" + uuid.NewString()[:8]
	req := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"username":"`+name+`","password":"hunter2-secret"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "198.51.100.23:5555"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("status %d", rec.Code)
	}
	got := drainProbe(t, w)
	var entry *oplog.Entry
	for i := range got {
		if got[i].Route == "/api/v1/auth/login" {
			entry = &got[i]
		}
	}
	if entry == nil {
		t.Fatalf("no sign-in entry: %+v", got)
	}
	if entry.Category != "auth" || entry.Result != oplog.ResultFailure || entry.ActorName != name || entry.IP != "198.51.100.23" {
		t.Fatalf("sign-in entry: %+v", entry)
	}
	if !strings.HasPrefix(entry.Message, "Sign in failed") {
		t.Fatalf("message: %q", entry.Message)
	}
	raw, _ := json.Marshal(entry)
	if strings.Contains(string(raw), "hunter2-secret") {
		t.Fatalf("password leaked: %s", raw)
	}
}

func TestAutoAuditRecordsAdminChangeOnce(t *testing.T) {
	pool := testPool(t)
	activityProbe(t)
	s := &Server{Pool: pool, Audit: audit.New(pool)}
	admin := seedQueueUser(t, pool, "")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM audit_events WHERE actor_user_id=$1`, admin.ID)
	})
	h := activityRouter(s, func(r chi.Router) {
		r.Put("/api/v1/admin/quotas", func(w http.ResponseWriter, r *http.Request) {
			noteActor(r, admin, "session")
			writeJSON(w, 200, map[string]any{})
		})
		r.Put("/api/v1/admin/maintenance", func(w http.ResponseWriter, r *http.Request) {
			noteActor(r, admin, "session")
			s.Audit.Event(r.Context(), &admin.ID, "maintenance.update", "", "", nil)
			writeJSON(w, 200, map[string]any{})
		})
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("PUT", "/api/v1/admin/quotas", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("PUT", "/api/v1/admin/maintenance", nil))
	reqID := rec.Header().Get("X-Request-Id")
	var n int
	for i := 0; i < 50 && n < 2; i++ {
		_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE actor_user_id=$1`, admin.ID).Scan(&n)
		if n < 2 {
			sleepBrief()
		}
	}
	if n != 2 {
		t.Fatalf("audit rows %d, want 2 (one auto, one explicit, no duplicate)", n)
	}
	var action, gotReq string
	if err := pool.QueryRow(context.Background(), `SELECT action, request_id FROM audit_events WHERE actor_user_id=$1 AND action='Update quotas'`, admin.ID).Scan(&action, &gotReq); err != nil {
		t.Fatal(err)
	}
	if gotReq != reqID {
		t.Fatalf("request id %q want %q", gotReq, reqID)
	}
}

func sleepBrief() { time.Sleep(20 * time.Millisecond) }

func TestAdminLogsAndAuditEndpointsFilterAndPaginate(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	marker := "ep-" + uuid.NewString()[:8]
	admin := seedQueueUser(t, pool, "")
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM operational_logs WHERE request_id LIKE $1`, marker+"%")
		_, _ = pool.Exec(c, `DELETE FROM audit_events WHERE request_id LIKE $1`, marker+"%")
	})
	for i := 0; i < 3; i++ {
		st := 200
		if err := oplog.Write(ctx, pool, oplog.Entry{Level: "info", Category: "library", Message: "Start library scan " + marker,
			RequestID: marker + "-a", IP: "203.0.113.50", ActorName: admin.Username, Status: &st, Result: oplog.ResultSuccess}); err != nil {
			t.Fatal(err)
		}
	}
	if err := oplog.Write(ctx, pool, oplog.Entry{Level: "error", Category: "job", Message: "boom " + marker, RequestID: marker + "-b", Result: oplog.ResultFailure}); err != nil {
		t.Fatal(err)
	}
	a := audit.New(pool)
	for i := 0; i < 3; i++ {
		req := &oplog.Request{ID: marker + "-audit", IP: "198.51.100.77"}
		a.Event(oplog.WithRequest(ctx, req), &admin.ID, "quota.update."+marker, "t", "", map[string]any{"secret": "x"})
	}
	s := &Server{Pool: pool}

	rec := httptest.NewRecorder()
	s.adminLogs(rec, httptest.NewRequest("GET", "/api/v1/admin/logs?q="+marker+"&limit=2&ip=203.0.113", nil))
	var page struct {
		Items      []map[string]any `json:"items"`
		NextCursor string           `json:"next_cursor"`
		Categories []string         `json:"categories"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.NextCursor == "" || len(page.Categories) == 0 {
		t.Fatalf("page: %d next=%q cats=%v", len(page.Items), page.NextCursor, page.Categories)
	}
	if page.Items[0]["ip"] != "203.0.113.50" || page.Items[0]["result"] != "success" || page.Items[0]["request_id"] != marker+"-a" {
		t.Fatalf("item: %v", page.Items[0])
	}
	rec = httptest.NewRecorder()
	s.adminLogs(rec, httptest.NewRequest("GET", "/api/v1/admin/logs?q="+marker+"&level=error&result=failure", nil))
	_ = json.Unmarshal(rec.Body.Bytes(), &page)
	if len(page.Items) != 1 || page.Items[0]["category"] != "job" {
		t.Fatalf("error filter: %v", page.Items)
	}

	rec = httptest.NewRecorder()
	s.adminAudit(rec, httptest.NewRequest("GET", "/api/v1/admin/audit?q="+marker+"&limit=2&actor="+admin.Username, nil))
	var audits struct {
		Items []struct {
			Username  string         `json:"username"`
			IP        string         `json:"ip"`
			RequestID string         `json:"request_id"`
			Meta      map[string]any `json:"meta"`
		} `json:"items"`
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &audits); err != nil {
		t.Fatal(err)
	}
	if len(audits.Items) != 2 || audits.NextCursor == "" {
		t.Fatalf("audit page: %+v", audits)
	}
	it := audits.Items[0]
	if it.Username != admin.Username || it.IP != "198.51.100.77" || it.RequestID != marker+"-audit" || it.Meta["secret"] != "[redacted]" {
		t.Fatalf("audit item: %+v", it)
	}
	rec = httptest.NewRecorder()
	s.adminAudit(rec, httptest.NewRequest("GET", "/api/v1/admin/audit?q="+marker+"&limit=2&cursor="+audits.NextCursor, nil))
	_ = json.Unmarshal(rec.Body.Bytes(), &audits)
	if len(audits.Items) != 1 || audits.NextCursor != "" {
		t.Fatalf("audit page 2: %+v", audits)
	}
}

func TestSessionRequestsRecordTheSignedInUser(t *testing.T) {
	pool := testPool(t)
	w := activityProbe(t)
	s := &Server{Pool: pool, Auth: auth.New(pool), Limit: ratelimit.New(), Audit: audit.New(pool)}
	u := seedQueueUser(t, pool, "")
	tok, _, err := s.Auth.CreateSession(context.Background(), u.ID, "test", "127.0.0.1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM sessions WHERE user_id=$1`, u.ID) })
	req := httptest.NewRequest("PUT", "/api/v1/admin/maintenance", strings.NewReader(`{"maintenance":false}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	got := drainProbe(t, w)
	if len(got) == 0 {
		t.Fatalf("no entry (status %d)", rec.Code)
	}
	e := got[len(got)-1]
	if e.ActorID == nil || *e.ActorID != u.ID || !strings.Contains(e.ActorName, u.Username) || e.Details["auth"] != "session" {
		t.Fatalf("actor not recorded: %+v", e)
	}
}
