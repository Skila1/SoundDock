package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Provider interface {
	Name() string
	Lookup(ctx context.Context, artist, album, title string) (map[string]any, error)
}

var (
	metaHTTP = http.DefaultClient
	mbAPI    = "https://musicbrainz.org"
	caaAPI   = "https://coverartarchive.org"
	mbDelay  = 1100 * time.Millisecond
)

const mbUserAgent = "SoundDock/0.1 (https://github.com/sounddock/sounddock)"

type MusicBrainz struct {
	UA         string
	DurationMS int
}

func (m MusicBrainz) Name() string { return "musicbrainz" }

func luceneEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// musicBrainzSearchURL includes title when present (recording search) plus artist,
// optional album, and a duration window so lookup is not artist+album only.
func musicBrainzSearchURL(artist, album, title string, durationMS int) string {
	artist = strings.TrimSpace(artist)
	album = strings.TrimSpace(album)
	title = strings.TrimSpace(title)
	var path, q string
	if title != "" {
		path = "/ws/2/recording/"
		q = fmt.Sprintf(`recording:"%s"`, luceneEscape(title))
		if artist != "" {
			q += fmt.Sprintf(` AND artist:"%s"`, luceneEscape(artist))
		}
		if album != "" {
			q += fmt.Sprintf(` AND release:"%s"`, luceneEscape(album))
		}
		if durationMS > 0 {
			lo := durationMS - 15000
			if lo < 0 {
				lo = 0
			}
			q += fmt.Sprintf(` AND dur:[%d TO %d]`, lo, durationMS+15000)
		}
	} else {
		path = "/ws/2/release/"
		q = fmt.Sprintf(`artist:"%s" AND release:"%s"`, luceneEscape(artist), luceneEscape(album))
	}
	return strings.TrimRight(mbAPI, "/") + path + "?query=" + url.QueryEscape(q) + "&fmt=json&limit=5"
}

func mbGet(ctx context.Context, rawURL, ua string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if ua == "" {
		ua = mbUserAgent
	}
	req.Header.Set("User-Agent", ua)
	resp, err := metaHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil, fmt.Errorf("musicbrainz %d", resp.StatusCode)
	}
	var raw map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&raw); err != nil {
		return nil, err
	}
	if mbDelay > 0 {
		time.Sleep(mbDelay)
	}
	return raw, nil
}

func (m MusicBrainz) Lookup(ctx context.Context, artist, album, title string) (map[string]any, error) {
	return mbGet(ctx, musicBrainzSearchURL(artist, album, title, m.DurationMS), m.UA)
}

// LookupRecording loads official genres, tags, and artist-credits for a recording MBID.
func (m MusicBrainz) LookupRecording(ctx context.Context, mbid string) (map[string]any, error) {
	mbid = strings.TrimSpace(mbid)
	if mbid == "" {
		return nil, fmt.Errorf("mbid required")
	}
	u := strings.TrimRight(mbAPI, "/") + "/ws/2/recording/" + url.PathEscape(mbid) + "?inc=genres+tags+artist-credits+releases&fmt=json"
	return mbGet(ctx, u, m.UA)
}

type CoverArt struct{}

func (CoverArt) Name() string { return "coverartarchive" }

func (CoverArt) Lookup(ctx context.Context, _, _, mbid string) (map[string]any, error) {
	if mbid == "" {
		return nil, fmt.Errorf("mbid required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(caaAPI, "/")+"/release/"+url.PathEscape(mbid), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", mbUserAgent)
	resp, err := metaHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no cover")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("coverart %d", resp.StatusCode)
	}
	var raw map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func coverArtFrontURL(raw map[string]any) string {
	if raw == nil {
		return ""
	}
	images, _ := raw["images"].([]any)
	pick := func(m map[string]any) string {
		if u, _ := m["image"].(string); u != "" {
			return u
		}
		thumbs, _ := m["thumbnails"].(map[string]any)
		for _, k := range []string{"1200", "large", "500", "small", "250"} {
			if u, _ := thumbs[k].(string); u != "" {
				return u
			}
		}
		return ""
	}
	for _, img := range images {
		m, _ := img.(map[string]any)
		if m == nil {
			continue
		}
		if front, _ := m["front"].(bool); front {
			if u := pick(m); u != "" {
				return u
			}
		}
	}
	for _, img := range images {
		m, _ := img.(map[string]any)
		if m == nil {
			continue
		}
		if u := pick(m); u != "" {
			return u
		}
	}
	return ""
}

func fetchURLBytes(ctx context.Context, rawURL string) ([]byte, error) {
	if rawURL == "" {
		return nil, fmt.Errorf("empty url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", mbUserAgent)
	resp, err := metaHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// FetchFront downloads the front cover image for a release MBID from Cover Art Archive.
func (c CoverArt) FetchFront(ctx context.Context, mbid string) ([]byte, error) {
	raw, err := c.Lookup(ctx, "", "", mbid)
	if err != nil {
		return nil, err
	}
	u := coverArtFrontURL(raw)
	if u != "" {
		if img, ferr := fetchURLBytes(ctx, u); ferr == nil && len(img) > 0 {
			return img, nil
		}
	}
	return fetchURLBytes(ctx, strings.TrimRight(caaAPI, "/")+"/release/"+url.PathEscape(mbid)+"/front")
}
