package scrobble

import (
	"testing"

	"github.com/google/uuid"
)

func TestEvalProgressThreshold(t *testing.T) {
	id := uuid.MustParse("00000000-0000-4000-8000-000000000050")
	st, out := Eval(State{}, Event{TrackID: id, PositionMS: 30000, DurationMS: 185000, Kind: "progress"})
	if !out.CountPlay || !out.InsertHistory || !st.Counted {
		t.Fatalf("expected play count at 30s: %#v %#v", st, out)
	}
	_, out2 := Eval(st, Event{TrackID: id, PositionMS: 5000, DurationMS: 185000, Kind: "progress"})
	if out2.CountPlay || out2.InsertHistory {
		t.Fatal("seek back after counted must not double")
	}
}

func TestEvalHalfDurationShortTrack(t *testing.T) {
	id := uuid.New()
	st, out := Eval(State{}, Event{TrackID: id, PositionMS: 20000, DurationMS: 40000, Kind: "progress"})
	if !out.CountPlay || !st.Counted {
		t.Fatalf("50%% of 40s should count: %#v %#v", st, out)
	}
}

func TestEvalSkipBeforeThreshold(t *testing.T) {
	id := uuid.New()
	st, out := Eval(State{TrackID: id}, Event{TrackID: id, PositionMS: 5000, DurationMS: 185000, Kind: "skip"})
	if !out.CountSkip || out.CountPlay || out.InsertHistory || !st.Counted {
		t.Fatalf("skip before threshold: %#v %#v", st, out)
	}
	_, out2 := Eval(st, Event{TrackID: id, PositionMS: 5000, DurationMS: 185000, Kind: "skip"})
	if out2.CountSkip {
		t.Fatal("duplicate skip must not increment skip_count")
	}
}

func TestEvalStopAfterIsNotSkip(t *testing.T) {
	id := uuid.New()
	st, out := Eval(State{TrackID: id}, Event{TrackID: id, PositionMS: 5000, DurationMS: 185000, Kind: "skip", StopAfter: true})
	if out.CountSkip || out.CountPlay || out.InsertHistory || st.Counted {
		t.Fatalf("stop after current is not a skip: %#v %#v", st, out)
	}
}

func TestEvalSkipAfterCountNotSkip(t *testing.T) {
	id := uuid.New()
	prev := State{TrackID: id, Counted: true, MaxPositionMS: 40000}
	_, out := Eval(prev, Event{TrackID: id, PositionMS: 40000, DurationMS: 185000, Kind: "skip"})
	if out.CountSkip || out.CountPlay {
		t.Fatalf("skip after counted should not increment skip: %#v", out)
	}
}

func TestEvalRepeatOneNewStart(t *testing.T) {
	id := uuid.New()
	prev := State{TrackID: id, Counted: true, MaxPositionMS: 180000}
	st, out := Eval(prev, Event{TrackID: id, PositionMS: 500, DurationMS: 185000, Kind: "progress"})
	if !out.ResetStart || st.Counted {
		t.Fatalf("repeat loop should be a new start: %#v %#v", st, out)
	}
}

func TestEvalSeekPastThresholdCounts(t *testing.T) {
	id := uuid.New()
	_, out := Eval(State{}, Event{TrackID: id, PositionMS: 90000, DurationMS: 185000, Kind: "progress"})
	if !out.CountPlay {
		t.Fatal("seek past threshold should count")
	}
}
