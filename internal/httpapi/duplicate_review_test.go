package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/auth"
)

func TestDuplicateReviewMountRegistersRoutes(t *testing.T) {
	r := chi.NewRouter()
	(&Server{}).MountDuplicateReview(r)
	got := map[string]bool{}
	if err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got[method+" "+route] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, need := range []string{
		"GET /duplicate-review",
		"POST /duplicate-review/{id}/merge",
		"POST /duplicate-review/{id}/ignore",
	} {
		if !got[need] {
			t.Fatalf("missing %s", need)
		}
	}
}

func TestDuplicateReviewMergeForbiddenWithoutPerm(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "user"}
	req := authedJSON(u, http.MethodPost, "/duplicate-review/"+uuid.New().String()+"/merge", map[string]any{
		"winner_id": uuid.New(), "loser_ids": []uuid.UUID{uuid.New()},
	})
	rec := httptest.NewRecorder()
	s.adminDuplicateReviewMerge(rec, req)
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

func TestDuplicateReviewMergeHasPermViaAdminFlag(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "admin", IsAdmin: true}
	req := authedJSON(u, http.MethodPost, "/duplicate-review/"+uuid.New().String()+"/merge", map[string]any{
		"winner_id": uuid.New(), "loser_ids": []uuid.UUID{uuid.New()},
	})
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", uuid.New().String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	s.adminDuplicateReviewMerge(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("admin should pass HasPerm then 503 without pool, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestDuplicateReviewMergeHasPermViaNamedPermission(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "ops", Permissions: []string{tracksMergePerm}}
	req := authedJSON(u, http.MethodPost, "/duplicate-review/"+uuid.New().String()+"/merge", map[string]any{
		"winner_id": uuid.New(), "loser_ids": []uuid.UUID{uuid.New()},
	})
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", uuid.New().String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	s.adminDuplicateReviewMerge(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("named perm should pass HasPerm then 503 without pool, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestDuplicateReviewListEmptyWithoutPool(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/duplicate-review", nil)
	rec := httptest.NewRecorder()
	s.adminDuplicateReviewList(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var groups []any
	if err := json.Unmarshal(rec.Body.Bytes(), &groups); err != nil {
		t.Fatalf("expected JSON array: %v body %s", err, rec.Body.String())
	}
	if len(groups) != 0 {
		t.Fatalf("groups %v", groups)
	}
}

func TestDuplicateReviewMergeMapsTrackInUseTo409(t *testing.T) {
	pool := testPool(t)
	requireReviewSchema(t, pool)
	ctx := context.Background()
	fix := seedDuplicateReview(t, pool)

	sid := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO playback_sessions (id, kind, owner_key, current_track_id, status)
		VALUES ($1,'web_device',$2,$3,'playing')`,
		sid, "dup-"+fix.user.String()[:8], fix.loser); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_sessions WHERE id=$1`, sid)
	})

	s := &Server{Pool: pool}
	admin := &auth.User{ID: fix.user, Username: "admin", IsAdmin: true}
	req := authedJSON(admin, http.MethodPost, "/duplicate-review/"+fix.reviewID.String()+"/merge", map[string]any{
		"winner_id": fix.winner, "loser_ids": []uuid.UUID{fix.loser},
	})
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", fix.reviewID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	s.adminDuplicateReviewMerge(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	out := decodeMap(t, rec)
	if out["code"] != "track_in_use" {
		t.Fatalf("code %v", out["code"])
	}
	if out["loser_id"] != fix.loser.String() {
		t.Fatalf("loser_id %v want %s", out["loser_id"], fix.loser)
	}

	var perm int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM permissions WHERE name=$1`, tracksMergePerm).Scan(&perm); err != nil || perm != 1 {
		t.Fatalf("permissions seed: count=%d err=%v", perm, err)
	}
}

func TestDuplicateReviewIgnore(t *testing.T) {
	pool := testPool(t)
	requireReviewSchema(t, pool)
	fix := seedDuplicateReview(t, pool)
	s := &Server{Pool: pool}
	admin := &auth.User{ID: fix.user, Username: "admin", IsAdmin: true}
	req := authedJSON(admin, http.MethodPost, "/duplicate-review/"+fix.reviewID.String()+"/ignore", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", fix.reviewID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	s.adminDuplicateReviewIgnore(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM duplicate_review_groups WHERE id=$1`, fix.reviewID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "ignored" {
		t.Fatalf("status %s", status)
	}
}

type dupFix struct {
	prov, lib, winner, loser, user, groupID, reviewID uuid.UUID
}

func requireReviewSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema='public' AND table_name='duplicate_review_groups'`).Scan(&n)
	if err != nil || n == 0 {
		t.Skip("0017 duplicate_review_groups not applied")
	}
}

func seedDuplicateReview(t *testing.T, pool *pgxpool.Pool) dupFix {
	t.Helper()
	ctx := context.Background()
	f := dupFix{
		prov: uuid.New(), lib: uuid.New(), winner: uuid.New(), loser: uuid.New(),
		user: uuid.New(), groupID: uuid.New(), reviewID: uuid.New(),
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc)
		VALUES ($1,$2,'managed',$3)`, f.prov, "dup-"+f.prov.String()[:8], []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id) VALUES ($1,$2,'music',$3)`,
		f.lib, "dup-lib-"+f.lib.String()[:8], f.prov); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title, duration_ms) VALUES
		($1,$3,'winner',180000), ($2,$3,'loser',181000)`, f.winner, f.loser, f.lib); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`,
		f.user, "dup-"+f.user.String()[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO duplicate_groups (id, method, blocking_key) VALUES ($1,'content_hash',$2)`,
		f.groupID, "content_hash:test-"+f.groupID.String()[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO duplicate_review_groups (id, group_id, status, reason, track_ids)
		VALUES ($1,$2,'open','content_hash',$3)`,
		f.reviewID, f.groupID, []uuid.UUID{f.winner, f.loser}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM duplicate_review_groups WHERE id=$1`, f.reviewID)
		_, _ = pool.Exec(c, `DELETE FROM duplicate_groups WHERE id=$1`, f.groupID)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=ANY($1)`, []uuid.UUID{f.winner, f.loser})
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, f.lib)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, f.prov)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id=$1`, f.user)
	})
	return f
}
