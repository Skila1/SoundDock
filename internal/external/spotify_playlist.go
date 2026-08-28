package external

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

var spotifyAPIBase = "https://api.spotify.com/v1"

type spotifyTrackJSON struct {
	ID, Name, ISRC, Type string
	DurationMS           int  `json:"duration_ms"`
	Explicit             bool `json:"explicit"`
	ExternalIDs          struct {
		ISRC string `json:"isrc"`
	} `json:"external_ids"`
	Artists []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Album struct {
		Name   string `json:"name"`
		Images []struct {
			URL string `json:"url"`
		} `json:"images"`
	} `json:"album"`
	ExternalURLs struct {
		Spotify string `json:"spotify"`
	} `json:"external_urls"`
}

func spotifyItemCount(tracksRaw, itemsRaw json.RawMessage) int {
	n := decodeCountBox(tracksRaw)
	if m := decodeCountBox(itemsRaw); m > n {
		n = m
	}
	return n
}

func decodeCountBox(raw json.RawMessage) int {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var obj struct {
		Total int `json:"total"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Total > 0 {
		return obj.Total
	}
	var n int
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		return n
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil {
		return len(arr)
	}
	return 0
}

func trackFromSpotifyPageItem(raw json.RawMessage) (Track, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return Track{}, false
	}
	var wrap struct {
		Track json.RawMessage `json:"track"`
		Item  json.RawMessage `json:"item"`
	}
	_ = json.Unmarshal(raw, &wrap)
	payload := wrap.Item
	if len(payload) == 0 || string(payload) == "null" {
		payload = wrap.Track
	}
	if len(payload) == 0 || string(payload) == "null" {
		payload = raw
	}
	var tr spotifyTrackJSON
	if json.Unmarshal(payload, &tr) != nil {
		return Track{}, false
	}
	if tr.Type != "" && tr.Type != "track" {
		return Track{}, false
	}
	if tr.ID == "" || tr.Name == "" {
		return Track{}, false
	}
	isrc := tr.ExternalIDs.ISRC
	if isrc == "" {
		isrc = tr.ISRC
	}
	arts := make([]string, 0, len(tr.Artists))
	for _, a := range tr.Artists {
		if a.Name != "" {
			arts = append(arts, a.Name)
		}
	}
	art := ""
	if len(tr.Album.Images) > 0 {
		art = tr.Album.Images[0].URL
	}
	return Track{
		Provider:   "spotify",
		ID:         tr.ID,
		Title:      tr.Name,
		Artists:    arts,
		Album:      tr.Album.Name,
		DurationMS: tr.DurationMS,
		ISRC:       isrc,
		Artwork:    art,
		Explicit:   tr.Explicit,
		SourceURL:  tr.ExternalURLs.Spotify,
	}, true
}

func spotifyPlaylistURL(playlistID, kind string) string {
	id := url.PathEscape(playlistID)
	switch kind {
	case "tracks":
		return spotifyAPIBase + "/playlists/" + id + "/tracks?limit=100&additional_types=track"
	default:
		return spotifyAPIBase + "/playlists/" + id + "/items?limit=50&additional_types=track"
	}
}

func spotifyAccessDenied(err error) bool {
	return httpStatus(err) == http.StatusForbidden
}

func fetchSpotifyPlaylistItems(ctx context.Context, tok *accessToken, playlistID string) ([]Track, error) {
	tracks, err := pageSpotifyPlaylistItems(ctx, tok, spotifyPlaylistURL(playlistID, "items"))
	if err == nil {
		return tracks, nil
	}
	if httpStatus(err) == http.StatusNotFound {
		return pageSpotifyPlaylistItems(ctx, tok, spotifyPlaylistURL(playlistID, "tracks"))
	}
	return nil, err
}

func pageSpotifyPlaylistItems(ctx context.Context, tok *accessToken, start string) ([]Track, error) {
	next := start
	var tracks []Track
	for next != "" {
		var page struct {
			Next  string
			Items []json.RawMessage
		}
		if err := httpJSONAuth(ctx, "GET", next, tok, nil, &page); err != nil {
			return tracks, err
		}
		for _, raw := range page.Items {
			if tr, ok := trackFromSpotifyPageItem(raw); ok {
				tracks = append(tracks, tr)
			}
		}
		next = page.Next
	}
	return tracks, nil
}
