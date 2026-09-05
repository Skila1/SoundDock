package metadata

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMatchConfidenceTitleArtistDuration(t *testing.T) {
	c := MatchConfidence("Numb", "Linkin Park", 187000, "Numb", "Linkin Park", 187500)
	if c < MinEnrichConfidence {
		t.Fatalf("high-confidence match scored %v", c)
	}
}

func TestMatchConfidenceLowOnWrongTitle(t *testing.T) {
	c := MatchConfidence("Numb", "Linkin Park", 187000, "In The End", "Linkin Park", 187000)
	if c >= MinEnrichConfidence {
		t.Fatalf("wrong title must be low confidence, got %v", c)
	}
}

func TestMatchConfidenceDurationMismatchCapped(t *testing.T) {
	c := MatchConfidence("Numb", "Linkin Park", 187000, "Numb", "Linkin Park", 400000)
	if c >= MinEnrichConfidence {
		t.Fatalf("duration mismatch must not pass, got %v", c)
	}
}

func TestApplyMusicBrainzLowConfidenceDoesNotOverwrite(t *testing.T) {
	p := Probe{Title: "Numb", Artist: "Linkin Park", Album: "Keep Me", Year: 2003, DurationMS: 187000, Source: "embedded", Confidence: 0.9}
	applyMusicBrainz(&p, map[string]any{
		"recordings": []any{
			map[string]any{
				"title":         "Completely Different",
				"length":        float64(30000),
				"score":         float64(20),
				"artist-credit": []any{map[string]any{"name": "Someone Else"}},
				"releases": []any{
					map[string]any{"id": "wrong-mbid", "title": "Wrong Album", "date": "1999-01-01"},
				},
			},
		},
	})
	if p.Album != "Keep Me" || p.Year != 2003 || p.MBID != "" || p.Source != "embedded" {
		t.Fatalf("low confidence overwrote probe: %+v", p)
	}
}

func TestApplyMusicBrainzHighConfidenceFillsMissing(t *testing.T) {
	p := Probe{Title: "Numb", Artist: "Linkin Park", DurationMS: 187000}
	applyMusicBrainz(&p, map[string]any{
		"recordings": []any{
			map[string]any{
				"title":         "Numb",
				"length":        float64(187650),
				"score":         float64(100),
				"artist-credit": []any{map[string]any{"name": "Linkin Park"}},
				"releases": []any{
					map[string]any{"id": "rel-1", "title": "Meteora", "date": "2003-03-25"},
				},
			},
		},
	})
	if p.MBID != "rel-1" || p.Album != "Meteora" || p.Year != 2003 || p.Source != "musicbrainz" {
		t.Fatalf("expected fill, got %+v", p)
	}
	if p.Confidence < MinEnrichConfidence {
		t.Fatalf("confidence %v", p.Confidence)
	}
}

func TestApplyMusicBrainzDoesNotReplaceExistingAlbum(t *testing.T) {
	p := Probe{Title: "Numb", Artist: "Linkin Park", Album: "Meteora Deluxe", Year: 2003, DurationMS: 187000}
	applyMusicBrainz(&p, map[string]any{
		"recordings": []any{
			map[string]any{
				"title":         "Numb",
				"length":        float64(187000),
				"score":         float64(100),
				"artist-credit": []any{map[string]any{"name": "Linkin Park"}},
				"releases": []any{
					map[string]any{"id": "rel-1", "title": "Meteora", "date": "1999-01-01"},
				},
			},
		},
	})
	if p.Album != "Meteora Deluxe" || p.Year != 2003 {
		t.Fatalf("must not overwrite existing tags: %+v", p)
	}
	if p.MBID != "rel-1" {
		t.Fatal("missing mbid fill")
	}
}

func TestEnrichCoverArtNoopWithoutMBID(t *testing.T) {
	p := Probe{Title: "Numb"}
	EnrichCoverArt(t.Context(), nil, &p)
	if len(p.Picture) != 0 {
		t.Fatal("must not fetch without pool/mbid")
	}
}

func TestPickGenresPrefersOfficial(t *testing.T) {
	got := pickGenres(map[string]any{
		"genres": []any{
			map[string]any{"name": "nu metal", "count": float64(12)},
			map[string]any{"name": "alternative rock", "count": float64(8)},
		},
		"tags": []any{
			map[string]any{"name": "seen live", "count": float64(99)},
			map[string]any{"name": "favorite", "count": float64(50)},
		},
	}, 5)
	if len(got) != 2 || got[0] != "Nu Metal" || got[1] != "Alternative Rock" {
		t.Fatalf("got %#v", got)
	}
}

func TestPickGenresSkipsJunkTags(t *testing.T) {
	got := pickGenres(map[string]any{
		"tags": []any{
			map[string]any{"name": "seen live", "count": float64(20)},
			map[string]any{"name": "hip hop", "count": float64(4)},
		},
	}, 5)
	if len(got) != 1 || got[0] != "Hip Hop" {
		t.Fatalf("got %#v", got)
	}
}

func TestParseArtistCreditsFeatured(t *testing.T) {
	credits := parseArtistCredits(map[string]any{
		"artist-credit": []any{
			map[string]any{
				"name":       "Kanye West",
				"joinphrase": " feat. ",
				"artist":     map[string]any{"id": "a1", "name": "Kanye West", "sort-name": "West, Kanye"},
			},
			map[string]any{
				"name":   "Jamie Foxx",
				"artist": map[string]any{"id": "a2", "name": "Jamie Foxx", "sort-name": "Foxx, Jamie"},
			},
		},
	})
	if len(credits) != 2 {
		t.Fatalf("len %d", len(credits))
	}
	if credits[0].Role != "primary" || credits[0].MBID != "a1" || credits[0].SortName != "West, Kanye" {
		t.Fatalf("primary %#v", credits[0])
	}
	if credits[1].Role != "featured" || credits[1].MBID != "a2" {
		t.Fatalf("featured %#v", credits[1])
	}
}

func TestApplyMusicBrainzFillsGenreAndArtistMapping(t *testing.T) {
	p := Probe{Title: "Numb", Artist: "Linkin Park", DurationMS: 187000}
	applyMusicBrainz(&p, map[string]any{
		"recordings": []any{
			map[string]any{
				"id":            "rec-1",
				"title":         "Numb",
				"length":        float64(187650),
				"score":         float64(100),
				"artist-credit": []any{map[string]any{"name": "Linkin Park", "artist": map[string]any{"id": "art-1", "name": "Linkin Park", "sort-name": "Linkin Park"}}},
				"genres":        []any{map[string]any{"name": "nu metal", "count": float64(10)}},
				"releases":      []any{map[string]any{"id": "rel-1", "title": "Meteora", "date": "2003-03-25"}},
			},
		},
	})
	if p.MBID != "rel-1" || p.RecordingMBID != "rec-1" {
		t.Fatalf("mbids %+v", p)
	}
	if p.Genre != "Nu Metal" || len(p.Genres) != 1 {
		t.Fatalf("genre %q %#v", p.Genre, p.Genres)
	}
	if p.ArtistMBID != "art-1" {
		t.Fatalf("artist mbid %q", p.ArtistMBID)
	}
}

func TestApplyMusicBrainzDoesNotReplaceExistingGenre(t *testing.T) {
	p := Probe{Title: "Numb", Artist: "Linkin Park", Genre: "Rock", DurationMS: 187000}
	applyMusicBrainz(&p, map[string]any{
		"recordings": []any{
			map[string]any{
				"id":            "rec-1",
				"title":         "Numb",
				"length":        float64(187000),
				"score":         float64(100),
				"artist-credit": []any{map[string]any{"name": "Linkin Park"}},
				"genres":        []any{map[string]any{"name": "nu metal", "count": float64(10)}},
				"releases":      []any{map[string]any{"id": "rel-1", "title": "Meteora", "date": "2003-03-25"}},
			},
		},
	})
	if p.Genre != "Rock" {
		t.Fatalf("overwrote genre: %q", p.Genre)
	}
	if len(p.Genres) != 1 || p.Genres[0] != "Nu Metal" {
		t.Fatalf("expected extra genre mapping, got %#v", p.Genres)
	}
}

func TestEnrichMusicBrainzForcedLooksUpWithoutPool(t *testing.T) {
	prevAPI, prevDelay := mbAPI, mbDelay
	t.Cleanup(func() { mbAPI = prevAPI; mbDelay = prevDelay })
	mbDelay = 0
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/recording/") && !strings.Contains(r.URL.RawQuery, "query=") {
			_, _ = io.WriteString(w, `{"id":"rec-1","title":"Numb","genres":[{"name":"rock","count":4}],"artist-credit":[{"name":"Linkin Park","artist":{"id":"a1","name":"Linkin Park"}}],"releases":[{"id":"rel-1","title":"Meteora","date":"2003-03-25"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"recordings":[{"id":"rec-1","title":"Numb","length":187000,"score":100,"artist-credit":[{"name":"Linkin Park"}],"releases":[{"id":"rel-1","title":"Meteora","date":"2003-03-25"}]}]}`)
	}))
	defer srv.Close()
	mbAPI = srv.URL
	p := Probe{Title: "Numb", Artist: "Linkin Park", DurationMS: 187000}
	EnrichMusicBrainzForced(t.Context(), &p)
	if p.MBID != "rel-1" || p.Genre != "Rock" {
		t.Fatalf("forced enrich: %+v hits=%d", p, hits)
	}
}

func TestGenreList(t *testing.T) {
	if got := GenreList(Probe{Genres: []string{"Rock", "Pop"}}); len(got) != 2 {
		t.Fatalf("%#v", got)
	}
	if got := GenreList(Probe{Genre: "Jazz"}); len(got) != 1 || got[0] != "Jazz" {
		t.Fatalf("%#v", got)
	}
}
