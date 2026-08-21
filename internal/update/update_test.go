package update

import "testing"

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
