package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/playback"
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

func TestWritePlaybackConflict(t *testing.T) {
	cases := []struct {
		err  error
		code string
		ok   bool
	}{
		{playback.ErrBindConflict, "bind_conflict", true},
		{playback.ErrLeaseConflict, "lease_conflict", true},
		{playback.ErrCommandConflict, "command_conflict", true},
		{playback.ErrUndoStale, "undo_stale", true},
		{errNotInVoice, "", false},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		ok := writePlaybackConflict(rec, tc.err)
		if ok != tc.ok {
			t.Fatalf("%v ok=%v want %v", tc.err, ok, tc.ok)
		}
		if !tc.ok {
			continue
		}
		if rec.Code != 409 {
			t.Fatalf("%v status %d", tc.err, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), tc.code) {
			t.Fatalf("%v body %s", tc.err, rec.Body.String())
		}
	}
}

func TestExtraInt64Map(t *testing.T) {
	n, ok := extraInt64Map(map[string]any{"expected_binding_revision": float64(7)}, "expected_binding_revision")
	if !ok || n != 7 {
		t.Fatalf("got %d %v", n, ok)
	}
	n, ok = extraInt64Map(map[string]any{"generation": int64(3)}, "generation")
	if !ok || n != 3 {
		t.Fatalf("int64 %d %v", n, ok)
	}
}

func TestControlOutputPref(t *testing.T) {
	if controlOutputPref(map[string]any{"output_pref": "Browser"}) != "browser" {
		t.Fatal("output_pref")
	}
	if controlOutputPref(map[string]any{"pref": "discord"}) != "discord" {
		t.Fatal("pref")
	}
	if controlOutputPref(map[string]any{"renderer_kind": "browser"}) != "browser" {
		t.Fatal("renderer_kind")
	}
}

func TestUUIDFromQueue(t *testing.T) {
	id := uuid.MustParse("00000000-0000-4000-8000-0000000000aa")
	if uuidFromQueue(map[string]any{"playback_instance_id": id}, "playback_instance_id") != id {
		t.Fatal("uuid")
	}
	ptr := &id
	if uuidFromQueue(map[string]any{"playback_instance_id": ptr}, "playback_instance_id") != id {
		t.Fatal("ptr")
	}
	if uuidFromQueue(map[string]any{"playback_instance_id": id.String()}, "playback_instance_id") != id {
		t.Fatal("string")
	}
	if uuidFromQueue(nil, "playback_instance_id") != uuid.Nil {
		t.Fatal("nil map")
	}
	if stringFromQueue(map[string]any{"renderer_id": "lease-1"}, "renderer_id") != "lease-1" {
		t.Fatal("string")
	}
	rid := "lease-2"
	if stringFromQueue(map[string]any{"renderer_id": &rid}, "renderer_id") != "lease-2" {
		t.Fatal("string ptr")
	}
	if floatFromQueue(map[string]any{"playback_rate": 1.25}, "playback_rate") != 1.25 {
		t.Fatal("float")
	}
}
