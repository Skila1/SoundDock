package external

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

func TestSpotifyItemCountPrefersItemsTotal(t *testing.T) {
	tracks := json.RawMessage(`{"href":"https://example/tracks","total":0}`)
	items := json.RawMessage(`{"href":"https://example/items","total":42}`)
	if n := spotifyItemCount(tracks, items); n != 42 {
		t.Fatalf("got %d", n)
	}
}

func TestSpotifyItemCountTracksFallback(t *testing.T) {
	tracks := json.RawMessage(`{"total":17}`)
	if n := spotifyItemCount(tracks, nil); n != 17 {
		t.Fatalf("got %d", n)
	}
}

func TestTrackFromSpotifyPageItemSupportsItemAndTrack(t *testing.T) {
	rawTrack := json.RawMessage(`{"track":{"id":"t1","name":"Brown","duration_ms":180000,"artists":[{"name":"Artist"}],"album":{"name":"LP","images":[{"url":"https://img"}]},"external_urls":{"spotify":"https://open.spotify.com/track/t1"},"external_ids":{"isrc":"USABC"}}}`)
	tr, ok := trackFromSpotifyPageItem(rawTrack)
	if !ok || tr.ID != "t1" || tr.Title != "Brown" || tr.ISRC != "USABC" || tr.DurationMS != 180000 {
		t.Fatalf("track wrap %+v ok=%v", tr, ok)
	}
	rawItem := json.RawMessage(`{"item":{"id":"t2","name":"Go","type":"track","duration_ms":90,"artists":[{"name":"B"}]}}`)
	tr, ok = trackFromSpotifyPageItem(rawItem)
	if !ok || tr.ID != "t2" || tr.Title != "Go" {
		t.Fatalf("item wrap %+v ok=%v", tr, ok)
	}
}

func TestTrackFromSpotifyPageItemSkipsEpisodes(t *testing.T) {
	raw := json.RawMessage(`{"item":{"id":"e1","name":"Talk","type":"episode"}}`)
	if _, ok := trackFromSpotifyPageItem(raw); ok {
		t.Fatal("episode should be skipped")
	}
}

func withSpotifyAPI(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	orig := spotifyAPIBase
	spotifyAPIBase = srv.URL
	t.Cleanup(func() { spotifyAPIBase = orig })
}

func sampleItemsPage() []byte {
	return []byte(`{"items":[{"track":{"id":"t1","name":"Brown","type":"track","duration_ms":180000,"artists":[{"name":"A"}]}}],"next":""}`)
}

func TestFetchSpotifyItemsFallback404Only(t *testing.T) {
	var itemsHits, tracksHits atomic.Int32
	withSpotifyAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/items"):
			itemsHits.Add(1)
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"status":404}}`))
		case strings.Contains(r.URL.Path, "/tracks"):
			tracksHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(sampleItemsPage())
		default:
			http.NotFound(w, r)
		}
	})
	tracks, err := fetchSpotifyPlaylistItems(context.Background(), &accessToken{value: "tok"}, "pl1")
	if err != nil {
		t.Fatal(err)
	}
	if itemsHits.Load() != 1 || tracksHits.Load() != 1 {
		t.Fatalf("items=%d tracks=%d", itemsHits.Load(), tracksHits.Load())
	}
	if len(tracks) != 1 || tracks[0].ID != "t1" {
		t.Fatalf("%+v", tracks)
	}
}

func TestFetchSpotifyItemsNoFallbackOn403(t *testing.T) {
	testNoFallbackStatus(t, http.StatusForbidden)
}

func TestFetchSpotifyItemsNoFallbackOn401(t *testing.T) {
	testNoFallbackStatus(t, http.StatusUnauthorized)
}

func TestFetchSpotifyItemsNoFallbackOn429(t *testing.T) {
	testNoFallbackStatus(t, http.StatusTooManyRequests)
}

func TestFetchSpotifyItemsNoFallbackOn5xx(t *testing.T) {
	testNoFallbackStatus(t, http.StatusInternalServerError)
}

func testNoFallbackStatus(t *testing.T, status int) {
	t.Helper()
	var tracksHits atomic.Int32
	withSpotifyAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/tracks") && !strings.Contains(r.URL.Path, "/items") {
			tracksHits.Add(1)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":{"status":`+http.StatusText(status)+`}}`)
	})
	_, err := fetchSpotifyPlaylistItems(context.Background(), &accessToken{value: "tok"}, "pl1")
	if err == nil {
		t.Fatal("expected error")
	}
	if httpStatus(err) != status {
		t.Fatalf("status %d want %d err=%v", httpStatus(err), status, err)
	}
	if tracksHits.Load() != 0 {
		t.Fatalf("tracks fallback hit on %d", status)
	}
}

func TestFetchSpotifyItems401RetriesOnce(t *testing.T) {
	var itemsHits, refreshes atomic.Int32
	withSpotifyAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/items") {
			http.NotFound(w, r)
			return
		}
		n := itemsHits.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"status":401}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(sampleItemsPage())
	})
	tok := &accessToken{
		value: "stale",
		refresh: func(context.Context) (string, error) {
			refreshes.Add(1)
			return "fresh", nil
		},
	}
	tracks, err := fetchSpotifyPlaylistItems(context.Background(), tok, "pl1")
	if err != nil {
		t.Fatal(err)
	}
	if itemsHits.Load() != 2 || refreshes.Load() != 1 {
		t.Fatalf("items=%d refreshes=%d", itemsHits.Load(), refreshes.Load())
	}
	if tok.value != "fresh" || len(tracks) != 1 {
		t.Fatalf("tok=%s tracks=%+v", tok.value, tracks)
	}
	_, err = fetchSpotifyPlaylistItems(context.Background(), tok, "pl1")
	if err != nil {
		t.Fatal(err)
	}
	if refreshes.Load() != 1 {
		t.Fatalf("refresh must run once, got %d", refreshes.Load())
	}
}

func TestGetPlaylistItemsKeepsSnapshotOnMetadata403(t *testing.T) {
	withSpotifyAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/playlists/pl1") {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"status":403}}`))
			return
		}
		if strings.Contains(r.URL.Path, "/items") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(sampleItemsPage())
			return
		}
		http.NotFound(w, r)
	})
	meta, tracks, err := GetPlaylistItems(context.Background(), "spotify", "tok", "", "pl1")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Snapshot != "" {
		t.Fatalf("403 metadata must not invent a snapshot: %q", meta.Snapshot)
	}
	if retainedSnapshot("snap-keep", meta.Snapshot) != "snap-keep" {
		t.Fatal("previous snapshot must be kept")
	}
	if len(tracks) != 1 {
		t.Fatalf("items still load: %d", len(tracks))
	}
}

func TestFillStubNotInKeepIDs(t *testing.T) {
	stub := uuid.New()
	var keep []uuid.UUID
	if id, ok := keepMembership(stub, false); ok {
		keep = append(keep, id)
	}
	if len(keep) != 0 {
		t.Fatalf("stub must not be membership: %v", keep)
	}
	if id, ok := keepMembership(stub, true); !ok || id != stub {
		t.Fatal("playable track is membership")
	}
}

func TestImportCoalesceKeyPerPlaylist(t *testing.T) {
	u := uuid.New()
	a := ImportPayload{UserID: u, Provider: "spotify", ExternalID: "pl1"}.CoalesceKey()
	b := ImportPayload{UserID: u, Provider: "spotify", ExternalID: "pl1"}.CoalesceKey()
	c := ImportPayload{UserID: u, Provider: "spotify", ExternalID: "pl2"}.CoalesceKey()
	d := ImportPayload{UserID: uuid.New(), Provider: "spotify", ExternalID: "pl1"}.CoalesceKey()
	if a == "" || a != b {
		t.Fatalf("same playlist must coalesce: %q %q", a, b)
	}
	if a == c || a == d {
		t.Fatalf("different playlist or user must not share key: %q %q %q", a, c, d)
	}
}
