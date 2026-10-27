package external

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/scapex"
)

func TestPickYouTubeIDRequiresTitleAndArtist(t *testing.T) {
	hits := []scapex.Hit{
		{ID: "majortomxxx", Title: "Peter Schilling - Major Tom (Coming Home) Official Video", Artist: "Peter Schilling"},
		{ID: "randomxxxx1", Title: "Workout mix 2024", Artist: "Various"},
	}
	got, _ := pickYouTubeID(hits, "Major Tom (Coming Home)", []string{"Peter Schilling"}, 0)
	if got != "majortomxxx" {
		t.Fatalf("got %q", got)
	}
	if id, _ := pickYouTubeID(hits, "dominga la mave", []string{"Someone"}, 0); id != "" {
		t.Fatal("unrelated query should not pick a hit")
	}
}

func TestPickYouTubeIDFalloutTracks(t *testing.T) {
	cases := []struct {
		title   string
		artists []string
		dur     int
		hits    []scapex.Hit
		want    string
	}{
		{
			"Butcher Pete, Pt. 1", []string{"Roy Brown"}, 160000,
			[]scapex.Hit{
				{ID: "cover", Title: "Butcher Pete Part 1 (cover)", Artist: "Some Band", DurationMS: 161000},
				{ID: "orig", Title: "Roy Brown - Butcher Pete (Part 1)", Artist: "Fallout Radio", DurationMS: 158000},
			},
			"orig",
		},
		{
			"Civilization (Bongo, Bongo, Bongo)", []string{"Danny Kaye", "The Andrews Sisters"}, 180000,
			[]scapex.Hit{{ID: "civ", Title: "Civilization (Bongo, Bongo, Bongo)", Artist: "The Andrews Sisters - Topic", DurationMS: 181000}},
			"civ",
		},
		{
			"I Don't Want to Set the World on Fire", []string{"The Ink Spots"}, 182000,
			[]scapex.Hit{
				{ID: "live", Title: "Ink Spots - I Dont Want To Set The World On Fire LIVE", Artist: "x", DurationMS: 200000},
				{ID: "ink", Title: "The Ink Spots - I Don't Want to Set the World on Fire", Artist: "InkSpotsVEVO", DurationMS: 183000},
			},
			"ink",
		},
		{
			"Crazy He Calls Me - Single Version", []string{"Billie Holiday"}, 190000,
			[]scapex.Hit{{ID: "bh", Title: "Crazy He Calls Me", Artist: "Billie Holiday - Topic", DurationMS: 189000}},
			"bh",
		},
		{
			"Way Back Home - feat. Conor Maynard (Sam Feldt Edit)", []string{"SHAUN", "Conor Maynard"}, 0,
			[]scapex.Hit{
				{ID: "sped", Title: "SHAUN - Way Back Home (sped up)", Artist: "Speed Songs"},
				{ID: "wbh", Title: "SHAUN feat. Conor Maynard - Way Back Home (Sam Feldt Edit)", Artist: "Spinnin' Records"},
			},
			"wbh",
		},
		{
			"Atom Bomb Baby", []string{"The Five Stars"}, 150000,
			[]scapex.Hit{{ID: "abb", Title: "Atom Bomb Baby - Fallout 4", Artist: "Vault Tunes", DurationMS: 151000}},
			"abb",
		},
	}
	for _, c := range cases {
		if got, _ := pickYouTubeID(c.hits, c.title, c.artists, c.dur); got != c.want {
			t.Errorf("%q: got %q, want %q", c.title, got, c.want)
		}
	}
}

func TestYouTubeQueries(t *testing.T) {
	got := youtubeQueries("Crazy He Calls Me - Single Version", []string{"Billie Holiday", "X"}, "Lady Day")
	want := []string{"Billie Holiday - Crazy He Calls Me", "Crazy He Calls Me Billie Holiday", "Crazy He Calls Me Lady Day"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}

type fakeFiller struct {
	queries []string
	errs    []error
	hits    map[string][]scapex.Hit
	fetched []string
}

func (f *fakeFiller) Search(_ context.Context, q string, _ int) ([]scapex.Hit, error) {
	f.queries = append(f.queries, q)
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return nil, err
		}
	}
	return f.hits[q], nil
}

func (f *fakeFiller) Fetch(_ context.Context, refs []string) ([]uuid.UUID, error) {
	f.fetched = append(f.fetched, refs...)
	return []uuid.UUID{uuid.New()}, nil
}

func TestFillFromYouTubeLadderAndRetry(t *testing.T) {
	f := &fakeFiller{
		errs: []error{context.DeadlineExceeded, nil, nil},
		hits: map[string][]scapex.Hit{
			"Atom Bomb Baby The Five Stars": {{ID: "abb", Title: "The Five Stars - Atom Bomb Baby", DurationMS: 150000}},
		},
	}
	id, err := fillFromYouTube(context.Background(), f, "Atom Bomb Baby", []string{"The Five Stars"}, "", 150000)
	if err != nil || id == uuid.Nil {
		t.Fatalf("id=%v err=%v", id, err)
	}
	want := []string{"The Five Stars - Atom Bomb Baby", "The Five Stars - Atom Bomb Baby", "Atom Bomb Baby The Five Stars"}
	if !reflect.DeepEqual(f.queries, want) || !reflect.DeepEqual(f.fetched, []string{"abb"}) {
		t.Fatalf("queries=%q fetched=%q", f.queries, f.fetched)
	}
}

func TestFillFromYouTubeReportsSearchErrors(t *testing.T) {
	boom := errors.New("scapex down")
	f := &fakeFiller{errs: []error{boom, boom, boom}}
	if _, err := fillFromYouTube(context.Background(), f, "Song", []string{"A"}, "", 0); !errors.Is(err, boom) {
		t.Fatalf("err=%v", err)
	}
}

type anySearcher struct {
	hits  []scapex.Hit
	calls int
}

func (a *anySearcher) Search(_ context.Context, _ string, _ int) ([]scapex.Hit, error) {
	a.calls++
	return a.hits, nil
}

// Spotify titles carry " - <version>" suffixes and accents that YouTube
// uploads spell differently; these used to be skipped when queueing a playlist.
func TestFindYouTubeSpotifyTitles(t *testing.T) {
	decoy := scapex.Hit{ID: "decoy", Title: "Workout Mix 2026 Nonstop", Artist: "Gym Hits"}
	cases := []struct {
		title   string
		artists []string
		hit     scapex.Hit
	}{
		{"Say My Name - Hard Edit", []string{"ALYSSA", "Colin Hennerz"}, scapex.Hit{ID: "saymyname", Title: "ALYSSA & Colin Hennerz - Say My Name (Hard Edit)", Artist: "Hardstyle Vibes"}},
		{"Don't Stop Me Now - Remastered 2011", []string{"Queen"}, scapex.Hit{ID: "queen", Title: "Queen - Don't Stop Me Now (Official Video)", Artist: "Queen Official"}},
		{"Café", []string{"Artist"}, scapex.Hit{ID: "cafe", Title: "Artist - Cafe (Official Audio)", Artist: "Artist"}},
		{"Bad Habits - Acoustic Version", []string{"Ed Sheeran"}, scapex.Hit{ID: "habits", Title: "Ed Sheeran - Bad Habits [Acoustic Version]", Artist: "Ed Sheeran"}},
		{"Lose Yourself - From \"8 Mile\" Soundtrack", []string{"Eminem"}, scapex.Hit{ID: "lose", Title: "Eminem - Lose Yourself [HD]", Artist: "EminemMusic"}},
		{"Dönmüyor Giden", []string{"Tarkan"}, scapex.Hit{ID: "tarkan", Title: "TARKAN – Dönmüyor Giden (Official Visualiser)", Artist: "Tarkan"}},
	}
	for _, c := range cases {
		s := &anySearcher{hits: []scapex.Hit{decoy, c.hit}}
		h, ok, err := FindYouTube(context.Background(), s, c.title, c.artists, "", 0)
		if err != nil || !ok || h.ID != c.hit.ID {
			t.Errorf("%q: got %q ok=%v err=%v", c.title, h.ID, ok, err)
		}
	}
}

func TestFindYouTubeTriesNextQuery(t *testing.T) {
	f := &fakeFiller{hits: map[string][]scapex.Hit{
		"Atom Bomb Baby The Five Stars": {{ID: "abb", Title: "The Five Stars - Atom Bomb Baby"}},
	}}
	h, ok, err := FindYouTube(context.Background(), f, "Atom Bomb Baby", []string{"The Five Stars"}, "", 0)
	if err != nil || !ok || h.ID != "abb" || len(f.fetched) != 0 {
		t.Fatalf("hit=%q ok=%v err=%v fetched=%q", h.ID, ok, err, f.fetched)
	}
}
