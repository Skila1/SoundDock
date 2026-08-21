package scrobble

import "github.com/google/uuid"

type Event struct {
	TrackID    uuid.UUID
	PositionMS int
	DurationMS int
	Source     string
	Kind       string // progress | skip
}

type State struct {
	TrackID       uuid.UUID
	Counted       bool
	MaxPositionMS int
}

type Outcome struct {
	CountPlay     bool
	CountSkip     bool
	InsertHistory bool
	ResetStart    bool
}

// Eval applies listen.json rules to a single event.
func Eval(prev State, ev Event) (State, Outcome) {
	src := ev.Source
	if src == "" {
		src = "web"
	}
	_ = src
	dur := ev.DurationMS
	if dur < 1 {
		dur = 1
	}
	next := prev
	var out Outcome

	sameTrack := prev.TrackID != uuid.Nil && prev.TrackID == ev.TrackID
	if !sameTrack {
		next = State{TrackID: ev.TrackID}
		out.ResetStart = true
	}

	// Repeat-one loop: position near start after we were near the end.
	if sameTrack && ev.PositionMS < 3000 && prev.MaxPositionMS >= int(float64(dur)*0.8) {
		next = State{TrackID: ev.TrackID}
		out.ResetStart = true
		sameTrack = true
	}

	if ev.PositionMS > next.MaxPositionMS {
		next.MaxPositionMS = ev.PositionMS
	}

	if ev.Kind == "skip" {
		if !next.Counted {
			out.CountSkip = true
		}
		return next, out
	}

	thresh := dur / 2
	if thresh > 30000 || thresh <= 0 {
		thresh = 30000
	}
	if dur < 60000 {
		thresh = dur / 2
		if thresh < 1 {
			thresh = 1
		}
	}
	if !next.Counted && ev.PositionMS >= thresh {
		next.Counted = true
		out.CountPlay = true
		out.InsertHistory = true
	}
	return next, out
}
