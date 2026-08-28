package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/jobs"
)

func (s *Server) writeJobErr(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	switch {
	case errors.Is(err, jobs.ErrQueueFull):
		writeErr(w, 429, "queue_full", err.Error())
	case errors.Is(err, jobs.ErrPoolDisabled):
		writeErr(w, 503, "pool_disabled", err.Error())
	default:
		writeErr(w, 500, "job", err.Error())
	}
}

func (s *Server) adminWorkersGet(w http.ResponseWriter, r *http.Request) {
	if s.Jobs == nil {
		writeJSON(w, 200, map[string]any{"pools": jobs.Infos(), "running": []any{}, "jobs": []any{}})
		return
	}
	writeJSON(w, 200, map[string]any{
		"pools":   s.Jobs.Status(r.Context()),
		"running": s.Jobs.RunningJobs(r.Context()),
		"jobs":    s.Jobs.RecentJobs(r.Context(), 80),
	})
}

func (s *Server) adminWorkersPut(w http.ResponseWriter, r *http.Request) {
	if s.Jobs == nil {
		writeErr(w, 503, "jobs", "worker runner is not available")
		return
	}
	var body struct {
		Pools jobs.Configs `json:"pools"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	cfg, err := s.Jobs.Apply(r.Context(), body.Pools)
	if err != nil {
		writeErr(w, 400, "invalid", err.Error())
		return
	}
	s.Audit.Event(r.Context(), &currentUser(r).ID, "workers.update", "", r.RemoteAddr, map[string]any{"pools": cfg})
	writeJSON(w, 200, map[string]any{"ok": true, "pools": s.Jobs.Status(r.Context())})
}

func (s *Server) retryJob(w http.ResponseWriter, r *http.Request) {
	if s.Jobs == nil {
		writeErr(w, 503, "jobs", "worker runner is not available")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "invalid", "invalid job id")
		return
	}
	if err := s.Jobs.Retry(r.Context(), id); err != nil {
		if errors.Is(err, jobs.ErrNotRetryable) {
			writeErr(w, 409, "not_retryable", "only failed or cancelled jobs can be retried")
			return
		}
		s.writeJobErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
