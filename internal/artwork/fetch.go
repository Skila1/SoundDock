package artwork

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var (
	artHTTP   = &http.Client{Timeout: 12 * time.Second}
	ytVideoID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
)

// FetchYouTubeThumb downloads a SoundDock-owned copy of the public YouTube thumbnail.
func FetchYouTubeThumb(ctx context.Context, videoID string) ([]byte, error) {
	id := strings.TrimSpace(videoID)
	if !ytVideoID.MatchString(id) {
		return nil, fmt.Errorf("invalid youtube id")
	}
	for _, name := range []string{"hqdefault.jpg", "mqdefault.jpg"} {
		u := "https://i.ytimg.com/vi/" + id + "/" + name
		img, err := fetchURL(ctx, u)
		if err == nil && len(img) > 32 {
			return img, nil
		}
	}
	return nil, fmt.Errorf("no youtube thumb")
}

func fetchURL(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "SoundDock/0.1")
	resp, err := artHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}
