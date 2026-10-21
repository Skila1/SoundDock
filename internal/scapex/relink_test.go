package scapex

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestFinishReusedWithoutIntentsSucceeds(t *testing.T) {
	svc := NewService(stubYT{}, NewDockWithPool(nil, t.TempDir()))
	orig := uuid.New()
	failed := false
	fail := func(err error) ([]uuid.UUID, error) {
		failed = true
		return nil, err
	}
	ids, err := svc.finishReused(context.Background(), FetchOpts{}, uuid.New(), uuid.New(), []uuid.UUID{orig}, fail)
	if err != nil || failed {
		t.Fatalf("an import fill whose video is already on disk must succeed: %v", err)
	}
	if len(ids) != 1 || ids[0] != orig {
		t.Fatalf("ids %v", ids)
	}
}

func TestFinishReusedNothingSkippedIsTerminal(t *testing.T) {
	svc := NewService(stubYT{}, NewDockWithPool(nil, t.TempDir()))
	fail := func(err error) ([]uuid.UUID, error) { return nil, WrapFetchError(err) }
	_, err := svc.finishReused(context.Background(), FetchOpts{}, uuid.New(), uuid.New(), nil, fail)
	if ClassifyFetchError(err) != FetchTerminal {
		t.Fatalf("class %v err %v", ClassifyFetchError(err), err)
	}
}

func TestOriginalPendingIsRetryable(t *testing.T) {
	if ClassifyFetchError(errOriginalPending) != FetchRetryable {
		t.Fatal("an original still settling must retry, not fail the intents")
	}
	if ClassifyFetchError(WrapFetchError(errOriginalPending)) != FetchRetryable {
		t.Fatal("wrapping must keep it retryable")
	}
}

func TestTailLinesKeepsLastNonEmpty(t *testing.T) {
	raw := []byte("one\n\ntwo\nthree\n  \nfour\nfive\nsix\n")
	if got := tailLines(raw, 3); got != "four | five | six" {
		t.Fatalf("got %q", got)
	}
	if got := tailLines(nil, 3); got != "" {
		t.Fatalf("empty stderr: %q", got)
	}
	long := []byte(strings.Repeat("x", 5000))
	if got := tailLines(long, 1); len(got) != 1200 {
		t.Fatalf("tail must be capped, len %d", len(got))
	}
}

func TestWithStderrAddsYTDLPOutput(t *testing.T) {
	base := errors.New("no audio files after yt-dlp (is ffmpeg installed?)")
	err := withStderr(base, []byte("[youtube] abc: Downloading webpage\nERROR: [youtube] abc: Sign in to confirm your age\n"))
	if !errors.Is(err, base) {
		t.Fatal("must wrap the original error")
	}
	if !strings.Contains(err.Error(), "Sign in to confirm your age") {
		t.Fatalf("stderr tail missing: %v", err)
	}
	if ClassifyFetchError(err) != FetchTerminal {
		t.Fatalf("an age gate stays terminal: %v", err)
	}
	if got := withStderr(base, nil); got != base {
		t.Fatalf("no stderr keeps the error as is: %v", got)
	}
}

func TestOriginalTrackSharedByHasOriginalAndRelink(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	d := NewDockWithPool(pool, t.TempDir())
	var lib uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM libraries ORDER BY created_at LIMIT 1`).Scan(&lib); err != nil {
		t.Skip("no library in test database")
	}
	vid := fmt.Sprintf("Rl%09d", uuid.New().ID()%1000000000)
	var older, newer uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO tracks (library_id, title, acquisition, acquisition_ref) VALUES ($1,'old','youtube',$2) RETURNING id`, lib, vid).Scan(&older); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO tracks (library_id, title, acquisition, acquisition_ref, created_at) VALUES ($1,'new','youtube',$2, now()+interval '1 second') RETURNING id`, lib, vid).Scan(&newer); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE id IN ($1,$2)`, older, newer)
	})
	// Only the older row holds the original; the newer one has a transcode.
	if _, err := pool.Exec(ctx, `
		INSERT INTO track_files (track_id, library_id, storage_key, size_bytes, quality)
		VALUES ($1,$3,$4,1,'original'), ($2,$3,$5,1,'transcode')`,
		older, newer, lib, "rl/"+vid+".m4a", "rl/"+vid+".opus"); err != nil {
		t.Skip(err)
	}
	if !d.HasOriginal(ctx, lib, vid) {
		t.Fatal("HasOriginal must see the original")
	}
	if got := d.relinkIntentsToOriginal(ctx, uuid.Nil, lib, vid); got != older {
		t.Fatalf("relink must target the track holding the original, got %v want %v", got, older)
	}
}
