package ingest

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/jobs"
)

func TestURLHandlerRejectsPlaylistURLs(t *testing.T) {
	h := (&Service{}).URLHandler(nil)
	err := h(context.Background(), jobs.Job{Payload: []byte(`{"url":"https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M"}`)})
	if err == nil || !strings.Contains(err.Error(), "External Playlist Import") {
		t.Fatalf("got %v", err)
	}
}

func TestCreateUploadRejectsNonAudio(t *testing.T) {
	s := New(nil, nil, nil, t.TempDir(), 10)
	_, _, err := s.CreateUpload(context.Background(), uuid.New(), uuid.New(), "cover.jpg", 12)
	if err == nil {
		t.Fatal("expected reject")
	}
}
