package httpapi

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/artwork"
	"github.com/sounddock/sounddock/internal/auth"
)

func TestTrackArtworkNullAlbumIsNotFoundNotCrash(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	stor, lib, trackID := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO storage_providers (id, name, type, config_enc) VALUES ($1,$2,'local',$3)`, stor, "art-"+stor.String()[:8], []byte("/tmp")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO libraries (id, name, kind, storage_provider_id) VALUES ($1,$2,'music',$3)`, lib, "art-"+lib.String()[:8], stor); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tracks (id, library_id, title, duration_ms, acquisition, acquisition_ref) VALUES ($1,$2,'No Album',1000,'youtube','not-a-video')`, trackID, lib); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM artwork_assets WHERE owner_id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, lib)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, stor)
	})

	dir := t.TempDir()
	s := &Server{Pool: pool, Art: artwork.New(pool, dir)}
	admin := &auth.User{ID: uuid.New(), Username: "admin", IsAdmin: true, Permissions: []string{"tracks.read"}}

	rec := httptest.NewRecorder()
	req := authedJSON(admin, http.MethodGet, "/api/v1/tracks/"+trackID.String()+"/artwork?size=card", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", trackID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	s.trackArtwork(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("null album without art %d %s", rec.Code, rec.Body.String())
	}

	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 40, B: 40, A: 255})
		}
	}
	buf := &bytes.Buffer{}
	if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Art.Save(ctx, "track", trackID, "test", bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}

	rec = httptest.NewRecorder()
	req = authedJSON(admin, http.MethodGet, "/api/v1/tracks/"+trackID.String()+"/artwork?size=card", nil)
	rctx = chi.NewRouteContext()
	rctx.URLParams.Add("id", trackID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	s.trackArtwork(rec, req)
	if rec.Code != 200 {
		t.Fatalf("track-owned art %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "image/jpeg" && rec.Body.Len() < 32 {
		t.Fatalf("content %s len %d", rec.Header().Get("Content-Type"), rec.Body.Len())
	}
}
