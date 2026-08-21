package stream

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSignVerify(t *testing.T) {
	key := []byte("test-key-32-bytes-long-enough!!")
	id := uuid.New()
	tok := Sign(key, id, time.Minute, "original")
	got, err := Verify(key, tok)
	if err != nil {
		t.Fatal(err)
	}
	if got.TrackID != id {
		t.Fatal(got.TrackID)
	}
	if _, err := Verify(key, tok+"x"); err == nil {
		t.Fatal("expected fail")
	}
}
