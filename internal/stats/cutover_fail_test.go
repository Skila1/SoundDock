package stats

import (
	"context"
	"testing"
)

func TestCutoverFailureBeforeSwapLeavesHistoryReader(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := SetReader(ctx, pool, ReaderHistory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = SetReader(context.Background(), pool, ReaderHistory)
	})

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err := CutoverToEvents(cancelled, pool)
	if err == nil {
		t.Fatal("cancelled cutover should fail")
	}
	if ReaderIsEvents(ctx, pool) {
		t.Fatal("listen_reader must stay history when rebuild fails before swap")
	}
}
