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
	"github.com/sounddock/sounddock/internal/minilib"
)

func TestPersonalLibraryPrivacy(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	hash, _ := auth.HashPassword("x")
	ownerID, peerID := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{ownerID, peerID} {
		if _, err := pool.Exec(ctx, `INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,$3,$2)`, id, "pl-"+id.String()[:8], hash); err != nil {
			t.Fatal(err)
		}
	}
	if err := minilib.SetVisibility(ctx, pool, ownerID, "private"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM personal_library_owners WHERE user_id IN ($1,$2)`, ownerID, peerID)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id IN ($1,$2)`, ownerID, peerID)
	})

	s := &Server{Pool: pool}
	owner := &auth.User{ID: ownerID, Username: "owner", Permissions: []string{"tracks.read"}}
	peer := &auth.User{ID: peerID, Username: "peer", Permissions: []string{"tracks.read"}}
	admin := &auth.User{ID: peerID, Username: "admin", IsAdmin: true, Permissions: []string{"admin", "tracks.read"}}

	rec := httptest.NewRecorder()
	req := authedJSON(peer, http.MethodGet, "/api/v1/users/"+ownerID.String()+"/library", nil)
	req = withChiURL(req, "id", ownerID.String())
	s.userPersonalLibrary(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("peer private %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = authedJSON(owner, http.MethodGet, "/api/v1/users/"+ownerID.String()+"/library", nil)
	req = withChiURL(req, "id", ownerID.String())
	s.userPersonalLibrary(rec, req)
	if rec.Code != 200 {
		t.Fatalf("owner %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = authedJSON(admin, http.MethodGet, "/api/v1/users/"+ownerID.String()+"/library", nil)
	req = withChiURL(req, "id", ownerID.String())
	s.userPersonalLibrary(rec, req)
	if rec.Code != 200 {
		t.Fatalf("admin %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["inspecting"] != true {
		t.Fatalf("admin should inspect: %+v", body)
	}

	if err := minilib.SetVisibility(ctx, pool, ownerID, "public"); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	req = authedJSON(peer, http.MethodGet, "/api/v1/users/"+ownerID.String()+"/library", nil)
	req = withChiURL(req, "id", ownerID.String())
	s.userPersonalLibrary(rec, req)
	if rec.Code != 200 {
		t.Fatalf("peer public %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = authedJSON(owner, http.MethodGet, "/api/v1/me/library", nil)
	s.myPersonalLibrary(rec, req)
	if rec.Code != 200 {
		t.Fatalf("me library %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = authedJSON(admin, http.MethodGet, "/api/v1/admin/users/"+ownerID.String()+"/library", nil)
	req = withChiURL(req, "id", ownerID.String())
	s.adminUserPersonalLibrary(rec, req)
	if rec.Code != 200 {
		t.Fatalf("admin inspect %d %s", rec.Code, rec.Body.String())
	}
}

func withChiURL(r *http.Request, key, val string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}
