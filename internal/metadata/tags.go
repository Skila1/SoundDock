package metadata

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type Probe struct {
	Title           string
	Artist          string
	AlbumArtist     string
	Album           string
	Genre           string
	Year            int
	Track           int
	TrackTotal      int
	Disc            int
	DiscTotal       int
	Composer        string
	Comment         string
	Lyrics          string
	Explicit        *bool
	DurationMS      int
	Codec           string
	Container       string
	Bitrate         int
	SampleRate      int
	Channels        int
	BitDepth        int
	Picture         []byte
	PictureMIME     string
	ReplayGainTrack *float64
	ReplayGainAlbum *float64
	MBID            string
}

func FromFile(path string) (Probe, error) {
	p := Probe{Container: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")}
	ff, err := ffprobe(path)
	if err == nil {
		p = ff
		p.Container = strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	}
	if p.Title == "" {
		base := filepath.Base(path)
		p.Title = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if p.Disc == 0 {
		p.Disc = 1
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
			CodecType  string `json:"codec_type"`
			CodecName  string `json:"codec_name"`
			SampleRate string `json:"sample_rate"`
			Channels   int    `json:"channels"`
			BitsPerRaw *int   `json:"bits_per_raw_sample,omitempty"`
			Tags       map[string]string `json:"tags"`
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
			}
			break
		}
	}
	tags := map[string]string{}
	for k, v := range raw.Format.Tags {
		tags[strings.ToLower(k)] = v
	}
	p.Title = tags["title"]
	p.Artist = tags["artist"]
	p.AlbumArtist = tags["album_artist"]
	if p.AlbumArtist == "" {
		p.AlbumArtist = tags["albumartist"]
	}
	p.Album = tags["album"]
	p.Genre = tags["genre"]
	p.Composer = tags["composer"]
	p.Comment = tags["comment"]
	p.Lyrics = tags["lyrics"]
	p.MBID = tags["musicbrainz_trackid"]
	if y, err := strconv.Atoi(tags["date"]); err == nil {
		p.Year = y
	} else if y, err := strconv.Atoi(tags["year"]); err == nil {
		p.Year = y
	} else if len(tags["date"]) >= 4 {
		if y, err := strconv.Atoi(tags["date"][:4]); err == nil {
			p.Year = y
		}
	}
	if tr := tags["track"]; tr != "" {
		p.Track, _ = strconv.Atoi(strings.Split(tr, "/")[0])
	}
	if d := tags["disc"]; d != "" {
		p.Disc, _ = strconv.Atoi(strings.Split(d, "/")[0])
	}
	if g, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(tags["replaygain_track_gain"], " dB")), 64); err == nil {
		p.ReplayGainTrack = &g
	}
	if g, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(tags["replaygain_album_gain"], " dB")), 64); err == nil {
		p.ReplayGainAlbum = &g
	}
	return p, nil
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
}
