package retention

import (
	"path"
	"regexp"
	"strings"
)

var youtubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func IsYouTubeID(s string) bool {
	return youtubeID.MatchString(strings.TrimSpace(s))
}

// YouTubeIDFromKey extracts a video id from inbox/{id}.ext storage keys.
func YouTubeIDFromKey(key string) string {
	k := strings.ReplaceAll(strings.TrimSpace(key), "\\", "/")
	k = strings.TrimPrefix(k, "/")
	base := strings.TrimSuffix(path.Base(k), path.Ext(k))
	if !IsYouTubeID(base) {
		return ""
	}
	if strings.HasPrefix(k, "inbox/") || strings.Contains(k, "/inbox/") {
		return base
	}
	return ""
}
