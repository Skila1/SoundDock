package scrobble

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

func TestHandleListenSingleInsert(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	track := uuid.MustParse("00000000-0000-4000-8000-000000000050")
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE id=$1`, track).Scan(&n); err != nil || n != 1 {
		t.Skip("fixture track missing")
	}
	userID := uuid.New()
	username := "p1hl-" + userID.String()[:8]
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`, userID, username); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM listen_output_segments WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM listen_instance_state WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM listen_events WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM listen_history WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM play_counts WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM scrobble_listen_state WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	svc := New(pool, nil, nil)
	ev := Event{TrackID: track, DurationMS: 185000, Source: "web", Kind: "progress"}
	ev.PositionMS = 10000
	if err := svc.HandleListen(ctx, userID, ev); err != nil {
		t.Fatal(err)
	}
	ev.PositionMS = 30000
	if err := svc.HandleListen(ctx, userID, ev); err != nil {
		t.Fatal(err)
	}
	ev.PositionMS = 40000
	if err := svc.HandleListen(ctx, userID, ev); err != nil {
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

	skipEv := Event{TrackID: track, PositionMS: 40000, DurationMS: 185000, Source: "web", Kind: "skip"}
	if err := svc.HandleListen(ctx, userID, skipEv); err != nil {
		t.Fatal(err)
	}
	if err := svc.HandleListen(ctx, userID, skipEv); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count, skip_count FROM play_counts WHERE user_id=$1 AND track_id=$2`, userID, track).Scan(&plays, &skips); err != nil {
		t.Fatal(err)
	}
	if plays != 1 || skips != 0 {
		t.Fatalf("skip after counted plays=%d skips=%d", plays, skips)
	}
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM listen_history WHERE user_id=$1 AND track_id=$2`, userID, track).Scan(&hist)
	if hist != 1 {
		t.Fatalf("skip must not insert history, got %d", hist)
	}
}

func TestHandleListenSkipOnce(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	track := uuid.MustParse("00000000-0000-4000-8000-000000000050")
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE id=$1`, track).Scan(&n); err != nil || n != 1 {
		t.Skip("fixture track missing")
	}
	userID := uuid.New()
	username := "p1sk-" + userID.String()[:8]
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`, userID, username); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM listen_output_segments WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM listen_instance_state WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM listen_events WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM listen_history WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM play_counts WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM scrobble_listen_state WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	svc := New(pool, nil, nil)
	ev := Event{TrackID: track, PositionMS: 5000, DurationMS: 185000, Source: "web", Kind: "progress"}
	if err := svc.HandleListen(ctx, userID, ev); err != nil {
		t.Fatal(err)
	}
	skipEv := Event{TrackID: track, PositionMS: 5000, DurationMS: 185000, Source: "web", Kind: "skip"}
	if err := svc.HandleListen(ctx, userID, skipEv); err != nil {
		t.Fatal(err)
	}
	if err := svc.HandleListen(ctx, userID, skipEv); err != nil {
		t.Fatal(err)
	}
	var plays, skips, hist int
	if err := pool.QueryRow(ctx, `SELECT count, skip_count FROM play_counts WHERE user_id=$1 AND track_id=$2`, userID, track).Scan(&plays, &skips); err != nil {
		t.Fatal(err)
	}
	if plays != 0 || skips != 1 {
		t.Fatalf("one skip_count bump per skip: plays=%d skips=%d", plays, skips)
	}
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM listen_history WHERE user_id=$1 AND track_id=$2`, userID, track).Scan(&hist)
	if hist != 0 {
		t.Fatalf("skip must not insert history, got %d", hist)
	}
}

func seedHandleListenUser(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	track := uuid.MustParse("00000000-0000-4000-8000-000000000050")
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE id=$1`, track).Scan(&n); err != nil || n != 1 {
		t.Skip("fixture track missing")
	}
	username := "p1ev-" + userID.String()[:8]
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`, userID, username); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM listen_output_segments WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM listen_instance_state WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM listen_events WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM listen_history WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM play_counts WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM scrobble_listen_state WHERE user_id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	return track
}

func TestHandleListenAccumulatedQualifiesEvents(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	userID := uuid.New()
	track := seedHandleListenUser(t, pool, userID)
	svc := New(pool, nil, nil)
	instance := uuid.New()
	now := time.Now().UTC()
	for i := 0; i <= 30; i++ {
		ev := Event{
			TrackID: track, PositionMS: i * 1000, DurationMS: 185000, Source: "web", Kind: "progress",
			PlaybackInstanceID: instance, PlayheadSequence: int64(i + 1),
			RendererKind: "browser", RendererID: "lease", ClientID: "lease",
			Status: "playing", PlaybackRate: 1, At: now.Add(time.Duration(i) * time.Second),
		}
		if err := svc.HandleListen(ctx, userID, ev); err != nil {
			t.Fatal(err)
		}
	}
	var plays, hist, events int
	if err := pool.QueryRow(ctx, `SELECT count FROM play_counts WHERE user_id=$1 AND track_id=$2`, userID, track).Scan(&plays); err != nil {
		t.Fatal(err)
	}
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM listen_history WHERE user_id=$1 AND track_id=$2`, userID, track).Scan(&hist)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM listen_events WHERE user_id=$1 AND kind='qualify'`, userID).Scan(&events)
	if plays != 1 || hist != 1 {
		t.Fatalf("legacy history plays=%d hist=%d", plays, hist)
	}
	if events != 1 {
		t.Fatalf("shadow qualify %d", events)
	}
}

func TestHandleListenSeekPastTHistoryNotEvents(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	userID := uuid.New()
	track := seedHandleListenUser(t, pool, userID)
	svc := New(pool, nil, nil)
	instance := uuid.New()
	now := time.Now().UTC()
	ev := Event{
		TrackID: track, PositionMS: 0, DurationMS: 185000, Source: "web", Kind: "progress",
		PlaybackInstanceID: instance, PlayheadSequence: 1,
		RendererKind: "browser", RendererID: "lease", ClientID: "lease",
		Status: "playing", PlaybackRate: 1, At: now,
	}
	if err := svc.HandleListen(ctx, userID, ev); err != nil {
		t.Fatal(err)
	}
	ev.PositionMS = 40000
	ev.PlayheadSequence = 2
	ev.At = now.Add(time.Second)
	if err := svc.HandleListen(ctx, userID, ev); err != nil {
		t.Fatal(err)
	}
	var hist, events int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM listen_history WHERE user_id=$1 AND track_id=$2`, userID, track).Scan(&hist)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM listen_events WHERE user_id=$1 AND kind='qualify'`, userID).Scan(&events)
	if hist != 1 {
		t.Fatalf("legacy seek-past-T must still write history, got %d", hist)
	}
	if events != 0 {
		t.Fatalf("shadow seek must not qualify, got %d", events)
	}
}
