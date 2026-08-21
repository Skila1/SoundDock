package transcode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNeedsCompress(t *testing.T) {
	if !NeedsCompress("album/01.wav", "") || !NeedsCompress("x.AIFF", "") {
		t.Fatal("wav/aiff should compress")
	}
	if NeedsCompress("song.flac", "flac") || NeedsCompress("song.mp3", "mp3") || NeedsCompress("song.m4a", "aac") {
		t.Fatal("already compressed formats should be left alone")
	}
	if NeedsCompress("song.m4a", "alac") {
		t.Fatal("alac is already lossless-compressed")
	}
}

func TestReplaceExt(t *testing.T) {
	if got := ReplaceExt("Album/01 Track.wav", ".flac"); got != "Album/01 Track.flac" {
		t.Fatal(got)
	}
}

func TestCompressToFLAC(t *testing.T) {
	if !FFmpegAvailable() {
		t.Skip("ffmpeg not in PATH")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "tone.wav")
	cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.25", "-c:a", "pcm_s16le", src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make wav: %v %s", err, out)
	}
	in, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	dst, name := PrepareStore(context.Background(), src, "tone.wav", "pcm_s16le")
	if dst == src || name != "tone.flac" {
		t.Fatalf("path=%s name=%s", dst, name)
	}
	defer os.Remove(dst)
	out, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if out.Size() >= in.Size() {
		t.Fatalf("flac %d not smaller than wav %d", out.Size(), in.Size())
	}
}

func TestPrepareStoreSkipsFlac(t *testing.T) {
	p, name := PrepareStore(context.Background(), "x.flac", "x.flac", "flac")
	if p != "x.flac" || name != "x.flac" {
		t.Fatalf("%s %s", p, name)
	}
}
