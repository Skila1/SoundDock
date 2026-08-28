package metadata

import "testing"

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
