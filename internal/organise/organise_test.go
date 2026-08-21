package organise

import "testing"

func TestApply(t *testing.T) {
	p := Apply("", Vars{AlbumArtist: "Linkin Park", Album: "Meteora", Year: 2003, Track: 13, Title: "Numb", Ext: "flac", Disc: 1, DiscCount: 1})
	if p != "Linkin Park/Meteora (2003)/13 - Numb.flac" {
		t.Fatal(p)
	}
	p = Apply("", Vars{Artist: "A", Album: "B", Year: 2000, Track: 1, Title: "T", Ext: "mp3", Disc: 2, DiscCount: 2})
	if p != "A/B (2000)/2-01 - T.mp3" {
		t.Fatal(p)
	}
}
