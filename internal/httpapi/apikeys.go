package httpapi

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/sounddock/sounddock/internal/auth"
)

// Known API key scopes. admin grants everything. Each admin.<area> scope grants
// that part of /api/v1/admin, and admin.<area>.read grants only its GET routes.
var apiKeyScopeMeta = []struct {
	Name string
	Desc string
}{
	{"admin", "Full administration: every admin page and setting"},
	{"tracks.read", "Read the catalogue"},
	{"tracks.stream", "Stream audio"},
	{"playlists.write", "Create and edit playlists"},
	{"history.read", "Read listening history"},
	{"library.upload", "Upload to writable libraries"},
	{"library.import_url", "Remote URL import"},
	{"library.create", "Create libraries"},
	{"library.migrate", "Migrate libraries"},
}

// adminArea is one part of the administration API that a key can be given on
// its own. Prefixes are paths under /api/v1/admin; the longest match wins.
type adminArea struct {
	Scope    string
	Label    string
	Desc     string
	Prefixes []string
	// Implies are finer permissions the admin routes in this area also check.
	Implies []string
	// ReadOnly areas offer only the .read scope: changing them could make the
	// key (or someone else) a full administrator, which needs the admin scope.
	ReadOnly bool
}

var adminAreas = []adminArea{
	{Scope: "admin.overview", Label: "Dashboard and health", Desc: "Overview, health, diagnostics, database, maintenance, announcements, quotas",
		Prefixes: []string{"/overview", "/health", "/diagnostics", "/database", "/maintenance", "/announcement", "/quotas", "/demo", "/listen-compare", "/stats"}},
	{Scope: "admin.audit", Label: "Activity and audit logs", Desc: "Audit events, logs, errors, playback sessions, webhook and Discord logs",
		Prefixes: []string{"/audit", "/logs", "/inspect", "/errors", "/scans/errors", "/webhooks/deliveries", "/external/errors",
			"/integrations/discord/logs", "/playback", "/acquisition", "/media/holds", "/discord/runtime"}},
	{Scope: "admin.jobs", Label: "Jobs and workers", Desc: "Background jobs, worker pools and scans",
		Prefixes: []string{"/jobs", "/workers", "/scans"}},
	{Scope: "admin.users", Label: "Users and groups", Desc: "See users, groups (roles) and permissions. Changing them needs Administrator.",
		Prefixes: []string{"/users", "/discord-users", "/roles", "/permissions"}, ReadOnly: true},
	{Scope: "admin.libraries", Label: "Libraries and storage", Desc: "Libraries, storage, grants, library health and duplicates",
		Prefixes: []string{"/library", "/libraries", "/library-grants-strict", "/storage", "/duplicates"}, Implies: []string{"tracks.merge"}},
	{Scope: "admin.media", Label: "Media settings", Desc: "Metadata, transcoding, lyrics, stream and download policy, retention, archive",
		Prefixes: []string{"/metadata", "/transcode", "/lyrics", "/stream-policy", "/acquisition-policy", "/retention", "/archive"}, Implies: []string{"lyrics.configure"}},
	{Scope: "admin.backups", Label: "Backups", Desc: "Backups, backup settings and restore",
		Prefixes: []string{"/backups"}},
	{Scope: "admin.integrations", Label: "Integrations and Discord", Desc: "API keys, webhooks, playlist providers and the Discord bot",
		Prefixes: []string{"/integrations", "/webhooks"}},
	{Scope: "admin.updates", Label: "Updates", Desc: "Check for and apply updates",
		Prefixes: []string{"/updates"}},
}

func knownAPIKeyScope(name string) bool {
	for _, s := range apiKeyScopeMeta {
		if s.Name == name {
			return true
		}
	}
	for _, a := range adminAreas {
		if (name == a.Scope && !a.ReadOnly) || name == a.Scope+".read" {
			return true
		}
	}
	return false
}

func normalizeAPIKeyScopes(in []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if !knownAPIKeyScope(s) {
			return nil, fmt.Errorf("unknown scope %q", s)
		}
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one scope is required")
	}
	return out, nil
}

// isAdminScope reports whether a scope is about the administration API.
func isAdminScope(s string) bool {
	return s == "admin" || strings.HasPrefix(s, "admin.")
}

// expandAdminScopes adds the finer permissions that admin area scopes imply.
func expandAdminScopes(scopes []string) []string {
	out := append([]string{}, scopes...)
	have := map[string]bool{}
	for _, s := range out {
		have[s] = true
	}
	for _, a := range adminAreas {
		if !have[a.Scope] {
			continue
		}
		for _, p := range a.Implies {
			if !have[p] {
				have[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// adminAreaFor finds the area that owns a path under /api/v1/admin (or the
// top-level /api/v1/duplicates, which belongs with libraries).
func adminAreaFor(path string) *adminArea {
	rel := strings.TrimPrefix(path, "/api/v1/admin")
	if rel == path {
		if path == "/api/v1/duplicates" {
			rel = "/duplicates"
		} else {
			return nil
		}
	}
	var best *adminArea
	bestLen := -1
	for i := range adminAreas {
		for _, p := range adminAreas[i].Prefixes {
			if (rel == p || strings.HasPrefix(rel, p+"/")) && len(p) > bestLen {
				best, bestLen = &adminAreas[i], len(p)
			}
		}
	}
	return best
}

// adminScopeAllows reports whether a scoped key may call an admin route. Full
// administrators (session users and keys with the admin scope) always may.
func adminScopeAllows(u *auth.User, method, path string) bool {
	if u == nil {
		return false
	}
	if u.IsAdmin {
		return true
	}
	rel := strings.TrimPrefix(path, "/api/v1/admin")
	// Library scopes that predate the admin areas.
	if method == http.MethodPost && rel == "/libraries" && auth.HasPerm(u, "library.create") {
		return true
	}
	if method == http.MethodPost && strings.HasPrefix(rel, "/libraries/") && strings.HasSuffix(rel, "/migrate") && auth.HasPerm(u, "library.migrate") {
		return true
	}
	area := adminAreaFor(path)
	if area == nil {
		return false
	}
	if !area.ReadOnly && auth.HasPerm(u, area.Scope) {
		return true
	}
	readOnly := method == http.MethodGet || method == http.MethodHead
	return readOnly && auth.HasPerm(u, area.Scope+".read")
}

// requireAdminScope lets administrators through, and API keys whose scopes
// cover the admin area of the route.
func (s *Server) requireAdminScope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !adminScopeAllows(currentUser(r), r.Method, r.URL.Path) {
			writeErr(w, 403, "forbidden", "administrator required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// scopesWithin reports whether a caller may hand out these scopes on a new key:
// administrators may give any; a scoped key only what it holds itself.
func scopesWithin(u *auth.User, scopes []string) bool {
	if u != nil && u.IsAdmin {
		return true
	}
	for _, sc := range scopes {
		if sc == "admin" || !auth.HasPerm(u, sc) {
			return false
		}
	}
	return true
}

// integrationLibraryAction maps a library grant action to the key scopes that
// give an integration key (not tied to a user) that action on every library.
func integrationLibraryAction(u *auth.User, action string) bool {
	switch action {
	case "read":
		return auth.HasPerm(u, "tracks.read") || auth.HasPerm(u, "tracks.stream")
	case "stream":
		return auth.HasPerm(u, "tracks.stream")
	case "write":
		return auth.HasPerm(u, "library.upload") || auth.HasPerm(u, "library.import_url") || auth.HasPerm(u, "library.create")
	}
	return false
}
