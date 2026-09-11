package jobs

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/sounddock/sounddock/internal/testdb"
)

func TestRunJobCancelsHandlerContext(t *testing.T) {
	pool := testdb.Open(t)
	r := New(pool, slog.Default())
	started := make(chan struct{})
	gotCancel := make(chan struct{})
	r.Register("tracks.metadata", func(ctx context.Context, job Job) error {
		close(started)
		select {
		case <-ctx.Done():
			close(gotCancel)
			return ctx.Err()
		case <-time.After(8 * time.Second):
			return nil
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)
	defer r.Drain()

	id, err := r.Enqueue(ctx, "tracks.metadata", map[string]any{"test": true})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("job did not start")
	}
	if err := r.RequestCancel(ctx, id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gotCancel:
	case <-time.After(3 * time.Second):
		t.Fatal("handler context was not cancelled")
	}
}
