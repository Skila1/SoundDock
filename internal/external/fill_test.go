package external

import (
	"testing"

	"github.com/sounddock/sounddock/internal/scapex"
)

func TestPickYouTubeIDRequiresTitleAndArtist(t *testing.T) {
	hits := []scapex.Hit{
		{ID: "majortomxxx", Title: "Peter Schilling - Major Tom (Coming Home) Official Video", Artist: "Peter Schilling"},
		{ID: "randomxxxx1", Title: "Workout mix 2024", Artist: "Various"},
	}
	got := pickYouTubeID(hits, "Major Tom (Coming Home)", []string{"Peter Schilling"})
	if got != "majortomxxx" {
		t.Fatalf("got %q", got)
	}
	if pickYouTubeID(hits, "dominga la mave", []string{"Someone"}) != "" {
		t.Fatal("unrelated query should not pick a hit")
	}
}

func TestYouTubeQuery(t *testing.T) {
	if youtubeQuery("Major Tom", []string{"Peter Schilling"}) != "Peter Schilling Major Tom" {
		t.Fatal(youtubeQuery("Major Tom", []string{"Peter Schilling"}))
	}
}
