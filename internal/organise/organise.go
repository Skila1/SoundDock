package organise

import (
	"fmt"
	"path"
	"strings"
)

type Vars struct {
	AlbumArtist string
	Artist      string
	Album       string
	Year        int
	Disc        int
	DiscCount   int
	Track       int
	Title       string
	Ext         string
	Edition     string
}

func Apply(tmpl string, v Vars) string {
	if tmpl == "" {
		tmpl = "{album_artist}/{album} ({year})/{disc}{track} - {title}.{ext}"
	}
	aa := strings.TrimSpace(v.AlbumArtist)
	if aa == "" {
		aa = v.Artist
	}
	aa = safe(aa)
	disc := ""
	if v.DiscCount > 1 {
		disc = fmt.Sprintf("%d-", v.Disc)
	}
	repl := map[string]string{
		"{album_artist}": aa,
		"{artist}":       safe(v.Artist),
		"{album}":        safe(v.Album),
		"{year}":         year(v.Year),
		"{disc}":         disc,
		"{track}":        fmt.Sprintf("%02d", v.Track),
		"{title}":        safe(v.Title),
		"{ext}":          strings.TrimPrefix(v.Ext, "."),
		"{edition}":      safe(v.Edition),
	}
	out := tmpl
	for k, val := range repl {
		out = strings.ReplaceAll(out, k, val)
	}
	out = strings.ReplaceAll(out, "\\", "/")
	return path.Clean(out)
}

func year(y int) string {
	if y <= 0 {
		return "0000"
	}
	return fmt.Sprintf("%04d", y)
}

func safe(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Unknown"
	}
	repl := strings.NewReplacer("/", "-", "\\", "-", ":", " -", "*", "", "?", "", "\"", "", "<", "", ">", "", "|", "-")
	return strings.TrimSpace(repl.Replace(s))
}
