package stats

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

func TestReaderIsEventsNilPool(t *testing.T) {
	if ReaderIsEvents(context.Background(), nil) {
		t.Fatal("missing pool must default to history")
	}
}

func TestSetReaderRejectsInvalid(t *testing.T) {
	if err := SetReader(context.Background(), nil, ReaderEvents); err == nil {
		t.Fatal("expected error without pool")
	}
	if err := SetReader(context.Background(), nil, "both"); err == nil {
		t.Fatal("invalid mode")
	}
}

func TestCutoverToEventsNilPool(t *testing.T) {
	if _, err := CutoverToEvents(context.Background(), nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestCutoverToEventsRebuildsPlayCounts(t *testing.T) {
	if os.Getenv("SD_TEST_DATABASE_URL") == "" {
		t.Skip("SD_TEST_DATABASE_URL not set")
	}
	pool := testPool(t)
	ctx := context.Background()
	track := uuid.MustParse("00000000-0000-4000-8000-000000000050")
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE id=$1`, track).Scan(&n); err != nil || n != 1 {
		t.Skip("fixture track missing")
	}
	var eventsOK int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM listen_events WHERE false`).Scan(&eventsOK); err != nil {
		t.Skip("listen_events not present")
	}

	userID := uuid.New()
	username := "w5r-" + userID.String()[:8]
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`, userID, username); err != nil {
		t.Skip(err)
	}

	var prevReader *string
	var raw string
	if err := pool.QueryRow(ctx, `SELECT value #>> '{}' FROM server_settings WHERE key=$1`, ReaderKey).Scan(&raw); err == nil {
		prev := raw
		prevReader = &prev
	}

	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM listen_events WHERE user_id=$1`, userID)
		_, _ = pool.Exec(c, `DELETE FROM listen_history WHERE user_id=$1`, userID)
		_, _ = pool.Exec(c, `DELETE FROM play_counts WHERE user_id=$1`, userID)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id=$1`, userID)
		if prevReader != nil {
			_, _ = pool.Exec(c, `
				INSERT INTO server_settings (key, value) VALUES ($1, to_jsonb($2::text))
				ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, ReaderKey, *prevReader)
		} else {
			_, _ = pool.Exec(c, `DELETE FROM server_settings WHERE key=$1`, ReaderKey)
		}
	})

	if _, err := pool.Exec(ctx, `
		INSERT INTO listen_history (user_id, track_id, played_at, duration_ms, source)
		VALUES ($1,$2,now(),180000,'web')`, userID, track); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO play_counts (user_id, track_id, count, skip_count, last_played_at)
		VALUES ($1,$2,99,5,now())`, userID, track); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO listen_events (
			playback_instance_id, user_id, track_id, kind, accumulated_listened_ms,
			listened_ms, track_duration_ms, qualified_play, skipped, legacy_backfill, source, started_at
		) VALUES
			($1,$2,$3,'qualify',180000,NULL,180000,true,false,true,'web',now()),
			($4,$2,$3,'skip',5000,5000,180000,false,true,false,'web',now())`,
		uuid.New(), userID, track, uuid.New()); err != nil {
		t.Fatal(err)
	}

	if ReaderIsEvents(ctx, pool) {
		if err := SetReader(ctx, pool, ReaderHistory); err != nil {
			t.Fatal(err)
		}
	}
	if ReaderIsEvents(ctx, pool) {
		t.Fatal("pre-cutover must read history")
	}

	res, err := CutoverToEvents(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if res.Reader != ReaderEvents {
		t.Fatalf("reader %s", res.Reader)
	}
	if !ReaderIsEvents(ctx, pool) {
		t.Fatal("flag must flip to events last")
	}

	var plays, skips int
	if err := pool.QueryRow(ctx, `SELECT count, skip_count FROM play_counts WHERE user_id=$1 AND track_id=$2`, userID, track).Scan(&plays, &skips); err != nil {
		t.Fatal(err)
	}
	if plays != 1 || skips != 1 {
		t.Fatalf("play_counts from events: count=%d skip_count=%d", plays, skips)
	}

	var hist int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM listen_history WHERE user_id=$1`, userID).Scan(&hist); err != nil {
		t.Fatal(err)
	}
	if hist != 1 {
		t.Fatalf("listen_history must be kept, got %d", hist)
	}
}
