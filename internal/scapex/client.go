package scapex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Client pings the ScapeX sidecar on the Docker network.
type Client struct {
	base string
	http *http.Client
}

type Hit struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	Title      string `json:"title"`
	Artist     string `json:"artist,omitempty"`
	Album      string `json:"album,omitempty"`
	DurationMS int    `json:"duration_ms,omitempty"`
	StreamURL  string `json:"stream_url,omitempty"`
	ArtworkURL string `json:"artwork_url,omitempty"`
}

func New(base string) *Client {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return nil
	}
	return &Client{
		base: base,
		http: &http.Client{Timeout: 0},
	}
}

func (c *Client) Ready(ctx context.Context) bool {
	if c == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/healthz", nil)
	if err != nil {
		return false
	}
	res, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	return res.StatusCode < 300
}

func (c *Client) Search(ctx context.Context, q string, limit int) ([]Hit, error) {
	if c == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 8
	}
	v := url.Values{}
	v.Set("q", q)
	v.Set("limit", strconv.Itoa(limit))
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/search?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("scapex search %d: %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		Results []Hit `json:"results"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out.Results, nil
}

func (c *Client) Fetch(ctx context.Context, refs []string) ([]uuid.UUID, error) {
	if c == nil {
		return nil, fmt.Errorf("ScapeX is not running")
	}
	body, _ := json.Marshal(map[string]any{"urls": refs})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/fetch", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		var ae struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &ae)
		msg := ae.Error
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		return nil, fmt.Errorf("%s", msg)
	}
	var out struct {
		TrackIDs []uuid.UUID `json:"track_ids"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out.TrackIDs, nil
}

func SongQuery(q string) string {
	q = strings.TrimSpace(q)
	q = stripPlayTokens(q)
	if len(q) < 2 {
		return ""
	}
	return q
}

func stripPlayTokens(q string) string {
	fields := strings.Fields(q)
	var keep []string
	for _, f := range fields {
		low := strings.ToLower(f)
		if strings.HasPrefix(low, "played:") || strings.HasPrefix(low, "never_played:") ||
			strings.HasPrefix(low, "neverplayed:") || strings.HasPrefix(low, "last_played:") ||
			strings.HasPrefix(low, "lastplayed:") {
			continue
		}
		keep = append(keep, f)
	}
	return strings.Join(keep, " ")
}

func AlreadyInLibrary(title, artist string, local []map[string]any) bool {
	nt := norm(title)
	na := norm(artist)
	if nt == "" {
		return false
	}
	for _, row := range local {
		if fmt.Sprint(row["type"]) != "track" {
			continue
		}
		if norm(fmt.Sprint(row["title"])) != nt {
			continue
		}
		if na == "" || strings.Contains(norm(fmt.Sprint(row["artist"])), na) || strings.Contains(na, norm(fmt.Sprint(row["artist"]))) {
			return true
		}
	}
	return false
}

func norm(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "’", "'")
	return s
}

func ParseTrackRefs(ids []string) (tracks []uuid.UUID, youtube []string) {
	for _, raw := range ids {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if id, err := uuid.Parse(raw); err == nil {
			tracks = append(tracks, id)
			continue
		}
		if u := watchURL(raw); u != "" {
			youtube = append(youtube, u)
		}
	}
	return
}

func watchURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) == 11 {
		return "https://www.youtube.com/watch?v=" + raw
	}
	low := strings.ToLower(raw)
	if strings.Contains(low, "youtube.com") || strings.Contains(low, "youtu.be") {
		return raw
	}
	return ""
}
