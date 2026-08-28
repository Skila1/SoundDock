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

func isYouTubePlaylist(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	p := strings.ToLower(u.Path)
	if strings.Contains(p, "/playlist") {
		return true
	}
	if u.Query().Get("list") != "" && u.Query().Get("v") == "" && !strings.Contains(p, "/watch") && !strings.Contains(p, "/shorts") {
		return true
	}
	return false
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

func ytThumb(id string) string {
	if id == "" {
		return ""
	}
	return "https://i.ytimg.com/vi/" + id + "/mqdefault.jpg"
}
