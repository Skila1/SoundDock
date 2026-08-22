package httpapi

import (
	"fmt"
	"strings"
)

// Known API key scopes. admin grants every route that requireAdmin or HasPerm checks.
var apiKeyScopeMeta = []struct {
	Name string
	Desc string
}{
	{"admin", "Full administration: logs, diagnostics, Discord, updates, users"},
	{"tracks.read", "Read the catalogue"},
	{"tracks.stream", "Stream audio"},
	{"playlists.write", "Create and edit playlists"},
	{"history.read", "Read listening history"},
	{"library.upload", "Upload to writable libraries"},
	{"library.import_url", "Remote URL import"},
	{"library.create", "Create libraries"},
	{"library.migrate", "Migrate libraries"},
}

func knownAPIKeyScope(name string) bool {
	for _, s := range apiKeyScopeMeta {
		if s.Name == name {
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
