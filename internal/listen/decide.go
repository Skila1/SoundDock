package listen

import (
	"errors"

	"github.com/google/uuid"
)

var ErrSource = errors.New("invalid listen source")

const (
	actionNone = iota
	actionPlay
	actionSkip
)

func ThresholdMs(durationMs int) int {
	if durationMs <= 0 {
		return 30000
	}
	half := durationMs / 2
	if half < 30000 {
		return half
	}
	return 30000
}

func Reached(positionMs, durationMs int) bool {
	return positionMs >= ThresholdMs(durationMs)
}

func NormalizeSource(s string) (string, bool) {
	switch s {
	case "", "web":
		return "web", true
	case "discord", "import":
		return s, true
	default:
		return "", false
	}
}

func nearEnd(durationMs int) int {
	if durationMs <= 0 {
		return 30000
	}
	n := int(float64(durationMs) * 0.9)
	th := ThresholdMs(durationMs)
	if n < th {
		return th
	}
	return n
}

func decide(st playState, ev Event) (playState, int) {
	if ev.TrackID != uuid.Nil && ev.TrackID != st.trackID {
		st = playState{trackID: ev.TrackID}
	}
	if ev.Kind == "skip" {
		if ev.StopAfter {
			return playState{}, actionNone
		}
		if st.counted {
			return playState{}, actionNone
		}
		return playState{}, actionSkip
	}
	if st.counted && ev.PositionMs < ThresholdMs(ev.DurationMs) && st.lastPos >= nearEnd(ev.DurationMs) {
		st = playState{trackID: ev.TrackID, lastPos: ev.PositionMs}
	}
	if !st.counted && Reached(ev.PositionMs, ev.DurationMs) {
		st.counted = true
		st.lastPos = ev.PositionMs
		if st.trackID == uuid.Nil {
			st.trackID = ev.TrackID
		}
		return st, actionPlay
	}
	st.lastPos = ev.PositionMs
	if st.trackID == uuid.Nil {
		st.trackID = ev.TrackID
	}
	return st, actionNone
}
