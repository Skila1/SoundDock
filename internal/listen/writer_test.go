package listen

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("SD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SD_TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skip(err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skip(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestWriterNilPoolCountsOnce(t *testing.T) {
	w := New(nil)
	track := uuid.MustParse("00000000-0000-4000-8000-000000000050")
	ev := Event{UserID: uuid.New(), TrackID: track, DurationMs: 185000, Source: "discord", Kind: "progress"}
	ev.PositionMs = 30000
	if err := w.Record(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	ev.PositionMs = 40000
	if err := w.Record(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	ev.Kind = "skip"
	ev.PositionMs = 5000
	if err := w.Record(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
}

func TestWriterPlayOnceThenSkip(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	track := uuid.MustParse("00000000-0000-4000-8000-000000000050")
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE id=$1`, track).Scan(&n); err != nil || n != 1 {
		t.Skip("fixture track missing")
	}
	userID := uuid.New()
	username := "p1l-" + userID.String()[:8]
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`, userID, username); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM listen_history WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM play_counts WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	w := New(pool)
	ev := Event{UserID: userID, TrackID: track, DurationMs: 185000, Source: "web", Kind: "progress"}
	ev.PositionMs = 10000
	if err := w.Record(ctx, ev); err != nil {
		t.Fatal(err)
	}
	ev.PositionMs = 30000
	if err := w.Record(ctx, ev); err != nil {
		t.Fatal(err)
	}
	ev.PositionMs = 40000
	if err := w.Record(ctx, ev); err != nil {
		t.Fatal(err)
	}
	var plays, skips, hist int
	if err := pool.QueryRow(ctx, `SELECT count, skip_count FROM play_counts WHERE user_id=$1 AND track_id=$2`, userID, track).Scan(&plays, &skips); err != nil {
		t.Fatal(err)
	}
	if plays != 1 || skips != 0 {
		t.Fatalf("plays=%d skips=%d", plays, skips)
	}
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM listen_history WHERE user_id=$1 AND track_id=$2`, userID, track).Scan(&hist)
	if hist != 1 {
		t.Fatalf("history %d", hist)
	}

	w2 := New(pool)
	skipEv := Event{UserID: userID, TrackID: track, PositionMs: 5000, DurationMs: 185000, Source: "web", Kind: "skip"}
	if err := w2.Record(ctx, skipEv); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count, skip_count FROM play_counts WHERE user_id=$1 AND track_id=$2`, userID, track).Scan(&plays, &skips); err != nil {
		t.Fatal(err)
	}
	if plays != 1 || skips != 1 {
		t.Fatalf("after skip plays=%d skips=%d", plays, skips)
	}
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM listen_history WHERE user_id=$1 AND track_id=$2`, userID, track).Scan(&hist)
	if hist != 1 {
		t.Fatalf("skip must not insert history, got %d", hist)
	}
}
