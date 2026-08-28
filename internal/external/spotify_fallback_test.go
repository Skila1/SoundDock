package external

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchSpotifyPlaylistItemsFallsBackOn404Only(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.Contains(r.URL.Path, "/items") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{
				"track": map[string]any{"id": "t1", "name": "Song", "type": "track"},
			}},
		})
	}))
	defer srv.Close()
	prev := spotifyAPIBase
	spotifyAPIBase = srv.URL
	t.Cleanup(func() { spotifyAPIBase = prev })

	tracks, err := fetchSpotifyPlaylistItems(context.Background(), &accessToken{value: "tok"}, "pl1")
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].ID != "t1" {
		t.Fatalf("%+v", tracks)
	}
	if len(paths) < 2 || !strings.Contains(paths[0], "/items") || !strings.Contains(paths[1], "/tracks") {
		t.Fatalf("paths %v", paths)
	}
}

func TestFetchSpotifyPlaylistItemsNoFallbackOnNon404(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			hits := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				if strings.Contains(r.URL.Path, "/tracks") {
					t.Fatal("must not fall back to /tracks")
				}
				w.WriteHeader(code)
			}))
			defer srv.Close()
			prev := spotifyAPIBase
			spotifyAPIBase = srv.URL
			t.Cleanup(func() { spotifyAPIBase = prev })

			_, err := fetchSpotifyPlaylistItems(context.Background(), &accessToken{value: "tok"}, "pl1")
			if httpStatus(err) != code {
				t.Fatalf("err=%v status=%d", err, httpStatus(err))
			}
			if hits != 1 {
				t.Fatalf("hits %d", hits)
			}
		})
	}
}

func TestHTTPJSONAuthRetries401Once(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		authz := r.Header.Get("Authorization")
		if n == 1 {
			if authz != "Bearer stale" {
				t.Fatalf("first %s", authz)
			}
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if authz != "Bearer fresh" {
			t.Fatalf("retry %s", authz)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	tok := &accessToken{
		value: "stale",
		refresh: func(context.Context) (string, error) {
			return "fresh", nil
		},
	}
	var out map[string]any
	if err := httpJSONAuth(context.Background(), "GET", srv.URL, tok, nil, &out); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("calls %d", n)
	}
	if tok.once != true {
		t.Fatal("refresh must run only once")
	}

	n = 0
	tok2 := &accessToken{value: "stale", once: true, refresh: func(context.Context) (string, error) {
		t.Fatal("must not refresh again")
		return "", nil
	}}
	if err := httpJSONAuth(context.Background(), "GET", srv.URL, tok2, nil, &out); httpStatus(err) != http.StatusUnauthorized {
		t.Fatalf("second 401 should not retry: %v", err)
	}
}

func TestRetainedSnapshotKeepsPreviousOnBlank403(t *testing.T) {
	if got := retainedSnapshot("snap-keep", ""); got != "snap-keep" {
		t.Fatalf("%s", got)
	}
	if !spotifyAccessDenied(&HTTPStatusError{Status: http.StatusForbidden}) {
		t.Fatal("403 is access denied")
	}
}
