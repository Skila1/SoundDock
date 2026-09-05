package playback

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestShouldReplenishAutoplay(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                string
		autoplay, stopAfter bool
		remaining           int
		want                bool
	}{
		{"off", false, false, 1, false},
		{"stop after", true, true, 1, false},
		{"long queue", true, false, 3, false},
		{"current plus one", true, false, 2, true},
		{"last track", true, false, 1, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ShouldReplenishAutoplay(c.autoplay, c.stopAfter, c.remaining); got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestSkipEndedRefillsWhenAutoplayFillerAddsTracks(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := seedUser(t, pool)
	a, b := fixtureTracks(t, pool)
	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Replace(ctx, sid, []uuid.UUID{a}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE playback_sessions SET autoplay=true WHERE id=$1`, sid); err != nil {
		t.Fatal(err)
	}
	e.SetAutoplayFiller(func(ctx context.Context, id uuid.UUID) error {
		return e.Add(ctx, id, []uuid.UUID{b}, false)
	})
	if err := e.Control(ctx, sid, "skip", map[string]any{"ended": true}); err != nil {
		t.Fatal(err)
	}
	q, err := e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if q["status"] != "playing" {
		t.Fatalf("status %v", q["status"])
	}
	if fmtTrack(q["current_track_id"]) != b.String() {
		t.Fatalf("current %v want %s", q["current_track_id"], b)
	}
	items, _ := q["items"].([]map[string]any)
	if len(items) < 2 {
		t.Fatalf("items %d", len(items))
	}
}

func TestSkipEndedStopsWhenAutoplayOff(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e := New(pool)
	userID := seedUser(t, pool)
	a, _ := fixtureTracks(t, pool)
	sid, err := e.WebSession(ctx, userID, "browser-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Replace(ctx, sid, []uuid.UUID{a}, 0); err != nil {
		t.Fatal(err)
	}
	if err := e.Control(ctx, sid, "skip", map[string]any{"ended": true}); err != nil {
		t.Fatal(err)
	}
	q, err := e.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if q["status"] != "stopped" {
		t.Fatalf("status %v", q["status"])
	}
}
