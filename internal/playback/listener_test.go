package playback

import "testing"

func TestHasAudioListenerCases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   ListenerSnap
		want bool
	}{
		{"browser rendering", ListenerSnap{RendererKind: "browser", Status: "playing", RendererID: "tab-1"}, true},
		{"browser paused not listener", ListenerSnap{RendererKind: "browser", Status: "paused", RendererID: "tab-1"}, false},
		{"browser empty id", ListenerSnap{RendererKind: "browser", Status: "playing"}, false},
		{"discord plus human", ListenerSnap{RendererKind: "discord", Status: "playing", RendererID: "bot", DiscordHumans: 1}, true},
		{"discord zero humans", ListenerSnap{RendererKind: "discord", Status: "playing", RendererID: "bot", DiscordHumans: 0}, false},
		{"discord humans without lease", ListenerSnap{RendererKind: "discord", Status: "playing", DiscordHumans: 1}, false},
		{"sse presence only", ListenerSnap{RendererKind: "none", Status: "playing"}, false},
		{"no listener", ListenerSnap{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := HasAudioListener(c.in); got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}
