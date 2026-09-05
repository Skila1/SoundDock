package playback

import (
	"context"

	"github.com/google/uuid"
)

// ShouldReplenishAutoplay is the radio-fill gate: on, not stopping after this
// track, and at most one upcoming item (current plus one more).
func ShouldReplenishAutoplay(autoplay, stopAfter bool, remaining int) bool {
	return autoplay && !stopAfter && remaining <= 2
}

func (e *Engine) runAutoplayFiller(ctx context.Context, sid uuid.UUID) {
	if e == nil || e.filler == nil || sid == uuid.Nil {
		return
	}
	_ = e.filler(ctx, sid)
}

// MaybeReplenish fills from autoplay while the current track is still playing
// so a later natural skip does not hit an empty queue.
func (e *Engine) MaybeReplenish(ctx context.Context, sid uuid.UUID) {
	e.runAutoplayFiller(ctx, sid)
}
