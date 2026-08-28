package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/auth"
	"github.com/sounddock/sounddock/internal/stream"
)

func TestGrantActionsAllow(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		actions []string
		want    string
		strict  bool
		allow   bool
	}{
		{"default read+stream empty", nil, "read", false, true},
		{"empty stream compat", []string{}, "stream", false, true},
		{"empty write no", []string{}, "write", false, false},
		{"unknown as visibility", []string{"admin"}, "read", false, true},
		{"unknown stream compat", []string{"foo"}, "stream", false, true},
		{"unknown write no", []string{"foo"}, "write", false, false},
		{"explicit read only", []string{"read"}, "stream", false, false},
		{"explicit read ok", []string{"read"}, "read", false, true},
		{"explicit stream", []string{"read", "stream"}, "stream", false, true},
		{"write listed", []string{"write"}, "write", false, true},
		{"write listed no read", []string{"write"}, "read", false, false},
		{"strict empty hidden", []string{}, "read", true, false},
		{"strict unknown hidden", []string{"foo"}, "stream", true, false},
		{"strict explicit", []string{"read"}, "read", true, true},
		{"strict missing stream", []string{"read"}, "stream", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := grantActionsAllow(c.actions, c.want, c.strict); got != c.allow {
				t.Fatalf("grantActionsAllow(%v, %s, strict=%v)=%v want %v", c.actions, c.want, c.strict, got, c.allow)
			}
		})
	}
}

func TestLibraryGrantRoutesRegistered(t *testing.T) {
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
		"GET /api/v1/admin/libraries/{id}/grants",
		"POST /api/v1/admin/libraries/{id}/grants",
		"PATCH /api/v1/admin/libraries/{id}/grants/{grantID}",
		"DELETE /api/v1/admin/libraries/{id}/grants/{grantID}",
		"GET /api/v1/tracks/{id}/stream",
	} {
		if !got[n] {
			t.Fatalf("unregistered %s", n)
		}
	}
}

type grantFix struct {
	sid, libA, libB, userID, adminID, trackID uuid.UUID
}

func seedGrantLibs(t *testing.T, pool *pgxpool.Pool) grantFix {
	t.Helper()
	ctx := context.Background()
	fix := grantFix{
		sid: uuid.New(), libA: uuid.New(), libB: uuid.New(),
		userID: uuid.New(), adminID: uuid.New(), trackID: uuid.New(),
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_providers (id, name, type, config_enc)
		VALUES ($1, $2, 'local', $3)`, fix.sid, "w10-"+fix.sid.String()[:8], []byte(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO libraries (id, name, kind, storage_provider_id, root_prefix, read_only)
		VALUES ($1, $2, 'music', $3, '', false), ($4, $5, 'music', $3, '', false)`,
		fix.libA, "A-"+fix.libA.String()[:8], fix.sid,
		fix.libB, "B-"+fix.libB.String()[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, username, password_hash, display_name)
		VALUES ($1,$2,'x',$2), ($3,$4,'x',$4)`,
		fix.userID, "w10u-"+fix.userID.String()[:8],
		fix.adminID, "w10a-"+fix.adminID.String()[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tracks (id, library_id, title, duration_ms, acquisition)
		VALUES ($1,$2,'w10 stub',0,'youtube')`, fix.trackID, fix.libA); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM library_grants WHERE library_id IN ($1,$2)`, fix.libA, fix.libB)
		_, _ = pool.Exec(c, `DELETE FROM tracks WHERE id=$1`, fix.trackID)
		_, _ = pool.Exec(c, `DELETE FROM libraries WHERE id IN ($1,$2)`, fix.libA, fix.libB)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id IN ($1,$2)`, fix.userID, fix.adminID)
		_, _ = pool.Exec(c, `DELETE FROM storage_providers WHERE id=$1`, fix.sid)
		_, _ = pool.Exec(c, `DELETE FROM server_settings WHERE key=$1`, settingLibraryGrantsStrict)
	})
	return fix
}

func TestLibraryIDsAdminSeesAllNonAdminGrantedOnly(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := &Server{Pool: pool}
	fix := seedGrantLibs(t, pool)
	if _, err := pool.Exec(ctx, `
		INSERT INTO library_grants (library_id, user_id, actions)
		VALUES ($1,$2, ARRAY['read','stream'])`, fix.libA, fix.userID); err != nil {
		t.Fatal(err)
	}
	admin := &auth.User{ID: fix.adminID, Username: "admin", IsAdmin: true}
	user := &auth.User{ID: fix.userID, Username: "user"}
	adminIDs := s.libraryIDs(ctx, admin)
	if !uuidIn(adminIDs, fix.libA) || !uuidIn(adminIDs, fix.libB) {
		t.Fatalf("admin should see both libs, got %v", adminIDs)
	}
	userIDs := s.libraryIDs(ctx, user)
	if !uuidIn(userIDs, fix.libA) {
		t.Fatalf("user should see granted libA, got %v", userIDs)
	}
	if uuidIn(userIDs, fix.libB) {
		t.Fatalf("user must not see libB, got %v", userIDs)
	}
}

func TestLibraryIDsEmptyActionsCompatAndStrict(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := &Server{Pool: pool}
	fix := seedGrantLibs(t, pool)
	if _, err := pool.Exec(ctx, `
		INSERT INTO library_grants (library_id, user_id, actions)
		VALUES ($1,$2, ARRAY[]::text[])`, fix.libA, fix.userID); err != nil {
		t.Fatal(err)
	}
	user := &auth.User{ID: fix.userID, Username: "user"}
	got := s.libraryIDs(ctx, user)
	if !uuidIn(got, fix.libA) {
		t.Fatalf("empty actions visible when strict=false, got %v", got)
	}
	if err := s.putSetting(ctx, settingLibraryGrantsStrict, true); err != nil {
		t.Fatal(err)
	}
	hidden := s.libraryIDs(ctx, user)
	if uuidIn(hidden, fix.libA) {
		t.Fatalf("empty actions hidden when strict=true without read, got %v", hidden)
	}
	if uuidIn(s.libraryIDsFor(ctx, user, "stream"), fix.libA) {
		t.Fatal("strict empty should not grant stream")
	}
}

func TestStreamDeniedWithoutStreamActionWhenUserPresent(t *testing.T) {
	pool := testPool(t)
	s := &Server{Pool: pool, SignKey: []byte("w10-sign-key-32-bytes-long!!!!")}
	fix := seedGrantLibs(t, pool)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO library_grants (library_id, user_id, actions)
		VALUES ($1,$2, ARRAY['read'])`, fix.libA, fix.userID); err != nil {
		t.Fatal(err)
	}
	u := &auth.User{ID: fix.userID, Username: "user", Permissions: []string{"tracks.stream"}}
	req := authedJSON(u, http.MethodGet, "/api/v1/tracks/"+fix.trackID.String()+"/stream", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", fix.trackID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	s.streamTrack(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "library_grant" {
		t.Fatalf("code %v want library_grant", body["code"])
	}

	tok := stream.Sign(s.SignKey, fix.trackID, time.Hour, "")
	hmac := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/"+fix.trackID.String()+"/stream?token="+tok, nil)
	hmac = hmac.WithContext(context.WithValue(hmac.Context(), chi.RouteCtxKey, rctx))
	hrec := httptest.NewRecorder()
	s.streamTrack(hrec, hmac)
	if hrec.Code != http.StatusConflict {
		t.Fatalf("HMAC-only stub want 409, got %d %s", hrec.Code, hrec.Body.String())
	}
}

func TestLibraryGrantRoleDeleteSucceeds(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := &Server{Pool: pool}
	fix := seedGrantLibs(t, pool)
	var gid uuid.UUID
	err := pool.QueryRow(ctx, `
		INSERT INTO library_grants (library_id, role_id, actions)
		SELECT $1, id, ARRAY['read','stream'] FROM roles WHERE name='User'
		RETURNING id`, fix.libA).Scan(&gid)
	if err != nil {
		t.Fatal(err)
	}
	admin := &auth.User{ID: fix.adminID, Username: "admin", IsAdmin: true}
	req := authedJSON(admin, http.MethodDelete, "/api/v1/admin/libraries/"+fix.libA.String()+"/grants/"+gid.String(), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", fix.libA.String())
	rctx.URLParams.Add("grantID", gid.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	s.adminLibraryGrantDelete(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["ok"] != true {
		t.Fatalf("body %v", body)
	}
	if rec.Code == http.StatusConflict {
		t.Fatal("role grant delete must not return 409 role_grant")
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM library_grants WHERE id=$1`, gid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("grant still present")
	}
}

func uuidIn(ids []uuid.UUID, want uuid.UUID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
