package lyrics

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	lrcStampRe = regexp.MustCompile(`\[(\d{1,2}):(\d{2})(?:[\.:](\d{1,3}))?]`)
	wordStampRe = regexp.MustCompile(`<(\d{1,2}):(\d{2})(?:[\.:](\d{1,3}))?>`)
)

type lrcCue struct {
	start int
	end   int
	tms   int
	word  bool
}

// ParseLines extracts synced cues from an LRC body. Unstamped lines are skipped.
// Enhanced LRC (<mm:ss.xx>word) and A2 ([mm:ss]syl[mm:ss]la) become word timestamps.
// Plain line-timed LRC gets interpolated word times across each line span.
func ParseLines(body string) []Line {
	var out []Line
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		cues := collectCues(line)
		if len(cues) == 0 {
			continue
		}
		text := strings.Join(strings.Fields(lrcStampRe.ReplaceAllString(wordStampRe.ReplaceAllString(line, ""), "")), " ")
		if text == "" {
			continue
		}
		words := wordsFromCues(line, cues)
		lineStamps := lineStampTimes(cues, line)
		if len(lineStamps) == 0 {
			continue
		}
		for _, tms := range lineStamps {
			out = append(out, Line{Tms: tms, Text: text, Words: cloneWords(words)})
		}
	}
	attachWords(out)
	return out
}

func collectCues(line string) []lrcCue {
	var cues []lrcCue
	for _, loc := range lrcStampRe.FindAllStringSubmatchIndex(line, -1) {
		tms, ok := stampMS(line[loc[0]:loc[1]], lrcStampRe)
		if !ok {
			continue
		}
		cues = append(cues, lrcCue{start: loc[0], end: loc[1], tms: tms})
	}
	for _, loc := range wordStampRe.FindAllStringSubmatchIndex(line, -1) {
		tms, ok := stampMS(line[loc[0]:loc[1]], wordStampRe)
		if !ok {
			continue
		}
		cues = append(cues, lrcCue{start: loc[0], end: loc[1], tms: tms, word: true})
	}
	if len(cues) == 0 {
		return nil
	}
	for i := 0; i < len(cues); i++ {
		for j := i + 1; j < len(cues); j++ {
			if cues[j].start < cues[i].start {
				cues[i], cues[j] = cues[j], cues[i]
			}
		}
	}
	return cues
}

func stampMS(raw string, re *regexp.Regexp) (int, bool) {
	m := re.FindStringSubmatch(raw)
	if len(m) < 3 {
		return 0, false
	}
	min, _ := strconv.Atoi(m[1])
	sec, _ := strconv.Atoi(m[2])
	frac := 0
	if len(m) > 3 && m[3] != "" {
		digits := m[3]
		switch len(digits) {
		case 1:
			frac, _ = strconv.Atoi(digits)
			frac *= 100
		case 2:
			frac, _ = strconv.Atoi(digits)
			frac *= 10
		default:
			if len(digits) > 3 {
				digits = digits[:3]
			}
			frac, _ = strconv.Atoi(digits)
		}
	}
	return min*60000 + sec*1000 + frac, true
}

func lineStampTimes(cues []lrcCue, line string) []int {
	var stamps []int
	hasWord := false
	for _, c := range cues {
		if c.word {
			hasWord = true
			continue
		}
		stamps = append(stamps, c.tms)
	}
	if len(stamps) == 0 {
		return []int{cues[0].tms}
	}
	if hasWord || !clusteredLineStamps(cues, line) {
		return stamps[:1]
	}
	return stamps
}

func clusteredLineStamps(cues []lrcCue, line string) bool {
	var lineCues []lrcCue
	for _, c := range cues {
		if !c.word {
			lineCues = append(lineCues, c)
		}
	}
	if len(lineCues) < 2 {
		return true
	}
	for i := 0; i < len(lineCues)-1; i++ {
		gap := strings.TrimSpace(line[lineCues[i].end:lineCues[i+1].start])
		if gap != "" {
			return false
		}
	}
	return true
}

func wordsFromCues(line string, cues []lrcCue) []Word {
	if len(cues) < 2 {
		return nil
	}
	var words []Word
	for i, c := range cues {
		from := c.end
		to := len(line)
		if i+1 < len(cues) {
			to = cues[i+1].start
		}
		text := strings.TrimSpace(line[from:to])
		if text == "" {
			continue
		}
		words = append(words, Word{Tms: c.tms, Text: text})
	}
	if len(words) < 2 {
		return nil
	}
	return words
}

func attachWords(lines []Line) {
	for i := range lines {
		if len(lines[i].Words) > 0 {
			continue
		}
		end := lines[i].Tms + 4000
		if i+1 < len(lines) && lines[i+1].Tms > lines[i].Tms {
			end = lines[i+1].Tms
		}
		lines[i].Words = interpolateWords(lines[i].Text, lines[i].Tms, end)
	}
}

func interpolateWords(text string, start, end int) []Word {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return nil
	}
	if end <= start {
		end = start + 1000*len(parts)
	}
	weights := make([]int, len(parts))
	total := 0
	for i, p := range parts {
		n := utf8.RuneCountInString(p)
		if n < 1 {
			n = 1
		}
		weights[i] = n
		total += n
	}
	out := make([]Word, 0, len(parts))
	acc := 0
	span := end - start
	for i, p := range parts {
		out = append(out, Word{Tms: start + span*acc/total, Text: p})
		acc += weights[i]
	}
	return out
}

func cloneWords(in []Word) []Word {
	if len(in) == 0 {
		return nil
	}
	out := make([]Word, len(in))
	copy(out, in)
	return out
}
