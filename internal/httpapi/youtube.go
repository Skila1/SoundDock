package httpapi

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
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

// Fetch enqueues a coalesced scapex.fetch and returns stub track IDs without waiting.
func (y isolatedYT) Fetch(ctx context.Context, refs []string) ([]uuid.UUID, error) {
	if y.s == nil || y.s.ScapeX == nil {
		return nil, errScapeXDown
	}
	return y.s.enqueueYouTubeRefs(ctx, refs)
}

type fetchPayload struct {
	URLs          []string `json:"urls"`
	SourceRefs    []string `json:"source_refs"`
	CoalesceKey   string   `json:"coalesce_key"`
	DestLibraryID string   `json:"dest_library_id"`
	MediaPolicyID string   `json:"media_policy_id"`
}

func (s *Server) jobScapeXFetch(ctx context.Context, job jobs.Job) error {
	var p fetchPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return err
	}
	if s.ScapeX == nil {
		return errScapeXDown
	}
	refs := p.URLs
	if len(refs) == 0 {
		refs = p.SourceRefs
	}
	lib := uuid.Nil
	if p.DestLibraryID != "" {
		lib, _ = uuid.Parse(p.DestLibraryID)
	}
	policy := p.MediaPolicyID
	if policy == "" {
		policy = scapex.DefaultMediaPolicy
	}
	userID := uuid.Nil
	if intents, err := scapex.ListJobIntents(ctx, s.Pool, job.ID); err == nil {
		for _, in := range intents {
			if in.UserID != uuid.Nil {
				userID = in.UserID
				break
			}
		}
	}
	ids, err := s.ScapeX.RunFetchJob(ctx, scapex.FetchOpts{
		JobID:       job.ID,
		URLs:        refs,
		DestLibrary: lib,
		Policy:      policy,
		UserID:      userID,
		Quota:       s.CheckQuota,
		Player:      s.Play,
	})
	if err != nil {
		_ = scapex.FailJobIntents(ctx, s.Pool, job.ID, err.Error())
		return err
	}
	if s.Jobs != nil {
		s.Jobs.SetResult(ctx, job.ID, map[string]any{"track_ids": ids})
	}
	return nil
}

// ApplyReadyIntents is exported for W6-http.
func (s *Server) ApplyReadyIntents(ctx context.Context, jobID uuid.UUID) error {
	return scapex.ApplyReadyIntents(ctx, s.Pool, s.Play, jobID)
}

func userIDFromCtx(ctx context.Context) uuid.UUID {
	if u, ok := ctx.Value(userKey).(*auth.User); ok && u != nil {
		return u.ID
	}
	return uuid.Nil
}
