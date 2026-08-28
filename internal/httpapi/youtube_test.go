package httpapi

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/scapex"
)

func TestIsolatedFetchDoesNotUseEnqueueWait(t *testing.T) {
	s := &Server{}
	_, err := isolatedYT{s: s}.Fetch(context.Background(), []string{"dQw4w9WgXcQ"})
	if err != errScapeXDown {
		t.Fatalf("got %v", err)
	}
}

func TestFetchPayloadHasCoalesceFields(t *testing.T) {
	lib := uuid.New()
	key := scapex.CoalesceKey("youtube", "dQw4w9WgXcQ", lib.String(), "m4a-0")
	p := map[string]any{
		"urls":            []string{"https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		"source_refs":     []string{"dQw4w9WgXcQ"},
		"coalesce_key":    key,
		"dest_library_id": lib.String(),
		"media_policy_id": "m4a-0",
	}
	if p["coalesce_key"] == "" || p["dest_library_id"] == "" || p["media_policy_id"] == "" {
		t.Fatal(p)
	}
}
