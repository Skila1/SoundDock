package scrobble

import (
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

type ImportResult struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
	Matched  int `json:"matched"`
}

func (s *Service) ImportHistory(ctx context.Context, userID uuid.UUID, provider string) (ImportResult, error) {
	_ = EnsureSchema(ctx, s.pool)
	switch provider {
	case "lastfm":
		return s.importLastFM(ctx, userID)
	case "listenbrainz":
		return s.importListenBrainz(ctx, userID)
	default:
		return ImportResult{}, fmt.Errorf("unknown provider")
	}
}

func (s *Service) matchTrack(ctx context.Context, title, artist string) (uuid.UUID, bool) {
	if s.search == nil || strings.TrimSpace(title) == "" {
		return uuid.Nil, false
	}
	q := strings.TrimSpace(title + " " + artist)
	hits, err := s.search.Search(ctx, q, []string{"track"}, nil, 5)
	if err != nil || len(hits) == 0 {
		return uuid.Nil, false
	}
	for _, h := range hits {
		if h.Type == "track" {
			return h.ID, true
		}
	}
	return uuid.Nil, false
}

func (s *Service) importLastFM(ctx context.Context, userID uuid.UUID) (ImportResult, error) {
	user, _ := s.lastFMSession(ctx, userID)
	if user == "" {
		return ImportResult{}, fmt.Errorf("Last.fm is not connected")
	}
	apiKey, _ := s.lastFMKeys(ctx)
	if apiKey == "" {
		return ImportResult{}, fmt.Errorf("Last.fm API key is not configured")
	}
	var res ImportResult
	for page := 1; page <= 10; page++ {
		u := url.Values{}
		u.Set("method", "user.getRecentTracks")
		u.Set("user", user)
		u.Set("api_key", apiKey)
		u.Set("format", "json")
		u.Set("limit", "200")
		u.Set("page", strconv.Itoa(page))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ws.audioscrobbler.com/2.0/?"+u.Encode(), nil)
		if err != nil {
			return res, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return res, err
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		var out struct {
			RecentTracks struct {
				Track []struct {
					Name   string `json:"name"`
					Artist struct {
						Text string `json:"#text"`
					} `json:"artist"`
					Date struct {
						UTS string `json:"uts"`
					} `json:"date"`
					Duration string `json:"duration"`
				} `json:"track"`
			} `json:"recenttracks"`
		}
		if json.Unmarshal(raw, &out) != nil || len(out.RecentTracks.Track) == 0 {
			break
		}
		for _, tr := range out.RecentTracks.Track {
			id, ok := s.matchTrack(ctx, tr.Name, tr.Artist.Text)
			if !ok {
				res.Skipped++
				continue
			}
			res.Matched++
			uts, _ := strconv.ParseInt(tr.Date.UTS, 10, 64)
			if uts == 0 {
				res.Skipped++
				continue
			}
			dur, _ := strconv.Atoi(tr.Duration)
			if err := s.InsertImported(ctx, userID, id, time.Unix(uts, 0).UTC(), dur*1000); err != nil {
				res.Skipped++
				continue
			}
			res.Imported++
		}
		if len(out.RecentTracks.Track) < 200 {
			break
		}
	}
	return res, nil
}

func (s *Service) importListenBrainz(ctx context.Context, userID uuid.UUID) (ImportResult, error) {
	user, token := s.listenBrainzToken(ctx, userID)
	if token == "" || user == "" {
		return ImportResult{}, fmt.Errorf("ListenBrainz is not connected")
	}
	var res ImportResult
	minTS := int64(0)
	for n := 0; n < 10; n++ {
		u := fmt.Sprintf("https://api.listenbrainz.org/1/user/%s/listens?count=100", url.PathEscape(user))
		if minTS > 0 {
			u += "&max_ts=" + strconv.FormatInt(minTS, 10)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return res, err
		}
		req.Header.Set("Authorization", "Token "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return res, err
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		var out struct {
			Payload struct {
				Listens []struct {
					ListenedAt    int64 `json:"listened_at"`
					TrackMetadata struct {
						ArtistName  string `json:"artist_name"`
						TrackName   string `json:"track_name"`
						ReleaseName string `json:"release_name"`
					} `json:"track_metadata"`
				} `json:"listens"`
			} `json:"payload"`
		}
		if json.Unmarshal(raw, &out) != nil || len(out.Payload.Listens) == 0 {
			break
		}
		for _, l := range out.Payload.Listens {
			if minTS == 0 || l.ListenedAt < minTS {
				minTS = l.ListenedAt
			}
			id, ok := s.matchTrack(ctx, l.TrackMetadata.TrackName, l.TrackMetadata.ArtistName)
			if !ok {
				res.Skipped++
				continue
			}
			res.Matched++
			if err := s.InsertImported(ctx, userID, id, time.Unix(l.ListenedAt, 0).UTC(), 0); err != nil {
				res.Skipped++
				continue
			}
			res.Imported++
		}
		if len(out.Payload.Listens) < 100 {
			break
		}
	}
	return res, nil
}
