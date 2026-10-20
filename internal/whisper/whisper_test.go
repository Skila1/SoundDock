package whisper

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewRejectsBadURL(t *testing.T) {
	for _, raw := range []string{"", "whisper:8080", "ftp://x", "http://"} {
		if _, err := New(raw); err == nil {
			t.Fatalf("New(%q) accepted", raw)
		}
	}
	c, err := New("http://whisper:8080/")
	if err != nil || c.URL() != "http://whisper:8080" {
		t.Fatalf("%v %v", c, err)
	}
}

func TestHealth(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("path %s", r.URL.Path)
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	c, _ := New(srv.URL)
	if err := c.Health(t.Context()); err != nil {
		t.Fatal(err)
	}
	status = http.StatusServiceUnavailable
	if err := c.Health(t.Context()); err != ErrLoading {
		t.Fatalf("want ErrLoading, got %v", err)
	}
}

func TestTranscribeSendsFormAndParses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/inference" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if got := r.FormValue("response_format"); got != "verbose_json" {
			t.Errorf("response_format %q", got)
		}
		if got := r.FormValue("language"); got != "auto" {
			t.Errorf("language %q", got)
		}
		if got := r.FormValue("prompt"); got != "Song lyrics" {
			t.Errorf("prompt %q", got)
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(f)
		if string(b) != "RIFF-audio" {
			t.Errorf("file %q", b)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"task":"transcribe","language":"english","duration":3.5,"text":" hi there",
			"segments":[{"id":0,"text":" hi there","start":0.5,"end":2.25,
			"words":[{"word":" hi","start":0.5,"end":1.0,"probability":0.9},{"word":" there","start":1.0,"end":2.25,"probability":0.8}]}]}`)
	}))
	defer srv.Close()
	c, _ := New(srv.URL)
	res, err := c.Transcribe(t.Context(), strings.NewReader("RIFF-audio"), Options{Prompt: "Song lyrics"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Language != "english" || len(res.Segments) != 1 || res.Segments[0].Start != 0.5 || len(res.Segments[0].Words) != 2 {
		t.Fatalf("%+v", res)
	}
}

func TestTranscribeReportsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"failed to read audio data"}`)
	}))
	defer srv.Close()
	c, _ := New(srv.URL)
	_, err := c.Transcribe(t.Context(), strings.NewReader("x"), Options{})
	if err == nil || !strings.Contains(err.Error(), "failed to read audio data") {
		t.Fatalf("got %v", err)
	}
}
