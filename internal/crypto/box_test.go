package cryptox

import "testing"

func TestWeakMasterKey(t *testing.T) {
	if !WeakMasterKey("") {
		t.Fatal("empty")
	}
	if !WeakMasterKey("change-me-to-32-plus-random-bytes") {
		t.Fatal("example")
	}
	if WeakMasterKey("wave8-master-key-test") {
		t.Fatal("real key")
	}
}

func TestNewRejectsWeakKey(t *testing.T) {
	if _, err := New(""); err != ErrNoKey {
		t.Fatalf("empty: %v", err)
	}
	if _, err := New("change-me-to-32-plus-random-bytes"); err != ErrNoKey {
		t.Fatalf("example: %v", err)
	}
	box, err := New("wave8-master-key-test")
	if err != nil || box == nil || len(box.key) == 0 {
		t.Fatal(err)
	}
}

func TestSigningKeyNilWhenWeak(t *testing.T) {
	if SigningKey("") != nil {
		t.Fatal("empty")
	}
	if SigningKey("change-me-to-32-plus-random-bytes") != nil {
		t.Fatal("example")
	}
	if len(SigningKey("wave8-master-key-test")) == 0 {
		t.Fatal("real key")
	}
}
