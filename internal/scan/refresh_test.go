package scan

import (
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/jobs"
)

func TestRefreshAllNilScanner(t *testing.T) {
	s := &Scanner{}
	if err := s.RefreshAll(t.Context(), nil, uuid.Nil); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshHandlerRegisteredName(t *testing.T) {
	if JobRefresh != "metadata.refresh" {
		t.Fatalf("%s", JobRefresh)
	}
	if jobs.PoolForType(JobRefresh) != jobs.PoolMaintenance {
		t.Fatal("refresh must run on the maintenance pool")
	}
}

func TestHasArtworkNilPool(t *testing.T) {
	s := &Scanner{}
	if s.hasArtwork(t.Context(), refreshRow{TrackID: uuid.New()}) {
		t.Fatal("nil pool")
	}
}
