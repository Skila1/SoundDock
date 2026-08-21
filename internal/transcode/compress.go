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
	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-i", src, "-map", "0:a:0", "-map_metadata", "0", "-c:a", "flac", "-compression_level", "5", dst)
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
	storeName = origName
	outPath = src
	if src == "" || !FFmpegAvailable() || !NeedsCompress(origName, codec) {
		return outPath, storeName
	}
	in, err := os.Stat(src)
	if err != nil {
		return outPath, storeName
	}
	dst, err := CompressToFLAC(ctx, src)
	if err != nil {
		return outPath, storeName
	}
	out, err := os.Stat(dst)
	if err != nil || out.Size() >= in.Size() {
		os.Remove(dst)
		return src, origName
	}
	return dst, ReplaceExt(origName, ".flac")
}
