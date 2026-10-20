package httpapi

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
)

func keyUser(scopes ...string) *auth.User {
	u := &auth.User{ID: uuid.New(), Integration: true, Permissions: expandAdminScopes(scopes)}
	for _, s := range scopes {
		if s == "admin" {
			u.IsAdmin = true
		}
	}
	return u
}

func TestAdminAreaForPicksLongestPrefix(t *testing.T) {
	cases := map[string]string{
		"/api/v1/admin/audit":                     "admin.audit",
		"/api/v1/admin/logs":                      "admin.audit",
		"/api/v1/admin/scans/errors":              "admin.audit",
		"/api/v1/admin/scans":                     "admin.jobs",
		"/api/v1/admin/integrations/discord/logs": "admin.audit",
		"/api/v1/admin/integrations/discord":      "admin.integrations",
		"/api/v1/admin/acquisition":               "admin.audit",
		"/api/v1/admin/acquisition-policy":        "admin.media",
		"/api/v1/admin/library-grants-strict":     "admin.libraries",
		"/api/v1/admin/libraries/x/grants":        "admin.libraries",
		"/api/v1/admin/stats/rebuild":             "admin.overview",
		"/api/v1/duplicates":                      "admin.libraries",
	}
	for path, want := range cases {
		a := adminAreaFor(path)
		if a == nil || a.Scope != want {
			t.Errorf("%s: got %v want %s", path, a, want)
		}
	}
	if adminAreaFor("/api/v1/admin/unknown") != nil || adminAreaFor("/api/v1/tracks") != nil {
		t.Fatal("unmapped paths must not match an area")
	}
}

func TestAuditOnlyKeyReadsLogsAndNothingElse(t *testing.T) {
	u := keyUser("admin.audit.read")
	if !adminScopeAllows(u, http.MethodGet, "/api/v1/admin/audit") || !adminScopeAllows(u, http.MethodGet, "/api/v1/admin/logs") {
		t.Fatal("audit read key must read audit and logs")
	}
	for _, c := range []struct{ m, p string }{
		{http.MethodGet, "/api/v1/admin/users"},
		{http.MethodGet, "/api/v1/admin/backups"},
		{http.MethodPut, "/api/v1/admin/retention"},
		{http.MethodPost, "/api/v1/admin/audit"},
	} {
		if adminScopeAllows(u, c.m, c.p) {
			t.Errorf("audit read key allowed %s %s", c.m, c.p)
		}
	}
}

func TestAreaScopeAllowsWritesAndImpliedPerms(t *testing.T) {
	u := keyUser("admin.media")
	if !adminScopeAllows(u, http.MethodPut, "/api/v1/admin/retention") {
		t.Fatal("media key must change retention")
	}
	if !auth.HasPerm(u, "lyrics.configure") {
		t.Fatal("media scope must imply lyrics.configure")
	}
	if adminScopeAllows(u, http.MethodGet, "/api/v1/admin/logs") {
		t.Fatal("media key must not read logs")
	}
}

func TestUsersAreaIsReadOnlyForKeys(t *testing.T) {
	if knownAPIKeyScope("admin.users") {
		t.Fatal("admin.users must not be grantable; only admin.users.read")
	}
	u := keyUser("admin.users.read")
	if !adminScopeAllows(u, http.MethodGet, "/api/v1/admin/users") {
		t.Fatal("users read key must list users")
	}
	if adminScopeAllows(u, http.MethodPatch, "/api/v1/admin/users/x") || adminScopeAllows(u, http.MethodPost, "/api/v1/admin/roles/x/members") {
		t.Fatal("users read key must not change users or groups")
	}
}

func TestLegacyLibraryScopesReachTheirAdminRoutes(t *testing.T) {
	u := keyUser("library.create", "library.migrate")
	if !adminScopeAllows(u, http.MethodPost, "/api/v1/admin/libraries") {
		t.Fatal("library.create must create libraries")
	}
	if !adminScopeAllows(u, http.MethodPost, "/api/v1/admin/libraries/abc/migrate") {
		t.Fatal("library.migrate must migrate libraries")
	}
	if adminScopeAllows(u, http.MethodDelete, "/api/v1/admin/libraries/abc") {
		t.Fatal("library.create must not delete libraries")
	}
}

func TestFullAdminKeyAllowsEverything(t *testing.T) {
	u := keyUser("admin")
	if !adminScopeAllows(u, http.MethodPost, "/api/v1/admin/updates/apply") || !adminScopeAllows(u, http.MethodGet, "/api/v1/admin/anything") {
		t.Fatal("admin scope must allow every admin route")
	}
}

func TestScopedKeyCannotMintWiderKeys(t *testing.T) {
	u := keyUser("admin.integrations", "tracks.read")
	if !scopesWithin(u, []string{"tracks.read"}) {
		t.Fatal("may hand out a scope it holds")
	}
	if scopesWithin(u, []string{"admin"}) || scopesWithin(u, []string{"admin.backups"}) {
		t.Fatal("must not hand out scopes it lacks")
	}
	if !scopesWithin(&auth.User{IsAdmin: true}, []string{"admin"}) {
		t.Fatal("administrators may hand out any scope")
	}
}

func TestIntegrationLibraryActions(t *testing.T) {
	read := keyUser("tracks.read")
	if !integrationLibraryAction(read, "read") || integrationLibraryAction(read, "stream") || integrationLibraryAction(read, "write") {
		t.Fatal("tracks.read gives read only")
	}
	up := keyUser("tracks.stream", "library.upload")
	if !integrationLibraryAction(up, "stream") || !integrationLibraryAction(up, "write") {
		t.Fatal("stream and upload scopes give stream and write")
	}
}

func TestPATAdminScopesOnlyForAdministrators(t *testing.T) {
	admin := applyPATScopes(&auth.User{IsAdmin: true, Permissions: []string{"tracks.read"}}, []string{"admin.audit.read", "tracks.read"})
	if admin.IsAdmin || !auth.HasPerm(admin, "admin.audit.read") || !auth.HasPerm(admin, "tracks.read") {
		t.Fatalf("admin token got %+v", admin)
	}
	user := applyPATScopes(&auth.User{Permissions: []string{"tracks.read"}}, []string{"admin.audit.read", "tracks.read"})
	if auth.HasPerm(user, "admin.audit.read") {
		t.Fatal("a non-administrator token must not gain admin scopes")
	}
}
