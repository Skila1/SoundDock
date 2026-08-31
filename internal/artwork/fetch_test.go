package artwork

import "testing"

func TestFetchYouTubeThumbRejectsBadID(t *testing.T) {
	if _, err := FetchYouTubeThumb(t.Context(), "not-a-video"); err == nil {
		t.Fatal("expected invalid id")
	}
	if _, err := FetchYouTubeThumb(t.Context(), ""); err == nil {
		t.Fatal("expected empty id")
	}
}
