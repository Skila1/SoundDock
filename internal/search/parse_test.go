package search

import (
	"testing"
	"time"
)

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

func TestParsePlayFilters(t *testing.T) {
	q := Parse(`played:never artist:"Linkin Park"`)
	if !q.NeverPlayed || q.Artist != "Linkin Park" || q.Text != "" {
		t.Fatalf("never played: %+v", q)
	}
	q = Parse("numb played:yes")
	if !q.HasPlayed || q.Text != "numb" {
		t.Fatalf("has played: %+v", q)
	}
	q = Parse("never_played:true")
	if !q.NeverPlayed {
		t.Fatalf("never_played alias: %+v", q)
	}
	q = Parse("last_played:7d")
	if q.LastPlayedWithin != 7*24*time.Hour {
		t.Fatalf("within: %+v", q)
	}
	q = Parse("last_played:>30d")
	if q.LastPlayedBefore != 30*24*time.Hour {
		t.Fatalf("before: %+v", q)
	}
	q = Parse("last_played:2024-06-01")
	if q.LastPlayedAfter == nil || q.LastPlayedAfter.Format("2006-01-02") != "2024-06-01" {
		t.Fatalf("after date: %+v", q)
	}
	q = Parse(`title:Numb lastplayed:2w`)
	if q.Title != "Numb" || q.LastPlayedWithin != 14*24*time.Hour {
		t.Fatalf("lastplayed alias: %+v", q)
	}
}
