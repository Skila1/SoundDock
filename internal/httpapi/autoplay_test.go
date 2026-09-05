package httpapi

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/playback"
)

func TestReplenishAutoplayAddsWhenQueueIsShort(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	play := playback.New(pool)
	s := &Server{Pool: pool, Play: play}
	u := seedQueueUser(t, pool, "")
	sid, err := play.WebSession(ctx, u.ID, "web")
	if err != nil {
		t.Fatal(err)
	}
	a, b := httpFixtureTracks(t, pool)
	if err := play.Replace(ctx, sid, []uuid.UUID{a}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE playback_sessions SET autoplay=true, user_id=$2 WHERE id=$1`, sid, u.ID); err != nil {
		t.Fatal(err)
	}
	s.autoplaySelectHook = func(context.Context, uuid.UUID, []uuid.UUID) []uuid.UUID {
		return []uuid.UUID{b}
	}
	if err := s.ReplenishAutoplay(ctx, sid); err != nil {
		t.Fatal(err)
	}
	q, err := play.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	items, _ := q["items"].([]map[string]any)
	if len(items) < 2 {
		t.Fatalf("items %d", len(items))
	}
}

func TestReplenishAutoplayNoopsWhenOff(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	play := playback.New(pool)
	s := &Server{Pool: pool, Play: play}
	u := seedQueueUser(t, pool, "")
	sid, err := play.WebSession(ctx, u.ID, "web")
	if err != nil {
		t.Fatal(err)
	}
	a, b := httpFixtureTracks(t, pool)
	if err := play.Replace(ctx, sid, []uuid.UUID{a}, 0); err != nil {
		t.Fatal(err)
	}
	called := 0
	s.autoplaySelectHook = func(context.Context, uuid.UUID, []uuid.UUID) []uuid.UUID {
		called++
		return []uuid.UUID{b}
	}
	if err := s.ReplenishAutoplay(ctx, sid); err != nil {
		t.Fatal(err)
	}
	if called != 0 {
		t.Fatal("autoplay off must not select")
	}
}

func httpFixtureTracks(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, uuid.UUID) {
	t.Helper()
	a := uuid.MustParse("00000000-0000-4000-8000-000000000050")
	b := uuid.MustParse("00000000-0000-4000-8000-000000000051")
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM tracks WHERE id IN ($1,$2)`, a, b).Scan(&n); err != nil || n != 2 {
		t.Skip("fixture tracks missing")
	}
	return a, b
}
