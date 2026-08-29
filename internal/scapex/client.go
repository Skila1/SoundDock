package scapex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Client talks to in-process YouTube fetch, or an optional leftover HTTP sidecar.
type Client struct {
	base string
	http *http.Client
	svc  *Service
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

// NewLocal runs YouTube search and fetch inside SoundDock (no sidecar).
func NewLocal(svc *Service) *Client {
	if svc == nil {
		return nil
	}
	return &Client{svc: svc, http: &http.Client{Timeout: 0}}
}

func (c *Client) Ready(ctx context.Context) bool {
	if c == nil {
		return false
	}
	if c.svc != nil {
		_, err := exec.LookPath("yt-dlp")
		return err == nil
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
	if c.svc != nil {
		return c.svc.Search(ctx, q, limit)
	}
	if limit <= 0 {
		limit = 8
	}
	v := url.Values{}
	v.Set("q", q)
	v.Set("limit", strconv.Itoa(limit))
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
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

func (c *Client) ListPlaylist(ctx context.Context, raw string, limit int) (PlaylistListing, error) {
	if c == nil {
		return PlaylistListing{}, fmt.Errorf("YouTube search is not available")
	}
	if c.svc != nil {
		return c.svc.ListPlaylist(ctx, raw, limit)
	}
	if limit <= 0 {
		limit = MaxPlaylistQueue
	}
	hits, err := c.Search(ctx, raw, limit)
	if err != nil {
		return PlaylistListing{}, err
	}
	return PlaylistListing{ID: PlaylistID(raw), Title: "", Hits: hits, Total: len(hits)}, nil
}

func (c *Client) Fetch(ctx context.Context, refs []string) ([]uuid.UUID, error) {
	if c == nil {
		return nil, fmt.Errorf("YouTube fetch is not available")
	}
	if c.svc != nil {
		return c.svc.RunFetchJob(ctx, FetchOpts{URLs: refs, Policy: DefaultMediaPolicy})
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

func (c *Client) RunFetchJob(ctx context.Context, opts FetchOpts) ([]uuid.UUID, error) {
	if c == nil {
		return nil, fmt.Errorf("YouTube fetch is not available")
	}
	if c.svc != nil {
		return c.svc.RunFetchJob(ctx, opts)
	}
	return c.Fetch(ctx, opts.URLs)
}

// RunReplaceAcquire downloads to a job-scoped dir and does not wait on HTTP.
func (c *Client) RunReplaceAcquire(ctx context.Context, opts ReplaceOpts) ([]LocalTrack, error) {
	if c == nil || c.svc == nil {
		return nil, fmt.Errorf("YouTube fetch is not available")
	}
	return c.svc.AcquireReplace(ctx, opts)
}

func (c *Client) JobWork(jobID uuid.UUID) string {
	if c == nil || c.svc == nil || c.svc.dock == nil {
		return ""
	}
	return c.svc.dock.JobWork(jobID)
}

func (c *Client) DestLibrary(ctx context.Context) (uuid.UUID, error) {
	if c == nil || c.svc == nil || c.svc.dock == nil {
		return uuid.Nil, fmt.Errorf("no writable SoundDock library")
	}
	return c.svc.dock.LibraryID(ctx)
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
