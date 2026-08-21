package external

import (
	"net/url"
	"strings"
)

type Ref struct {
	Provider string
	ID       string
}

// ParsePlaylistURL detects Spotify, YouTube, SoundCloud, and Apple Music playlist URLs.
// It does not fetch anything and is not a Remote Import target.
func ParsePlaylistURL(raw string) (Ref, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Ref{}, false
	}
	if strings.HasPrefix(raw, "spotify:playlist:") {
		return Ref{Provider: "spotify", ID: strings.TrimPrefix(raw, "spotify:playlist:")}, true
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return Ref{}, false
	}
	host := strings.ToLower(u.Hostname())
	path := strings.Trim(u.Path, "/")
	parts := strings.Split(path, "/")

	switch {
	case host == "open.spotify.com" || host == "play.spotify.com":
		for i, p := range parts {
			if p == "playlist" && i+1 < len(parts) {
				return Ref{Provider: "spotify", ID: strings.Split(parts[i+1], "?")[0]}, true
			}
		}
	case host == "www.youtube.com" || host == "youtube.com" || host == "music.youtube.com" || host == "youtu.be" || host == "m.youtube.com":
		if id := u.Query().Get("list"); id != "" && !strings.EqualFold(id, "WL") {
			return Ref{Provider: "youtube", ID: id}, true
		}
	case strings.Contains(host, "soundcloud.com"):
		for i, p := range parts {
			if p == "sets" && i+1 < len(parts) {
				return Ref{Provider: "soundcloud", ID: path}, true
			}
		}
	case host == "music.apple.com" || strings.HasSuffix(host, ".music.apple.com"):
		for i, p := range parts {
			if p == "playlist" && i+1 < len(parts) {
				id := parts[len(parts)-1]
				return Ref{Provider: "apple_music", ID: id}, true
			}
			_ = i
		}
	}
	return Ref{}, false
}

func IsPlaylistURL(raw string) bool {
	_, ok := ParsePlaylistURL(raw)
	return ok
}
