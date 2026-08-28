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

func TestRequireLibraryHelpersWithoutPool(t *testing.T) {
	s := &Server{}
	id := uuid.New()
	u := &auth.User{ID: uuid.New(), Username: "u"}

	rec := httptest.NewRecorder()
	req := authedJSON(u, http.MethodGet, "/api/v1/tracks/"+id.String(), nil)
	if s.requireTrackLibrary(rec, req, id, "read") {
		t.Fatal("read without pool should fail")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("track read %d body %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	if s.requireAlbumLibrary(rec, req, id, "read") {
		t.Fatal("album read without pool should fail")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("album read %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	if s.requireArtistLibrary(rec, req, id, "read") {
		t.Fatal("artist read without pool should fail")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("artist read %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	if s.requireTrackLibrary(rec, req, id, "stream") {
		t.Fatal("stream without pool should fail")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("stream %d", rec.Code)
	}
}

func TestDuplicatesRequiresAdmin(t *testing.T) {
	s := &Server{}
	h := s.requireAdmin(http.HandlerFunc(s.duplicates))
	u := &auth.User{ID: uuid.New(), Username: "user"}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedJSON(u, http.MethodGet, "/api/v1/duplicates", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin %d body %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "forbidden" {
		t.Fatalf("code %v", body["code"])
	}
}

func TestMergeArtistsRequiresCapabilityWithoutPool(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "user"}
	req := authedJSON(u, http.MethodPost, "/api/v1/artists/merge", map[string]any{
		"into": uuid.New().String(),
		"from": uuid.New().String(),
	})
	rec := httptest.NewRecorder()
	s.mergeArtists(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "forbidden" {
		t.Fatalf("code %v", body["code"])
	}
}

func TestWriteAcquireErrLibraryGrant(t *testing.T) {
	s := &Server{}
	for _, msg := range []string{"library stream not granted", "library write not granted"} {
		rec := httptest.NewRecorder()
		if !s.writeAcquireErr(rec, errString(msg)) {
			t.Fatalf("expected handled %s", msg)
		}
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s status %d", msg, rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body["code"] != "library_grant" {
			t.Fatalf("%s code %v", msg, body["code"])
		}
	}
}

type catalogFix struct {
	grantFix
	albumID, nullAlbumID, artistID uuid.UUID
}

func seedCatalogIDOR(t *testing.T, pool *pgxpool.Pool) catalogFix {
	t.Helper()
	fix := catalogFix{grantFix: seedGrantLibs(t, pool)}
	fix.albumID = uuid.New()
	fix.nullAlbumID = uuid.New()
	fix.artistID = uuid.New()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO albums (id, title, library_id) VALUES ($1,$2,$3)`,
		fix.albumID, "w4-album", fix.libA); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO albums (id, title) VALUES ($1,$2)`,
		fix.nullAlbumID, "w4-null-album"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO artists (id, name) VALUES ($1,$2)`,
		fix.artistID, "w4-artist"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO track_artists (track_id, artist_id, role, position)
		VALUES ($1,$2,'primary',0)`, fix.trackID, fix.artistID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE tracks SET album_id=$2 WHERE id=$1`, fix.trackID, fix.albumID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `UPDATE tracks SET album_id=NULL WHERE id=$1`, fix.trackID)
		_, _ = pool.Exec(c, `DELETE FROM track_artists WHERE artist_id=$1`, fix.artistID)
		_, _ = pool.Exec(c, `DELETE FROM album_artists WHERE artist_id=$1`, fix.artistID)
		_, _ = pool.Exec(c, `DELETE FROM artists WHERE id=$1`, fix.artistID)
		_, _ = pool.Exec(c, `DELETE FROM albums WHERE id IN ($1,$2)`, fix.albumID, fix.nullAlbumID)
	})
	return fix
}

func catalogGet(s *Server, u *auth.User, path, param, id string, h http.HandlerFunc) *httptest.ResponseRecorder {
	req := authedJSON(u, http.MethodGet, path, nil)
	rctx := chi.NewRouteContext()
	if param != "" {
		rctx.URLParams.Add(param, id)
	}
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func assertJSONCode(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d body %s want %d", rec.Code, rec.Body.String(), status)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != code {
		t.Fatalf("code %v want %s body %s", body["code"], code, rec.Body.String())
	}
}

func TestCatalogIDORHiddenGET404(t *testing.T) {
	pool := testPool(t)
	s := &Server{Pool: pool}
	fix := seedCatalogIDOR(t, pool)
	u := &auth.User{ID: fix.userID, Username: "user", Permissions: []string{"tracks.read", "tracks.stream"}}

	cases := []struct {
		name  string
		path  string
		id    uuid.UUID
		param string
		h     http.HandlerFunc
	}{
		{"track", "/api/v1/tracks/" + fix.trackID.String(), fix.trackID, "id", s.getTrack},
		{"metadata", "/api/v1/tracks/" + fix.trackID.String() + "/metadata", fix.trackID, "id", s.getTrackMetadata},
		{"lyrics", "/api/v1/tracks/" + fix.trackID.String() + "/lyrics", fix.trackID, "id", s.getTrackLyrics},
		{"playability", "/api/v1/tracks/" + fix.trackID.String() + "/playability", fix.trackID, "id", s.getTrackPlayability},
		{"waveform", "/api/v1/tracks/" + fix.trackID.String() + "/waveform", fix.trackID, "id", s.getTrackWaveform},
		{"album", "/api/v1/albums/" + fix.albumID.String(), fix.albumID, "id", s.getAlbum},
		{"null album", "/api/v1/albums/" + fix.nullAlbumID.String(), fix.nullAlbumID, "id", s.getAlbum},
		{"artist", "/api/v1/artists/" + fix.artistID.String(), fix.artistID, "id", s.getArtist},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := catalogGet(s, u, c.path, c.param, c.id.String(), c.h)
			assertJSONCode(t, rec, http.StatusNotFound, "not_found")
		})
	}
}

func TestCatalogIDORHiddenStreamMint403(t *testing.T) {
	pool := testPool(t)
	s := &Server{Pool: pool, SignKey: []byte("w4-sign-key-32-bytes-long!!!!!!")}
	fix := seedCatalogIDOR(t, pool)
	u := &auth.User{ID: fix.userID, Username: "user", Permissions: []string{"tracks.stream"}}

	req := authedJSON(u, http.MethodPost, "/api/v1/stream-tokens", map[string]any{"track_id": fix.trackID.String()})
	rec := httptest.NewRecorder()
	s.streamTokens(rec, req)
	assertJSONCode(t, rec, http.StatusForbidden, "library_grant")

	mint := authedJSON(u, http.MethodPost, "/api/v1/me/offline/tokens", map[string]any{
		"track_id":  fix.trackID.String(),
		"device_id": "w4-device",
	})
	mrec := httptest.NewRecorder()
	s.mintOfflineToken(mrec, mint)
	assertJSONCode(t, mrec, http.StatusForbidden, "library_grant")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", fix.trackID.String())
	streamReq := authedJSON(u, http.MethodGet, "/api/v1/tracks/"+fix.trackID.String()+"/stream", nil)
	streamReq = streamReq.WithContext(context.WithValue(streamReq.Context(), chi.RouteCtxKey, rctx))
	srec := httptest.NewRecorder()
	s.streamTrack(srec, streamReq)
	assertJSONCode(t, srec, http.StatusForbidden, "library_grant")
}

func TestCatalogIDORAdminAndGrantedSucceed(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := &Server{Pool: pool, SignKey: []byte("w4-sign-key-32-bytes-long!!!!!!")}
	fix := seedCatalogIDOR(t, pool)
	if _, err := pool.Exec(ctx, `
		INSERT INTO library_grants (library_id, user_id, actions)
		VALUES ($1,$2, ARRAY['read','stream','write'])`, fix.libA, fix.userID); err != nil {
		t.Fatal(err)
	}
	granted := &auth.User{ID: fix.userID, Username: "user", Permissions: []string{"tracks.read", "tracks.stream"}}
	admin := &auth.User{ID: fix.adminID, Username: "admin", IsAdmin: true, Permissions: []string{"tracks.read"}}

	for _, u := range []*auth.User{granted, admin} {
		rec := catalogGet(s, u, "/api/v1/tracks/"+fix.trackID.String(), "id", fix.trackID.String(), s.getTrack)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s getTrack %d %s", u.Username, rec.Code, rec.Body.String())
		}
		rec = catalogGet(s, u, "/api/v1/albums/"+fix.albumID.String(), "id", fix.albumID.String(), s.getAlbum)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s getAlbum %d %s", u.Username, rec.Code, rec.Body.String())
		}
		rec = catalogGet(s, u, "/api/v1/artists/"+fix.artistID.String(), "id", fix.artistID.String(), s.getArtist)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s getArtist %d %s", u.Username, rec.Code, rec.Body.String())
		}
		rec = catalogGet(s, u, "/api/v1/tracks/"+fix.trackID.String()+"/playability", "id", fix.trackID.String(), s.getTrackPlayability)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s playability %d %s", u.Username, rec.Code, rec.Body.String())
		}
	}

	req := authedJSON(granted, http.MethodPost, "/api/v1/stream-tokens", map[string]any{"track_id": fix.trackID.String()})
	rec := httptest.NewRecorder()
	s.streamTokens(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("granted mint %d %s", rec.Code, rec.Body.String())
	}

	nullRec := catalogGet(s, granted, "/api/v1/albums/"+fix.nullAlbumID.String(), "id", fix.nullAlbumID.String(), s.getAlbum)
	assertJSONCode(t, nullRec, http.StatusNotFound, "not_found")
	adminNull := catalogGet(s, admin, "/api/v1/albums/"+fix.nullAlbumID.String(), "id", fix.nullAlbumID.String(), s.getAlbum)
	if adminNull.Code != http.StatusOK {
		t.Fatalf("admin null album %d %s", adminNull.Code, adminNull.Body.String())
	}
}

func TestListAlbumsDropsNullLibrary(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := &Server{Pool: pool}
	fix := seedCatalogIDOR(t, pool)
	if _, err := pool.Exec(ctx, `
		INSERT INTO library_grants (library_id, user_id, actions)
		VALUES ($1,$2, ARRAY['read','stream'])`, fix.libA, fix.userID); err != nil {
		t.Fatal(err)
	}
	u := &auth.User{ID: fix.userID, Username: "user", Permissions: []string{"tracks.read"}}
	req := authedJSON(u, http.MethodGet, "/api/v1/albums", nil)
	rec := httptest.NewRecorder()
	s.listAlbums(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	sawGranted, sawNull := false, false
	for _, row := range rows {
		id, _ := row["id"].(string)
		if id == fix.albumID.String() {
			sawGranted = true
		}
		if id == fix.nullAlbumID.String() {
			sawNull = true
		}
	}
	if !sawGranted {
		t.Fatal("granted album missing from list")
	}
	if sawNull {
		t.Fatal("NULL-library album must not appear for non-admin")
	}
}

func TestDuplicatesAdminAllowed(t *testing.T) {
	pool := testPool(t)
	s := &Server{Pool: pool}
	fix := seedGrantLibs(t, pool)
	admin := &auth.User{ID: fix.adminID, Username: "admin", IsAdmin: true}
	h := s.requireAdmin(http.HandlerFunc(s.duplicates))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedJSON(admin, http.MethodGet, "/api/v1/duplicates", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin duplicates %d %s", rec.Code, rec.Body.String())
	}
	user := &auth.User{ID: fix.userID, Username: "user"}
	urec := httptest.NewRecorder()
	h.ServeHTTP(urec, authedJSON(user, http.MethodGet, "/api/v1/duplicates", nil))
	assertJSONCode(t, urec, http.StatusForbidden, "forbidden")
}

func TestAlbumArtistMutationRequiresWrite(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := &Server{Pool: pool}
	fix := seedCatalogIDOR(t, pool)
	editor := editorUser(fix.userID, "editor")

	patchAlbum := func() *httptest.ResponseRecorder {
		req := authedJSON(editor, http.MethodPatch, "/api/v1/albums/"+fix.albumID.String()+"/metadata", map[string]any{"title": "x"})
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", fix.albumID.String())
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		s.patchAlbumMetadata(rec, req)
		return rec
	}
	rec := patchAlbum()
	assertJSONCode(t, rec, http.StatusForbidden, "library_grant")

	patchArtist := func() *httptest.ResponseRecorder {
		req := authedJSON(editor, http.MethodPatch, "/api/v1/artists/"+fix.artistID.String()+"/metadata", map[string]any{"name": "y"})
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", fix.artistID.String())
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		s.patchArtistMetadata(rec, req)
		return rec
	}
	rec = patchArtist()
	assertJSONCode(t, rec, http.StatusForbidden, "library_grant")

	if _, err := pool.Exec(ctx, `
		INSERT INTO library_grants (library_id, user_id, actions)
		VALUES ($1,$2, ARRAY['read','stream','write'])`, fix.libA, fix.userID); err != nil {
		t.Fatal(err)
	}
	rec = patchAlbum()
	if rec.Code != http.StatusOK {
		t.Fatalf("album write %d %s", rec.Code, rec.Body.String())
	}
	rec = patchArtist()
	if rec.Code != http.StatusOK {
		t.Fatalf("artist write %d %s", rec.Code, rec.Body.String())
	}

	admin := &auth.User{ID: fix.adminID, Username: "admin", IsAdmin: true}
	req := authedJSON(admin, http.MethodPatch, "/api/v1/albums/"+fix.albumID.String(), map[string]any{"title": "admin-title"})
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", fix.albumID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	arec := httptest.NewRecorder()
	s.patchAlbum(arec, req)
	if arec.Code != http.StatusOK {
		t.Fatalf("admin patchAlbum %d %s", arec.Code, arec.Body.String())
	}
}

func TestMergeArtistsWriteOnTouchedLibraries(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := &Server{Pool: pool}
	fix := seedCatalogIDOR(t, pool)
	into := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO artists (id, name) VALUES ($1,$2)`, into, "w4-into"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM artists WHERE id=$1`, into)
	})

	merger := &auth.User{ID: fix.userID, Username: "merge", Permissions: []string{"tracks.merge"}}
	req := authedJSON(merger, http.MethodPost, "/api/v1/artists/merge", map[string]any{
		"into": into.String(), "from": fix.artistID.String(),
	})
	rec := httptest.NewRecorder()
	s.mergeArtists(rec, req)
	assertJSONCode(t, rec, http.StatusForbidden, "library_grant")

	if _, err := pool.Exec(ctx, `
		INSERT INTO library_grants (library_id, user_id, actions)
		VALUES ($1,$2, ARRAY['read','stream','write'])`, fix.libA, fix.userID); err != nil {
		t.Fatal(err)
	}
	req = authedJSON(merger, http.MethodPost, "/api/v1/artists/merge", map[string]any{
		"into": into.String(), "from": fix.artistID.String(),
	})
	rec = httptest.NewRecorder()
	s.mergeArtists(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("merge with write %d %s", rec.Code, rec.Body.String())
	}

	admin := &auth.User{ID: fix.adminID, Username: "admin", IsAdmin: true}
	from2 := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO artists (id, name) VALUES ($1,$2)`, from2, "w4-from2"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM artists WHERE id=$1`, from2)
	})
	areq := authedJSON(admin, http.MethodPost, "/api/v1/artists/merge", map[string]any{
		"into": into.String(), "from": from2.String(),
	})
	arec := httptest.NewRecorder()
	s.mergeArtists(arec, areq)
	if arec.Code != http.StatusOK {
		t.Fatalf("admin merge %d %s", arec.Code, arec.Body.String())
	}
}
