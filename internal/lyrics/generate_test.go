package lyrics

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/sounddock/sounddock/internal/whisper"
)

func TestSegmentsToLRCPlainAndWords(t *testing.T) {
	got := SegmentsToLRC([]Segment{
		{StartMS: 1500, Text: " [Music] "},
		{StartMS: 2250, Text: " Hello there"},
		{StartMS: 65010, Text: " Sing it, now", Words: []Word{
			{Tms: 65010, Text: " Sing"}, {Tms: 65400, Text: " it"}, {Tms: 65600, Text: ","}, {Tms: 66000, Text: " now"},
		}},
		{StartMS: 70000, Text: " ♪ "},
	})
	want := "[00:02.25]Hello there\n[01:05.01]<01:05.01>Sing <01:05.40>it, <01:06.00>now"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	lines := ParseLines(got)
	if len(lines) != 2 || lines[1].Tms != 65010 || len(lines[1].Words) != 3 || lines[1].Words[1].Text != "it," {
		t.Fatalf("parsed %+v", lines)
	}
}

func TestSegmentsToLRCEmpty(t *testing.T) {
	if got := SegmentsToLRC([]Segment{{Text: "(applause)"}, {Text: "  "}}); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestWhisperWordsGroupIntoLines(t *testing.T) {
	seg := func(text string, start, end float64) whisper.Segment {
		return whisper.Segment{Text: text, Start: start, End: end}
	}
	words := fromWhisper([]whisper.Segment{
		seg("", 5.09, 5.19),
		seg(" Walking", 5.14, 5.56), seg(" down", 5.56, 5.82), seg(" the", 5.82, 6.02),
		seg(" road", 6.35, 6.62), seg(" tonight.", 6.62, 7.1),
		seg(" [Music]", 7.2, 7.9),
		seg(" Sing", 16.86, 17.2), seg(" it", 17.2, 17.4), seg(" louder", 17.4, 17.8),
		seg(" no", 19.0, 19.2), seg(" stop", 19.2, 19.5),
	})
	got := SegmentsToLRC(groupLines(words))
	want := "[00:05.14]<00:05.14>Walking <00:05.56>down <00:05.82>the <00:06.35>road <00:06.62>tonight.\n" +
		"[00:16.86]<00:16.86>Sing <00:17.20>it <00:17.40>louder\n" +
		"[00:19.00]<00:19.00>no <00:19.20>stop"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestGroupLinesBreaksAtCommaAndLength(t *testing.T) {
	var in []timedWord
	for i, w := range strings.Fields("one two three four, five six seven eight nine ten eleven twelve") {
		in = append(in, timedWord{start: i * 300, end: i*300 + 250, text: w})
	}
	lines := groupLines(in)
	if len(lines) != 2 || lines[0].Text != "one two three four," || len(lines[1].Words) != 8 {
		t.Fatalf("%+v", lines)
	}
}

func TestGeneratedLyricsShowWithoutLRCLIB(t *testing.T) {
	s := &Service{
		listFn: func(_ context.Context, _ uuid.UUID) ([]Result, error) {
			return []Result{{Source: SourceWhisper, Body: "[00:01.00]hi", Timed: true}}, nil
		},
		urlFn: func(context.Context) string { return "" },
	}
	if got := s.GetLyrics(t.Context(), Meta{Title: "a", Artist: "b"}); got.Source != SourceWhisper {
		t.Fatalf("got %+v", got)
	}
}

func TestNormalizeLanguage(t *testing.T) {
	for in, want := range map[string]string{"": "auto", "EN": "en", "auto": "auto", "e1": "auto", "english": "auto", "haw": "haw"} {
		if got := NormalizeLanguage(in); got != want {
			t.Fatalf("%q: got %q want %q", in, got, want)
		}
	}
}
