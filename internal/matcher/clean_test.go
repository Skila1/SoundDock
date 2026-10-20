package matcher

import (
	"reflect"
	"testing"
)

func TestCleanTitle(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Butcher Pete, Pt. 1", "butcher pete pt 1"},
		{"Civilization (Bongo, Bongo, Bongo)", "civilization"},
		{"I Don't Want to Set the World on Fire", "i dont want to set the world on fire"},
		{"Atom Bomb Baby", "atom bomb baby"},
		{"Crazy He Calls Me - Single Version", "crazy he calls me"},
		{"Way Back Home - feat. Conor Maynard (Sam Feldt Edit)", "way back home"},
		{"Let It Be - Remastered 2009", "let it be"},
		{"Bohemian Rhapsody - 2011 Remaster", "bohemian rhapsody"},
		{"Help! - Mono", "help"},
		{"Blue Moon - From \"Fallout 4\"", "blue moon"},
		{"Smells Like Teen Spirit - Live at Reading", "smells like teen spirit"},
		{"Song - Radio Edit", "song"},
		{"Song - Bonus Track", "song"},
		{"Wonderwall - Acoustic", "wonderwall acoustic"},
		{"Numb (feat. Jay-Z) [Remastered]", "numb"},
		{"Lean On ft. MØ", "lean on"},
		{"Beyoncé & Café", "beyonce and cafe"},
		{"(Sittin' On) The Dock of the Bay", "the dock of the bay"},
		{"(I Can't Get No) Satisfaction - Mono Version", "satisfaction"},
		{"[Untitled]", "untitled"},
	}
	for _, c := range cases {
		if got := CleanTitle(c.in); got != c.want {
			t.Errorf("CleanTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWords(t *testing.T) {
	got := Words("Butcher Pete, Pt. 1")
	if want := []string{"butcher", "pete", "part", "1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestCleanArtist(t *testing.T) {
	cases := map[string]string{
		"The Ink Spots":      "ink spots",
		"Roy Brown - Topic":  "roy brown",
		"PeterSchillingVEVO": "peterschilling",
		"Sigur Rós":          "sigur ros",
		"Simon & Garfunkel":  "simon and garfunkel",
	}
	for in, want := range cases {
		if got := CleanArtist(in); got != want {
			t.Errorf("CleanArtist(%q) = %q, want %q", in, got, want)
		}
	}
}
