package scapex

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Player is the playback.Engine surface apply needs. Do not import httpapi.
type Player interface {
	Get(ctx context.Context, sid uuid.UUID) (map[string]any, error)
	Replace(ctx context.Context, sid uuid.UUID, tracks []uuid.UUID, start int) error
	Add(ctx context.Context, sid uuid.UUID, tracks []uuid.UUID, next bool) error
	DropTracks(ctx context.Context, tracks []uuid.UUID) error
}

const (
	ApplyPlay     = "play"
	ApplyAppend   = "append"
	ApplyNext     = "next"
	ApplyPresent  = "present"
	ApplySkipAuth = "skip_auth"
	ApplyStale    = "stale"
)

type SessionSnap struct {
	StateRevision int64
	InstanceID    uuid.UUID
	CurrentTrack  uuid.UUID
	Status        string
	QueueTrackIDs []uuid.UUID
	QueueItemIDs  []uuid.UUID
}

func sessionIdle(snap SessionSnap) bool {
	if snap.CurrentTrack == uuid.Nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(snap.Status)) {
	case "", "stopped", "idle", "interrupted":
		return true
	}
	return false
}

func trackInQueue(snap SessionSnap, trackID uuid.UUID) bool {
	if trackID == uuid.Nil {
		return false
	}
	if snap.CurrentTrack == trackID {
		return true
	}
	for _, id := range snap.QueueTrackIDs {
		if id == trackID {
			return true
		}
	}
	return false
}

func itemPresent(snap SessionSnap, itemID uuid.UUID) bool {
	if itemID == uuid.Nil {
		return false
	}
	for _, id := range snap.QueueItemIDs {
		if id == itemID {
			return true
		}
	}
	return false
}

// DecideApply chooses play vs queue-append without interrupting a newer instance.
// Stale play (revision/instance mismatch while the session is busy) becomes append.
func DecideApply(in Intent, snap SessionSnap, trackID uuid.UUID) string {
	if trackID == uuid.Nil {
		trackID = in.TrackID
	}
	if trackInQueue(snap, trackID) {
		return ApplyPresent
	}
	switch in.Intent {
	case IntentPlay:
		if in.ExpectedInstanceID != uuid.Nil && snap.InstanceID != uuid.Nil && snap.InstanceID != in.ExpectedInstanceID {
			return ApplyAppend
		}
		if snap.StateRevision == in.ExpectedStateRevision || sessionIdle(snap) {
			return ApplyPlay
		}
		return ApplyAppend
	case IntentNext:
		return ApplyNext
	default:
		if in.QueueAfterItemID != uuid.Nil && !itemPresent(snap, in.QueueAfterItemID) {
			return ApplyAppend
		}
		if in.QueueAfterItemID != uuid.Nil && snap.CurrentTrack != uuid.Nil {
			// After-item still present: if we cannot splice, append unless it is current (play-next).
			for i, id := range snap.QueueItemIDs {
				if id == in.QueueAfterItemID {
					if i < len(snap.QueueTrackIDs) && snap.QueueTrackIDs[i] == snap.CurrentTrack {
						return ApplyNext
					}
					break
				}
			}
		}
		return ApplyAppend
	}
}

func parseUUID(v any) uuid.UUID {
	switch t := v.(type) {
	case uuid.UUID:
		return t
	case *uuid.UUID:
		if t != nil {
			return *t
		}
	case string:
		id, _ := uuid.Parse(t)
		return id
	}
	return uuid.Nil
}

func parseInt64(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	}
	return 0
}

func SnapFromGet(q map[string]any) SessionSnap {
	snap := SessionSnap{
		StateRevision: parseInt64(q["state_revision"]),
		InstanceID:    parseUUID(q["playback_instance_id"]),
		CurrentTrack:  parseUUID(q["current_track_id"]),
		Status:        strings.TrimSpace(fmtString(q["status"])),
	}
	items, _ := q["items"].([]map[string]any)
	for _, it := range items {
		snap.QueueItemIDs = append(snap.QueueItemIDs, parseUUID(it["id"]))
		snap.QueueTrackIDs = append(snap.QueueTrackIDs, parseUUID(it["track_id"]))
	}
	return snap
}

func fmtString(v any) string {
	s, _ := v.(string)
	return s
}

// ApplyReadyIntents revalidates waiters on a completed fetch and applies the
// first authorized intent. Remaining authorized waiters for the same job are
// treated as queue appends so one download can satisfy many requesters.
func ApplyReadyIntents(ctx context.Context, pool *pgxpool.Pool, play Player, jobID uuid.UUID) error {
	if pool == nil || jobID == uuid.Nil {
		return nil
	}
	_ = SetIntentsStatus(ctx, pool, jobID, StatusReady, []string{StatusQueued, StatusDownloading, StatusProcessing, StatusScanning})
	intents, err := ListJobIntents(ctx, pool, jobID)
	if err != nil {
		return err
	}
	appliedPlay := false
	for _, in := range intents {
		if in.Status == StatusApplied || in.Status == StatusFailed || in.Status == StatusCancelled || in.Status == StatusStale {
			continue
		}
		ok, err := UserMayAccessLibrary(ctx, pool, in.UserID, in.DestLibraryID)
		if err != nil {
			return err
		}
		if !ok {
			setIntent(ctx, pool, in.ID, StatusCancelled, "requester not authorized")
			continue
		}
		if in.SessionID == uuid.Nil || play == nil {
			setIntent(ctx, pool, in.ID, StatusApplied, "")
			continue
		}
		if !sessionExists(ctx, pool, in.SessionID) {
			setIntent(ctx, pool, in.ID, StatusStale, "session gone")
			continue
		}
		q, err := play.Get(ctx, in.SessionID)
		if err != nil {
			setIntent(ctx, pool, in.ID, StatusStale, err.Error())
			continue
		}
		snap := SnapFromGet(q)
		trackID := in.TrackID
		action := DecideApply(in, snap, trackID)
		if appliedPlay && action == ApplyPlay {
			action = ApplyAppend
		}
		if err := applyAction(ctx, play, in.SessionID, trackID, action); err != nil {
			setIntent(ctx, pool, in.ID, StatusFailed, err.Error())
			continue
		}
		if action == ApplyPlay {
			appliedPlay = true
		}
		setIntent(ctx, pool, in.ID, StatusApplied, "")
	}
	return nil
}

func applyAction(ctx context.Context, play Player, sid, trackID uuid.UUID, action string) error {
	if play == nil || trackID == uuid.Nil || sid == uuid.Nil {
		return nil
	}
	switch action {
	case ApplyPlay:
		return play.Replace(ctx, sid, []uuid.UUID{trackID}, 0)
	case ApplyNext:
		return play.Add(ctx, sid, []uuid.UUID{trackID}, true)
	case ApplyAppend:
		return play.Add(ctx, sid, []uuid.UUID{trackID}, false)
	default:
		return nil
	}
}
