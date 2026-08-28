package lyrics

import (
	"regexp"
	"strconv"
	"strings"
)

var lrcStampRe = regexp.MustCompile(`\[(\d{1,2}):(\d{2})(?:[\.:](\d{1,3}))?]`)

// ParseLines extracts synced cues from an LRC body. Unstamped lines are skipped.
func ParseLines(body string) []Line {
	var out []Line
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		stamps := lrcStampRe.FindAllStringSubmatchIndex(line, -1)
		if len(stamps) == 0 {
			continue
		}
		text := strings.TrimSpace(lrcStampRe.ReplaceAllString(line, ""))
		for _, loc := range stamps {
			m := lrcStampRe.FindStringSubmatch(line[loc[0]:loc[1]])
			if len(m) < 3 {
				continue
			}
			min, _ := strconv.Atoi(m[1])
			sec, _ := strconv.Atoi(m[2])
			frac := 0
			if m[3] != "" {
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
			out = append(out, Line{
				Tms:  min*60000 + sec*1000 + frac,
				Text: text,
			})
		}
	}
	return out
}
