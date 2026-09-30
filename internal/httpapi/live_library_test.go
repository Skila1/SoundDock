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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/artwork"
	"github.com/sounddock/sounddock/internal/auth"
	"github.com/sounddock/sounddock/internal/minilib"
)

type catalogFixture struct {
	lib, album, emptyAlbum, keepAlbum, artist, keepArtist uuid.UUID
	tracks                                                []uuid.UUID
	keepTrack                                             uuid.UUID
}

func seedCatalog(t *testing.T, pool *pgxpool.Pool) catalogFixture {
	t.Helper()
	ctx := context.Background()
	stor := uuid.New()
	f := catalogFixture{lib: uuid.New(), album: uuid.New(), keepAlbum: uuid.New(), artist: uuid.New(), keepArtist: uuid.New(), keepTrack: uuid.New()}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO storage_providers (id, name, type, config_enc) VALUES ($1,$2,'local',$3)`, stor, "cat-"+stor.String()[:8], []byte("/tmp"))
	exec(`INSERT INTO libraries (id, name, kind, storage_provider_id) VALUES ($1,$2,'music',$3)`, f.lib, "cat-"+f.lib.String()[:8], stor)
	exec(`INSERT INTO artists (id, name) VALUES ($1,'Gone Artist'), ($2,'Kept Artist')`, f.artist, f.keepArtist)
	exec(`INSERT INTO albums (id, title, library_id) VALUES ($1,'Gone Album',$3), ($2,'Kept Album',$3)`, f.album, f.keepAlbum, f.lib)
	exec(`INSERT INTO album_artists (album_id, artist_id) VALUES ($1,$2), ($3,$4)`, f.album, f.artist, f.keepAlbum, f.keepArtist)
	for i := 0; i < 2; i++ {
		id := uuid.New()
		f.tracks = append(f.tracks, id)
		exec(`INSERT INTO tracks (id, library_id, album_id, title, duration_ms) VALUES ($1,$2,$3,'Gone',1000)`, id, f.lib, f.album)
		exec(`INSERT INTO track_artists (track_id, artist_id) VALUES ($1,$2)`, id, f.artist)
	}
	exec(`INSERT INTO tracks (id, library_id, album_id, title, duration_ms) VALUES ($1,$2,$3,'Kept',1000)`, f.keepTrack, f.lib, f.keepAlbum)
	exec(`INSERT INTO track_artists (track_id, artist_id) VALUES ($1,$2)`, f.keepTrack, f.keepArtist)
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM artwork_assets WHERE owner_id = ANY($1)`, []uuid.UUID{f.album, f.keepAlbum, f.keepTrack})
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE library_id=$1`, f.lib)
		_, _ = pool.Exec(c, `DELETE FROM albums WHERE library_id=$1`, f.lib)
		_, _ = pool.Exec(c, `DELETE FROM artists WHERE id = ANY($1)`, []uuid.UUID{f.artist, f.keepArtist})
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, f.lib)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, stor)
	})
	return f
}

func TestDeleteTracksPrunesOnlyEmptiedAlbumsAndArtists(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	f := seedCatalog(t, pool)
	s := &Server{Pool: pool}
	n, _, err := s.deleteTrackIDs(ctx, f.tracks, false, uuid.Nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("deleted %d, want 2", n)
	}
	count := func(sql string, args ...any) int {
		var c int
		if err := pool.QueryRow(ctx, sql, args...).Scan(&c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	if count(`SELECT count(*) FROM albums WHERE id=$1`, f.album) != 0 {
		t.Fatal("album left empty by the delete should be gone")
	}
	if count(`SELECT count(*) FROM artists WHERE id=$1`, f.artist) != 0 {
		t.Fatal("artist left empty by the delete should be gone")
	}
	if count(`SELECT count(*) FROM albums WHERE id=$1`, f.keepAlbum) != 1 || count(`SELECT count(*) FROM artists WHERE id=$1`, f.keepArtist) != 1 {
		t.Fatal("albums and artists that still have songs must stay")
	}
}

func solidJPEG(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, c)
		}
	}
	buf := &bytes.Buffer{}
	if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func servedColor(t *testing.T, rec *httptest.ResponseRecorder) (r, g, b uint32) {
	t.Helper()
	if rec.Code != 200 {
		t.Fatalf("artwork status %d %s", rec.Code, rec.Body.String())
	}
	img, err := jpeg.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ = img.At(4, 4).RGBA()
	return r >> 8, g >> 8, b >> 8
}

func TestUploadedAlbumCoverWinsOverEmbeddedArt(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	f := seedCatalog(t, pool)
	s := &Server{Pool: pool, Art: artwork.New(pool, t.TempDir())}
	admin := &auth.User{ID: uuid.New(), Username: "admin", IsAdmin: true, Permissions: []string{"tracks.read"}}

	red := color.RGBA{R: 220, A: 255}
	blue := color.RGBA{B: 220, A: 255}
	green := color.RGBA{G: 220, A: 255}
	// Track has embedded art; the album gets a user upload, then a rescan adds newer embedded album art.
	if _, err := s.Art.Save(ctx, "track", f.keepTrack, "embedded", bytes.NewReader(solidJPEG(t, red))); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Art.Save(ctx, "album", f.keepAlbum, "user", bytes.NewReader(solidJPEG(t, blue))); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Art.Save(ctx, "album", f.keepAlbum, "embedded", bytes.NewReader(solidJPEG(t, green))); err != nil {
		t.Fatal(err)
	}

	get := func(handler http.HandlerFunc, path string, id uuid.UUID) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := authedJSON(admin, http.MethodGet, path, nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", id.String())
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		handler(rec, req)
		return rec
	}
	if r, g, b := servedColor(t, get(s.albumArtwork, "/api/v1/albums/x/artwork?size=card", f.keepAlbum)); b < 150 || r > 80 || g > 80 {
		t.Fatalf("album page shows %d,%d,%d; the uploaded cover must beat newer embedded art", r, g, b)
	}
	if r, g, b := servedColor(t, get(s.trackArtwork, "/api/v1/tracks/x/artwork?size=card", f.keepTrack)); b < 150 || r > 80 {
		t.Fatalf("track shows %d,%d,%d; the uploaded album cover must beat the track's embedded art", r, g, b)
	}

	rec := httptest.NewRecorder()
	req := authedJSON(admin, http.MethodDelete, "/api/v1/albums/x/artwork", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", f.keepAlbum.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	s.deleteAlbumArtwork(rec, req)
	if rec.Code != 200 {
		t.Fatalf("remove cover %d %s", rec.Code, rec.Body.String())
	}
	if r, g, b := servedColor(t, get(s.albumArtwork, "/api/v1/albums/x/artwork?size=card", f.keepAlbum)); g < 150 || b > 80 {
		t.Fatalf("after removing the upload the embedded cover should show, got %d,%d,%d", r, g, b)
	}
}

func TestRemoveFromMyLibrary(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	f := seedCatalog(t, pool)
	uid := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`, uid, "ml-"+uid.String()[:8]); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM personal_library_owners WHERE user_id=$1`, uid)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id=$1`, uid)
	})
	all := append([]uuid.UUID{f.keepTrack}, f.tracks...)
	if err := minilib.Record(ctx, pool, "user", uid, "", all); err != nil {
		t.Fatal(err)
	}
	s := &Server{Pool: pool}
	u := &auth.User{ID: uid, Username: "ml", Permissions: []string{"tracks.read"}}
	del := func(body map[string]any) int {
		rec := httptest.NewRecorder()
		s.removeFromMyLibrary(rec, authedJSON(u, http.MethodDelete, "/api/v1/me/library", body))
		return rec.Code
	}
	entries := func() int {
		var n int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM personal_library_entries e JOIN personal_library_owners o ON o.id=e.owner_id WHERE o.user_id=$1`, uid).Scan(&n)
		return n
	}
	if code := del(map[string]any{"track_ids": []string{f.keepTrack.String()}}); code != 200 {
		t.Fatalf("remove one %d", code)
	}
	if entries() != 2 {
		t.Fatalf("entries %d after removing one, want 2", entries())
	}
	if code := del(map[string]any{}); code != 400 {
		t.Fatalf("empty body should be rejected, got %d", code)
	}
	if code := del(map[string]any{"all": true}); code != 200 || entries() != 0 {
		t.Fatalf("clear: code %d entries %d", code, entries())
	}
	var tracks int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE id = ANY($1)`, all).Scan(&tracks)
	if tracks != 3 {
		t.Fatal("clearing My Library must not delete catalogue tracks")
	}
}

func TestCrossProcessStateSignalAlsoPublishesPlayhead(t *testing.T) {
	s, pool := wave1HTTPServer(t)
	u := seedQueueUser(t, pool, "")
	ctx := context.Background()
	sid, err := s.Play.WebSession(ctx, u.ID, "browser-live")
	if err != nil {
		t.Fatal(err)
	}
	sub := s.sessionHub().subscribe(sid)
	defer s.sessionHub().unsubscribe(sid, sub)

	// What the Discord worker's skip/pause/seek sends from its own process.
	s.handleLivePayload(ctx, `{"t":"session.state","sid":"`+sid.String()+`","scope":"session"}`)

	got := map[string]bool{}
	for len(sub.ch) > 0 {
		ev := <-sub.ch
		got[ev.name] = true
	}
	if !got[sseEventState] || !got[sseEventPlayhead] {
		t.Fatalf("events %v: a state change from another process must also move the progress bar", got)
	}
}
