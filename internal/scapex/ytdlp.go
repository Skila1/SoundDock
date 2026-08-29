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

type ytdlpPlaylist struct {
	ID            string            `json:"id"`
	Title         string            `json:"title"`
	PlaylistCount int               `json:"playlist_count"`
	Entries       []json.RawMessage `json:"entries"`
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
	if IsPlaylistQuery(query) {
		return false, PlaylistURL(query)
	}
	if src := WatchURL(query); src != "" {
		return true, src
	}
	if limit <= 0 {
		limit = 8
	}
	return false, "ytsearch" + strconv.Itoa(limit) + ":" + query
}

func (y *ytDLP) Search(ctx context.Context, query string, limit int) ([]Hit, error) {
	if IsPlaylistQuery(query) {
		listing, err := y.ListPlaylist(ctx, query, limit)
		return listing.Hits, err
	}
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

func (y *ytDLP) ListPlaylist(ctx context.Context, raw string, limit int) (PlaylistListing, error) {
	spec := PlaylistURL(raw)
	if spec == "" {
		return PlaylistListing{}, fmt.Errorf("not a YouTube playlist URL")
	}
	if limit <= 0 {
		limit = MaxPlaylistQueue
	}
	if limit > MaxPlaylistQueue {
		limit = MaxPlaylistQueue
	}
	args := []string{
		"--skip-download",
		"--no-warnings",
		"--no-progress",
		"--flat-playlist",
		"--yes-playlist",
		"--playlist-end", strconv.Itoa(limit),
		"-J", spec,
	}
	out, err := y.run(ctx, args...)
	if err != nil {
		return PlaylistListing{}, err
	}
	listing := parsePlaylistDump(out)
	if listing.ID == "" {
		listing.ID = PlaylistID(raw)
	}
	if listing.Total < len(listing.Hits) {
		listing.Total = len(listing.Hits)
	}
	if len(listing.Hits) >= limit && listing.Total > limit {
		listing.Truncated = true
	}
	return listing, nil
}

func parsePlaylistDump(raw []byte) PlaylistListing {
	raw = bytes.TrimSpace(raw)
	if i := bytes.IndexByte(raw, '{'); i > 0 {
		raw = raw[i:]
	}
	var dump ytdlpPlaylist
	if err := json.Unmarshal(raw, &dump); err != nil || len(dump.Entries) == 0 {
		hits := parseFlatHits(raw)
		return PlaylistListing{Hits: hits, Total: len(hits)}
	}
	hits := make([]Hit, 0, len(dump.Entries))
	for _, ent := range dump.Entries {
		ent = bytes.TrimSpace(ent)
		if len(ent) == 0 || ent[0] != '{' {
			continue
		}
		var row ytdlpFlat
		if json.Unmarshal(ent, &row) != nil || !videoIDRe.MatchString(row.ID) {
			continue
		}
		title := strings.TrimSpace(row.Title)
		if title == "[Deleted video]" || title == "[Private video]" || title == "[Unavailable]" {
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
			Title:      firstNonEmpty(title, row.ID),
			Artist:     artist,
			DurationMS: int(row.Duration * 1000),
			StreamURL:  watch,
			ArtworkURL: ytThumb(row.ID),
		})
	}
	total := dump.PlaylistCount
	if total < len(hits) {
		total = len(hits)
	}
	return PlaylistListing{
		ID:    dump.ID,
		Title: strings.TrimSpace(dump.Title),
		Hits:  hits,
		Total: total,
	}
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
	return y.FetchPolicy(ctx, mediaURL, destDir, DefaultMediaPolicy)
}

func (y *ytDLP) FetchPolicy(ctx context.Context, mediaURL, destDir, policy string) ([]LocalTrack, error) {
	src := WatchURL(mediaURL)
	vid := VideoID(src)
	if src == "" || vid == "" {
		return nil, fmt.Errorf("not an allowlisted YouTube watch URL or video id")
	}
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
	}
	args = append(args, FormatArgs(policy)...)
	args = append(args,
		"--embed-metadata",
		"--write-info-json",
		"--no-write-playlist-metafiles",
		"--no-playlist",
		"-o", filepath.Join(destDir, "%(id)s.%(ext)s"),
		src,
	)
	if _, err := y.run(ctx, args...); err != nil {
		return nil, err
	}
	return collectDownloads(destDir, vid)
}

var uploadExt = map[string]bool{
	".mp3": true, ".flac": true, ".aac": true, ".m4a": true, ".alac": true,
	".ogg": true, ".opus": true, ".wav": true, ".oga": true, ".aif": true, ".aiff": true,
}

func collectDownloads(dir string, videoIDs ...string) ([]LocalTrack, error) {
	if _, err := os.Stat(dir); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(videoIDs))
	seenID := map[string]bool{}
	for _, id := range videoIDs {
		id = strings.TrimSpace(id)
		if id == "" || seenID[id] {
			continue
		}
		seenID[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		matches, err := filepath.Glob(filepath.Join(dir, "*.info.json"))
		if err != nil {
			return nil, err
		}
		for _, p := range matches {
			base := filepath.Base(p)
			id := strings.TrimSuffix(base, ".info.json")
			if id == "" || id == base || seenID[id] {
				continue
			}
			seenID[id] = true
			ids = append(ids, id)
		}
	}
	infos := map[string]ytdlpInfo{}
	var files []string
	seenFile := map[string]bool{}
	addAudio := func(p string) {
		ext := strings.ToLower(filepath.Ext(p))
		if !uploadExt[ext] || seenFile[p] {
			return
		}
		seenFile[p] = true
		files = append(files, p)
	}
	if len(ids) > 0 {
		for _, id := range ids {
			raw, err := os.ReadFile(filepath.Join(dir, id+".info.json"))
			if err == nil {
				var inf ytdlpInfo
				if json.Unmarshal(raw, &inf) == nil && inf.ID != "" {
					infos[inf.ID] = inf
				}
			}
			matches, err := filepath.Glob(filepath.Join(dir, id+".*"))
			if err != nil {
				return nil, err
			}
			for _, p := range matches {
				addAudio(p)
			}
		}
	} else {
		for ext := range uploadExt {
			matches, err := filepath.Glob(filepath.Join(dir, "*"+ext))
			if err != nil {
				return nil, err
			}
			for _, p := range matches {
				addAudio(p)
			}
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
