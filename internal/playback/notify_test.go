package playback

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEncodeSignalStaysUnder2KB(t *testing.T) {
	sig := Signal{
		T:     "session.state",
		SID:   "00000000-0000-4000-8000-000000000001",
		Rev:   99,
		Scope: "session",
	}
	b, err := EncodeSignal(sig)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > notifySoftLimit {
		t.Fatalf("payload %d", len(b))
	}
	if strings.Contains(string(b), "items") {
		t.Fatal("queue body leaked")
	}
}

func TestEncodeSignalDropsOptionalFieldsWhenHuge(t *testing.T) {
	ids := make([]string, 400)
	for i := range ids {
		ids[i] = "00000000-0000-4000-8000-0000000000aa"
	}
	b, err := EncodeSignal(Signal{
		T:     "resource.invalidate",
		Scope: "library",
		IDs:   ids,
		Keys:  []string{"tracks", "albums", "playlists"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > notifySoftLimit {
		t.Fatalf("payload %d", len(b))
	}
	var got Signal
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Resync {
		t.Fatal("expected resync fallback")
	}
	if len(got.IDs) != 0 {
		t.Fatal("ids must be dropped")
	}
}

func TestPlaylistInvalidateSignalIsCompact(t *testing.T) {
	b, err := EncodeSignal(Signal{
		T:     "resource.invalidate",
		Scope: "user",
		Actor: "00000000-0000-4000-8000-000000000001",
		Keys:  []string{"playlists", "playlist", "unmatched", "me-providers"},
		IDs:   []string{"00000000-0000-4000-8000-000000000010"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > notifySoftLimit {
		t.Fatalf("payload %d", len(b))
	}
	s := string(b)
	if strings.Contains(s, "items") || strings.Contains(s, "queue") {
		t.Fatal("queue body leaked")
	}
}

func TestPersonalLibraryInvalidateSignalIsCompact(t *testing.T) {
	b, err := EncodeSignal(Signal{
		T:     "resource.invalidate",
		Scope: "user",
		Actor: "00000000-0000-4000-8000-000000000001",
		Keys:  []string{"personal-library", "home"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > notifySoftLimit {
		t.Fatalf("payload %d", len(b))
	}
	var got Signal
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Scope != "user" || len(got.Keys) != 2 {
		t.Fatalf("%+v", got)
	}
}
