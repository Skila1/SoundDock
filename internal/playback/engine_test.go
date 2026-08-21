package playback

import (
	"testing"

	"github.com/google/uuid"
)

func TestWebOwnerKey(t *testing.T) {
	uid := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	got := WebOwnerKey(uid, "browser-1")
	want := "00000000-0000-4000-8000-000000000001:browser-1"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestNextIndexTable(t *testing.T) {
	a := uuid.MustParse("00000000-0000-4000-8000-0000000000a1")
	b := uuid.MustParse("00000000-0000-4000-8000-0000000000a2")
	ar1 := uuid.MustParse("00000000-0000-4000-8000-0000000000b1")
	ar2 := uuid.MustParse("00000000-0000-4000-8000-0000000000b2")
	items := []queueMeta{
		{Position: 0, AlbumID: a, ArtistID: ar1, Disc: 1, TrackNo: 2},
		{Position: 1, AlbumID: b, ArtistID: ar2, Disc: 1, TrackNo: 1},
		{Position: 2, AlbumID: a, ArtistID: ar1, Disc: 1, TrackNo: 1},
	}
	zero := func(int) int { return 0 }
	tests := []struct {
		name           string
		idx, delta     int
		repeat, mode   string
		shuffle, ended bool
		wantStop       bool
		wantIdx        int
	}{
		{name: "sequential", idx: 0, delta: 1, repeat: "off", wantIdx: 1},
		{name: "queue wrap", idx: 2, delta: 1, repeat: "queue", wantIdx: 0},
		{name: "off end stops", idx: 2, delta: 1, repeat: "off", wantIdx: 2, wantStop: true},
		{name: "repeat one ended stays", idx: 1, delta: 1, repeat: "one", ended: true, wantIdx: 1},
		{name: "repeat one explicit next", idx: 1, delta: 1, repeat: "one", ended: false, wantIdx: 2},
		{name: "previous", idx: 1, delta: -1, repeat: "off", wantIdx: 0},
		{name: "album next same album", idx: 2, delta: 1, shuffle: true, mode: "album", repeat: "off", wantIdx: 0},
		{name: "smart prefers other artist", idx: 0, delta: 1, shuffle: true, mode: "smart", wantIdx: 1},
		{name: "random uses rng offset", idx: 0, delta: 1, shuffle: true, mode: "random", wantIdx: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, stop := nextIndex(items, tc.idx, tc.delta, tc.repeat, tc.mode, tc.shuffle, tc.ended, zero)
			if stop != tc.wantStop || got != tc.wantIdx {
				t.Fatalf("got idx=%d stop=%v want idx=%d stop=%v", got, stop, tc.wantIdx, tc.wantStop)
			}
		})
	}
}

func TestExtraInt(t *testing.T) {
	tests := []struct {
		v    any
		want int
		ok   bool
	}{
		{1.0, 1, true},
		{3, 3, true},
		{int64(4), 4, true},
		{"5", 5, true},
		{nil, 0, false},
	}
	for _, tc := range tests {
		got, ok := extraInt(map[string]any{"from": tc.v}, "from")
		if ok != tc.ok || got != tc.want {
			t.Fatalf("%v: got %d %v want %d %v", tc.v, got, ok, tc.want, tc.ok)
		}
	}
	if _, ok := extraInt(nil, "from"); ok {
		t.Fatal("nil extra")
	}
}

func TestPartyActive(t *testing.T) {
	now := mustTime("2026-01-15T12:00:00Z")
	exp := mustTime("2026-01-15T13:00:00Z")
	past := mustTime("2026-01-15T11:00:00Z")
	if !partyActive(true, &exp, now) {
		t.Fatal("future expiry")
	}
	if partyActive(true, &past, now) {
		t.Fatal("past expiry")
	}
	if partyActive(false, &exp, now) {
		t.Fatal("disabled")
	}
	if !partyActive(true, nil, now) {
		t.Fatal("no expiry")
	}
}

func TestReplayGainOff(t *testing.T) {
	g := 6.0
	if ReplayGainMultiplier("off", &g, &g, -18) != 1 {
		t.Fatal("off")
	}
}
