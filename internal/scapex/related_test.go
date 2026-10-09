package scapex

import "testing"

func TestLooksLikeMusic(t *testing.T) {
	cases := []struct {
		title string
		ms    int
		want  bool
	}{
		{"Drake - Hotline Bling", 267_000, true},
		{"Foo Fighters - Best Of You (Official Music Video)", 256_000, true},
		{"Radiohead - Creep (Lyrics)", 0, true},
		{"Drake Interview on Hot 97", 1_800_000, false},
		{"Kendrick Lamar REACTION | First time hearing", 600_000, false},
		{"Taylor Swift Full Album 2024", 3_600_000, false},
		{"lofi hip hop 1 hour mix", 3_600_000, false},
		{"Blinding Lights (slowed + reverb)", 240_000, false},
		{"Song (Official Audio)", 30_000, false},
		{"Making of the album | Behind the Scenes", 400_000, false},
	}
	for _, c := range cases {
		if got := LooksLikeMusic(Hit{ID: "abcdefghijk", Title: c.title, DurationMS: c.ms}); got != c.want {
			t.Errorf("LooksLikeMusic(%q) = %v, want %v", c.title, got, c.want)
		}
	}
}
