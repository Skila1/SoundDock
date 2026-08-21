package metadata

import (
	"regexp"
	"strings"
	"unicode"
)

var numberedPrefix = regexp.MustCompile(`^\d{1,4}[\.\)]\s+`)

func ParseAudioName(name string) (artist, title string) {
	left, right, ok := splitArtistTitle(name)
	if !ok {
		base := displayBase(name)
		if base == "" || LooksLikeHash(base) {
			return "", ""
		}
		return "", base
	}
	return OrientParts(left, right, Probe{}, nil)
}

func displayBase(name string) string {
	base := strings.TrimSpace(name)
	base = strings.ReplaceAll(base, "\\", "/")
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	base = stripAudioExt(base)
	base = strings.ReplaceAll(base, "_", " ")
	base = strings.ReplaceAll(base, "\u2013", "-")
	base = strings.ReplaceAll(base, "\u2014", "-")
	base = strings.ReplaceAll(base, "  ", " ")
	base = numberedPrefix.ReplaceAllString(base, "")
	return strings.TrimSpace(base)
}

func splitArtistTitle(name string) (left, right string, ok bool) {
	base := displayBase(name)
	if base == "" || LooksLikeHash(base) {
		return "", "", false
	}
	if i := strings.Index(base, " - "); i > 0 {
		left = strings.TrimSpace(base[:i])
		right = strings.TrimSpace(base[i+3:])
		if left != "" && right != "" && !isAllDigits(left) {
			return left, right, true
		}
	}
	return "", base, false
}

func hasRealTag(s string) bool {
	s = strings.TrimSpace(s)
	return s != "" && !LooksLikeHash(s) && !looksNumberedTitle(s)
}

func namesEqual(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// OrientParts decides which side is artist vs title.
// Embedded tags win. Then a name already in the library. Then Artist - Title.
func OrientParts(left, right string, tagged Probe, known func(string) bool) (artist, title string) {
	if hasRealTag(tagged.Title) && hasRealTag(tagged.Artist) {
		return tagged.Artist, tagged.Title
	}
	if hasRealTag(tagged.Title) {
		if namesEqual(tagged.Title, left) {
			return right, left
		}
		if namesEqual(tagged.Title, right) {
			return left, right
		}
	}
	if hasRealTag(tagged.Artist) {
		if namesEqual(tagged.Artist, left) {
			return left, right
		}
		if namesEqual(tagged.Artist, right) {
			return right, left
		}
	}
	if known != nil {
		kl, kr := known(left), known(right)
		if kl && !kr {
			return left, right
		}
		if kr && !kl {
			return right, left
		}
	}
	if looksLikeSongTitle(left) && !looksLikeSongTitle(right) {
		return right, left
	}
	if looksLikeSongTitle(right) && !looksLikeSongTitle(left) {
		return left, right
	}
	return left, right
}

func looksLikeSongTitle(s string) bool {
	lower := strings.ToLower(s)
	for _, tok := range []string{
		" remix", "(remix", " remaster", "(remaster", " live)", "(live",
		" feat.", " feat ", " ft.", " featuring ", " acoustic", " radio edit",
		" version)", " mix)",
	} {
		if strings.Contains(lower, tok) {
			return true
		}
	}
	return false
}

func stripAudioExt(name string) string {
	lower := strings.ToLower(name)
	for _, ext := range []string{".mp3", ".flac", ".aac", ".m4a", ".alac", ".ogg", ".opus", ".wav", ".oga", ".aif", ".aiff"} {
		if strings.HasSuffix(lower, ext) {
			return name[:len(name)-len(ext)]
		}
	}
	return name
}

func looksNumberedTitle(s string) bool {
	return numberedPrefix.MatchString(strings.TrimSpace(s))
}

func LooksLikeHash(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "\\", "/")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	s = stripAudioExt(s)
	if len(s) < 32 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func applyFilenameFallback(p *Probe, known func(string) bool, names ...string) {
	extra := append([]string{}, names...)
	if p.Title != "" {
		extra = append(extra, p.Title)
	}

	if LooksLikeHash(p.Title) || looksNumberedTitle(p.Title) {
		p.Title = ""
	} else {
		p.Title = strings.TrimSpace(numberedPrefix.ReplaceAllString(p.Title, ""))
	}
	if LooksLikeHash(p.Artist) || looksNumberedTitle(p.Artist) {
		p.Artist = ""
	}
	if hasRealTag(p.Title) && !hasRealTag(p.Artist) {
		if _, _, ok := splitArtistTitle(p.Title); ok {
			extra = append([]string{p.Title}, extra...)
			p.Title = ""
		}
	}

	tagged := *p
	if hasRealTag(p.Title) && hasRealTag(p.Artist) {
		if p.AlbumArtist == "" {
			p.AlbumArtist = p.Artist
		}
		return
	}

	var left, right, loneTitle string
	var splitOK bool
	for _, n := range extra {
		if strings.TrimSpace(n) == "" {
			continue
		}
		if l, r, ok := splitArtistTitle(n); ok {
			left, right, splitOK = l, r, true
			break
		}
		if loneTitle == "" {
			base := displayBase(n)
			if base != "" && !LooksLikeHash(base) {
				loneTitle = base
			}
		}
	}
	if splitOK {
		artist, title := OrientParts(left, right, tagged, known)
		if !hasRealTag(p.Title) {
			p.Title = title
		}
		if !hasRealTag(p.Artist) {
			p.Artist = artist
		}
	} else if !hasRealTag(p.Title) && loneTitle != "" {
		p.Title = loneTitle
	}
	if p.AlbumArtist == "" && p.Artist != "" {
		p.AlbumArtist = p.Artist
	}
}

func ApplyOriginalName(p *Probe, originalName string) {
	ApplyOriginalNameKnown(p, originalName, nil)
}

func ApplyOriginalNameKnown(p *Probe, originalName string, known func(string) bool) {
	if p == nil {
		return
	}
	applyFilenameFallback(p, known, originalName)
}
