package jobs

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testJobsPool(t *testing.T) *pgxpool.Pool {
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

func TestEnqueueCoalescedSameKeyOneJob(t *testing.T) {
	pool := testJobsPool(t)
	ctx := context.Background()
	r := New(pool, nil)
	key := "youtube|dQw4w9WgXcQ|" + uuid.NewString() + "|m4a-0"
	payload := map[string]any{
		"urls":            []string{"https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		"source_refs":     []string{"dQw4w9WgXcQ"},
		"dest_library_id": uuid.New().String(),
		"media_policy_id": "m4a-0",
	}
	a, err := r.EnqueueCoalesced(ctx, "scapex.fetch", key, payload)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM jobs WHERE id=$1`, a) })
	b, err := r.EnqueueCoalesced(ctx, "scapex.fetch", key, payload)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("expected one job, got %s and %s", a, b)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE payload->>'coalesce_key'=$1`, key).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("jobs with key: %d", n)
	}
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT payload FROM jobs WHERE id=$1`, a).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["coalesce_key"] != key {
		t.Fatalf("payload %#v", got)
	}
}

func TestEnqueueCoalescedDifferentDestOrPolicyTwoJobs(t *testing.T) {
	pool := testJobsPool(t)
	ctx := context.Background()
	r := New(pool, nil)
	libA, libB := uuid.New(), uuid.New()
	ref := "kXYiU_JCYtU"
	keyA := "youtube|" + ref + "|" + libA.String() + "|m4a-0"
	keyB := "youtube|" + ref + "|" + libB.String() + "|m4a-0"
	keyC := "youtube|" + ref + "|" + libA.String() + "|opus-0"
	a, err := r.EnqueueCoalesced(ctx, "scapex.fetch", keyA, map[string]any{"source_refs": []string{ref}, "media_policy_id": "m4a-0", "dest_library_id": libA.String()})
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.EnqueueCoalesced(ctx, "scapex.fetch", keyB, map[string]any{"source_refs": []string{ref}, "media_policy_id": "m4a-0", "dest_library_id": libB.String()})
	if err != nil {
		t.Fatal(err)
	}
	c, err := r.EnqueueCoalesced(ctx, "scapex.fetch", keyC, map[string]any{"source_refs": []string{ref}, "media_policy_id": "opus-0", "dest_library_id": libA.String()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM jobs WHERE id=ANY($1)`, []uuid.UUID{a, b, c})
	})
	if a == b || a == c || b == c {
		t.Fatalf("expected three jobs, got %s %s %s", a, b, c)
	}
}
