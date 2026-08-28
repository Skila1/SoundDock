package playback

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestCommandReceiptRetryAfterEngineRestart(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	e1 := New(pool)
	userID := seedUser(t, pool)
	sid, err := e1.WebSession(ctx, userID, "retry-1")
	if err != nil {
		t.Fatal(err)
	}
	cmd := uuid.NewString()
	if err := e1.Control(ctx, sid, "volume", map[string]any{"volume": 0.33, "command_id": cmd}); err != nil {
		t.Fatal(err)
	}
	e2 := New(pool)
	if err := e2.Control(ctx, sid, "volume", map[string]any{"volume": 0.33, "command_id": cmd}); err != nil {
		t.Fatal(err)
	}
	if err := e2.Control(ctx, sid, "volume", map[string]any{"volume": 0.9, "command_id": cmd}); !errors.Is(err, ErrCommandConflict) {
		t.Fatalf("hash mismatch: %v", err)
	}
	q, err := e2.Get(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if q["volume"] != 0.33 {
		t.Fatalf("replay/conflict mutated volume %v", q["volume"])
	}
}
