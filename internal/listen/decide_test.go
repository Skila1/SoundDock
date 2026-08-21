package listen

import (
	"testing"

	"github.com/google/uuid"
)

func TestThresholdMs(t *testing.T) {
	tests := []struct {
		dur, want int
	}{
		{185000, 30000},
		{40000, 20000},
		{0, 30000},
		{10000, 5000},
	}
	for _, tc := range tests {
		if got := ThresholdMs(tc.dur); got != tc.want {
			t.Fatalf("dur %d got %d want %d", tc.dur, got, tc.want)
		}
	}
}

func TestDecideTable(t *testing.T) {
	track := uuid.MustParse("00000000-0000-4000-8000-000000000050")
	other := uuid.MustParse("00000000-0000-4000-8000-000000000051")
	dur := 185000
	tests := []struct {
		name string
		st   playState
		ev   Event
		act  int
		want playState
	}{
		{
			name: "progress under threshold",
			ev:   Event{TrackID: track, PositionMs: 10000, DurationMs: dur, Kind: "progress"},
			act:  actionNone,
			want: playState{trackID: track, lastPos: 10000},
		},
		{
			name: "play at 30s",
			st:   playState{trackID: track, lastPos: 10000},
			ev:   Event{TrackID: track, PositionMs: 30000, DurationMs: dur, Kind: "progress"},
			act:  actionPlay,
			want: playState{trackID: track, counted: true, lastPos: 30000},
		},
		{
			name: "pause continues same start",
			st:   playState{trackID: track, lastPos: 20000},
			ev:   Event{TrackID: track, PositionMs: 20000, DurationMs: dur, Kind: "progress"},
			act:  actionNone,
			want: playState{trackID: track, lastPos: 20000},
		},
		{
			name: "seek past threshold counts",
			st:   playState{trackID: track, lastPos: 0},
			ev:   Event{TrackID: track, PositionMs: 40000, DurationMs: dur, Kind: "progress"},
			act:  actionPlay,
			want: playState{trackID: track, counted: true, lastPos: 40000},
		},
		{
			name: "seek back after counted does not double",
			st:   playState{trackID: track, counted: true, lastPos: 40000},
			ev:   Event{TrackID: track, PositionMs: 5000, DurationMs: dur, Kind: "progress"},
			act:  actionNone,
			want: playState{trackID: track, counted: true, lastPos: 5000},
		},
		{
			name: "repeat one loop is new start",
			st:   playState{trackID: track, counted: true, lastPos: 185000},
			ev:   Event{TrackID: track, PositionMs: 0, DurationMs: dur, Kind: "progress"},
			act:  actionNone,
			want: playState{trackID: track, lastPos: 0},
		},
		{
			name: "skip before threshold",
			st:   playState{trackID: track, lastPos: 5000},
			ev:   Event{TrackID: track, PositionMs: 5000, DurationMs: dur, Kind: "skip"},
			act:  actionSkip,
			want: playState{},
		},
		{
			name: "skip after counted is not skip",
			st:   playState{trackID: track, counted: true, lastPos: 40000},
			ev:   Event{TrackID: track, PositionMs: 40000, DurationMs: dur, Kind: "skip"},
			act:  actionNone,
			want: playState{},
		},
		{
			name: "stop after current is not skip",
			st:   playState{trackID: track, lastPos: 5000},
			ev:   Event{TrackID: track, PositionMs: 5000, DurationMs: dur, Kind: "skip", StopAfter: true},
			act:  actionNone,
			want: playState{},
		},
		{
			name: "new track is new start",
			st:   playState{trackID: track, counted: true, lastPos: 40000},
			ev:   Event{TrackID: other, PositionMs: 0, DurationMs: dur, Kind: "progress"},
			act:  actionNone,
			want: playState{trackID: other, lastPos: 0},
		},
		{
			name: "short track 50 percent first",
			ev:   Event{TrackID: track, PositionMs: 20000, DurationMs: 40000, Kind: "progress"},
			act:  actionPlay,
			want: playState{trackID: track, counted: true, lastPos: 20000},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, act := decide(tc.st, tc.ev)
			if act != tc.act || got != tc.want {
				t.Fatalf("got %+v act=%d want %+v act=%d", got, act, tc.want, tc.act)
			}
		})
	}
}

func TestNormalizeSource(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"", "web", true},
		{"web", "web", true},
		{"discord", "discord", true},
		{"import", "import", true},
		{"other", "", false},
	}
	for _, tc := range tests {
		got, ok := NormalizeSource(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("%q: %q %v", tc.in, got, ok)
		}
	}
}
