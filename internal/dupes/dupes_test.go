package dupes

import (
	"testing"

	"github.com/google/uuid"
)

func TestFindHandlesArtistTitleOrderAndPunctuation(t *testing.T) {
	tr := func(title, artist string, ms int) Track {
		return Track{ID: uuid.New(), Title: title, Artist: artist, DurationMS: ms}
	}
	groups := Find([]Track{
		tr("The Ink Spots - I Don't Want to Set the World on Fire", "", 180000),
		tr("I Don't Want To Set The World On Fire (Official Audio)", "The Ink Spots", 181000),
		tr("I don't want to set the world on fire - The Ink Spots", "", 179500),
		tr("Atom Bomb Baby", "The Five Stars", 150000),
		tr("Butcher Pete, Pt. 1", "Roy Brown", 160000),
	})
	if len(groups) != 1 {
		t.Fatalf("want 1 group, got %d: %+v", len(groups), groups)
	}
	if g := groups[0]; len(g.Tracks) != 3 || g.Score < 8 {
		t.Fatalf("group %+v", g)
	}
}

func TestFindScoresDifferentLengthsLow(t *testing.T) {
	groups := Find([]Track{
		{ID: uuid.New(), Title: "Intro", Artist: "A", DurationMS: 60000},
		{ID: uuid.New(), Title: "Intro", Artist: "B", DurationMS: 200000},
	})
	if len(groups) != 0 {
		t.Fatalf("want no confident group, got %+v", groups)
	}
}
