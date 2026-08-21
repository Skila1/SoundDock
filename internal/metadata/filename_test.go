package metadata

import "testing"

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

func TestApplyFilenameFallback(t *testing.T) {
	p := Probe{Title: "321. Commodores - Nightshift"}
	applyFilenameFallback(&p, "uploads/ab/321. Commodores - Nightshift.mp3")
	if p.Title != "Nightshift" || p.Artist != "Commodores" {
		t.Fatalf("%#v", p)
	}
	tagged := Probe{Title: "Nightshift", Artist: "Commodores"}
	applyFilenameFallback(&tagged, "321. Commodores - Nightshift.mp3")
	if tagged.Title != "Nightshift" || tagged.Artist != "Commodores" {
		t.Fatalf("should keep real tags %#v", tagged)
	}
}
