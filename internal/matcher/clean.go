package matcher

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Spotify appends version info as " - <suffix>". Acoustic, remix and mix
// suffixes are kept because they name a different recording.
var versionSuffixRe = regexp.MustCompile(`(?i)^(` +
	`(\d{4}\s+)?(digital(ly)?\s+)?re-?master(ed)?(\s+version)?(\s+\d{4})?(\s+version)?` +
	`|(single|album|mono|stereo|original|radio|lp|45|7"?|clean|explicit|remastered|\d{4})\s+(version|edit|mix)` +
	`|mono|stereo|radio edit|single|edit|explicit|clean` +
	`|live(\s.*)?|recorded\s.*|from\s.*|bonus(\s+track)?(\s+version)?` +
	`|feat\.?\s.*|ft\.?\s.*|featuring\s.*` +
	`)$`)

var bracketRe = regexp.MustCompile(`\([^()]*\)|\[[^\[\]]*\]|\{[^{}]*\}`)
var featTailRe = regexp.MustCompile(`(?i)(^|\s)(feat\.?|ft\.|featuring)\s.*$`)

var foldTable = map[rune]string{
	'ø': "o", 'Ø': "o", 'æ': "ae", 'Æ': "ae", 'œ': "oe", 'Œ': "oe", 'ß': "ss",
	'đ': "d", 'Đ': "d", 'ł': "l", 'Ł': "l", 'ı': "i", 'þ': "th", 'ð': "d",
}

// BaseTitle drops Spotify version suffixes, bracketed text and featured
// artists, keeping the original casing. Used to build search queries.
func BaseTitle(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), " – ", " - ")
	parts := strings.Split(s, " - ")
	kept := parts[:1]
	for _, p := range parts[1:] {
		if !versionSuffixRe.MatchString(strings.TrimSpace(p)) {
			kept = append(kept, p)
		}
	}
	s = strings.Join(kept, " - ")
	for {
		next := bracketRe.ReplaceAllString(s, " ")
		if next == s {
			break
		}
		s = next
	}
	s = featTailRe.ReplaceAllString(s, "")
	s = strings.Join(strings.Fields(s), " ")
	return strings.Trim(s, " -")
}

// Fold lowercases, strips accents, turns "&" into "and" and punctuation into
// spaces. Apostrophes vanish so "Don't" and "Dont" agree.
func Fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r == '\'' || r == '’' || r == '`' || r == '´':
		case r == '&':
			b.WriteString(" and ")
		case foldTable[r] != "":
			b.WriteString(foldTable[r])
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// CleanTitle is the comparison form of a song title. If everything is in
// brackets the bracketed text is used instead of returning nothing.
func CleanTitle(s string) string {
	out := Fold(BaseTitle(s))
	if out == "" {
		out = Fold(s)
	}
	return out
}

// Words returns the clean title tokens, with "pt" spelled out as "part".
func Words(s string) []string {
	w := strings.Fields(CleanTitle(s))
	for i := range w {
		if w[i] == "pt" {
			w[i] = "part"
		}
	}
	return w
}

// CleanArtist folds an artist or channel name, dropping " - Topic", a
// trailing "VEVO" and a leading "the".
func CleanArtist(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, " - Topic")
	if len(s) > 4 && strings.EqualFold(s[len(s)-4:], "vevo") {
		s = s[:len(s)-4]
	}
	s = Fold(s)
	s = strings.TrimPrefix(s, "the ")
	return s
}
