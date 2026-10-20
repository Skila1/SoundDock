package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/storage"
	"github.com/sounddock/sounddock/internal/transcode"
)

// GenerateJob is the job type that transcribes a track's audio into lyrics.
const GenerateJob = "lyrics.generate"

// GeneratePayload names the track to transcribe.
type GeneratePayload struct {
	TrackID uuid.UUID `json:"track_id"`
}

// Segment is one stretch of recognised speech. Times are milliseconds from
// the start of the track.
type Segment struct {
	StartMS int
	EndMS   int
	Text    string
	Words   []Word
}

// TranscribeOptions tunes one transcription request.
type TranscribeOptions struct {
	Language string // ISO 639-1 code, or "" / "auto" to detect
	Prompt   string // optional hint, e.g. the song title and artist
}

// Transcriber turns 16 kHz mono WAV audio into timed segments. The whisper
// package implements it against a whisper.cpp server.
type Transcriber interface {
	Transcribe(ctx context.Context, wav io.Reader, opts TranscribeOptions) ([]Segment, error)
}

// Generator runs lyrics.generate jobs.
type Generator struct {
	pool    *pgxpool.Pool
	tr      Transcriber
	getProv func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error)

	// decode is swapped in tests; it streams 16 kHz mono WAV for a track file.
	decode func(ctx context.Context, src string) (io.ReadCloser, error)
}

// NewGenerator builds a Generator. tr may be nil when no whisper.cpp server
// is configured; jobs then fail with a clear message.
func NewGenerator(pool *pgxpool.Pool, tr Transcriber, getProv func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error)) *Generator {
	return &Generator{pool: pool, tr: tr, getProv: getProv, decode: decodeWAV}
}

// ErrGeneratorOff means lyrics generation is switched off or not set up.
var ErrGeneratorOff = errors.New("lyrics generation is not set up")

// Handler transcribes the track and stores the result with source "whisper".
// Hand-edited lyrics and locked lyrics are never replaced.
func (g *Generator) Handler() jobs.Handler {
	return func(ctx context.Context, job jobs.Job) error {
		var p GeneratePayload
		if err := json.Unmarshal(job.Payload, &p); err != nil || p.TrackID == uuid.Nil {
			return fmt.Errorf("%w: bad payload", jobs.ErrTerminal)
		}
		if g.tr == nil || !LoadConfig(ctx, g.pool).GenerateEnabled {
			return fmt.Errorf("%w: %v", jobs.ErrTerminal, ErrGeneratorOff)
		}
		if !transcode.FFmpegAvailable() {
			return fmt.Errorf("%w: ffmpeg is not installed", jobs.ErrTerminal)
		}
		svc := New(g.pool, nil)
		if svc.lyricsLocked(ctx, p.TrackID) {
			return nil
		}
		meta, src, cleanup, err := g.trackSource(ctx, p.TrackID)
		if err != nil {
			return err
		}
		defer cleanup()
		wav, err := g.decode(ctx, src)
		if err != nil {
			return fmt.Errorf("decode audio: %w", err)
		}
		defer wav.Close()
		cfg := LoadConfig(ctx, g.pool)
		segs, err := g.tr.Transcribe(ctx, wav, TranscribeOptions{
			Language: cfg.GenerateLanguage,
			Prompt:   promptFor(meta),
		})
		if err != nil {
			return fmt.Errorf("whisper.cpp: %w", err)
		}
		body := SegmentsToLRC(segs)
		if body == "" {
			// No singing was recognised. Store nothing so the track can be
			// retried later with a different model or language.
			return fmt.Errorf("%w: no lyrics were recognised in this track", jobs.ErrTerminal)
		}
		return svc.saveProvider(ctx, p.TrackID, SourceWhisper, body, true)
	}
}

func promptFor(meta Meta) string {
	if meta.Title == "" {
		return ""
	}
	if meta.Artist == "" {
		return "Song lyrics: " + meta.Title + "."
	}
	return "Song lyrics: " + meta.Title + " by " + meta.Artist + "."
}

// trackSource finds the track's primary file and returns a local path ffmpeg
// can read, plus a cleanup func that is always safe to call.
func (g *Generator) trackSource(ctx context.Context, trackID uuid.UUID) (Meta, string, func(), error) {
	noop := func() {}
	if g.pool == nil || g.getProv == nil {
		return Meta{}, "", noop, fmt.Errorf("%w: no database", jobs.ErrTerminal)
	}
	var meta Meta
	var key string
	var lib uuid.UUID
	err := g.pool.QueryRow(ctx, `
		SELECT t.title,
		       coalesce((
		         SELECT string_agg(a.name, ', ' ORDER BY ta.position)
		         FROM track_artists ta JOIN artists a ON a.id=ta.artist_id
		         WHERE ta.track_id=t.id
		       ), ''),
		       f.storage_key, f.library_id
		FROM tracks t
		JOIN track_files f ON f.track_id=t.id AND f.deleted_at IS NULL
		WHERE t.id=$1
		ORDER BY (f.quality='original') DESC, f.created_at
		LIMIT 1`, trackID).Scan(&meta.Title, &meta.Artist, &key, &lib)
	if err != nil {
		return Meta{}, "", noop, fmt.Errorf("%w: track has no audio file", jobs.ErrTerminal)
	}
	meta.TrackID = trackID
	prov, _, _, err := g.getProv(ctx, lib)
	if err != nil {
		return Meta{}, "", noop, err
	}
	path, cleanup, err := storage.LocalPath(ctx, prov, key, "sd-lyrics-*")
	if err != nil {
		return Meta{}, "", noop, err
	}
	return meta, path, cleanup, nil
}

// decodeWAV streams the track as 16 kHz mono 16-bit WAV, the input
// whisper.cpp expects.
func decodeWAV(ctx context.Context, src string) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error",
		"-i", src, "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-f", "wav", "pipe:1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &cmdReader{ReadCloser: out, cmd: cmd, stderr: &stderr}, nil
}

type cmdReader struct {
	io.ReadCloser
	cmd    *exec.Cmd
	stderr *strings.Builder
	done   bool
}

func (c *cmdReader) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	if err == io.EOF && !c.done {
		c.done = true
		if werr := c.cmd.Wait(); werr != nil {
			return n, fmt.Errorf("ffmpeg: %v %s", werr, strings.TrimSpace(c.stderr.String()))
		}
	}
	return n, err
}

func (c *cmdReader) Close() error {
	_ = c.ReadCloser.Close()
	if !c.done {
		c.done = true
		_ = c.cmd.Wait()
	}
	return nil
}

// SegmentsToLRC writes segments as LRC. Segments with word timings become
// enhanced LRC (<mm:ss.xx>word) so the player can highlight each word.
// Blank segments and whisper's non-speech markers like "[Music]" are dropped.
func SegmentsToLRC(segs []Segment) string {
	var b strings.Builder
	for _, s := range segs {
		text := cleanSegmentText(s.Text)
		if text == "" {
			continue
		}
		b.WriteString(lrcStamp('[', ']', s.StartMS))
		words := cleanWords(s.Words)
		if len(words) >= 2 {
			for i, w := range words {
				if i > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(lrcStamp('<', '>', w.Tms))
				b.WriteString(w.Text)
			}
		} else {
			b.WriteString(text)
		}
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func cleanWords(in []Word) []Word {
	var out []Word
	for _, w := range in {
		t := strings.TrimSpace(w.Text)
		if t == "" || isNoiseMarker(t) {
			continue
		}
		// whisper.cpp reports tokens, not words: a token that does not start
		// with a space continues the previous word ("sing" + "ing", "you" + ",").
		if len(out) > 0 && !strings.HasPrefix(w.Text, " ") {
			out[len(out)-1].Text += t
			continue
		}
		out = append(out, Word{Tms: w.Tms, Text: t})
	}
	return out
}

func cleanSegmentText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" || isNoiseMarker(s) {
		return ""
	}
	return s
}

// isNoiseMarker matches whisper's annotations such as [Music], (applause)
// or a lone ♪.
func isNoiseMarker(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	if (strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]")) || (strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")")) {
		return true
	}
	return strings.Trim(s, "♪♫ .-*") == ""
}

func lrcStamp(open, close byte, ms int) string {
	if ms < 0 {
		ms = 0
	}
	return fmt.Sprintf("%c%02d:%02d.%02d%c", open, ms/60000, ms/1000%60, ms%1000/10, close)
}
