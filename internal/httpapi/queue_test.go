package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestRequestDeviceID(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*http.Request)
		extra map[string]any
		want  string
	}{
		{
			name: "extra",
			setup: func(r *http.Request) {
			},
			extra: map[string]any{"device_id": "from-extra"},
			want:  "from-extra",
		},
		{
			name: "query",
			setup: func(r *http.Request) {
				q := r.URL.Query()
				q.Set("device_id", "from-query")
				r.URL.RawQuery = q.Encode()
			},
			want: "from-query",
		},
		{
			name: "header",
			setup: func(r *http.Request) {
				r.Header.Set("X-Device-ID", "from-header")
			},
			want: "from-header",
		},
		{
			name: "cookie",
			setup: func(r *http.Request) {
				r.AddCookie(&http.Cookie{Name: "sd_device", Value: "from-cookie"})
			},
			want: "from-cookie",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/v1/me/queue", nil)
			tc.setup(r)
			if got := requestDeviceID(r, tc.extra); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestRequestPlaybackTarget(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*http.Request)
		extra      map[string]any
		bodyTarget string
		want       string
	}{
		{name: "default empty", setup: func(*http.Request) {}, want: ""},
		{
			name: "query",
			setup: func(r *http.Request) {
				q := r.URL.Query()
				q.Set("target", "Discord")
				r.URL.RawQuery = q.Encode()
			},
			want: "discord",
		},
		{
			name: "header",
			setup: func(r *http.Request) {
				r.Header.Set("X-Playback-Target", " discord ")
			},
			want: "discord",
		},
		{
			name: "body wins over query",
			setup: func(r *http.Request) {
				q := r.URL.Query()
				q.Set("target", "web")
				r.URL.RawQuery = q.Encode()
			},
			bodyTarget: "discord",
			want:       "discord",
		},
		{
			name:       "extra wins",
			setup:      func(*http.Request) {},
			extra:      map[string]any{"target": "discord"},
			bodyTarget: "web",
			want:       "discord",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/v1/me/queue", nil)
			tc.setup(r)
			if got := requestPlaybackTarget(r, tc.extra, tc.bodyTarget); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestTrackIDFromQueue(t *testing.T) {
	id := uuid.MustParse("00000000-0000-4000-8000-000000000050")
	if got := trackIDFromQueue(map[string]any{"current_track_id": id}); got != id {
		t.Fatal("uuid")
	}
	p := &id
	if got := trackIDFromQueue(map[string]any{"current_track_id": p}); got != id {
		t.Fatal("pointer")
	}
	if got := trackIDFromQueue(map[string]any{"current_track_id": (*uuid.UUID)(nil)}); got != uuid.Nil {
		t.Fatal("nil pointer")
	}
}

func TestAsString(t *testing.T) {
	id := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	if asString(id) != id.String() {
		t.Fatal("uuid")
	}
	if asString("x") != "x" {
		t.Fatal("string")
	}
}
