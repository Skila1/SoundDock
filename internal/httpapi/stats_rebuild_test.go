package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
	"github.com/sounddock/sounddock/internal/jobs"
)

func TestStatsRebuildRoutesRegistered(t *testing.T) {
	h := (&Server{}).Router()
	for _, path := range []struct{ method, url string }{
		{http.MethodGet, "/api/v1/admin/stats/rebuild"},
		{http.MethodPost, "/api/v1/admin/stats/rebuild"},
	} {
		req := httptest.NewRequest(path.method, path.url, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			t.Fatalf("%s %s is not registered", path.method, path.url)
		}
	}
}

func TestAdminStatsRebuildForbiddenWithoutPerm(t *testing.T) {
	s := &Server{}
	// Non-admin stub: HasPerm(stats.rebuild) is false even though the route
	// also sits behind requireAdmin. IsAdmin would make HasPerm true.
	u := &auth.User{ID: uuid.New(), Username: "user"}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := authedJSON(u, method, "/api/v1/admin/stats/rebuild", nil)
		rec := httptest.NewRecorder()
		if method == http.MethodGet {
			s.adminStatsRebuildGet(rec, req)
		} else {
			s.adminStatsRebuildPost(rec, req)
		}
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s without perm: status %d body %s", method, rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body["code"] != "forbidden" {
			t.Fatalf("%s code %v", method, body["code"])
		}
	}
}

func TestAdminStatsRebuildHasPermViaAdminFlag(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "admin", IsAdmin: true}
	req := authedJSON(u, http.MethodGet, "/api/v1/admin/stats/rebuild", nil)
	rec := httptest.NewRecorder()
	s.adminStatsRebuildGet(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	out := decodeMap(t, rec)
	if out["listen_reader"] != listenReaderHistory {
		t.Fatalf("listen_reader %v want %s (missing server_settings)", out["listen_reader"], listenReaderHistory)
	}
	if out["busy"] != false {
		t.Fatalf("busy %v", out["busy"])
	}
	if out["job"] != nil {
		t.Fatalf("job %v", out["job"])
	}
}

func TestAdminStatsRebuildHasPermViaNamedPermission(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "ops", Permissions: []string{statsRebuildPerm}}
	req := authedJSON(u, http.MethodPost, "/api/v1/admin/stats/rebuild", nil)
	rec := httptest.NewRecorder()
	s.adminStatsRebuildPost(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("named perm should pass HasPerm then 503 without Jobs, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestAdminStatsRebuildPostRequiresJobs(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "admin", IsAdmin: true}
	req := authedJSON(u, http.MethodPost, "/api/v1/admin/stats/rebuild", nil)
	rec := httptest.NewRecorder()
	s.adminStatsRebuildPost(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestAdminStatsRebuildDB(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := &Server{Pool: pool, Jobs: jobs.New(pool, nil)}
	admin := &auth.User{ID: uuid.New(), Username: "admin", IsAdmin: true}
	_, _ = pool.Exec(ctx, `
		UPDATE jobs SET status='cancelled', finished_at=now(), updated_at=now()
		WHERE type=$1 AND status IN ('queued','running','retry')`, statsRebuildJobType)

	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM jobs WHERE type=$1 AND payload->>'actor_id'=$2`, statsRebuildJobType, admin.ID.String())
	})

	get := authedJSON(admin, http.MethodGet, "/api/v1/admin/stats/rebuild", nil)
	grec := httptest.NewRecorder()
	s.adminStatsRebuildGet(grec, get)
	if grec.Code != http.StatusOK {
		t.Fatalf("GET status %d %s", grec.Code, grec.Body.String())
	}
	st := decodeMap(t, grec)
	if st["listen_reader"] != listenReaderHistory {
		t.Fatalf("default listen_reader %v", st["listen_reader"])
	}

	var perm int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM permissions WHERE name=$1`, statsRebuildPerm).Scan(&perm); err != nil || perm != 1 {
		t.Fatalf("permissions seed: count=%d err=%v", perm, err)
	}
	var attached int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM role_permissions rp
		JOIN roles r ON r.id=rp.role_id
		JOIN permissions p ON p.id=rp.permission_id
		WHERE r.name='Administrator' AND p.name=$1`, statsRebuildPerm).Scan(&attached); err != nil || attached < 1 {
		t.Fatalf("Administrator role_permissions seed: count=%d err=%v", attached, err)
	}

	post := authedJSON(admin, http.MethodPost, "/api/v1/admin/stats/rebuild", nil)
	prec := httptest.NewRecorder()
	s.adminStatsRebuildPost(prec, post)
	if prec.Code != http.StatusAccepted {
		t.Fatalf("POST status %d %s", prec.Code, prec.Body.String())
	}
	enqueued := decodeMap(t, prec)
	jid, _ := enqueued["job_id"].(string)
	if jid == "" {
		t.Fatalf("missing job_id %v", enqueued)
	}

	conflict := httptest.NewRecorder()
	s.adminStatsRebuildPost(conflict, authedJSON(admin, http.MethodPost, "/api/v1/admin/stats/rebuild", nil))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("second POST status %d %s", conflict.Code, conflict.Body.String())
	}
	cbody := decodeMap(t, conflict)
	if cbody["code"] != "rebuild_in_progress" {
		t.Fatalf("409 code %v", cbody["code"])
	}
	if cbody["job_id"] != jid {
		t.Fatalf("409 job_id %v want %s", cbody["job_id"], jid)
	}

	statusRec := httptest.NewRecorder()
	s.adminStatsRebuildGet(statusRec, authedJSON(admin, http.MethodGet, "/api/v1/admin/stats/rebuild", nil))
	got := decodeMap(t, statusRec)
	if got["busy"] != true {
		t.Fatalf("expected busy after enqueue, got %v", got)
	}
	job, _ := got["job"].(map[string]any)
	if job["id"] != jid {
		t.Fatalf("status job id %v want %s", job["id"], jid)
	}
}
