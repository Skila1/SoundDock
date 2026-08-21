package search

import "testing"

func TestParse(t *testing.T) {
	q := Parse(`artist:"Linkin Park" title:Numb album:Meteora`)
	if q.Artist != "Linkin Park" || q.Title != "Numb" || q.Album != "Meteora" {
		t.Fatalf("%+v", q)
	}
	q = Parse("linkin park numb")
	if q.Text != "linkin park numb" {
		t.Fatalf("%q", q.Text)
	}
	q = Parse("numb meteora")
	if q.Text != "numb meteora" {
		t.Fatal(q.Text)
	}
}
