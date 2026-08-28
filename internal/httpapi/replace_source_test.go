package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
	"github.com/sounddock/sounddock/internal/config"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/scapex"
)

func replaceSourceRequest(u *auth.User, trackID uuid.UUID, body any) *http.Request {
	req := authedJSON(u, http.MethodPost, "/api/v1/tracks/"+trackID.String()+"/replace-source", body)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", trackID.String())
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestReplaceSourceForbiddenWithoutPerm(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "user"}
	rec := httptest.NewRecorder()
	s.replaceSource(rec, replaceSourceRequest(u, uuid.New(), map[string]string{
		"url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
	}))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	out := decodeMap(t, rec)
	if out["code"] != "forbidden" {
		t.Fatalf("code %v", out["code"])
	}
}

func TestReplaceSourceHasPermViaAdminFlag(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "admin", IsAdmin: true}
	rec := httptest.NewRecorder()
	s.replaceSource(rec, replaceSourceRequest(u, uuid.New(), map[string]string{
		"source_ref": "dQw4w9WgXcQ",
	}))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("admin should pass HasPerm then 503 without DB, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestReplaceSourceHasPermViaNamedPermission(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "ops", Permissions: []string{replaceSourcePerm}}
	rec := httptest.NewRecorder()
	s.replaceSource(rec, replaceSourceRequest(u, uuid.New(), map[string]string{
		"url": "dQw4w9WgXcQ",
	}))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("named perm should pass HasPerm then 503 without DB, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestReplaceSourceRejectsNonYouTube(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "admin", IsAdmin: true}
	rec := httptest.NewRecorder()
	s.replaceSource(rec, replaceSourceRequest(u, uuid.New(), map[string]string{
		"url": "https://example.com/a.mp3",
	}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestMountReplaceSourceAndRegisterJobs(t *testing.T) {
	s := &Server{}
	s.RegisterReplaceJobs()
	r := chi.NewRouter()
	s.MountReplaceSource(r)
	req := httptest.NewRequest(http.MethodPost, "/tracks/"+uuid.New().String()+"/replace-source", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatal("route not mounted")
	}
	jr := jobs.New(nil, nil)
	s = &Server{Jobs: jr}
	s.RegisterReplaceJobs()
}

func TestReplaceSourceEnqueueDoesNotWait(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := &Server{Pool: pool, Jobs: jobs.New(pool, nil)}
	admin := &auth.User{ID: uuid.New(), Username: "admin", IsAdmin: true}
	provID, libID, trackID := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc)
		VALUES ($1,$2,'managed',$3)`, provID, "rs-"+provID.String()[:8], []byte(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id, root_prefix, read_only)
		VALUES ($1,$2,'music',$3,'',false)`, libID, "rs "+libID.String()[:8], provID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title) VALUES ($1,$2,'Queued Replace')`, trackID, libID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM jobs WHERE payload->>'track_id'=$1`, trackID.String())
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, libID)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, provID)
	})

	rec := httptest.NewRecorder()
	s.replaceSource(rec, replaceSourceRequest(admin, trackID, map[string]string{
		"source_ref": "dQw4w9WgXcQ",
	}))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	out := decodeMap(t, rec)
	if out["job_id"] == nil || out["coalesce_key"] == "" {
		t.Fatalf("contract %v", out)
	}

	var perm int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM permissions WHERE name=$1`, replaceSourcePerm).Scan(&perm); err != nil || perm != 1 {
		t.Fatalf("permissions seed: count=%d err=%v", perm, err)
	}
	var attached int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM role_permissions rp
		JOIN roles r ON r.id=rp.role_id
		JOIN permissions p ON p.id=rp.permission_id
		WHERE r.name='Administrator' AND p.name=$1`, replaceSourcePerm).Scan(&attached); err != nil || attached < 1 {
		t.Fatalf("Administrator role_permissions seed: count=%d err=%v", attached, err)
	}

	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM jobs WHERE id=$1`, out["job_id"]).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "queued" {
		t.Fatalf("handler waited on job: status %s", status)
	}
}

func TestCommitReplaceLocalsDoesNotClobberOldObject(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	root := t.TempDir()
	oldKey := "library/old-original.m4a"
	oldAbs := filepath.Join(root, filepath.FromSlash(oldKey))
	if err := os.MkdirAll(filepath.Dir(oldAbs), 0o755); err != nil {
		t.Fatal(err)
	}
	oldBytes := []byte("old-decoder-bytes")
	if err := os.WriteFile(oldAbs, oldBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "dQw4w9WgXcQ.m4a")
	if err := os.WriteFile(src, []byte("new-audio-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	provID, libID, trackID, jobID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc)
		VALUES ($1,$2,'managed',$3)`, provID, "rs2-"+provID.String()[:8], []byte(root)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id, root_prefix, read_only)
		VALUES ($1,$2,'music',$3,'',false)`, libID, "rs2 "+libID.String()[:8], provID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title) VALUES ($1,$2,'Live Replace')`, trackID, libID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO track_files (track_id, library_id, storage_key, size_bytes, content_hash, quality)
		VALUES ($1,$2,$3,$4,'oldhash','original')`, trackID, libID, oldKey, len(oldBytes)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM track_sources WHERE track_id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM track_files WHERE track_id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, libID)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, provID)
	})

	s := &Server{Pool: pool, Cfg: config.Config{ManagedDir: root}}
	retired, newKey, err := s.commitReplaceLocals(ctx, jobID, trackID, libID, "youtube", "dQw4w9WgXcQ", []scapex.LocalTrack{{
		Path: src, VideoID: "dQw4w9WgXcQ", Title: "Numb", DurationMS: 180000,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if newKey == oldKey {
		t.Fatal("reused old storage_key")
	}
	if len(retired) != 1 || retired[0].StorageKey != oldKey {
		t.Fatalf("retired %+v", retired)
	}
	got, err := os.ReadFile(oldAbs)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(oldBytes) {
		t.Fatalf("old object overwritten in place: %q", got)
	}
	newAbs := filepath.Join(root, filepath.FromSlash(newKey))
	if _, err := os.Stat(newAbs); err != nil {
		t.Fatalf("new object missing: %v", err)
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM track_sources WHERE track_id=$1 AND source_ref='dQw4w9WgXcQ'`, trackID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("track_sources count=%d err=%v", n, err)
	}
	var liveKey string
	if err := pool.QueryRow(ctx, `
		SELECT storage_key FROM track_files
		WHERE track_id=$1 AND quality='original' AND deleted_at IS NULL`, trackID).Scan(&liveKey); err != nil {
		t.Fatal(err)
	}
	if liveKey != newKey {
		t.Fatalf("stream would see %s want %s", liveKey, newKey)
	}

	// Session still current: keep old managed file.
	if _, err := pool.Exec(ctx, `
		INSERT INTO playback_sessions (id, kind, owner_key, current_track_id, status)
		VALUES ($1,'user',$2,$3,'playing')`, uuid.New(), "rs-"+trackID.String()[:8], trackID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM playback_sessions WHERE current_track_id=$1`, trackID)
	})
	s.maybeDeleteRetiredReplaceFiles(ctx, jobID, trackID, retired)
	if _, err := os.Stat(oldAbs); err != nil {
		t.Fatal("busy replace deleted old file still open")
	}
}

func TestReplaceSourceNASNotDeleted(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	root := t.TempDir()
	key := "album/nas-track.flac"
	abs := filepath.Join(root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("nas-audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	provID, libID, trackID := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc)
		VALUES ($1,$2,'local',$3)`, provID, "nas-"+provID.String()[:8], []byte(root)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id, root_prefix, read_only)
		VALUES ($1,$2,'music',$3,'',false)`, libID, "NAS "+libID.String()[:8], provID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, libID)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, provID)
	})
	s := &Server{Pool: pool}
	s.maybeDeleteRetiredReplaceFiles(ctx, uuid.New(), trackID, []scapex.RetiredFile{{
		LibraryID: libID, StorageKey: key,
	}})
	if _, err := os.Stat(abs); err != nil {
		t.Fatal("NAS object was deleted")
	}
}
