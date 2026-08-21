package external

import "testing"

func TestParsePlaylistURL(t *testing.T) {
	cases := []struct {
		in, provider, id string
		ok               bool
	}{
		{"https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M", "spotify", "37i9dQZF1DXcBWIGoYBM5M", true},
		{"https://open.spotify.com/playlist/abc123?si=foo", "spotify", "abc123", true},
		{"spotify:playlist:xyz", "spotify", "xyz", true},
		{"https://www.youtube.com/playlist?list=PLabc", "youtube", "PLabc", true},
		{"https://music.youtube.com/playlist?list=RDxyz", "youtube", "RDxyz", true},
		{"https://soundcloud.com/user/sets/gym", "soundcloud", "user/sets/gym", true},
		{"https://music.apple.com/us/playlist/favorites/pl.u-abc", "apple_music", "pl.u-abc", true},
		{"https://example.com/album/track.flac", "", "", false},
		{"https://open.spotify.com/track/abc", "", "", false},
	}
	for _, c := range cases {
		ref, ok := ParsePlaylistURL(c.in)
		if ok != c.ok {
			t.Fatalf("%s ok=%v want %v", c.in, ok, c.ok)
		}
		if !ok {
			continue
		}
		if ref.Provider != c.provider || ref.ID != c.id {
			t.Fatalf("%s got %+v want %s/%s", c.in, ref, c.provider, c.id)
		}
	}
}
