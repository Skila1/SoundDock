package scapex

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/jobs"
)

func TestIntentSurvivesFailedJobAndRetry(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	lib := uuid.MustParse("00000000-0000-4000-8000-000000000020")
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM libraries WHERE id=$1`, lib).Scan(&n); err != nil || n == 0 {
		t.Skip("fixture library missing")
	}
	userID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, username, password_hash, display_name) VALUES ($1,$2,'x',$2)`, userID, "int-"+userID.String()[:8]); err != nil {
		t.Skip(err)
	}
	runner := jobs.New(pool, nil)
	jobID, err := runner.Enqueue(ctx, "scapex.fetch", map[string]any{
		"urls":            []string{"https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		"source_refs":     []string{"dQw4w9WgXcQ"},
		"dest_library_id": lib.String(),
		"media_policy_id": "m4a-0",
		"coalesce_key":    "test|" + uuid.NewString(),
	})
	if err != nil {
		t.Fatal(err)
	}
	in := IntentInput{
		UserID: userID, DestLibraryID: lib, SourceRef: "dQw4w9WgXcQ",
		Provider: ProviderYouTube, Intent: IntentQueue, MediaPolicyID: "m4a-0",
	}
	intentID, err := InsertIntent(ctx, pool, in, jobID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM acquisition_intents WHERE id=$1`, intentID)
		_, _ = pool.Exec(c, `DELETE FROM jobs WHERE id=$1`, jobID)
		_, _ = pool.Exec(c, `DELETE FROM users WHERE id=$1`, userID)
	})

	if _, err := pool.Exec(ctx, `UPDATE jobs SET status='failed', last_error='injected' WHERE id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM acquisition_intents WHERE id=$1`, intentID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != StatusQueued {
		t.Fatalf("intent status %s after job fail, want queued", status)
	}
	if err := runner.Retry(ctx, jobID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM jobs WHERE id=$1`, jobID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "queued" {
		t.Fatalf("job retry status %s", status)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM acquisition_intents WHERE id=$1`, intentID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != StatusQueued {
		t.Fatalf("intent lost after retry: %s", status)
	}
}
