package search

import (
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Query struct {
	Text             string
	Artist           string
	Title            string
	Album            string
	NeverPlayed      bool
	HasPlayed        bool
	LastPlayedWithin time.Duration
	LastPlayedBefore time.Duration
	LastPlayedAfter  *time.Time
}

func Parse(q string) Query {
	q = strings.TrimSpace(q)
	out := Query{}
	var rest []string
	i := 0
	runes := []rune(q)
	for i < len(runes) {
		for i < len(runes) && unicode.IsSpace(runes[i]) {
			i++
		}
		if i >= len(runes) {
			break
		}
		field, ok := matchField(runes, i)
		if ok {
			i = field.next
			switch field.name {
			case "artist":
				out.Artist = field.value
			case "title":
				out.Title = field.value
			case "album":
				out.Album = field.value
			case "played":
				applyPlayed(&out, field.value)
			case "never_played", "neverplayed":
				if truthy(field.value) || field.value == "" {
					out.NeverPlayed = true
				}
			case "last_played", "lastplayed":
				applyLastPlayed(&out, field.value)
			default:
				rest = append(rest, field.raw)
			}
			continue
		}
		word, next := readToken(runes, i)
		rest = append(rest, word)
		i = next
	}
	out.Text = strings.TrimSpace(strings.Join(rest, " "))
	return out
}

func applyPlayed(out *Query, val string) {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "never", "no", "false", "0", "unplayed":
		out.NeverPlayed = true
	case "yes", "true", "1", "played":
		out.HasPlayed = true
	}
}

func applyLastPlayed(out *Query, val string) {
	val = strings.TrimSpace(val)
	if val == "" {
		return
	}
	if t, err := time.Parse("2006-01-02", val); err == nil {
		out.LastPlayedAfter = &t
		return
	}
	older := false
	if strings.HasPrefix(val, ">") {
		older = true
		val = strings.TrimPrefix(val, ">")
	}
	d, ok := parseLen(val)
	if !ok {
		return
	}
	if older {
		out.LastPlayedBefore = d
	} else {
		out.LastPlayedWithin = d
	}
}

func parseLen(v string) (time.Duration, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	if len(v) < 2 {
		return 0, false
	}
	u := v[len(v)-1]
	n, err := strconv.Atoi(v[:len(v)-1])
	if err != nil || n < 0 {
		return 0, false
	}
	switch u {
	case 'h':
		return time.Duration(n) * time.Hour, true
	case 'd':
		return time.Duration(n) * 24 * time.Hour, true
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, true
	case 'm':
		return time.Duration(n) * 30 * 24 * time.Hour, true
	case 'y':
		return time.Duration(n) * 365 * 24 * time.Hour, true
	}
	return 0, false
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "1", "true", "yes", "on":
		return true
	}
	return false
}

type fieldTok struct {
	name, value, raw string
	next             int
}

func matchField(r []rune, i int) (fieldTok, bool) {
	start := i
	j := i
	for j < len(r) && (unicode.IsLetter(r[j]) || r[j] == '_') {
		j++
	}
	if j >= len(r) || r[j] != ':' {
		return fieldTok{}, false
	}
	name := strings.ToLower(string(r[i:j]))
	j++
	val, next := readToken(r, j)
	return fieldTok{name: name, value: val, raw: string(r[start:next]), next: next}, true
}

func readToken(r []rune, i int) (string, int) {
	if i < len(r) && (r[i] == '"' || r[i] == '\'') {
		q := r[i]
		i++
		start := i
		for i < len(r) && r[i] != q {
			i++
		}
		val := string(r[start:i])
		if i < len(r) {
			i++
		}
		return val, i
	}
	start := i
	for i < len(r) && !unicode.IsSpace(r[i]) {
		i++
	}
	return string(r[start:i]), i
}
