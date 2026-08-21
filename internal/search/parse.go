package search

import (
	"strings"
	"unicode"
)

type Query struct {
	Text   string
	Artist string
	Title  string
	Album  string
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
