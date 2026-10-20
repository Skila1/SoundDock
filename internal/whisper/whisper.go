// Package whisper is a small client for the HTTP server that ships with
// whisper.cpp (https://github.com/ggml-org/whisper.cpp, MIT licence).
//
// SoundDock does not link whisper.cpp. The server runs as its own container
// and this package only speaks its documented API: GET /health and
// POST /inference. It depends on the standard library alone, so it can be
// lifted out and reused or offered upstream as is.
package whisper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client talks to one whisper.cpp server.
type Client struct {
	base string
	http *http.Client
}

// New returns a client for baseURL (for example http://whisper:8080).
// Transcription can take minutes on a CPU, so the HTTP client has no overall
// timeout; callers bound each request with their context.
func New(baseURL string) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("whisper: invalid server URL %q", baseURL)
	}
	return &Client{base: baseURL, http: &http.Client{}}, nil
}

// URL is the server address the client was built with.
func (c *Client) URL() string { return c.base }

// ErrLoading means the server is up but still loading its model.
var ErrLoading = errors.New("whisper: model is still loading")

// Health reports whether the server is ready to transcribe.
func (c *Client) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/health", nil)
	if err != nil {
		return err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	switch res.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusServiceUnavailable:
		return ErrLoading
	default:
		return fmt.Errorf("whisper: health returned status %d", res.StatusCode)
	}
}

// Options tunes one transcription.
type Options struct {
	// Language is an ISO 639-1 code such as "en", or "" / "auto" to detect it.
	Language string
	// Prompt is optional context that steers spelling, such as a song title.
	Prompt string
	// Temperature is the sampling temperature; 0 is the most deterministic.
	Temperature float64
	// SuppressNonSpeech drops tokens like "[Music]" during decoding.
	SuppressNonSpeech bool
	// MaxLen caps segment length in characters. 1 with SplitOnWord gives one
	// word per segment, which is the most reliable way to get word timings
	// (segment times are also the ones whisper.cpp maps back correctly when
	// the server runs with --vad). 0 keeps the server default.
	MaxLen      int
	SplitOnWord bool
}

// Word is one timed token. whisper.cpp reports tokens rather than whole
// words: a token without a leading space continues the previous word.
type Word struct {
	Word        string  `json:"word"`
	Start       float64 `json:"start"` // seconds
	End         float64 `json:"end"`   // seconds
	Probability float64 `json:"probability"`
}

// Segment is one stretch of recognised speech.
type Segment struct {
	ID           int     `json:"id"`
	Text         string  `json:"text"`
	Start        float64 `json:"start"` // seconds
	End          float64 `json:"end"`   // seconds
	Words        []Word  `json:"words"`
	AvgLogprob   float64 `json:"avg_logprob"`
	NoSpeechProb float64 `json:"no_speech_prob"`
}

// Result is the server's verbose_json response.
type Result struct {
	Task     string    `json:"task"`
	Language string    `json:"language"`
	Duration float64   `json:"duration"` // seconds of audio
	Text     string    `json:"text"`
	Segments []Segment `json:"segments"`
}

const maxResponse = 16 << 20

// Transcribe uploads audio (16 kHz mono 16-bit WAV is the safe input; other
// formats need the server's --convert flag) and returns timed segments.
func (c *Client) Transcribe(ctx context.Context, audio io.Reader, opts Options) (*Result, error) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		pw.CloseWithError(writeForm(mw, audio, opts))
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/inference", pr)
	if err != nil {
		pr.Close()
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		pr.Close()
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponse))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("whisper: inference returned status %d: %s", res.StatusCode, serverError(body))
	}
	var out Result
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("whisper: unreadable response: %w", err)
	}
	return &out, nil
}

func writeForm(mw *multipart.Writer, audio io.Reader, opts Options) error {
	lang := strings.TrimSpace(opts.Language)
	if lang == "" {
		lang = "auto"
	}
	fields := [][2]string{
		{"response_format", "verbose_json"},
		{"language", lang},
		{"temperature", strconv.FormatFloat(opts.Temperature, 'f', -1, 64)},
		{"token_timestamps", "true"},
	}
	if opts.MaxLen > 0 {
		fields = append(fields, [2]string{"max_len", strconv.Itoa(opts.MaxLen)})
	}
	if opts.SplitOnWord {
		fields = append(fields, [2]string{"split_on_word", "true"})
	}
	if opts.SuppressNonSpeech {
		fields = append(fields, [2]string{"suppress_non_speech", "true"})
	}
	if p := strings.TrimSpace(opts.Prompt); p != "" {
		fields = append(fields, [2]string{"prompt", p})
	}
	for _, f := range fields {
		if err := mw.WriteField(f[0], f[1]); err != nil {
			return err
		}
	}
	part, err := mw.CreateFormFile("file", "audio.wav")
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, audio); err != nil {
		return err
	}
	return mw.Close()
}

func serverError(body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return e.Error
	}
	s := strings.TrimSpace(string(body))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
