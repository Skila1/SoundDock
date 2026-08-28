package search

import (
	"strings"
)

var searchStop = map[string]bool{
	"a": true, "an": true, "and": true, "the": true, "of": true, "or": true,
	"to": true, "in": true, "on": true, "for": true, "is": true, "it": true,
	"la": true, "el": true, "de": true, "le": true, "da": true, "di": true,
	"du": true, "des": true, "los": true, "las": true, "un": true, "une": true,
	"feat": true, "ft": true, "vs": true, "with": true,
}

// SignificantTokens are the words a library hit must contain (title, artist, or album).
// Short articles are dropped when a longer word is present so "dominga la mave" does not
// match a track that merely shares a few trigrams with "mave".
func SignificantTokens(q string) []string {
	var long, short, all []string
	seen := map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(q)) {
		w = strings.Trim(w, `"'.,!?:;()[]{}`)
		if w == "" {
			continue
		}
		if seen[w] {
			continue
		}
		seen[w] = true
		all = append(all, w)
		if searchStop[w] {
			continue
		}
		n := 0
		for range w {
			n++
		}
		if n >= 3 {
			long = append(long, w)
		} else if n >= 2 {
			short = append(short, w)
		}
	}
	if len(long) > 0 {
		return long
	}
	if len(short) > 0 {
		return short
	}
	return all
}

func likeContains(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return "%" + s + "%"
}
