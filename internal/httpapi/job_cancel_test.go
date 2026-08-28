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
	"github.com/sounddock/sounddock/internal/jobs"
)

func TestCancelJobAllowlistHTTP(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	runner := jobs.New(pool, nil)
	s := &Server{Pool: pool, Jobs: runner}
	admin := &auth.User{ID: uuid.New(), Username: "cadmin", IsAdmin: true}

	insert := func(typ, status string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		if _, err := pool.Exec(ctx, `
			INSERT INTO jobs (id, type, status, payload, pool, progress)
			VALUES ($1,$2,$3,'{}','maintenance',0)`, id, typ, status); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM jobs WHERE id=$1`, id) })
		return id
	}

	cancel := func(id uuid.UUID) *httptest.ResponseRecorder {
		t.Helper()
		req := authedJSON(admin, http.MethodPost, "/api/v1/admin/jobs/"+id.String()+"/cancel", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", id.String())
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		s.cancelJob(rec, req)
		return rec
	}

	scanID := insert("library.scan", "queued")
	rec := cancel(scanID)
	if rec.Code != 200 {
		t.Fatalf("scan cancel %d %s", rec.Code, rec.Body.String())
	}

	mergeID := insert("library.merge", "queued")
	rec = cancel(mergeID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("merge cancel %d want 409 %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "not_cancellable" {
		t.Fatalf("code %v", body["code"])
	}

	rebuild := insert("stats.rebuild", "running")
	rec = cancel(rebuild)
	if rec.Code != http.StatusConflict {
		t.Fatalf("running rebuild cancel %d want 409", rec.Code)
	}

	recent := runner.RecentJobs(ctx, 20)
	var sawMerge, sawScan bool
	for _, j := range recent {
		if j.ID == mergeID && j.Cancellable {
			t.Fatal("merge must not be cancellable in listing")
		}
		if j.ID == mergeID {
			sawMerge = true
		}
		if j.ID == scanID {
			sawScan = true
			if j.Cancellable {
				t.Fatal("cancelled scan should not stay cancellable")
			}
		}
	}
	if !sawMerge || !sawScan {
		t.Fatalf("recent jobs missing merge=%v scan=%v", sawMerge, sawScan)
	}
}

func TestCancelJobRejectsDestructiveTypes(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := &Server{Pool: pool, Jobs: jobs.New(pool, nil)}
	admin := &auth.User{ID: uuid.New(), Username: "cadmin2", IsAdmin: true}
	for _, typ := range []string{"library.migrate", "tracks.bulk_delete", "backup.run"} {
		id := uuid.New()
		if _, err := pool.Exec(ctx, `
			INSERT INTO jobs (id, type, status, payload, pool) VALUES ($1,$2,'queued','{}','maintenance')`, id, typ); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM jobs WHERE id=$1`, id) })
		req := authedJSON(admin, http.MethodPost, "/api/v1/admin/jobs/"+id.String()+"/cancel", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", id.String())
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		s.cancelJob(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("%s cancel %d want 409", typ, rec.Code)
		}
	}
}
