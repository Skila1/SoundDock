package metadata

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestMusicBrainzSearchURLIncludesTitle(t *testing.T) {
	u := musicBrainzSearchURL("Linkin Park", "Meteora", "Numb", 187000)
	if !strings.Contains(u, "/ws/2/recording/") {
		t.Fatalf("expected recording search, got %s", u)
	}
	q, err := url.Parse(u)
	if err != nil {
		t.Fatal(err)
	}
	query := q.Query().Get("query")
	if !strings.Contains(query, `recording:"Numb"`) {
		t.Fatalf("title missing from query: %s", query)
	}
	if !strings.Contains(query, `artist:"Linkin Park"`) {
		t.Fatalf("artist missing: %s", query)
	}
	if !strings.Contains(query, "dur:[") {
		t.Fatalf("duration window missing: %s", query)
	}
}

func TestMusicBrainzSearchURLWithoutTitleUsesRelease(t *testing.T) {
	u := musicBrainzSearchURL("Linkin Park", "Meteora", "", 0)
	if !strings.Contains(u, "/ws/2/release/") {
		t.Fatalf("expected release fallback, got %s", u)
	}
	if strings.Contains(u, "recording:") {
		t.Fatal("must not search recordings without a title")
	}
}

func TestMusicBrainzLookupSendsTitle(t *testing.T) {
	prevAPI, prevDelay := mbAPI, mbDelay
	t.Cleanup(func() { mbAPI = prevAPI; mbDelay = prevDelay })
	mbDelay = 0
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"recordings":[]}`)
	}))
	defer srv.Close()
	mbAPI = srv.URL
	_, err := (MusicBrainz{DurationMS: 187000}).Lookup(t.Context(), "Linkin Park", "Meteora", "Numb")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotQuery, `recording:"Numb"`) {
		t.Fatalf("lookup ignored title: %s", gotQuery)
	}
}

func TestMusicBrainzLookupRecordingIncludesGenres(t *testing.T) {
	prevAPI, prevDelay := mbAPI, mbDelay
	t.Cleanup(func() { mbAPI = prevAPI; mbDelay = prevDelay })
	mbDelay = 0
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"rec-1","title":"Numb","genres":[{"name":"rock","count":3}]}`)
	}))
	defer srv.Close()
	mbAPI = srv.URL
	raw, err := (MusicBrainz{}).LookupRecording(t.Context(), "rec-1")
	if err != nil {
		t.Fatal(err)
	}
	if raw["id"] != "rec-1" {
		t.Fatalf("%v", raw)
	}
	if !strings.Contains(gotPath, "/ws/2/recording/rec-1") || !strings.Contains(gotPath, "inc=genres") {
		t.Fatalf("lookup path %s", gotPath)
	}
}

func TestCoverArtFrontURLPrefersFront(t *testing.T) {
	raw := map[string]any{
		"images": []any{
			map[string]any{"front": false, "image": "https://example/back.jpg"},
			map[string]any{"front": true, "image": "https://example/front.jpg"},
		},
	}
	if u := coverArtFrontURL(raw); u != "https://example/front.jpg" {
		t.Fatalf("got %s", u)
	}
}

func TestCoverArtFetchFrontDownloadsImage(t *testing.T) {
	prevAPI := caaAPI
	t.Cleanup(func() { caaAPI = prevAPI })
	const blob = "fake-jpeg-bytes"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/release/") && !strings.HasSuffix(r.URL.Path, "/front"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"images":[{"front":true,"image":"`+srvURL(r)+`/front.jpg"}]}`)
		case strings.HasSuffix(r.URL.Path, "/front.jpg"), strings.HasSuffix(r.URL.Path, "/front"):
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = io.WriteString(w, blob)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	caaAPI = srv.URL
	got, err := (CoverArt{}).FetchFront(t.Context(), "mbid-1")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != blob {
		t.Fatalf("got %q", got)
	}
}

func srvURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
