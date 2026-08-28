package httpapi

import (
	"context"
	"net/http"

	"github.com/sounddock/sounddock/internal/auth"
)

func permDescription(name string) string {
	switch name {
	case "tracks.merge":
		return "Merge libraries and duplicate tracks"
	case "tracks.replace_source":
		return "Replace a track original from an allowlisted YouTube source while playback continues"
	case "tracks.read":
		return "Read catalogue"
	case "tracks.stream":
		return "Stream audio"
	case "playlists.write":
		return "Create and edit playlists"
	case "history.read":
		return "Read listening history"
	default:
		return name
	}
}

// requirePerm wraps a handler with HasPerm(name) and seeds the permission.
func (s *Server) requirePerm(name string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !auth.HasPerm(currentUser(r), name) {
			writeErr(w, http.StatusForbidden, "forbidden", name+" not permitted")
			return
		}
		s.ensurePerm(r.Context(), name)
		next(w, r)
	}
}

// requirePermMW applies requirePerm to every handler on a chi group (Mount wrappers).
func (s *Server) requirePermMW(name string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return s.requirePerm(name, next.ServeHTTP)
	}
}

// ensurePerm seeds permissions.name and attaches it to Administrator.
// ON CONFLICT DO NOTHING — no numbered migration (0017 is Wave 8 duplicate review).
func (s *Server) ensurePerm(ctx context.Context, name string) {
	if s == nil || s.Pool == nil || name == "" {
		return
	}
	_, _ = s.Pool.Exec(ctx, `
		INSERT INTO permissions (name, description)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, name, permDescription(name))
	_, _ = s.Pool.Exec(ctx, `
		INSERT INTO role_permissions (role_id, permission_id)
		SELECT r.id, p.id FROM roles r, permissions p
		WHERE r.name = 'Administrator' AND p.name = $1
		ON CONFLICT DO NOTHING`, name)
}
