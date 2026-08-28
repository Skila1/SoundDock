package listen

import (
	"time"

	"github.com/google/uuid"
)

const (
	maxCreditPerMessageMS = 5000
	maxGapMS              = 15000
	wallSlack             = 1.25
)

// FSM is durable per-(playback_instance_id, user_id) listen state.
type FSM struct {
	TrackID        uuid.UUID
	AccumulatedMS  int64
	LastPositionMS int
	LastSequence   int64
	LastCheckpoint time.Time
	Qualified      bool
	Skipped        bool
	LastOutput     string
	StartedAt      time.Time
}

func HoldsBrowserLease(rendererKind, rendererID, clientID, deviceID string) bool {
	if rendererKind != "browser" || rendererID == "" {
		return false
	}
	return rendererID == clientID || (deviceID != "" && rendererID == deviceID)
}

func IsAudioListener(kind, rendererKind, rendererID, clientID, deviceID string, forced *bool) bool {
	if forced != nil {
		return *forced
	}
	if kind == "skip" {
		return true
	}
	if rendererKind == "discord" {
		return false
	}
	return HoldsBrowserLease(rendererKind, rendererID, clientID, deviceID)
}

func outputOf(rendererKind string) string {
	switch rendererKind {
	case "browser", "discord":
		return rendererKind
	default:
		return ""
	}
}

func playingStatus(status string) bool {
	return status == "" || status == "playing"
}

// Credit applies one checkpoint to st. drop means the message is stale and st is unchanged.
func Credit(st FSM, positionMS int, sequence int64, status string, rate float64, now time.Time) (credited int, next FSM, drop bool) {
	if sequence > 0 && st.LastSequence > 0 && sequence <= st.LastSequence {
		return 0, st, true
	}
	next = st
	if sequence > 0 {
		next.LastSequence = sequence
	}
	if now.IsZero() {
		now = time.Now()
	}
	if rate <= 0 {
		rate = 1
	}

	if st.LastCheckpoint.IsZero() {
		next.LastCheckpoint = now
		next.LastPositionMS = positionMS
		if !playingStatus(status) {
			return 0, next, false
		}
		if positionMS > maxCreditPerMessageMS {
			return 0, next, false
		}
		if positionMS > 0 {
			next.AccumulatedMS += int64(positionMS)
			return positionMS, next, false
		}
		return 0, next, false
	}

	wall := now.Sub(st.LastCheckpoint).Milliseconds()
	if wall < 0 {
		wall = 0
	}
	next.LastCheckpoint = now

	if !playingStatus(status) {
		next.LastPositionMS = positionMS
		return 0, next, false
	}
	if wall > maxGapMS {
		next.LastPositionMS = positionMS
		return 0, next, false
	}
	if positionMS <= st.LastPositionMS {
		next.LastPositionMS = positionMS
		return 0, next, false
	}

	forward := positionMS - st.LastPositionMS
	wallCap := int(float64(wall) * rate * wallSlack)
	if wallCap < 0 {
		wallCap = 0
	}
	if forward > wallCap && forward > maxCreditPerMessageMS {
		next.LastPositionMS = positionMS
		return 0, next, false
	}
	credit := forward
	if credit > wallCap {
		credit = wallCap
	}
	if credit > maxCreditPerMessageMS {
		credit = maxCreditPerMessageMS
	}
	if credit < 0 {
		credit = 0
	}
	next.LastPositionMS = positionMS
	next.AccumulatedMS += int64(credit)
	return credit, next, false
}
