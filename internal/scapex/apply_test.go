package scapex

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestDecideApplyStalePlayDoesNotSteal(t *testing.T) {
	track := uuid.New()
	other := uuid.New()
	newer := uuid.New()
	in := Intent{
		Intent:                IntentPlay,
		TrackID:               track,
		ExpectedStateRevision: 3,
		ExpectedInstanceID:    uuid.New(),
	}
	snap := SessionSnap{
		StateRevision: 9,
		InstanceID:    newer,
		CurrentTrack:  other,
		Status:        "playing",
		QueueTrackIDs: []uuid.UUID{other},
		QueueItemIDs:  []uuid.UUID{uuid.New()},
	}
	if got := DecideApply(in, snap, track); got != ApplyAppend {
		t.Fatalf("revision/instance mismatch should append, got %s", got)
	}
}

func TestDecideApplyPlayWhenRevisionMatches(t *testing.T) {
	track := uuid.New()
	in := Intent{Intent: IntentPlay, TrackID: track, ExpectedStateRevision: 4}
	snap := SessionSnap{StateRevision: 4, Status: "stopped"}
	if got := DecideApply(in, snap, track); got != ApplyPlay {
		t.Fatalf("got %s", got)
	}
}

func TestDecideApplyPlayWhenIdle(t *testing.T) {
	track := uuid.New()
	in := Intent{Intent: IntentPlay, TrackID: track, ExpectedStateRevision: 1}
	snap := SessionSnap{StateRevision: 8, Status: "stopped"}
	if got := DecideApply(in, snap, track); got != ApplyPlay {
		t.Fatalf("idle should play, got %s", got)
	}
}

func TestDecideApplyNewerInstanceNeverInterrupted(t *testing.T) {
	track := uuid.New()
	expected := uuid.New()
	newer := uuid.New()
	in := Intent{
		Intent:                IntentPlay,
		TrackID:               track,
		ExpectedInstanceID:    expected,
		ExpectedStateRevision: 2,
	}
	snap := SessionSnap{
		StateRevision: 2,
		InstanceID:    newer,
		CurrentTrack:  uuid.New(),
		Status:        "playing",
	}
	if got := DecideApply(in, snap, track); got != ApplyAppend {
		t.Fatalf("got %s", got)
	}
}

func TestDecideApplyAlreadyQueued(t *testing.T) {
	track := uuid.New()
	in := Intent{Intent: IntentQueue, TrackID: track}
	snap := SessionSnap{QueueTrackIDs: []uuid.UUID{track}, QueueItemIDs: []uuid.UUID{uuid.New()}}
	if got := DecideApply(in, snap, track); got != ApplyPresent {
		t.Fatalf("got %s", got)
	}
}

func TestApplyReadyIntentsStalePlayAppends(t *testing.T) {
	play := &fakePlayer{
		get: map[string]any{
			"state_revision":       int64(11),
			"playback_instance_id": uuid.New(),
			"current_track_id":     uuid.New(),
			"status":               "playing",
			"items":                []map[string]any{},
		},
	}
	in := Intent{Intent: IntentPlay, TrackID: uuid.New(), ExpectedStateRevision: 1, SessionID: uuid.New()}
	action := DecideApply(in, SnapFromGet(play.get), in.TrackID)
	if action != ApplyAppend {
		t.Fatalf("got %s", action)
	}
	if err := applyAction(context.Background(), play, in.SessionID, in.TrackID, action); err != nil {
		t.Fatal(err)
	}
	if play.replaced {
		t.Fatal("stale play must not Replace")
	}
	if !play.appended || play.next {
		t.Fatal("expected append")
	}
}

type fakePlayer struct {
	get      map[string]any
	replaced bool
	appended bool
	next     bool
}

func (f *fakePlayer) Get(context.Context, uuid.UUID) (map[string]any, error) { return f.get, nil }
func (f *fakePlayer) Replace(context.Context, uuid.UUID, []uuid.UUID, int) error {
	f.replaced = true
	return nil
}
func (f *fakePlayer) Add(_ context.Context, _ uuid.UUID, _ []uuid.UUID, next bool) error {
	f.appended = true
	f.next = next
	return nil
}
func (f *fakePlayer) DropTracks(context.Context, []uuid.UUID) error { return nil }
