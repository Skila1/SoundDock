package httpapi

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/external"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/scapex"
)

func (s *Server) YouTube() external.Filler {
	return isolatedYT{s: s}
}

type isolatedYT struct{ s *Server }

func (y isolatedYT) Search(ctx context.Context, q string, limit int) ([]scapex.Hit, error) {
	if y.s == nil || y.s.ScapeX == nil {
		return nil, nil
	}
	if y.s.Jobs == nil || !y.s.Jobs.Started() {
		return y.s.ScapeX.Search(ctx, q, limit)
	}
	var hits []scapex.Hit
	err := y.s.Jobs.Do(ctx, jobs.PoolSearch, func(ctx context.Context) error {
		var e error
		hits, e = y.s.ScapeX.Search(ctx, q, limit)
		return e
	})
	return hits, err
}

func (y isolatedYT) Fetch(ctx context.Context, refs []string) ([]uuid.UUID, error) {
	if y.s == nil || y.s.ScapeX == nil {
		return nil, errScapeXDown
	}
	if y.s.Jobs == nil || !y.s.Jobs.Started() {
		return y.s.ScapeX.Fetch(ctx, refs)
	}
	cfg := y.s.Jobs.Configs()[jobs.PoolAcquisition]
	timeout := time.Duration(cfg.TimeoutSeconds+15) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var out struct {
		TrackIDs []uuid.UUID `json:"track_ids"`
	}
	err := y.s.Jobs.EnqueueWait(ctx, "scapex.fetch", map[string]any{"urls": refs}, &out)
	return out.TrackIDs, err
}

func (s *Server) jobScapeXFetch(ctx context.Context, job jobs.Job) error {
	var p struct {
		URLs []string `json:"urls"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return err
	}
	if s.ScapeX == nil {
		return errScapeXDown
	}
	ids, err := s.ScapeX.Fetch(ctx, p.URLs)
	if err != nil {
		return err
	}
	s.Jobs.SetResult(ctx, job.ID, map[string]any{"track_ids": ids})
	return nil
}
