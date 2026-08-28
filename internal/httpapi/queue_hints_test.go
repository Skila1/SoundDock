package httpapi

import (
	"testing"

	"github.com/sounddock/sounddock/internal/scapex"
)

func TestCollectTrackHintsKeepsSearchMetadata(t *testing.T) {
	ids, hints := collectTrackHints([]string{"0B5I_nPxxxx"}, []queueTrackHint{
		{ID: "0B5I_nPxxxx", Title: "She's On Fire", Artist: "Amy Holland", DurationMS: 210000},
	})
	if len(ids) != 1 || ids[0] != "0B5I_nPxxxx" {
		t.Fatalf("ids %v", ids)
	}
	h := hints["0B5I_nPxxxx"]
	if h.Title != "She's On Fire" || h.Artist != "Amy Holland" || h.DurationMS != 210000 {
		t.Fatalf("%+v", h)
	}
	if hints[scapex.CanonicalSourceRef("0B5I_nPxxxx")].Title != "She's On Fire" {
		t.Fatal("hint not keyed by canonical ref")
	}
}
