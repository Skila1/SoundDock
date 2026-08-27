package scapex

import "testing"

func TestParseTrackRefs(t *testing.T) {
	tracks, yt := ParseTrackRefs([]string{
		"00000000-0000-4000-8000-000000000050",
		"kXYiU_JCYtU",
		"https://youtu.be/abcdefghijk",
	})
	if len(tracks) != 1 || len(yt) != 2 {
		t.Fatalf("tracks=%d yt=%d", len(tracks), len(yt))
	}
}

func TestSongQuery(t *testing.T) {
	if SongQuery("played:never") != "" {
		t.Fatal("filter only")
	}
	if SongQuery("numb played:never") != "numb" {
		t.Fatal("strip")
	}
	if SongQuery("https://youtu.be/kXYiU_JCYtU") != "https://youtu.be/kXYiU_JCYtU" {
		t.Fatal("youtube url")
	}
}

func TestAlreadyInLibrary(t *testing.T) {
	local := []map[string]any{{"type": "track", "title": "Numb", "artist": "Linkin Park"}}
	if !AlreadyInLibrary("Numb", "Linkin Park", local) {
		t.Fatal("expected match")
	}
	if AlreadyInLibrary("In The End", "Linkin Park", local) {
		t.Fatal("different title")
	}
}
