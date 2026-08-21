package opensubsonic

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/version"
)

// Router implements a conservative OpenSubsonic subset. Filesystem paths are never returned.
type Router struct {
	Pool *pgxpool.Pool
}

func (o *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/rest/"), ".view")
	switch action {
	case "ping":
		write(w, map[string]any{"status": "ok"})
	case "getLicense":
		write(w, map[string]any{"status": "ok", "license": map[string]any{"valid": true}})
	case "getMusicFolders":
		write(w, map[string]any{"status": "ok", "musicFolders": map[string]any{"musicFolder": []any{}}})
	default:
		write(w, map[string]any{"status": "failed", "error": map[string]any{"code": 70, "message": "not implemented"}})
	}
}

func write(w http.ResponseWriter, extra map[string]any) {
	body := map[string]any{
		"subsonic-response": map[string]any{
			"status": extra["status"], "version": "1.16.1", "type": "sounddock",
			"serverVersion": version.Version, "openSubsonic": true,
		},
	}
	for k, v := range extra {
		if k == "status" {
			continue
		}
		body["subsonic-response"].(map[string]any)[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
