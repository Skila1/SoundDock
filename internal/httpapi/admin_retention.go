package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/retention"
)

var logPolicyLabels = map[string]string{
	"listen_history":           "Listen history",
	"failed_jobs":              "Failed jobs",
	"discord_playback_errors":  "Discord playback errors",
	"api_usage":                "API usage aggregates",
	"audit_events":             "Audit events",
	"operational_logs":         "Operational logs",
}

func (s *Server) adminRetention(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := s.Pool.Query(ctx, `SELECT key, days FROM retention_policies ORDER BY key`)
	if err != nil {
		writeErr(w, 500, "retention", err.Error())
		return
	}
	defer rows.Close()
	var logs []map[string]any
	for rows.Next() {
		var key string
		var days int
		if rows.Scan(&key, &days) != nil {
			continue
		}
		label := logPolicyLabels[key]
		if label == "" {
			label = key
		}
		logs = append(logs, map[string]any{"key": key, "days": days, "label": label})
	}
	if logs == nil {
		logs = []map[string]any{}
	}

	libs, _ := s.Pool.Query(ctx, `
		SELECT l.id, l.name, l.read_only, l.retention_opt_in, sp.type,
		       (SELECT count(*) FROM tracks t WHERE t.library_id=l.id) AS tracks
		FROM libraries l
		JOIN storage_providers sp ON sp.id = l.storage_provider_id
		ORDER BY l.name`)
	var libraries []map[string]any
	if libs != nil {
		defer libs.Close()
		for libs.Next() {
			var id uuid.UUID
			var name, typ string
			var ro, opt bool
			var n int
			if libs.Scan(&id, &name, &ro, &opt, &typ, &n) != nil {
				continue
			}
			libraries = append(libraries, map[string]any{
				"id": id, "name": name, "read_only": ro, "retention_opt_in": opt,
				"storage_type": typ, "track_count": n, "managed": typ == "managed",
			})
		}
	}
	if libraries == nil {
		libraries = []map[string]any{}
	}

	exRows, _ := s.Pool.Query(ctx, `
		SELECT id, kind, target_id, created_at FROM retention_exclusions ORDER BY created_at DESC`)
	var exclusions []map[string]any
	if exRows != nil {
		defer exRows.Close()
		exclusions = scanMaps(exRows, "id", "kind", "target_id", "created_at")
	}

	out := map[string]any{
		"log_policies": logs,
		"media":        retention.LoadPolicy(ctx, s.Pool),
		"libraries":    libraries,
		"exclusions":   exclusions,
		"events":       []any{},
	}
	if s.Retention != nil {
		st, err := s.Retention.Status(ctx)
		if err == nil {
			out["status"] = st
		}
		ev, err := s.Retention.RecentEvents(ctx, 40)
		if err == nil {
			out["events"] = ev
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) adminPutRetention(w http.ResponseWriter, r *http.Request) {
	raw, err := decodeRaw(r)
	if err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	var body struct {
		LogPolicies map[string]int `json:"log_policies"`
		Media       *retention.Policy
		Libraries   []struct {
			ID             uuid.UUID `json:"id"`
			RetentionOptIn *bool     `json:"retention_opt_in"`
		} `json:"libraries"`
	}
	if json.Unmarshal(raw, &body) == nil && (body.LogPolicies != nil || body.Media != nil || body.Libraries != nil) {
		for k, d := range body.LogPolicies {
			_, _ = s.Pool.Exec(r.Context(), `
				INSERT INTO retention_policies (key, days) VALUES ($1,$2)
				ON CONFLICT (key) DO UPDATE SET days=EXCLUDED.days`, k, d)
		}
		if body.Media != nil {
			if err := retention.SavePolicy(r.Context(), s.Pool, *body.Media); err != nil {
				writeErr(w, 500, "retention", err.Error())
				return
			}
		}
		for _, lib := range body.Libraries {
			if lib.RetentionOptIn == nil || lib.ID == uuid.Nil {
				continue
			}
			_, _ = s.Pool.Exec(r.Context(), `UPDATE libraries SET retention_opt_in=$2 WHERE id=$1`, lib.ID, *lib.RetentionOptIn)
		}
		s.Audit.Event(r.Context(), &currentUser(r).ID, "retention.update", "", r.RemoteAddr, nil)
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	var days map[string]int
	if err := json.Unmarshal(raw, &days); err != nil {
		writeErr(w, 400, "invalid", "expected retention policy")
		return
	}
	for k, d := range days {
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO retention_policies (key, days) VALUES ($1,$2)
			ON CONFLICT (key) DO UPDATE SET days=EXCLUDED.days`, k, d)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) adminRetentionPreview(w http.ResponseWriter, r *http.Request) {
	if s.Retention == nil {
		writeErr(w, 503, "retention", "retention engine is not available")
		return
	}
	res, err := s.Retention.Preview(r.Context(), 100)
	if err != nil {
		writeErr(w, 500, "retention", err.Error())
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) adminRetentionRun(w http.ResponseWriter, r *http.Request) {
	if s.Jobs == nil {
		writeErr(w, 503, "jobs", "worker runner is not available")
		return
	}
	id, err := retention.EnqueueUnlessBusy(r.Context(), s.Pool, s.Jobs.Enqueue, retention.Payload{Force: true})
	if err != nil {
		s.writeJobErr(w, err)
		return
	}
	writeJSON(w, 202, map[string]any{"ok": true, "queued": true, "job_id": id})
}

func (s *Server) adminRetentionExclusion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind     string    `json:"kind"`
		TargetID uuid.UUID `json:"target_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	kind := strings.ToLower(strings.TrimSpace(body.Kind))
	switch kind {
	case "track", "album", "artist", "playlist", "library":
	default:
		writeErr(w, 400, "invalid", "kind must be track, album, artist, playlist, or library")
		return
	}
	if body.TargetID == uuid.Nil {
		writeErr(w, 400, "invalid", "target_id required")
		return
	}
	var id uuid.UUID
	err := s.Pool.QueryRow(r.Context(), `
		INSERT INTO retention_exclusions (kind, target_id) VALUES ($1,$2)
		ON CONFLICT (kind, target_id) DO UPDATE SET kind=EXCLUDED.kind
		RETURNING id`, kind, body.TargetID).Scan(&id)
	if err != nil {
		writeErr(w, 400, "exclusion", err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "kind": kind, "target_id": body.TargetID})
}

func (s *Server) adminDeleteRetentionExclusion(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "id")
		return
	}
	_, _ = s.Pool.Exec(r.Context(), `DELETE FROM retention_exclusions WHERE id=$1`, id)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func decodeRaw(r *http.Request) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := decodeJSON(r, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}
