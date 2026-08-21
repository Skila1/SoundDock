package metadata

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var numberedPrefix = regexp.MustCompile(`^\d{1,4}[\.\)]\s+`)

func ParseAudioName(name string) (artist, title string) {
	base := strings.TrimSpace(name)
	base = strings.ReplaceAll(base, "\\", "/")
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	base = stripAudioExt(base)
	base = strings.ReplaceAll(base, "_", " ")
	base = numberedPrefix.ReplaceAllString(base, "")
	base = strings.TrimSpace(base)
	if base == "" {
		return "", ""
	}
	if i := strings.Index(base, " - "); i > 0 {
		left := strings.TrimSpace(base[:i])
		right := strings.TrimSpace(base[i+3:])
		if left != "" && right != "" && !isAllDigits(left) {
			return left, right
		}
	}
	return "", base
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

func applyFilenameFallback(p *Probe, path string) {
	raw := stripAudioExt(filepath.Base(strings.ReplaceAll(path, "\\", "/")))
	if i := strings.LastIndex(raw, "/"); i >= 0 {
		raw = raw[i+1:]
	}
	artist, title := ParseAudioName(path)
	if title == "" {
		artist, title = ParseAudioName(p.Title)
	}
	if p.Title == "" || p.Title == raw || looksNumberedTitle(p.Title) {
		if title != "" {
			p.Title = title
		}
	}
	if p.Artist == "" && artist != "" {
		p.Artist = artist
	}
	if p.AlbumArtist == "" && artist != "" {
		p.AlbumArtist = artist
	}
}
