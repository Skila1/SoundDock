package metadata

import (
	"strings"
	"testing"
)

func TestParseAudioName(t *testing.T) {
	artist, title := ParseAudioName("321. Commodores - Nightshift.mp3")
	if artist != "Commodores" || title != "Nightshift" {
		t.Fatalf("%q %q", artist, title)
	}
	artist, title = ParseAudioName("78. Orchestral Manoeuvres In The Dark - Joan Of Arc (Maid Of Orleans).mp3")
	if artist != "Orchestral Manoeuvres In The Dark" || title != "Joan Of Arc (Maid Of Orleans)" {
		t.Fatalf("%q %q", artist, title)
	}
	artist, title = ParseAudioName("562. Joy Division - Atmosphere (2020 Digital Remaster).flac")
	if artist != "Joy Division" || title != "Atmosphere (2020 Digital Remaster)" {
		t.Fatalf("%q %q", artist, title)
	}
	artist, title = ParseAudioName("13 - Numb.mp3")
	if artist != "" || title != "13 - Numb" {
		t.Fatalf("album track should not become artist=13, got %q %q", artist, title)
	}
	artist, title = ParseAudioName("Nightshift.mp3")
	if artist != "" || title != "Nightshift" {
		t.Fatalf("%q %q", artist, title)
	}
}

func TestOrientParts(t *testing.T) {
	artist, title := OrientParts("Nightshift", "Commodores", Probe{Title: "Nightshift"}, nil)
	if artist != "Commodores" || title != "Nightshift" {
		t.Fatalf("title tag matching left: %q %q", artist, title)
	}
	artist, title = OrientParts("Commodores", "Nightshift", Probe{Title: "Nightshift"}, nil)
	if artist != "Commodores" || title != "Nightshift" {
		t.Fatalf("title tag matching right: %q %q", artist, title)
	}
	artist, title = OrientParts("Nightshift", "Commodores", Probe{Artist: "Commodores"}, nil)
	if artist != "Commodores" || title != "Nightshift" {
		t.Fatalf("artist tag matching right: %q %q", artist, title)
	}
	known := func(s string) bool { return strings.EqualFold(s, "Commodores") }
	artist, title = OrientParts("Nightshift", "Commodores", Probe{}, known)
	if artist != "Commodores" || title != "Nightshift" {
		t.Fatalf("known artist on the right: %q %q", artist, title)
	}
	artist, title = OrientParts("Commodores", "Nightshift", Probe{}, nil)
	if artist != "Commodores" || title != "Nightshift" {
		t.Fatalf("default Artist - Title: %q %q", artist, title)
	}
	artist, title = OrientParts("Nightshift (2020 Remaster)", "Commodores", Probe{}, nil)
	if artist != "Commodores" || title != "Nightshift (2020 Remaster)" {
		t.Fatalf("title-ish left side: %q %q", artist, title)
	}
}

func TestApplyFilenameFallback(t *testing.T) {
	p := Probe{Title: "321. Commodores - Nightshift"}
	applyFilenameFallback(&p, nil, "uploads/ab/deadbeef.mp3", p.Title)
	if p.Title != "Nightshift" || p.Artist != "Commodores" {
		t.Fatalf("%#v", p)
	}
	tagged := Probe{Title: "Nightshift", Artist: "Commodores"}
	applyFilenameFallback(&tagged, nil, "321. Nightshift - Commodores.mp3")
	if tagged.Title != "Nightshift" || tagged.Artist != "Commodores" {
		t.Fatalf("tags must win over reversed filename %#v", tagged)
	}
	reversed := Probe{}
	ApplyOriginalNameKnown(&reversed, "Nightshift - Commodores.mp3", func(s string) bool {
		return strings.EqualFold(s, "Commodores")
	})
	if reversed.Title != "Nightshift" || reversed.Artist != "Commodores" {
		t.Fatalf("library artist on the right should swap %#v", reversed)
	}
	empty := Probe{}
	ApplyOriginalName(&empty, "321. Commodores - Nightshift.mp3")
	if empty.Title != "Nightshift" || empty.Artist != "Commodores" {
		t.Fatalf("original upload name should win when tags are empty %#v", empty)
	}
	hashed := Probe{Title: "9b0d0d15a9cfc8d2aade34f6a5b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9"}
	ApplyOriginalName(&hashed, "321. Commodores - Nightshift.mp3")
	if hashed.Title != "Nightshift" || hashed.Artist != "Commodores" {
		t.Fatalf("hash titles must be replaced %#v", hashed)
	}
	if !LooksLikeHash("9b0d0d15a9cfc8d2aade34f6a5b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9.mp3") {
		t.Fatal("expected hash")
	}
	a, t2 := ParseAudioName("uploads/9b/9b0d0d15a9cfc8d2aade34f6a5b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9.mp3")
	if a != "" || t2 != "" {
		t.Fatalf("hash path must not become a title %q %q", a, t2)
	}
}
