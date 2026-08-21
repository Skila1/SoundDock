package metadata

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Probe struct {
	Title               string
	Artist              string
	AlbumArtist         string
	Album               string
	Genre               string
	Year                int
	Track               int
	TrackTotal          int
	Disc                int
	DiscTotal           int
	Composer            string
	Comment             string
	Lyrics              string
	Explicit            *bool
	DurationMS          int
	Codec               string
	Container           string
	Bitrate             int
	SampleRate          int
	Channels            int
	BitDepth            int
	Picture             []byte
	PictureMIME         string
	ReplayGainTrack     *float64
	ReplayGainAlbum     *float64
	ReplayGainTrackPeak *float64
	ReplayGainAlbumPeak *float64
	EncoderDelay        int
	EncoderPadding      int
	MBID                string
	Source              string
	Confidence          float64
}

var timedLyricRe = regexp.MustCompile(`\[\d{1,2}:\d{2}(?:[\.:]\d{1,3})?]`)

func LyricsTimed(body string) bool {
	return timedLyricRe.MatchString(body)
}

func FromFile(path string) (Probe, error) {
	p := Probe{Container: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")}
	ff, err := ffprobe(path)
	if err == nil {
		p = ff
		p.Container = strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	}
	if p.Title == "" || LooksLikeHash(p.Title) {
		p.Title = ""
	}
	applyFilenameFallback(&p, nil, path)
	if p.Disc == 0 {
		p.Disc = 1
	}
	if p.Source == "" {
		if hasRealTag(p.Title) && hasRealTag(p.Artist) {
			p.Source = "embedded"
			p.Confidence = 0.9
		} else if hasRealTag(p.Title) || hasRealTag(p.Artist) {
			p.Source = "filename"
			p.Confidence = 0.6
		} else {
			p.Source = "filename"
			p.Confidence = 0.3
		}
	}
	if len(p.Picture) == 0 {
		p.Picture, p.PictureMIME = extractAttachedPic(path)
	}
	return p, nil
}

func ffprobe(path string) (Probe, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-show_format", "-show_streams", "-print_format", "json", path)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return Probe{}, err
	}
	var raw struct {
		Format struct {
			Duration string            `json:"duration"`
			BitRate  string            `json:"bit_rate"`
			Tags     map[string]string `json:"tags"`
		} `json:"format"`
		Streams []struct {
			CodecType     string            `json:"codec_type"`
			CodecName     string            `json:"codec_name"`
			SampleRate    string            `json:"sample_rate"`
			Channels      int               `json:"channels"`
			BitsPerRaw    *int              `json:"bits_per_raw_sample,omitempty"`
			BitsPerSample *int              `json:"bits_per_sample,omitempty"`
			Disposition   map[string]int    `json:"disposition"`
			Tags          map[string]string `json:"tags"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		return Probe{}, err
	}
	p := Probe{}
	if d, err := strconv.ParseFloat(raw.Format.Duration, 64); err == nil {
		p.DurationMS = int(d * 1000)
	}
	if b, err := strconv.Atoi(raw.Format.BitRate); err == nil {
		p.Bitrate = b
	}
	for _, s := range raw.Streams {
		if s.CodecType == "audio" {
			p.Codec = s.CodecName
			p.Channels = s.Channels
			if sr, err := strconv.Atoi(s.SampleRate); err == nil {
				p.SampleRate = sr
			}
			if s.BitsPerRaw != nil {
				p.BitDepth = *s.BitsPerRaw
			} else if s.BitsPerSample != nil {
				p.BitDepth = *s.BitsPerSample
			}
			applyTags(&p, s.Tags)
			break
		}
	}
	applyTags(&p, raw.Format.Tags)
	return p, nil
}

func applyTags(p *Probe, tags map[string]string) {
	if p == nil || len(tags) == 0 {
		return
	}
	norm := map[string]string{}
	for k, v := range tags {
		norm[strings.ToLower(k)] = v
	}
	set := func(dst *string, keys ...string) {
		if dst == nil || *dst != "" {
			return
		}
		for _, k := range keys {
			if v := strings.TrimSpace(norm[k]); v != "" {
				*dst = v
				return
			}
		}
	}
	set(&p.Title, "title")
	set(&p.Artist, "artist")
	set(&p.AlbumArtist, "album_artist", "albumartist")
	set(&p.Album, "album")
	set(&p.Genre, "genre")
	set(&p.Composer, "composer")
	set(&p.Comment, "comment")
	set(&p.Lyrics, "lyrics", "unsyncedlyrics", "lyrics-eng", "lyrics:eng")
	set(&p.MBID, "musicbrainz_trackid", "musicbrainz track id", "ufid")
	if p.Year == 0 {
		if y, err := strconv.Atoi(norm["date"]); err == nil {
			p.Year = y
		} else if y, err := strconv.Atoi(norm["year"]); err == nil {
			p.Year = y
		} else if len(norm["date"]) >= 4 {
			if y, err := strconv.Atoi(norm["date"][:4]); err == nil {
				p.Year = y
			}
		}
	}
	if p.Track == 0 {
		if tr := norm["track"]; tr != "" {
			parts := strings.Split(tr, "/")
			p.Track, _ = strconv.Atoi(parts[0])
			if len(parts) > 1 {
				p.TrackTotal, _ = strconv.Atoi(parts[1])
			}
		}
	}
	if p.Disc == 0 {
		if d := norm["disc"]; d != "" {
			parts := strings.Split(d, "/")
			p.Disc, _ = strconv.Atoi(parts[0])
			if len(parts) > 1 {
				p.DiscTotal, _ = strconv.Atoi(parts[1])
			}
		}
	}
	if p.ReplayGainTrack == nil {
		p.ReplayGainTrack = parseGain(norm["replaygain_track_gain"])
	}
	if p.ReplayGainAlbum == nil {
		p.ReplayGainAlbum = parseGain(norm["replaygain_album_gain"])
	}
	if p.ReplayGainTrackPeak == nil {
		p.ReplayGainTrackPeak = parsePeak(norm["replaygain_track_peak"])
	}
	if p.ReplayGainAlbumPeak == nil {
		p.ReplayGainAlbumPeak = parsePeak(norm["replaygain_album_peak"])
	}
	if p.EncoderDelay == 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(norm["encoder_delay"])); err == nil {
			p.EncoderDelay = n
		}
	}
	if p.EncoderPadding == 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(norm["encoder_padding"])); err == nil {
			p.EncoderPadding = n
		}
	}
	if delay, pad, ok := parseITunSMPB(norm["itunes_smpb"], norm["itunsmpb"]); ok {
		if p.EncoderDelay == 0 {
			p.EncoderDelay = delay
		}
		if p.EncoderPadding == 0 {
			p.EncoderPadding = pad
		}
	}
}

func parseGain(s string) *float64 {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), " dB"))
	s = strings.TrimSuffix(s, "dB")
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	g, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &g
}

func parsePeak(s string) *float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	g, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &g
}

func parseITunSMPB(vals ...string) (delay, padding int, ok bool) {
	for _, raw := range vals {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		fields := strings.Fields(raw)
		if len(fields) < 3 {
			continue
		}
		d, err1 := strconv.ParseUint(fields[1], 16, 32)
		p, err2 := strconv.ParseUint(fields[2], 16, 32)
		if err1 != nil || err2 != nil {
			continue
		}
		return int(d), int(p), true
	}
	return 0, 0, false
}

func extractAttachedPic(path string) ([]byte, string) {
	tmp, err := os.CreateTemp("", "sd-pic-*.jpg")
	if err != nil {
		return nil, ""
	}
	dst := tmp.Name()
	tmp.Close()
	defer os.Remove(dst)
	cmd := exec.Command("ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-i", path, "-an", "-map", "0:v:0", "-c:v", "mjpeg", "-frames:v", "1", dst)
	if err := cmd.Run(); err != nil {
		return nil, ""
	}
	b, err := os.ReadFile(dst)
	if err != nil || len(b) < 32 {
		return nil, ""
	}
	mime := "image/jpeg"
	if bytes.HasPrefix(b, []byte{0x89, 0x50, 0x4e, 0x47}) {
		mime = "image/png"
	}
	return b, mime
}

func mergeProbe(dst *Probe, src Probe) {
	if dst.DurationMS == 0 {
		dst.DurationMS = src.DurationMS
	}
	if dst.Codec == "" {
		dst.Codec = src.Codec
	}
	if dst.Bitrate == 0 {
		dst.Bitrate = src.Bitrate
	}
	if dst.SampleRate == 0 {
		dst.SampleRate = src.SampleRate
	}
	if dst.Channels == 0 {
		dst.Channels = src.Channels
	}
	if dst.BitDepth == 0 {
		dst.BitDepth = src.BitDepth
	}
	if dst.ReplayGainTrack == nil {
		dst.ReplayGainTrack = src.ReplayGainTrack
	}
	if dst.ReplayGainAlbum == nil {
		dst.ReplayGainAlbum = src.ReplayGainAlbum
	}
	if dst.ReplayGainTrackPeak == nil {
		dst.ReplayGainTrackPeak = src.ReplayGainTrackPeak
	}
	if dst.ReplayGainAlbumPeak == nil {
		dst.ReplayGainAlbumPeak = src.ReplayGainAlbumPeak
	}
	if len(dst.Picture) == 0 {
		dst.Picture = src.Picture
		dst.PictureMIME = src.PictureMIME
	}
}
