package matcher

import "testing"

func TestNormaliseTitle(t *testing.T) {
	if got := NormaliseTitle("Numb (feat. Jay-Z)"); got != "numb" {
		t.Fatalf("got %q", got)
	}
	if NormaliseTitle("NUMB") != NormaliseTitle("numb") {
		t.Fatal("case")
	}
}

func TestMatchEmptyLibs(t *testing.T) {
	r := Match(nil, nil, nil, Query{Title: "Numb", ISRC: "US123"})
	if r.Status != "unmatched" {
		t.Fatalf("%+v", r)
	}
}
