package lyrics

import (
	"context"
	"io"
	"math"
	"strings"

	"github.com/sounddock/sounddock/internal/whisper"
)

// WhisperTranscriber adapts a whisper.cpp server client to Transcriber.
//
// It asks for one word per segment (max_len=1, split_on_word) and groups the
// words into lyric lines itself. With the server's voice activity detection
// on, instrumental stretches are skipped (so whisper does not invent lines
// over them) and per-segment times are the accurate ones.
type WhisperTranscriber struct {
	Client *whisper.Client
}

// Transcribe sends the audio to whisper.cpp and converts its segments.
func (w WhisperTranscriber) Transcribe(ctx context.Context, wav io.Reader, opts TranscribeOptions) ([]Segment, error) {
	res, err := w.Client.Transcribe(ctx, wav, whisper.Options{
		Language:          opts.Language,
		Prompt:            opts.Prompt,
		SuppressNonSpeech: true,
		MaxLen:            1,
		SplitOnWord:       true,
	})
	if err != nil {
		return nil, err
	}
	return groupLines(fromWhisper(res.Segments)), nil
}

type timedWord struct {
	start, end int
	text       string
}

// fromWhisper turns whisper segments into timed words. A segment usually
// holds one word; a longer one (from a server that ignores max_len) is kept
// as a single unit at its start time.
func fromWhisper(in []whisper.Segment) []timedWord {
	var out []timedWord
	for _, s := range in {
		text := strings.Join(strings.Fields(s.Text), " ")
		if text == "" || isNoiseMarker(text) {
			continue
		}
		out = append(out, timedWord{start: secMS(s.Start), end: secMS(s.End), text: text})
	}
	return out
}

const (
	lineGapMS    = 800 // a pause this long starts a new line
	lineMaxWords = 10
	commaBreakAt = 4 // break at a comma once a line has this many words
)

// groupLines builds lyric lines from timed words. Lines end at sentence
// punctuation, at a comma in a line that is already long enough, at a long
// pause, or at lineMaxWords.
func groupLines(words []timedWord) []Segment {
	var out []Segment
	var cur []Word
	prevEnd := 0
	flush := func() {
		if len(cur) == 0 {
			return
		}
		texts := make([]string, len(cur))
		for i, w := range cur {
			texts[i] = strings.TrimSpace(w.Text)
		}
		line := Segment{StartMS: cur[0].Tms, Text: strings.Join(texts, " "), Words: cur}
		if len(cur) == 1 {
			line.Words = nil
		}
		out = append(out, line)
		cur = nil
	}
	for _, tw := range words {
		if len(cur) > 0 && tw.start-prevEnd >= lineGapMS {
			flush()
		}
		prevEnd = tw.end
		// Each word after the first gets a leading space so SegmentsToLRC
		// treats it as a new word rather than a continuation.
		w := Word{Tms: tw.start, Text: tw.text}
		if len(cur) > 0 {
			w.Text = " " + w.Text
		}
		cur = append(cur, w)
		t := tw.text
		switch {
		case strings.HasSuffix(t, ".") || strings.HasSuffix(t, "!") || strings.HasSuffix(t, "?"):
			flush()
		case strings.HasSuffix(t, ",") && len(cur) >= commaBreakAt:
			flush()
		case len(cur) >= lineMaxWords:
			flush()
		}
	}
	flush()
	return out
}

func secMS(sec float64) int {
	if sec <= 0 || math.IsNaN(sec) {
		return 0
	}
	return int(math.Round(sec * 1000))
}
