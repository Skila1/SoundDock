package scan

import "testing"

func TestSkipScanKey(t *testing.T) {
	if !SkipScanKey("trash/00000000-0000-4000-8000-000000000060/uploads/aa/hash.flac") {
		t.Fatal("trash")
	}
	if !SkipScanKey("compressed/ab/abcd.flac") {
		t.Fatal("compressed")
	}
	if SkipScanKey("uploads/aa/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.flac") {
		t.Fatal("hash original must be scanned")
	}
}

func TestHashStorageKey(t *testing.T) {
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	got := HashStorageKey("", hash, ".flac")
	want := "uploads/aa/" + hash + ".flac"
	if got != want {
		t.Fatal(got)
	}
	if !IsHashStorageKey(want) {
		t.Fatal("fixture hash key")
	}
}

func TestCompanionQualityLabel(t *testing.T) {
	hash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	got := CompanionStorageKey("uploads/aa/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.wav", hash)
	if got != "compressed/bb/"+hash+".flac" {
		t.Fatal(got)
	}
}
