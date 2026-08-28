package scapex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type ytDLP struct {
	bin     string
	cookies string
	browser string
}

type ytdlpFlat struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Uploader   string  `json:"uploader"`
	Channel    string  `json:"channel"`
	Artist     string  `json:"artist"`
	Duration   float64 `json:"duration"`
	WebpageURL string  `json:"webpage_url"`
}

type ytdlpInfo struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Track      string  `json:"track"`
	Artist     string  `json:"artist"`
	Album      string  `json:"album"`
	Uploader   string  `json:"uploader"`
	Channel    string  `json:"channel"`
	Duration   float64 `json:"duration"`
	WebpageURL string  `json:"webpage_url"`
}

func (y *ytDLP) resolve() (string, error) {
	if y.bin != "" {
		p, err := exec.LookPath(y.bin)
		if err != nil {
			return "", fmt.Errorf("yt-dlp binary %q not found: %w", y.bin, err)
		}
		return p, nil
	}
	p, err := exec.LookPath("yt-dlp")
	if err != nil {
		return "", fmt.Errorf("yt-dlp not found on PATH; install yt-dlp and ffmpeg to search or download from YouTube")
	}
	return p, nil
}

func (y *ytDLP) extra() []string {
	var a []string
	if y.cookies != "" {
		a = append(a, "--cookies", y.cookies)
	}
	if y.browser != "" {
		a = append(a, "--cookies-from-browser", y.browser)
	}
	return a
}

func (y *ytDLP) run(ctx context.Context, args ...string) ([]byte, error) {
	bin, err := y.resolve()
	if err != nil {
		return nil, err
	}
	all := append(y.extra(), args...)
	cmd := exec.CommandContext(ctx, bin, all...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if len(msg) > 1200 {
			msg = msg[len(msg)-1200:]
		}
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("yt-dlp: %s", msg)
	}
	return stdout.Bytes(), nil
}

func searchTarget(query string, limit int) (lookup bool, spec string) {
	if src := WatchURL(query); src != "" {
		return true, src
	}
	if limit <= 0 {
		limit = 8
	}
	return false, "ytsearch" + strconv.Itoa(limit) + ":" + query
}

func (y *ytDLP) Search(ctx context.Context, query string, limit int) ([]Hit, error) {
	lookup, spec := searchTarget(query, limit)
	args := []string{"--skip-download", "--no-warnings", "--no-progress", "-j"}
	if lookup {
		args = append(args, "--no-playlist", spec)
	} else {
		args = append(args, "--flat-playlist", spec)
	}
	raw, err := y.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	if lookup {
		if hit, ok := parseInfoHit(raw); ok {
			return []Hit{hit}, nil
		}
	}
	return parseFlatHits(raw), nil
}

func parseInfoHit(raw []byte) (Hit, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return Hit{}, false
	}
	// yt-dlp -j on a watch URL is one object. Playlists or extra logs may be NDJSON.
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var inf ytdlpInfo
		if err := json.Unmarshal(line, &inf); err != nil || inf.ID == "" {
			continue
		}
		return hitFromInfo(inf), true
	}
	return Hit{}, false
}

func hitFromInfo(inf ytdlpInfo) Hit {
	id := inf.ID
	watch := inf.WebpageURL
	if watch == "" {
		watch = "https://www.youtube.com/watch?v=" + id
	}
	return Hit{
		Type:       "youtube",
		ID:         id,
		Title:      firstNonEmpty(inf.Track, inf.Title),
		Artist:     firstNonEmpty(inf.Artist, inf.Uploader, inf.Channel),
		Album:      inf.Album,
		DurationMS: int(inf.Duration * 1000),
		StreamURL:  watch,
		ArtworkURL: ytThumb(id),
	}
}

func parseFlatHits(raw []byte) []Hit {
	var hits []Hit
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] != '{' {
			continue
		}
		var row ytdlpFlat
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if row.ID == "" {
			continue
		}
		artist := firstNonEmpty(row.Artist, row.Uploader, row.Channel)
		watch := row.WebpageURL
		if watch == "" {
			watch = "https://www.youtube.com/watch?v=" + row.ID
		}
		hits = append(hits, Hit{
			Type:       "youtube",
			ID:         row.ID,
			Title:      row.Title,
			Artist:     artist,
			DurationMS: int(row.Duration * 1000),
			StreamURL:  watch,
			ArtworkURL: ytThumb(row.ID),
		})
	}
	return hits
}

func (y *ytDLP) Fetch(ctx context.Context, mediaURL, destDir string) ([]LocalTrack, error) {
	if destDir == "" {
		return nil, fmt.Errorf("dest dir required")
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}
	args := []string{
		"--no-warnings",
		"--no-progress",
		"--newline",
		"-x",
		"--audio-format", "m4a",
		"--audio-quality", "0",
		"--embed-metadata",
		"--write-info-json",
		"--no-write-playlist-metafiles",
		"-o", filepath.Join(destDir, "%(id)s.%(ext)s"),
	}
	if !isYouTubePlaylist(mediaURL) {
		args = append(args, "--no-playlist")
	}
	args = append(args, mediaURL)
	if _, err := y.run(ctx, args...); err != nil {
		return nil, err
	}
	return collectDownloads(destDir)
}

var uploadExt = map[string]bool{
	".mp3": true, ".flac": true, ".aac": true, ".m4a": true, ".alac": true,
	".ogg": true, ".opus": true, ".wav": true, ".oga": true, ".aif": true, ".aiff": true,
}

func collectDownloads(dir string) ([]LocalTrack, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	infos := map[string]ytdlpInfo{}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext == ".json" && strings.HasSuffix(strings.ToLower(name), ".info.json") {
			raw, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			var inf ytdlpInfo
			if json.Unmarshal(raw, &inf) != nil || inf.ID == "" {
				continue
			}
			infos[inf.ID] = inf
			continue
		}
		if uploadExt[ext] {
			files = append(files, filepath.Join(dir, name))
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no audio files after yt-dlp (is ffmpeg installed?)")
	}
	out := make([]LocalTrack, 0, len(files))
	for _, p := range files {
		id := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		inf := infos[id]
		title := firstNonEmpty(inf.Track, inf.Title, id)
		artist := firstNonEmpty(inf.Artist, inf.Uploader, inf.Channel)
		src := inf.WebpageURL
		if src == "" && inf.ID != "" {
			src = "https://www.youtube.com/watch?v=" + inf.ID
		}
		vid := inf.ID
		if vid == "" {
			vid = id
		}
		out = append(out, LocalTrack{
			Path:       p,
			Title:      title,
			Artist:     artist,
			Album:      inf.Album,
			VideoID:    vid,
			SourceURL:  src,
			DurationMS: int(inf.Duration * 1000),
		})
	}
	return out, nil
}

func getenv(k string) string { return strings.TrimSpace(os.Getenv(k)) }

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
