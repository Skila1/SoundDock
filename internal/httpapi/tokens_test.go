package httpapi

import "testing"

func TestPATPrefix(t *testing.T) {
	if got := patPrefix("sdp_abcdefghijk"); got != "sdp_abcdef" {
		t.Fatalf("got %q", got)
	}
	if got := patPrefix("short"); got != "short" {
		t.Fatalf("got %q", got)
	}
	if got := patPrefix("exactlyten"); got != "exactlyten" {
		t.Fatalf("got %q", got)
	}
}

func TestPATTokenPrefix(t *testing.T) {
	if patTokenPrefix != "sdp_" {
		t.Fatalf("unexpected prefix %q", patTokenPrefix)
	}
}
