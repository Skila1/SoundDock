package lyrics

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

func TestGetLyricsReturnsEmbeddedWithoutNetwork(t *testing.T) {
	id := uuid.New()
	var fetches atomic.Int32
	s := &Service{
		listFn: func(context.Context, uuid.UUID) ([]Result, error) {
			return []Result{{Source: SourceEmbedded, Timed: false, Body: "hello from tags"}}, nil
		},
		urlFn: func(context.Context) string { return "https://lrclib.net" },
		fetchFn: func(context.Context, string, Meta) (string, bool, error) {
			fetches.Add(1)
			t.Fatal("embedded hit must not use the network")
			return "from-network", false, nil
		},
		saveFn: func(context.Context, uuid.UUID, string, string, bool) error {
			t.Fatal("embedded hit must not write cache")
			return nil
		},
	}
	got := s.GetLyrics(context.Background(), Meta{Title: "Song", Artist: "A", TrackID: id})
	if fetches.Load() != 0 {
		t.Fatalf("fetch count %d", fetches.Load())
	}
	if got.Body != "hello from tags" || got.Source != SourceEmbedded {
		t.Fatalf("got %+v", got)
	}
}

func TestGetLyricsProviderDisabledNoHTTP(t *testing.T) {
	var fetches atomic.Int32
	s := &Service{
		listFn: func(context.Context, uuid.UUID) ([]Result, error) { return nil, nil },
		urlFn:  func(context.Context) string { return "" },
		fetchFn: func(context.Context, string, Meta) (string, bool, error) {
			fetches.Add(1)
			return "should-not-run", false, nil
		},
	}
	got := s.GetLyrics(context.Background(), Meta{Title: "Song", Artist: "A", TrackID: uuid.New()})
	if fetches.Load() != 0 {
		t.Fatalf("disabled provider must not HTTP, fetches=%d", fetches.Load())
	}
	if got.Body != "" || got.Source != "" {
		t.Fatalf("want empty, got %+v", got)
	}
}

func TestGetLyricsManualNotOverwritten(t *testing.T) {
	id := uuid.New()
	var saves atomic.Int32
	s := &Service{
		listFn: func(context.Context, uuid.UUID) ([]Result, error) {
			return []Result{
				{Source: SourceEmbedded, Body: "embedded"},
				{Source: SourceManual, Body: "typed by editor"},
			}, nil
		},
		urlFn: func(context.Context) string { return "https://lrclib.net" },
		fetchFn: func(context.Context, string, Meta) (string, bool, error) {
			t.Fatal("manual lyrics must not fetch a provider")
			return "provider", true, nil
		},
		saveFn: func(context.Context, uuid.UUID, string, string, bool) error {
			saves.Add(1)
			t.Fatal("manual lyrics must not be overwritten")
			return nil
		},
	}
	got := s.GetLyrics(context.Background(), Meta{Title: "Song", Artist: "A", TrackID: id})
	if saves.Load() != 0 {
		t.Fatalf("save count %d", saves.Load())
	}
	if got.Source != SourceManual || got.Body != "typed by editor" {
		t.Fatalf("got %+v", got)
	}
}

func TestGetLyricsUserSourceNotOverwritten(t *testing.T) {
	s := &Service{
		listFn: func(context.Context, uuid.UUID) ([]Result, error) {
			return []Result{{Source: SourceUser, Body: "from metadata editor"}}, nil
		},
		urlFn: func(context.Context) string { return "https://lrclib.net" },
		fetchFn: func(context.Context, string, Meta) (string, bool, error) {
			t.Fatal("user lyrics must not fetch")
			return "", false, nil
		},
	}
	got := s.GetLyrics(context.Background(), Meta{Title: "Song", Artist: "A", TrackID: uuid.New()})
	if got.Source != SourceUser || got.Body != "from metadata editor" {
		t.Fatalf("got %+v", got)
	}
}

func TestGetLyricsCachesProviderHit(t *testing.T) {
	id := uuid.New()
	var saved SourceSave
	s := &Service{
		listFn: func(context.Context, uuid.UUID) ([]Result, error) { return nil, nil },
		urlFn:  func(context.Context) string { return "https://lrclib.net" },
		fetchFn: func(context.Context, string, Meta) (string, bool, error) {
			return "[00:01.00] hi", true, nil
		},
		saveFn: func(_ context.Context, trackID uuid.UUID, source, body string, timed bool) error {
			saved = SourceSave{TrackID: trackID, Source: source, Body: body, Timed: timed}
			return nil
		},
	}
	got := s.GetLyrics(context.Background(), Meta{Title: "Song", Artist: "A", TrackID: id})
	if got.Source != SourceLRCLIB || got.Body != "[00:01.00] hi" || !got.Timed {
		t.Fatalf("got %+v", got)
	}
	if saved.Source != SourceLRCLIB || saved.TrackID != id || !saved.Timed {
		t.Fatalf("saved %+v", saved)
	}
}

type SourceSave struct {
	TrackID uuid.UUID
	Source  string
	Body    string
	Timed   bool
}

func TestGetLyricsFetchFailureNonFatal(t *testing.T) {
	s := &Service{
		listFn: func(context.Context, uuid.UUID) ([]Result, error) { return nil, nil },
		urlFn:  func(context.Context) string { return "https://lrclib.net" },
		fetchFn: func(context.Context, string, Meta) (string, bool, error) {
			return "", false, errors.New("network down")
		},
	}
	got := s.GetLyrics(context.Background(), Meta{Title: "Song", Artist: "A", TrackID: uuid.New()})
	if got.Body != "" {
		t.Fatalf("failure must return empty, got %+v", got)
	}
}

func TestNormalizeProviderURL(t *testing.T) {
	got, err := NormalizeProviderURL("")
	if err != nil || got != "" {
		t.Fatalf("empty: %q %v", got, err)
	}
	got, err = NormalizeProviderURL("https://lrclib.net")
	if err != nil || got != "https://lrclib.net" {
		t.Fatalf("lrclib: %q %v", got, err)
	}
	got, err = NormalizeProviderURL("https://lrclib.net/")
	if err != nil || got != "https://lrclib.net" {
		t.Fatalf("slash: %q %v", got, err)
	}
	if _, err := NormalizeProviderURL("https://genius.com"); err == nil {
		t.Fatal("genius must be rejected")
	}
	if _, err := NormalizeProviderURL("http://lrclib.net"); err == nil {
		t.Fatal("http must be rejected")
	}
}

func TestParseLines(t *testing.T) {
	lines := ParseLines("[00:12.00] hello\n[01:02.500] world\nplain")
	if len(lines) != 2 {
		t.Fatalf("len %d", len(lines))
	}
	if lines[0].Tms != 12000 || lines[0].Text != "hello" {
		t.Fatalf("first %+v", lines[0])
	}
	if lines[1].Tms != 62500 || lines[1].Text != "world" {
		t.Fatalf("second %+v", lines[1])
	}
}

func TestFetchLRCLIBRefusesUnknownHost(t *testing.T) {
	_, _, err := fetchLRCLIB(context.Background(), nil, "https://genius.com", Meta{Title: "a", Artist: "b"})
	if err == nil {
		t.Fatal("expected unknown host")
	}
}
