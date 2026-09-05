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

func TestCreateAndDeleteAlbum(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := &Server{Pool: pool}
	u := seedQueueUser(t, pool, "")
	u.IsAdmin = true
	fix := seedGrantLibs(t, pool)
	if _, err := pool.Exec(ctx, `
		INSERT INTO library_grants (library_id, user_id, actions)
		VALUES ($1,$2, ARRAY['read','stream','write'])`, fix.libA, u.ID); err != nil {
		t.Fatal(err)
	}

	req := authedJSON(u, http.MethodPost, "/api/v1/albums", map[string]any{
		"title":      "Skila singles",
		"artist":     "Skila",
		"library_id": fix.libA.String(),
		"track_ids":  []string{fix.trackID.String()},
	})
	rec := httptest.NewRecorder()
	s.createAlbum(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	albumID, _ := created["id"].(string)
	if albumID == "" {
		t.Fatal("missing id")
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM albums WHERE id=$1`, albumID)
	})
	var got uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT album_id FROM tracks WHERE id=$1`, fix.trackID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got.String() != albumID {
		t.Fatalf("track album %s want %s", got, albumID)
	}

	del := authedJSON(u, http.MethodDelete, "/api/v1/albums/"+albumID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", albumID)
	del = del.WithContext(context.WithValue(del.Context(), chi.RouteCtxKey, rctx))
	drec := httptest.NewRecorder()
	s.deleteAlbum(drec, del)
	if drec.Code != 200 {
		t.Fatalf("delete %d %s", drec.Code, drec.Body.String())
	}
	var after *uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT album_id FROM tracks WHERE id=$1`, fix.trackID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != nil {
		t.Fatalf("album_id should be null after delete, got %v", after)
	}
}

func TestCreateAlbumRequiresTitle(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), IsAdmin: true}
	req := authedJSON(u, http.MethodPost, "/api/v1/albums", map[string]any{"title": "  "})
	rec := httptest.NewRecorder()
	s.createAlbum(rec, req)
	if rec.Code != 400 {
		t.Fatalf("status %d", rec.Code)
	}
}
