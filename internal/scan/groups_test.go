package scan

import (
	"testing"

	"github.com/google/uuid"
)

func TestNormalizeBlockingPart(t *testing.T) {
	if got := NormalizeBlockingPart("  The  Beatles "); got != "the beatles" {
		t.Fatalf("%q", got)
	}
	if ArtistTitleBlockingKey("Radiohead", "Karma Police") != "radiohead\tkarma police" {
		t.Fatal(ArtistTitleBlockingKey("Radiohead", "Karma Police"))
	}
}

func TestSkipUnknownPlaceholders(t *testing.T) {
	if !skipArtistTitleBlock("Unknown Artist", "Numb") {
		t.Fatal("unknown artist")
	}
	if !skipArtistTitleBlock("Linkin Park", "Unknown Title") {
		t.Fatal("unknown title")
	}
	if skipArtistTitleBlock("Linkin Park", "Numb") {
		t.Fatal("real metadata")
	}
}

func TestClusterByDurationNotOneRowPerPair(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	tracks := []timedTrack{
		{ID: a, DurationMS: 180_000},
		{ID: b, DurationMS: 181_500},
		{ID: c, DurationMS: 182_900},
	}
	clusters := ClusterByDuration(tracks, DurationWindowMS)
	if len(clusters) != 1 {
		t.Fatalf("clusters=%d want 1 (not pairwise)", len(clusters))
	}
	if len(clusters[0]) != 3 {
		t.Fatalf("members=%d want 3", len(clusters[0]))
	}

	far := []timedTrack{
		{ID: a, DurationMS: 60_000},
		{ID: b, DurationMS: 180_000},
		{ID: c, DurationMS: 300_000},
	}
	split := ClusterByDuration(far, DurationWindowMS)
	if len(split) != 3 {
		t.Fatalf("far clusters=%d want 3 singles", len(split))
	}
	for i, cl := range split {
		if len(cl) != 1 {
			t.Fatalf("cluster %d size %d", i, len(cl))
		}
	}
}

func TestUniqueSortedUUIDs(t *testing.T) {
	a, b := uuid.MustParse("00000000-0000-4000-8000-000000000001"), uuid.MustParse("00000000-0000-4000-8000-000000000002")
	got := uniqueSortedUUIDs([]uuid.UUID{b, a, a, uuid.Nil})
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("%v", got)
	}
}
