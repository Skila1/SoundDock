package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type Provider interface {
	Name() string
	Lookup(ctx context.Context, artist, album, title string) (map[string]any, error)
}

type MusicBrainz struct {
	UA string
}

func (m MusicBrainz) Name() string { return "musicbrainz" }

func (m MusicBrainz) Lookup(ctx context.Context, artist, album, title string) (map[string]any, error) {
	q := fmt.Sprintf(`artist:"%s" AND release:"%s"`, artist, album)
	u := "https://musicbrainz.org/ws/2/release/?query=" + url.QueryEscape(q) + "&fmt=json&limit=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	ua := m.UA
	if ua == "" {
		ua = "SoundDock/0.1 (https://github.com/sounddock/sounddock)"
	}
	req.Header.Set("User-Agent", ua)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	time.Sleep(1100 * time.Millisecond) // MusicBrainz 1 req/s
	return raw, nil
}

type CoverArt struct{}

func (CoverArt) Name() string { return "coverartarchive" }

func (CoverArt) Lookup(ctx context.Context, _, _, mbid string) (map[string]any, error) {
	if mbid == "" {
		return nil, fmt.Errorf("mbid required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://coverartarchive.org/release/"+mbid, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "SoundDock/0.1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var raw map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&raw)
	return raw, nil
}
