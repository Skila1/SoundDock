package scan

import "testing"

func TestExtFromURLIgnoresQuery(t *testing.T) {
	got := ExtFromURL("https://example.com/album/track.flac?token=abc")
	if got != ".flac" {
		t.Fatalf("got %q", got)
	}
	if !IsAudioExt(got) {
		t.Fatal("flac should be audio")
	}
	if IsAudioName("cover.jpg") || IsAudioName("notes.txt") {
		t.Fatal("non-audio names")
	}
	if ResolveAudioExt("", "https://cdn.example/a.mp3?x=1", "") != ".mp3" {
		t.Fatal("url fallback")
	}
	if ResolveAudioExt("", "https://cdn.example/file", "audio/flac") != ".flac" {
		t.Fatal("content-type fallback")
	}
	if !IsAudioName("song.WAV") || !IsZipName("album.ZIP") || !IsUploadName("a.zip") {
		t.Fatal("upload names")
	}
	if IsUploadName("cover.jpg") {
		t.Fatal("jpg is not an upload")
	}
	if !IsZipContentType("application/zip") || IsZipContentType("application/gzip") {
		t.Fatal("zip content types")
	}
}
