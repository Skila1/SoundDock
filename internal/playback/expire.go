package playback

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/jobs"
)

type ExpirePayload struct {
	SessionID uuid.UUID `json:"session_id"`
}

func ExpireHandler(e *Engine) jobs.Handler {
	return func(ctx context.Context, job jobs.Job) error {
		var p ExpirePayload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return err
		}
		if p.SessionID == uuid.Nil {
			return nil
		}
		return e.ExpireParty(ctx, p.SessionID)
	}
}
