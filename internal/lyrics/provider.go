package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

var (
	errUnknownHost = errors.New("lyrics provider host is not allowlisted")
	errInvalidURL  = errors.New("lyrics provider URL is invalid")
)

// NormalizeProviderURL accepts empty (disabled) or https://lrclib.net.
// Unknown hosts are rejected. Default off is empty — no implied network.
func NormalizeProviderURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", errInvalidURL
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return "", errInvalidURL
	}
	host := strings.ToLower(u.Hostname())
	if !hostAllowed(host) {
		return "", errUnknownHost
	}
	return "https://" + AllowedHost, nil
}

func hostAllowed(host string) bool {
	return strings.EqualFold(strings.TrimSpace(host), AllowedHost)
}

func (s *Service) fetch(ctx context.Context, origin string, meta Meta) (string, bool, error) {
	if s.fetchFn != nil {
		return s.fetchFn(ctx, origin, meta)
	}
	return fetchLRCLIB(ctx, s.client, origin, meta)
}

type lrclibHit struct {
	PlainLyrics  string `json:"plainLyrics"`
	SyncedLyrics string `json:"syncedLyrics"`
}

func fetchLRCLIB(ctx context.Context, client *http.Client, origin string, meta Meta) (string, bool, error) {
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	if origin == "" {
		return "", false, nil
	}
	u, err := url.Parse(origin)
	if err != nil {
		return "", false, err
	}
	if !hostAllowed(u.Hostname()) {
		return "", false, errUnknownHost
	}
	q := url.Values{}
	q.Set("track_name", strings.TrimSpace(meta.Title))
	q.Set("artist_name", strings.TrimSpace(meta.Artist))
	if strings.TrimSpace(meta.Album) != "" {
		q.Set("album_name", strings.TrimSpace(meta.Album))
	}
	if meta.DurationMS > 0 {
		q.Set("duration", fmt.Sprintf("%d", meta.DurationMS/1000))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/api/get?"+q.Encode(), nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "SoundDock/1.0 (lyrics; lrclib)")
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return "", false, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return "", false, nil
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", false, fmt.Errorf("lrclib status %d", res.StatusCode)
	}
	ct := res.Header.Get("Content-Type")
	if ct != "" && !strings.Contains(strings.ToLower(ct), "json") {
		return "", false, errors.New("lrclib response is not json")
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", false, err
	}
	var hit lrclibHit
	if err := json.Unmarshal(body, &hit); err != nil {
		return "", false, err
	}
	synced := strings.TrimSpace(hit.SyncedLyrics)
	if synced != "" {
		return synced, true, nil
	}
	return strings.TrimSpace(hit.PlainLyrics), false, nil
}
