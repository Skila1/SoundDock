package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseImage(t *testing.T) {
	h, r, tag := parseImage("ghcr.io/skila1/sounddock:latest")
	if h != "ghcr.io" || r != "skila1/sounddock" || tag != "latest" {
		t.Fatalf("%s %s %s", h, r, tag)
	}
	h, r, tag = parseImage("postgres:16-alpine")
	if h != "docker.io" || r != "library/postgres" || tag != "16-alpine" {
		t.Fatalf("%s %s %s", h, r, tag)
	}
}

func TestDigestEqual(t *testing.T) {
	if !digestEqual("sha256:ABC", "SHA256:abc") {
		t.Fatal("expected equal")
	}
	if digestEqual("", "") {
		t.Fatal("empty is not a match")
	}
}

func TestRequestUpdate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SD_UPDATE_DIR", dir)
	if HelperOK() {
		t.Fatal("writable dir without helper marker is not a host helper")
	}
	if err := os.WriteFile(filepath.Join(dir, "helper"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !HelperOK() {
		t.Fatal("expected helper marker to be enough")
	}
	if err := RequestUpdate("skila"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "request"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "skila") {
		t.Fatalf("got %s", b)
	}
	if err := os.WriteFile(filepath.Join(dir, "applied"), []byte("ghcr.io/skila1/sounddock@sha256:abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	if AppliedDigest() != "sha256:abc" {
		t.Fatalf("digest %s", AppliedDigest())
	}
}
