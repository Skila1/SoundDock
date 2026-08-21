package ingest

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/storage"
)

func writeTestZip(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	add := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	add("Album/01 Track.flac", "flac")
	add("Album/02 Track.mp3", "mp3")
	add("Album/cover.jpg", "jpg")
	add("__MACOSX/._01 Track.flac", "mac")
	add("Album/.DS_Store", "ds")
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExtractZipAudioOnly(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "album.zip")
	writeTestZip(t, zipPath)
	dest := t.TempDir()
	prov, err := storage.NewLocal("t", dest, false)
	if err != nil {
		t.Fatal(err)
	}
	lib := uuid.New()
	s := New(nil, nil, nil, dir, 10<<20)
	n, root, libID, out, err := s.extractZip(context.Background(), zipPath, uuid.New(), lib, func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error) {
		return prov, lib, "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || libID != lib || out == nil {
		t.Fatalf("n=%d lib=%s", n, libID)
	}
	if !strings.Contains(root, "uploads/zip/") {
		t.Fatalf("root %s", root)
	}
	it, err := prov.List(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer it.Close()
	var keys []string
	for it.Next() {
		e := it.Entry()
		if !e.IsDir {
			keys = append(keys, e.Key)
		}
	}
	if len(keys) != 2 {
		t.Fatalf("keys %#v", keys)
	}
	if _, err := os.Stat(zipPath); !os.IsNotExist(err) {
		t.Fatal("staging zip should be removed")
	}
}

func TestSkipZipEntry(t *testing.T) {
	if !skipZipEntry("__MACOSX/foo") || !skipZipEntry("Album/.DS_Store") {
		t.Fatal("expected skip")
	}
	if skipZipEntry("Album/01.flac") {
		t.Fatal("should keep audio")
	}
}
