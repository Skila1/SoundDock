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
	"github.com/sounddock/sounddock/internal/lyrics"
)

func TestLyricsRoutesRegistered(t *testing.T) {
	h := (&Server{}).Router()
	r, ok := h.(*chi.Mux)
	if !ok {
		t.Fatalf("router type %T", h)
	}
	got := map[string]bool{}
	if err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got[method+" "+route] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{
		"GET /api/v1/tracks/{id}/lyrics",
		"GET /api/v1/admin/lyrics",
		"PUT /api/v1/admin/lyrics",
	} {
		if !got[n] {
			t.Fatalf("unregistered %s", n)
		}
	}
}

func TestAdminLyricsRequirePermForbidden(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "user"}
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		req := authedJSON(u, method, "/api/v1/admin/lyrics", map[string]any{"enabled": false})
		rec := httptest.NewRecorder()
		h := s.requirePerm(lyrics.PermConfigure, s.adminGetLyrics)
		if method == http.MethodPut {
			h = s.requirePerm(lyrics.PermConfigure, s.adminPutLyrics)
		}
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s without perm: status %d body %s", method, rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body["code"] != "forbidden" {
			t.Fatalf("code %v", body["code"])
		}
	}
}

func TestAdminLyricsAllowAdminFlag(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "admin", IsAdmin: true}
	req := authedJSON(u, http.MethodGet, "/api/v1/admin/lyrics", nil)
	rec := httptest.NewRecorder()
	s.requirePerm(lyrics.PermConfigure, s.adminGetLyrics).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	out := decodeMap(t, rec)
	if out["enabled"] != false {
		t.Fatalf("default enabled %v", out["enabled"])
	}
	if out["provider_url"] != "" {
		t.Fatalf("default url %v", out["provider_url"])
	}
}

func TestAdminLyricsRejectsUnknownHost(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "admin", IsAdmin: true}
	req := authedJSON(u, http.MethodPut, "/api/v1/admin/lyrics", map[string]any{
		"enabled":      true,
		"provider_url": "https://genius.com",
	})
	rec := httptest.NewRecorder()
	s.requirePerm(lyrics.PermConfigure, s.adminPutLyrics).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestAdminLyricsNamedPerm(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "ops", Permissions: []string{lyrics.PermConfigure}}
	req := authedJSON(u, http.MethodGet, "/api/v1/admin/lyrics", nil)
	rec := httptest.NewRecorder()
	s.requirePerm(lyrics.PermConfigure, s.adminGetLyrics).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("named perm should pass, got %d %s", rec.Code, rec.Body.String())
	}
}

func lyricsRequest(u *auth.User, trackID uuid.UUID) *http.Request {
	req := authedJSON(u, http.MethodGet, "/api/v1/tracks/"+trackID.String()+"/lyrics", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", trackID.String())
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestGetTrackLyricsEmptyWithoutPool(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "u"}
	rec := httptest.NewRecorder()
	s.getTrackLyrics(rec, lyricsRequest(u, uuid.New()))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestGetTrackLyricsEmbeddedDB(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	sid, libID, trackID := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc)
		VALUES ($1, $2, 'local', $3)`, sid, "lyh-"+sid.String()[:8], []byte(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id, root_prefix, read_only)
		VALUES ($1, $2, 'music', $3, '', false)`, libID, "lyh-"+libID.String()[:8], sid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title, duration_ms)
		VALUES ($1, $2, 'HTTP Lyric', 90000)`, trackID, libID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO lyrics (track_id, source, timed, body)
		VALUES ($1, 'embedded', true, '[00:01.00] hello')`, trackID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM lyrics WHERE track_id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=$1`, trackID)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id=$1`, libID)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, sid)
	})

	s := &Server{Pool: pool}
	u := &auth.User{ID: uuid.New(), Username: "u", IsAdmin: true}
	rec := httptest.NewRecorder()
	s.getTrackLyrics(rec, lyricsRequest(u, trackID))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	out := decodeMap(t, rec)
	if out["body"] != "[00:01.00] hello" || out["source"] != "embedded" || out["timed"] != true {
		t.Fatalf("got %v", out)
	}
	lines, _ := out["lines"].([]any)
	if len(lines) != 1 {
		t.Fatalf("lines %v", out["lines"])
	}
}

func TestAdminLyricsPutDB(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	var prev []byte
	had := pool.QueryRow(ctx, `SELECT value FROM server_settings WHERE key=$1`, lyrics.SettingKey).Scan(&prev) == nil
	t.Cleanup(func() {
		c := context.Background()
		if had {
			_, _ = pool.Exec(c, `
				INSERT INTO server_settings (key, value) VALUES ($1, $2::jsonb)
				ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, lyrics.SettingKey, prev)
			return
		}
		_, _ = pool.Exec(c, `DELETE FROM server_settings WHERE key=$1`, lyrics.SettingKey)
	})
	s := &Server{Pool: pool}
	u := &auth.User{ID: uuid.New(), Username: "admin", IsAdmin: true}
	req := authedJSON(u, http.MethodPut, "/api/v1/admin/lyrics", map[string]any{
		"enabled":      true,
		"provider_url": "https://lrclib.net",
	})
	rec := httptest.NewRecorder()
	s.requirePerm(lyrics.PermConfigure, s.adminPutLyrics).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	out := decodeMap(t, rec)
	if out["enabled"] != true || out["provider_url"] != "https://lrclib.net" {
		t.Fatalf("got %v", out)
	}
}
