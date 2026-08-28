package scapex

import "testing"

func TestIsPlaceholderTitle(t *testing.T) {
	for _, s := range []string{"", "Restoring", "YouTube 0B5I_nPxxxx", "YouTube Lz7CXAxxxxx"} {
		if !IsPlaceholderTitle(s) {
			t.Fatalf("%q should be a placeholder", s)
		}
	}
	if IsPlaceholderTitle("Ring My Bell") {
		t.Fatal("real title treated as placeholder")
	}
}

func TestStubTitleUsesHint(t *testing.T) {
	if got := stubTitle("0B5I_nPxxxx", TrackHint{Title: "She's On Fire"}); got != "She's On Fire" {
		t.Fatalf("got %q", got)
	}
	if got := stubTitle("0B5I_nPxxxx", TrackHint{}); got != "YouTube 0B5I_nPxxxx" {
		t.Fatalf("got %q", got)
	}
}
