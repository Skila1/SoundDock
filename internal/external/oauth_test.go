package external

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestImportBodyJSONTags(t *testing.T) {
	var body struct {
		URL      string `json:"url"`
		Mode     string `json:"mode"`
		Name     string `json:"name"`
		Interval string `json:"sync_interval"`
		Removal  string `json:"removal_policy"`
	}
	const payload = `{"url":"https://open.spotify.com/playlist/abc","mode":"sync","name":"Gym","sync_interval":"1h","removal_policy":"keep"}`
	if err := json.Unmarshal([]byte(payload), &body); err != nil {
		t.Fatal(err)
	}
	if body.URL == "" || body.Mode != "sync" || body.Name != "Gym" || body.Interval != "1h" || body.Removal != "keep" {
		t.Fatalf("tags not applied: %+v", body)
	}
}

func TestTokenFresh(t *testing.T) {
	if tokenFresh(time.Time{}) {
		t.Fatal("zero expiry is not fresh")
	}
	if tokenFresh(time.Now().Add(30 * time.Second)) {
		t.Fatal("token inside skew should refresh")
	}
	if !tokenFresh(time.Now().Add(time.Hour)) {
		t.Fatal("unexpired token should be fresh")
	}
}

func TestClassifyInvalidGrant(t *testing.T) {
	err := classifyTokenStatus(400, []byte(`{"error":"invalid_grant","error_description":"Refresh token revoked"}`))
	if !errors.Is(err, ErrNeedsReconnect) {
		t.Fatalf("got %v", err)
	}
	if errors.Is(err, ErrTemporary) {
		t.Fatal("invalid_grant is not temporary")
	}
}

func TestClassify5xx(t *testing.T) {
	err := classifyTokenStatus(503, []byte(`{"error":"server_error"}`))
	if !errors.Is(err, ErrTemporary) {
		t.Fatalf("got %v", err)
	}
	if errors.Is(err, ErrNeedsReconnect) {
		t.Fatal("5xx is not reconnect")
	}
}

func TestClassifyOther4xx(t *testing.T) {
	err := classifyTokenStatus(400, []byte(`{"error":"invalid_request"}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrNeedsReconnect) || errors.Is(err, ErrTemporary) {
		t.Fatalf("other 4xx should not force reconnect or temp: %v", err)
	}
}

func withSpotifyTokenURL(t *testing.T, u string) {
	t.Helper()
	orig := spotifyTokenURL
	spotifyTokenURL = u
	t.Cleanup(func() { spotifyTokenURL = orig })
}

func TestRefreshTokenSuccessAndRotation(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got, _ = url.ParseQuery(string(b))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"rotated","expires_in":3600}`))
	}))
	t.Cleanup(srv.Close)
	withSpotifyTokenURL(t, srv.URL)

	tok, err := RefreshToken(context.Background(), "spotify", "cid", "", "old-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if tok.Access != "new-access" || tok.Refresh != "rotated" {
		t.Fatalf("tok=%+v", tok)
	}
	if time.Until(tok.Expiry) < 50*time.Minute {
		t.Fatalf("expiry %v", tok.Expiry)
	}
	if got.Get("grant_type") != "refresh_token" || got.Get("refresh_token") != "old-refresh" || got.Get("client_id") != "cid" {
		t.Fatalf("form=%v", got)
	}
	if got.Get("client_secret") != "" {
		t.Fatal("PKCE refresh should omit empty secret")
	}
}

func TestRefreshTokenKeepsRefreshWhenOmitted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"a2","expires_in":3600}`))
	}))
	t.Cleanup(srv.Close)
	withSpotifyTokenURL(t, srv.URL)

	tok, err := RefreshToken(context.Background(), "spotify", "cid", "secret", "rt")
	if err != nil {
		t.Fatal(err)
	}
	if tok.Access != "a2" || tok.Refresh != "" {
		t.Fatalf("tok=%+v", tok)
	}
}

func TestRefreshTokenInvalidGrant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Invalid refresh token"}`))
	}))
	t.Cleanup(srv.Close)
	withSpotifyTokenURL(t, srv.URL)

	_, err := RefreshToken(context.Background(), "spotify", "cid", "", "dead")
	if !errors.Is(err, ErrNeedsReconnect) {
		t.Fatalf("got %v", err)
	}
}

func TestRefreshToken5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"server_error"}`))
	}))
	t.Cleanup(srv.Close)
	withSpotifyTokenURL(t, srv.URL)

	_, err := RefreshToken(context.Background(), "spotify", "cid", "", "rt")
	if !errors.Is(err, ErrTemporary) {
		t.Fatalf("got %v", err)
	}
}

func TestRefreshSingleflightPerAccount(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		time.Sleep(80 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"shared","expires_in":3600}`))
	}))
	t.Cleanup(srv.Close)
	withSpotifyTokenURL(t, srv.URL)

	const same = "account-a"
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, err := refreshFlight.do(same, func() (Token, error) {
				return RefreshToken(context.Background(), "spotify", "cid", "", "rt")
			})
			if err != nil {
				errs <- err
				return
			}
			if tok.Access != "shared" {
				errs <- errors.New(tok.Access)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if n.Load() != 1 {
		t.Fatalf("http calls=%d want 1", n.Load())
	}

	n.Store(0)
	started := make(chan struct{})
	var once sync.Once
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		once.Do(func() { close(started) })
		<-started
		time.Sleep(40 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"x","expires_in":3600}`))
	}))
	t.Cleanup(srv2.Close)
	withSpotifyTokenURL(t, srv2.URL)

	var wg2 sync.WaitGroup
	wg2.Add(2)
	go func() {
		defer wg2.Done()
		_, err := refreshFlight.do("account-a", func() (Token, error) {
			return RefreshToken(context.Background(), "spotify", "cid", "", "rt")
		})
		if err != nil {
			t.Error(err)
		}
	}()
	go func() {
		defer wg2.Done()
		_, err := refreshFlight.do("account-b", func() (Token, error) {
			return RefreshToken(context.Background(), "spotify", "cid", "", "rt")
		})
		if err != nil {
			t.Error(err)
		}
	}()
	wg2.Wait()
	if n.Load() != 2 {
		t.Fatalf("distinct accounts http calls=%d want 2", n.Load())
	}
}

func TestReconnectVsTemporaryMessages(t *testing.T) {
	if reconnectMessage("spotify") == temporaryMessage("spotify") {
		t.Fatal("reconnect and temporary copy must differ")
	}
	if reconnectMessage("spotify") == "" || temporaryMessage("spotify") == "" {
		t.Fatal("empty message")
	}
}
