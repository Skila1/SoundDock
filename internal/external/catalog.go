package external

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func ListPlaylists(ctx context.Context, provider, token, extraKey string) ([]Playlist, error) {
	switch provider {
	case "spotify":
		next := "https://api.spotify.com/v1/me/playlists?limit=50"
		out := []Playlist{}
		for next != "" {
			var raw struct {
				Next  string
				Items []struct {
					ID, Name, Description string
					SnapshotID            string `json:"snapshot_id"`
					Images                []struct{ URL string }
					Owner                 struct {
						DisplayName string `json:"display_name"`
					}
					Tracks struct{ Total int }
				}
			}
			if err := httpJSON(ctx, "GET", next, token, nil, &raw); err != nil {
				return nil, err
			}
			for _, it := range raw.Items {
				p := Playlist{ID: it.ID, Name: it.Name, Description: it.Description, Owner: it.Owner.DisplayName, TrackCount: it.Tracks.Total, Snapshot: it.SnapshotID}
				if len(it.Images) > 0 {
					p.Artwork = it.Images[0].URL
				}
				out = append(out, p)
			}
			next = raw.Next
		}
		return out, nil
	case "youtube":
		var raw struct {
			Items []struct {
				ID      string
				Snippet struct {
					Title, Description, ChannelTitle string
					Thumbnails                       map[string]struct{ URL string }
				}
				ContentDetails struct {
					ItemCount int `json:"itemCount"`
				}
			}
		}
		u := "https://www.googleapis.com/youtube/v3/playlists?part=snippet,contentDetails&mine=true&maxResults=50"
		if err := httpJSON(ctx, "GET", u, token, nil, &raw); err != nil {
			return nil, err
		}
		out := make([]Playlist, 0, len(raw.Items))
		for _, it := range raw.Items {
			p := Playlist{ID: it.ID, Name: it.Snippet.Title, Description: it.Snippet.Description, Owner: it.Snippet.ChannelTitle, TrackCount: it.ContentDetails.ItemCount}
			if th, ok := it.Snippet.Thumbnails["medium"]; ok {
				p.Artwork = th.URL
			}
			out = append(out, p)
		}
		return out, nil
	case "soundcloud":
		var items []map[string]any
		if err := httpJSON(ctx, "GET", "https://api.soundcloud.com/me/playlists", token, nil, &items); err != nil {
			return nil, err
		}
		out := []Playlist{}
		for _, it := range items {
			p := Playlist{ID: fmt.Sprint(it["id"]), Name: str(it["title"]), Description: str(it["description"]), TrackCount: num(it["track_count"])}
			out = append(out, p)
		}
		return out, nil
	case "apple_music":
		var raw struct {
			Data []struct {
				ID         string
				Attributes struct {
					Name, Description string
					Artwork           struct{ URL string }
					LastModifiedDate  string
				}
			}
		}
		reqURL := "https://api.music.apple.com/v1/me/library/playlists"
		// Apple uses Music-User-Token header; pass as extra via special prefix
		if err := appleJSON(ctx, reqURL, extraKey, token, &raw); err != nil {
			return nil, err
		}
		out := []Playlist{}
		for _, it := range raw.Data {
			out = append(out, Playlist{ID: it.ID, Name: it.Attributes.Name, Artwork: it.Attributes.Artwork.URL, Snapshot: it.Attributes.LastModifiedDate})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unknown provider")
	}
}

func GetPlaylistItems(ctx context.Context, provider, token, extra, playlistID string) (Playlist, []Track, error) {
	var meta Playlist
	meta.ID = playlistID
	var tracks []Track
	switch provider {
	case "spotify":
		var pl struct {
			Name, Description, SnapshotID string
			Images                        []struct{ URL string }
			Owner                         struct {
				DisplayName string `json:"display_name"`
			}
		}
		if err := httpJSON(ctx, "GET", "https://api.spotify.com/v1/playlists/"+url.PathEscape(playlistID), token, nil, &pl); err != nil {
			return meta, nil, err
		}
		meta.Name, meta.Description, meta.Snapshot, meta.Owner = pl.Name, pl.Description, pl.SnapshotID, pl.Owner.DisplayName
		if len(pl.Images) > 0 {
			meta.Artwork = pl.Images[0].URL
		}
		next := "https://api.spotify.com/v1/playlists/" + url.PathEscape(playlistID) + "/tracks?limit=100"
		for next != "" {
			var page struct {
				Next  string
				Items []struct {
					Track struct {
						ID, Name, ISRC string
						DurationMS     int `json:"duration_ms"`
						Explicit       bool
						ExternalIDs    struct{ ISRC string }
						Artists        []struct{ Name string }
						Album          struct {
							Name   string
							Images []struct{ URL string }
						}
						ExternalURLs struct{ Spotify string }
					}
				}
			}
			if err := httpJSON(ctx, "GET", next, token, nil, &page); err != nil {
				return meta, tracks, err
			}
			for _, it := range page.Items {
				tr := it.Track
				if tr.ID == "" || tr.Name == "" {
					continue
				}
				isrc := tr.ExternalIDs.ISRC
				if isrc == "" {
					isrc = tr.ISRC
				}
				arts := []string{}
				for _, a := range tr.Artists {
					arts = append(arts, a.Name)
				}
				art := ""
				if len(tr.Album.Images) > 0 {
					art = tr.Album.Images[0].URL
				}
				tracks = append(tracks, Track{Provider: "spotify", ID: tr.ID, Title: tr.Name, Artists: arts, Album: tr.Album.Name, DurationMS: tr.DurationMS, ISRC: isrc, Artwork: art, Explicit: tr.Explicit, SourceURL: tr.ExternalURLs.Spotify})
			}
			next = page.Next
		}
		meta.TrackCount = len(tracks)
		return meta, tracks, nil
	case "youtube":
		next := ""
		for {
			u := "https://www.googleapis.com/youtube/v3/playlistItems?part=snippet,contentDetails&maxResults=50&playlistId=" + url.QueryEscape(playlistID)
			if extra != "" && token == "" {
				u += "&key=" + url.QueryEscape(extra)
			}
			if next != "" {
				u += "&pageToken=" + url.QueryEscape(next)
			}
			var page struct {
				NextPageToken string
				Items         []struct {
					Snippet struct {
						Title, ChannelTitle string
						ResourceId          struct {
							VideoId string `json:"videoId"`
						} `json:"resourceId"`
						Thumbnails map[string]struct{ URL string }
					}
					ContentDetails struct {
						VideoId string `json:"videoId"`
					}
				}
			}
			bearer := token
			if err := httpJSON(ctx, "GET", u, bearer, nil, &page); err != nil {
				return meta, tracks, err
			}
			if meta.Name == "" {
				meta.Name = "YouTube playlist"
			}
			for _, it := range page.Items {
				vid := it.ContentDetails.VideoId
				if vid == "" {
					vid = it.Snippet.ResourceId.VideoId
				}
				thumb := ""
				if th, ok := it.Snippet.Thumbnails["medium"]; ok {
					thumb = th.URL
				}
				tracks = append(tracks, Track{
					Provider: "youtube", ID: vid, Title: it.Snippet.Title, Artists: []string{it.Snippet.ChannelTitle},
					Artwork: thumb, SourceURL: "https://www.youtube.com/watch?v=" + vid,
					Extra: map[string]any{"kind": "video"},
				})
			}
			if page.NextPageToken == "" {
				break
			}
			next = page.NextPageToken
		}
		meta.TrackCount = len(tracks)
		return meta, tracks, nil
	case "soundcloud":
		var pl map[string]any
		path := playlistID
		if !strings.Contains(path, "/") {
			path = "playlists/" + playlistID
		} else {
			path = "resolve?url=" + url.QueryEscape("https://soundcloud.com/"+playlistID)
		}
		if err := httpJSON(ctx, "GET", "https://api.soundcloud.com/"+path, token, nil, &pl); err != nil {
			return meta, nil, err
		}
		meta.Name = str(pl["title"])
		if arr, ok := pl["tracks"].([]any); ok {
			for _, raw := range arr {
				m, _ := raw.(map[string]any)
				user, _ := m["user"].(map[string]any)
				tracks = append(tracks, Track{
					Provider: "soundcloud", ID: fmt.Sprint(m["id"]), Title: str(m["title"]),
					Artists: []string{str(user["username"])}, DurationMS: num(m["duration"]),
					Artwork: str(m["artwork_url"]), SourceURL: str(m["permalink_url"]),
				})
			}
		}
		meta.TrackCount = len(tracks)
		return meta, tracks, nil
	case "apple_music":
		var raw struct {
			Data []struct {
				ID         string
				Attributes struct {
					Name             string
					ArtistName       string
					AlbumName        string
					DurationInMillis int
					Isrc             string
					Artwork          struct{ URL string }
				}
			}
		}
		u := "https://api.music.apple.com/v1/me/library/playlists/" + url.PathEscape(playlistID) + "/tracks"
		if strings.HasPrefix(playlistID, "pl.") && token == "" {
			u = "https://api.music.apple.com/v1/catalog/us/playlists/" + url.PathEscape(playlistID) + "/tracks"
		}
		if err := appleJSON(ctx, u, extra, token, &raw); err != nil {
			return meta, nil, err
		}
		for _, it := range raw.Data {
			tracks = append(tracks, Track{
				Provider: "apple_music", ID: it.ID, Title: it.Attributes.Name, Artists: []string{it.Attributes.ArtistName},
				Album: it.Attributes.AlbumName, DurationMS: it.Attributes.DurationInMillis, ISRC: it.Attributes.Isrc, Artwork: it.Attributes.Artwork.URL,
			})
		}
		meta.Name = playlistID
		meta.TrackCount = len(tracks)
		return meta, tracks, nil
	default:
		return meta, nil, fmt.Errorf("unknown provider")
	}
}

func appleJSON(ctx context.Context, rawURL, developerToken, userToken string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+developerToken)
	if userToken != "" {
		req.Header.Set("Music-User-Token", userToken)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("apple music: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func num(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	}
	return 0
}
