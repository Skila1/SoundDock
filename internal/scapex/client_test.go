package scapex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestWatchURLAndVideoID(t *testing.T) {
	if VideoID("kXYiU_JCYtU") != "kXYiU_JCYtU" {
		t.Fatal("bare id")
	}
	if VideoID("https://youtu.be/kXYiU_JCYtU") != "kXYiU_JCYtU" {
		t.Fatal("youtu.be")
	}
	if VideoID("https://www.youtube.com/watch?v=kXYiU_JCYtU&list=x") != "kXYiU_JCYtU" {
		t.Fatal("watch")
	}
	if WatchURL("kXYiU_JCYtU") != "https://www.youtube.com/watch?v=kXYiU_JCYtU" {
		t.Fatal("watch url")
	}
	if IsYouTubeURL("https://files.example/a.flac") {
		t.Fatal("direct file")
	}
}

func TestSearchTargetLooksUpWatchURLs(t *testing.T) {
	lookup, spec := searchTarget("https://youtu.be/kXYiU_JCYtU", 8)
	if !lookup || spec != "https://www.youtube.com/watch?v=kXYiU_JCYtU" {
		t.Fatalf("lookup=%v spec=%q", lookup, spec)
	}
	lookup, spec = searchTarget("kXYiU_JCYtU", 8)
	if !lookup || spec != "https://www.youtube.com/watch?v=kXYiU_JCYtU" {
		t.Fatalf("id lookup=%v spec=%q", lookup, spec)
	}
	lookup, spec = searchTarget("numb", 8)
	if lookup || spec != "ytsearch8:numb" {
		t.Fatalf("search=%v spec=%q", lookup, spec)
	}
	lookup, spec = searchTarget("https://www.youtube.com/playlist?list=PLabcDEF123", 8)
	if lookup || spec != "https://www.youtube.com/playlist?list=PLabcDEF123" {
		t.Fatalf("playlist lookup=%v spec=%q", lookup, spec)
	}
}

func TestPlaylistQuery(t *testing.T) {
	if !IsPlaylistQuery("https://www.youtube.com/playlist?list=PLabcDEF123") {
		t.Fatal("canonical playlist")
	}
	if !IsPlaylistQuery("https://music.youtube.com/playlist?list=OLAK5uy_abc") {
		t.Fatal("music album playlist")
	}
	if !IsPlaylistQuery("https://www.youtube.com/watch?v=kXYiU_JCYtU&list=PLabcDEF123") {
		t.Fatal("watch with user playlist")
	}
	if IsPlaylistQuery("https://www.youtube.com/watch?v=kXYiU_JCYtU&list=RDxyz") {
		t.Fatal("mix on a watch URL is a single video")
	}
	if IsPlaylistQuery("https://www.youtube.com/watch?v=kXYiU_JCYtU") {
		t.Fatal("plain watch")
	}
	if PlaylistID("https://www.youtube.com/playlist?list=WL") != "" {
		t.Fatal("watch later")
	}
}

func TestParsePlaylistDump(t *testing.T) {
	raw := []byte(`{"id":"PLabc","title":"Gym","playlist_count":3,"entries":[
		{"id":"kXYiU_JCYtU","title":"Numb","uploader":"LP","duration":187},
		null,
		{"id":"xxxxxxxxxxx","title":"[Deleted video]"},
		{"id":"abcdefghijk","title":"In The End","channel":"LP","duration":216}
	]}`)
	got := parsePlaylistDump(raw)
	if got.Title != "Gym" || got.ID != "PLabc" || len(got.Hits) != 2 {
		t.Fatalf("%+v", got)
	}
	if got.Hits[0].ID != "kXYiU_JCYtU" || got.Hits[1].Title != "In The End" {
		t.Fatalf("%+v", got.Hits)
	}
	if got.Total != 3 {
		t.Fatalf("total %d", got.Total)
	}
}

func TestParseInfoHit(t *testing.T) {
	raw := []byte(`{"id":"kXYiU_JCYtU","title":"Numb","track":"Numb","artist":"Linkin Park","duration":187,"webpage_url":"https://www.youtube.com/watch?v=kXYiU_JCYtU"}`)
	hit, ok := parseInfoHit(raw)
	if !ok || hit.ID != "kXYiU_JCYtU" || hit.Title != "Numb" || hit.Artist != "Linkin Park" || hit.DurationMS != 187000 {
		t.Fatalf("%+v ok=%v", hit, ok)
	}
}

func TestCollectDownloads(t *testing.T) {
	inbox := t.TempDir()
	if err := os.WriteFile(filepath.Join(inbox, "other.m4a"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(inbox, "job")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vid123.m4a"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	info := `{"id":"vid123","title":"Numb","artist":"Linkin Park","duration":187,"webpage_url":"https://www.youtube.com/watch?v=vid123"}`
	if err := os.WriteFile(filepath.Join(dir, "vid123.info.json"), []byte(info), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := collectDownloads(dir)
	if err != nil || len(got) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	if got[0].Title != "Numb" || got[0].VideoID != "vid123" {
		t.Fatalf("%+v", got[0])
	}
}

func TestCollectDownloadsGlobsExpectedID(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vid123.m4a"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "otherid.m4a"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	info := `{"id":"vid123","title":"Numb","artist":"Linkin Park","duration":187,"webpage_url":"https://www.youtube.com/watch?v=vid123"}`
	if err := os.WriteFile(filepath.Join(dir, "vid123.info.json"), []byte(info), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := collectDownloads(dir, "vid123")
	if err != nil || len(got) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	if got[0].VideoID != "vid123" || got[0].Title != "Numb" {
		t.Fatalf("%+v", got[0])
	}
}

type recYT struct {
	q     string
	limit int
	hits  []Hit
}

func (s *recYT) Search(_ context.Context, q string, limit int) ([]Hit, error) {
	s.q = q
	s.limit = limit
	return s.hits, nil
}
func (s *recYT) Fetch(context.Context, string, string) ([]LocalTrack, error) { return nil, nil }

func TestSearchLooksUpVideoURLs(t *testing.T) {
	yt := &recYT{hits: []Hit{{ID: "kXYiU_JCYtU", Title: "Numb"}}}
	svc := NewService(yt, nil)
	hits, err := svc.Search(context.Background(), "https://youtu.be/kXYiU_JCYtU", 8)
	if err != nil || len(hits) != 1 {
		t.Fatalf("%v %+v", err, hits)
	}
	if yt.limit != 1 || yt.q != "https://youtu.be/kXYiU_JCYtU" {
		t.Fatalf("q=%q limit=%d", yt.q, yt.limit)
	}
}

type listYT struct {
	recYT
	listing PlaylistListing
}

func (s *listYT) ListPlaylist(_ context.Context, _ string, limit int) (PlaylistListing, error) {
	s.limit = limit
	return s.listing, nil
}

func TestSearchPlaylistKeepsOrderAndLimit(t *testing.T) {
	yt := &listYT{
		listing: PlaylistListing{
			ID:    "PLabc",
			Title: "Gym",
			Hits:  []Hit{{ID: "aaaaaaaaaaa", Title: "One"}, {ID: "bbbbbbbbbbb", Title: "Two"}},
			Total: 2,
		},
	}
	svc := NewService(yt, nil)
	q := "https://www.youtube.com/playlist?list=PLabc"
	listing, err := svc.ListPlaylist(context.Background(), q, 50)
	if err != nil || listing.Title != "Gym" || len(listing.Hits) != 2 {
		t.Fatalf("%v %+v", err, listing)
	}
	if listing.Hits[0].Title != "One" || listing.Hits[1].Title != "Two" {
		t.Fatalf("order %+v", listing.Hits)
	}
	if yt.limit != 50 {
		t.Fatalf("limit %d", yt.limit)
	}
	hits, err := svc.Search(context.Background(), q, 50)
	if err != nil || len(hits) != 2 || hits[0].ArtworkURL == "" {
		t.Fatalf("%v %+v", err, hits)
	}
}

type stubYT struct {
	hits   []Hit
	tracks []LocalTrack
}

func (s stubYT) Search(context.Context, string, int) ([]Hit, error) { return s.hits, nil }
func (s stubYT) Fetch(context.Context, string, string) ([]LocalTrack, error) {
	return s.tracks, nil
}

func TestSearchHandler(t *testing.T) {
	svc := NewService(stubYT{hits: []Hit{{ID: "abc", Title: "Numb", Artist: "LP"}}}, nil)
	srv := httptest.NewServer(Handler(svc))
	t.Cleanup(srv.Close)
	res, err := http.Get(srv.URL + "/search?q=numb")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	var out struct {
		Results []Hit `json:"results"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Results) != 1 || out.Results[0].Type != "youtube" || out.Results[0].ArtworkURL == "" {
		t.Fatalf("%+v", out.Results)
	}
}
