package scapex

import (
	"sort"
	"strings"
	"unicode"
)

// Quality modifiers penalized unless the query asked for them.
var qualityModifiers = []string{
	"live", "remix", "slowed", "reverb", "nightcore", "instrumental", "karaoke", "cover",
}

func tokenizeQuery(q string) map[string]struct{} {
	out := map[string]struct{}{}
	var b strings.Builder
	flush := func() {
		w := strings.ToLower(strings.TrimSpace(b.String()))
		b.Reset()
		if w != "" {
			out[w] = struct{}{}
		}
	}
	for _, r := range q {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		flush()
	}
	flush()
	return out
}

func titleHasModifier(title, mod string) bool {
	_, ok := tokenizeQuery(title)[strings.ToLower(mod)]
	return ok
}

// HitPenalty is added to the original search index. Lower is better.
func HitPenalty(query, title, artist string) int {
	want := tokenizeQuery(query)
	blob := title + " " + artist
	pen := 0
	for _, m := range qualityModifiers {
		if _, asked := want[m]; asked {
			continue
		}
		if titleHasModifier(blob, m) {
			pen += 100
		}
	}
	return pen
}

// RankHits sorts a search page so unrequested live/remix/slowed/etc. results
// sink. Original provider order is the tie-break (stable).
func RankHits(query string, hits []Hit) []Hit {
	if len(hits) < 2 {
		return hits
	}
	type row struct {
		hit   Hit
		score int
		idx   int
	}
	rows := make([]row, len(hits))
	for i, h := range hits {
		rows[i] = row{hit: h, score: i + HitPenalty(query, h.Title, h.Artist), idx: i}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].score != rows[j].score {
			return rows[i].score < rows[j].score
		}
		return rows[i].idx < rows[j].idx
	})
	out := make([]Hit, len(rows))
	for i, r := range rows {
		out[i] = r.hit
	}
	return out
}
