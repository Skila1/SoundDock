package scapex

import (
	"context"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// YouTube searches and downloads audio through yt-dlp inside this container.
type YouTube interface {
	Search(ctx context.Context, query string, limit int) ([]Hit, error)
	Fetch(ctx context.Context, mediaURL, destDir string) ([]LocalTrack, error)
}

type Hit struct {
	Type       string  `json:"type"`
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Artist     string  `json:"artist,omitempty"`
	Album      string  `json:"album,omitempty"`
	DurationMS int     `json:"duration_ms,omitempty"`
	Score      float64 `json:"score,omitempty"`
	StreamURL  string  `json:"stream_url,omitempty"`
	ArtworkURL string  `json:"artwork_url,omitempty"`
}

type LocalTrack struct {
	Path       string
	Title      string
	Artist     string
	Album      string
	VideoID    string
	SourceURL  string
	DurationMS int
}

var videoIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// MaxPlaylistQueue is the most tracks search-bar playlist expansion will return.
const MaxPlaylistQueue = 400

var skipPlaylistIDs = map[string]bool{
	"WL": true,
	"LL": true,
}

func IsYouTubeURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com",
		"youtu.be", "www.youtu.be", "youtube-nocookie.com", "www.youtube-nocookie.com":
		return true
	}
	return strings.HasSuffix(host, ".youtube.com")
}

func VideoID(raw string) string {
	raw = strings.TrimSpace(raw)
	if videoIDRe.MatchString(raw) {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if v := u.Query().Get("v"); videoIDRe.MatchString(v) {
		return v
	}
	host := strings.ToLower(u.Hostname())
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if host == "youtu.be" || host == "www.youtu.be" {
		if len(parts) > 0 && videoIDRe.MatchString(parts[0]) {
			return parts[0]
		}
	}
	for i, p := range parts {
		if (p == "shorts" || p == "embed" || p == "live") && i+1 < len(parts) && videoIDRe.MatchString(parts[i+1]) {
			return parts[i+1]
		}
	}
	base := path.Base(u.Path)
	if videoIDRe.MatchString(base) {
		return base
	}
	return ""
}

func WatchURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if id := VideoID(raw); id != "" {
		return "https://www.youtube.com/watch?v=" + id
	}
	if IsYouTubeURL(raw) {
		return raw
	}
	return ""
}

// PlaylistID returns the list= id from a YouTube URL. Watch Later and Liked are ignored.
func PlaylistID(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	if !IsYouTubeURL(raw) {
		return ""
	}
	id := strings.TrimSpace(u.Query().Get("list"))
	if id == "" || skipPlaylistIDs[strings.ToUpper(id)] {
		return ""
	}
	return id
}

// PlaylistURL is the canonical public playlist page for list=.
func PlaylistURL(raw string) string {
	id := PlaylistID(raw)
	if id == "" {
		return ""
	}
	return "https://www.youtube.com/playlist?list=" + id
}

// IsPlaylistQuery is true for /playlist?list=… and for watch URLs whose list=
// is a real playlist (PL/OL/UU/FL), not an auto-generated Mix.
func IsPlaylistQuery(raw string) bool {
	id := PlaylistID(raw)
	if id == "" {
		return false
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	path := strings.ToLower(strings.Trim(u.Path, "/"))
	if path == "playlist" || strings.HasSuffix(path, "/playlist") {
		return true
	}
	upper := strings.ToUpper(id)
	for _, p := range []string{"PL", "OL", "UU", "FL"} {
		if strings.HasPrefix(upper, p) {
			return true
		}
	}
	return false
}

type PlaylistListing struct {
	ID        string
	Title     string
	Hits      []Hit
	Total     int
	Truncated bool
}

func ytThumb(id string) string {
	if id == "" {
		return ""
	}
	return "https://i.ytimg.com/vi/" + id + "/mqdefault.jpg"
}
