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

func editorUser(id uuid.UUID, name string) *auth.User {
	return &auth.User{ID: id, Username: name, Permissions: []string{"admin", "library.import_url"}}
}

func TestPatchTrackMetadataRequiresWriteGrant(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := &Server{Pool: pool}
	fix := seedGrantLibs(t, pool)

	title := "granted-write"
	patch := func(u *auth.User) *httptest.ResponseRecorder {
		t.Helper()
		req := authedJSON(u, http.MethodPatch, "/api/v1/tracks/"+fix.trackID.String()+"/metadata", map[string]any{"title": title})
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", fix.trackID.String())
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		s.patchTrackMetadata(rec, req)
		return rec
	}

	editor := editorUser(fix.userID, "editor")
	rec := patch(editor)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no grant: %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "library_grant" {
		t.Fatalf("code %v want library_grant", body["code"])
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO library_grants (library_id, user_id, actions)
		VALUES ($1,$2, ARRAY['read','stream'])`, fix.libA, fix.userID); err != nil {
		t.Fatal(err)
	}
	rec = patch(editor)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read grant must not write: %d %s", rec.Code, rec.Body.String())
	}

	if _, err := pool.Exec(ctx, `
		UPDATE library_grants SET actions = ARRAY['read','stream','write']
		WHERE library_id=$1 AND user_id=$2`, fix.libA, fix.userID); err != nil {
		t.Fatal(err)
	}
	rec = patch(editor)
	if rec.Code != 200 {
		t.Fatalf("user write grant: %d %s", rec.Code, rec.Body.String())
	}

	req := authedJSON(editor, http.MethodGet, "/api/v1/tracks/"+fix.trackID.String()+"/metadata", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", fix.trackID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	grec := httptest.NewRecorder()
	s.getTrackMetadata(grec, req)
	if grec.Code != 200 {
		t.Fatalf("read metadata %d %s", grec.Code, grec.Body.String())
	}
}

func TestPatchTrackMetadataRoleWriteGrant(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := &Server{Pool: pool}
	fix := seedGrantLibs(t, pool)

	var roleID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM roles WHERE name='User'`).Scan(&roleID); err != nil {
		t.Skip(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, fix.userID, roleID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO library_grants (library_id, user_id, actions)
		VALUES ($1,$2, ARRAY['read','stream'])`, fix.libA, fix.userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO library_grants (library_id, role_id, actions)
		VALUES ($1,$2, ARRAY['write'])`, fix.libA, roleID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM user_roles WHERE user_id=$1`, fix.userID)
	})

	editor := editorUser(fix.userID, "role-editor")
	req := authedJSON(editor, http.MethodPatch, "/api/v1/tracks/"+fix.trackID.String()+"/metadata", map[string]any{"title": "role-write"})
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", fix.trackID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	s.patchTrackMetadata(rec, req)
	if rec.Code != 200 {
		t.Fatalf("role write should union with user read grant: %d %s", rec.Code, rec.Body.String())
	}
}

func TestImportURLRequiresWriteGrant(t *testing.T) {
	pool := testPool(t)
	s := &Server{Pool: pool}
	fix := seedGrantLibs(t, pool)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO library_grants (library_id, user_id, actions)
		VALUES ($1,$2, ARRAY['read','stream'])`, fix.libA, fix.userID); err != nil {
		t.Fatal(err)
	}
	u := &auth.User{ID: fix.userID, Username: "imp", Permissions: []string{"library.import_url"}}
	req := authedJSON(u, http.MethodPost, "/api/v1/imports/url", map[string]any{
		"url":        "https://example.com/a.mp3",
		"library_id": fix.libA.String(),
	})
	rec := httptest.NewRecorder()
	s.importURL(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "library_grant" {
		t.Fatalf("code %v", body["code"])
	}
}

func TestLibraryIDsWriteFromRoleOrUser(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := &Server{Pool: pool}
	fix := seedGrantLibs(t, pool)
	user := &auth.User{ID: fix.userID, Username: "u"}
	if uuidIn(s.libraryIDsFor(ctx, user, "write"), fix.libA) {
		t.Fatal("no grant should not write")
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO library_grants (library_id, user_id, actions)
		VALUES ($1,$2, ARRAY['write'])`, fix.libA, fix.userID); err != nil {
		t.Fatal(err)
	}
	if !uuidIn(s.libraryIDsFor(ctx, user, "write"), fix.libA) {
		t.Fatal("user write grant")
	}
	if uuidIn(s.libraryIDsFor(ctx, user, "read"), fix.libA) {
		t.Fatal("write-only should not imply read")
	}
}
