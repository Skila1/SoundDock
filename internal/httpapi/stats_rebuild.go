package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
)

const (
	statsRebuildJobType = "stats.rebuild"
	statsRebuildPerm    = "stats.rebuild"
	listenReaderSetting = "listen_reader"
	listenReaderHistory = "history"
)

type statsRebuildJobView struct {
	ID         uuid.UUID  `json:"id"`
	Status     string     `json:"status"`
	Progress   int        `json:"progress"`
	LastError  *string    `json:"last_error"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

func statsRebuildBusy(status string) bool {
	return status == "queued" || status == "running" || status == "retry"
}

func (s *Server) requireStatsRebuild(w http.ResponseWriter, r *http.Request) bool {
	if !auth.HasPerm(currentUser(r), statsRebuildPerm) {
		writeErr(w, 403, "forbidden", "stats rebuild not permitted")
		return false
	}
	s.ensureStatsRebuildPerm(r.Context())
	return true
}

// ensureStatsRebuildPerm seeds permissions.stats.rebuild and attaches it to
// the Administrator role. No numbered migration (0016 is Wave 6).
func (s *Server) ensureStatsRebuildPerm(ctx context.Context) {
	if s.Pool == nil {
		return
	}
	_, _ = s.Pool.Exec(ctx, `
		INSERT INTO permissions (name, description)
		VALUES ('stats.rebuild', 'Enqueue listen stats rebuild and cutover to listen_events')
		ON CONFLICT DO NOTHING`)
	_, _ = s.Pool.Exec(ctx, `
		INSERT INTO role_permissions (role_id, permission_id)
		SELECT r.id, p.id FROM roles r, permissions p
		WHERE r.name = 'Administrator' AND p.name = 'stats.rebuild'
		ON CONFLICT DO NOTHING`)
}

func (s *Server) listenReaderMode(ctx context.Context) string {
	if s.Pool == nil {
		return listenReaderHistory
	}
	var v string
	err := s.Pool.QueryRow(ctx, `SELECT value #>> '{}' FROM server_settings WHERE key=$1`, listenReaderSetting).Scan(&v)
	if err != nil || v == "" {
		return listenReaderHistory
	}
	return v
}

func (s *Server) latestStatsRebuildJob(ctx context.Context) *statsRebuildJobView {
	if s.Pool == nil {
		return nil
	}
	var j statsRebuildJobView
	err := s.Pool.QueryRow(ctx, `
		SELECT id, status, progress, last_error, created_at, started_at, finished_at
		FROM jobs WHERE type=$1
		ORDER BY created_at DESC LIMIT 1`, statsRebuildJobType).
		Scan(&j.ID, &j.Status, &j.Progress, &j.LastError, &j.CreatedAt, &j.StartedAt, &j.FinishedAt)
	if err != nil {
		return nil
	}
	return &j
}

func (s *Server) activeStatsRebuildJobID(ctx context.Context) (uuid.UUID, bool) {
	if s.Pool == nil {
		return uuid.Nil, false
	}
	var id uuid.UUID
	err := s.Pool.QueryRow(ctx, `
		SELECT id FROM jobs
		WHERE type=$1 AND status IN ('queued','running','retry')
		ORDER BY created_at LIMIT 1`, statsRebuildJobType).Scan(&id)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

func (s *Server) adminStatsRebuildGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireStatsRebuild(w, r) {
		return
	}
	job := s.latestStatsRebuildJob(r.Context())
	busy := job != nil && statsRebuildBusy(job.Status)
	writeJSON(w, 200, map[string]any{
		"listen_reader": s.listenReaderMode(r.Context()),
		"busy":          busy,
		"job":           job,
	})
}

func (s *Server) adminStatsRebuildPost(w http.ResponseWriter, r *http.Request) {
	if !s.requireStatsRebuild(w, r) {
		return
	}
	if s.Jobs == nil {
		writeErr(w, 503, "jobs", "worker runner is not available")
		return
	}
	if id, ok := s.activeStatsRebuildJobID(r.Context()); ok {
		writeJSON(w, 409, map[string]any{
			"code":    "rebuild_in_progress",
			"message": "a stats rebuild is already queued or running",
			"job_id":  id,
		})
		return
	}
	payload := map[string]any{}
	if u := currentUser(r); u != nil {
		payload["actor_id"] = u.ID
	}
	jid, err := s.Jobs.Enqueue(r.Context(), statsRebuildJobType, payload)
	if err != nil {
		s.writeJobErr(w, err)
		return
	}
	if u := currentUser(r); u != nil && s.Audit != nil {
		s.Audit.Event(r.Context(), &u.ID, "stats.rebuild", jid.String(), r.RemoteAddr, nil)
	}
	writeJSON(w, 202, map[string]any{"job_id": jid})
}
