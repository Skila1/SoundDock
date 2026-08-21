package transcode

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
)

var flacSem = make(chan struct{}, 2)

const (
	PresetStandard    = "standard"
	PresetHigh        = "high"
	PresetFast        = "fast"
	QualityOriginal   = "original"
	QualityCompressed = "compressed"
)

type StoreOpts struct {
	KeepOriginal bool
	Preset       string
}

type StoreResult struct {
	Path          string
	StoreName     string
	OriginalSize  int64
	StoredSize    int64
	Compressed    bool
	CompanionPath string
	CompanionName string
	CompanionSize int64
}

func NormalizePreset(preset string) string {
	switch strings.ToLower(strings.TrimSpace(preset)) {
	case PresetHigh:
		return PresetHigh
	case PresetFast, "low":
		return PresetFast
	default:
		return PresetStandard
	}
}

func FLACCompressionLevel(preset string) string {
	switch NormalizePreset(preset) {
	case PresetHigh:
		return "12"
	case PresetFast:
		return "1"
	default:
		return "5"
	}
}

func Savings(original, stored int64) (saved int64, ratio float64) {
	if original <= 0 {
		return 0, 0
	}
	saved = original - stored
	if saved < 0 {
		saved = 0
	}
	ratio = float64(saved) / float64(original)
	return saved, ratio
}

func ReplaceExt(name, ext string) string {
	ext = strings.ToLower(ext)
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return strings.TrimSuffix(name, path.Ext(name)) + ext
}

func NeedsCompress(name, codec string) bool {
	codec = strings.ToLower(strings.TrimSpace(codec))
	ext := strings.ToLower(path.Ext(strings.TrimSpace(name)))
	switch ext {
	case ".flac", ".mp3", ".aac", ".ogg", ".opus", ".oga", ".alac":
		return strings.HasPrefix(codec, "pcm")
	case ".m4a":
		return strings.HasPrefix(codec, "pcm")
	case ".wav", ".aif", ".aiff", ".pcm":
		return true
	}
	if strings.HasPrefix(codec, "pcm") {
		return true
	}
	return false
}

func CompressToFLAC(ctx context.Context, src string) (string, error) {
	return CompressToFLACPreset(ctx, src, PresetStandard)
}

func CompressToFLACPreset(ctx context.Context, src, preset string) (string, error) {
	if src == "" {
		return "", fmt.Errorf("no source")
	}
	select {
	case flacSem <- struct{}{}:
		defer func() { <-flacSem }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	tmp, err := os.CreateTemp("", "sd-flac-*.flac")
	if err != nil {
		return "", err
	}
	dst := tmp.Name()
	tmp.Close()
	level := FLACCompressionLevel(preset)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-i", src, "-map", "0:a:0", "-map_metadata", "0", "-c:a", "flac", "-compression_level", level, dst)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		os.Remove(dst)
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("flac compress: %s", msg)
	}
	st, err := os.Stat(dst)
	if err != nil || st.Size() < 64 {
		os.Remove(dst)
		return "", fmt.Errorf("flac compress produced no output")
	}
	return dst, nil
}

// PrepareStore losslessly compresses PCM/WAV/AIFF to FLAC when that shrinks the file.
// If the returned path differs from src, the caller must remove it after use.
func PrepareStore(ctx context.Context, src, origName, codec string) (outPath, storeName string) {
	res := PrepareStoreOpts(ctx, src, origName, codec, StoreOpts{})
	return res.Path, res.StoreName
}

func PrepareStoreOpts(ctx context.Context, src, origName, codec string, opts StoreOpts) StoreResult {
	res := StoreResult{Path: src, StoreName: origName}
	if src == "" {
		return res
	}
	if in, err := os.Stat(src); err == nil {
		res.OriginalSize = in.Size()
		res.StoredSize = in.Size()
	}
	if !FFmpegAvailable() || !NeedsCompress(origName, codec) {
		return res
	}
	dst, err := CompressToFLACPreset(ctx, src, opts.Preset)
	if err != nil {
		return res
	}
	out, err := os.Stat(dst)
	if err != nil || (res.OriginalSize > 0 && out.Size() >= res.OriginalSize) {
		os.Remove(dst)
		return res
	}
	flacName := ReplaceExt(origName, ".flac")
	if opts.KeepOriginal {
		res.CompanionPath = dst
		res.CompanionName = flacName
		res.CompanionSize = out.Size()
		res.Compressed = true
		return res
	}
	res.Path = dst
	res.StoreName = flacName
	res.StoredSize = out.Size()
	res.Compressed = true
	return res
}
