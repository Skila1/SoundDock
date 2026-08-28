package scapex

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

func TestWaitTrackDoesNotStealByTitle(t *testing.T) {
	pool := testPool(t)
	d := NewDockWithPool(pool, t.TempDir())
	lib := uuid.MustParse("00000000-0000-4000-8000-000000000020")
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM tracks WHERE library_id=$1 AND title='Numb'`, lib).Scan(&n); err != nil || n < 1 {
		t.Skip("fixture Numb track missing")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	id, err := d.WaitTrack(ctx, lib, "NoSuchVid01", "Numb")
	if err == nil {
		t.Fatalf("stole track %s by title", id)
	}
	if id != uuid.Nil {
		t.Fatalf("got %s", id)
	}

	id, ok, err := d.findTrack(context.Background(), lib, "NoSuchVid01")
	if err != nil {
		t.Fatal(err)
	}
	if ok || id != uuid.Nil {
		t.Fatalf("findTrack stole %s by title", id)
	}
}
