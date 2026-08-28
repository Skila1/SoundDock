package lyrics

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/sounddock/sounddock/internal/metadata"
)

func (s *Service) externalOn(ctx context.Context) bool {
	if s == nil {
		return false
	}
	if s.urlFn != nil {
		return strings.TrimSpace(s.urlFn(ctx)) != ""
	}
	if s.pool == nil {
		return false
	}
	return LoadConfig(ctx, s.pool).ExternalEnabled
}

func (s *Service) localOn(ctx context.Context) bool {
	if s == nil {
		return true
	}
	if s.pool == nil {
		return true
	}
	return LoadConfig(ctx, s.pool).LocalEnabled
}

func (s *Service) lookupLocal(meta Meta) Result {
	if s == nil || strings.TrimSpace(s.localDir) == "" {
		return Result{}
	}
	artist := slugLyrics(meta.Artist)
	title := slugLyrics(meta.Title)
	if artist == "" || title == "" {
		return Result{}
	}
	candidates := []string{
		filepath.Join(s.localDir, artist, title+".lrc"),
		filepath.Join(s.localDir, artist, title+".txt"),
		filepath.Join(s.localDir, artist+" - "+title+".lrc"),
		filepath.Join(s.localDir, artist+" - "+title+".txt"),
	}
	for _, p := range candidates {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		body := strings.TrimSpace(string(b))
		if body == "" {
			continue
		}
		return Result{Body: body, Timed: metadata.LyricsTimed(body), Source: SourceLocal}
	}
	return Result{}
}

func slugLyrics(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '-' || r == '_' || r == '.':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
