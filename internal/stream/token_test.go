package stream

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSignVerify(t *testing.T) {
	key := []byte("test-key-32-bytes-long-enough!!")
	uid, id := uuid.New(), uuid.New()
	tok := Sign(key, uid, id, time.Minute, "original")
	got, err := Verify(key, tok)
	if err != nil {
		t.Fatal(err)
	}
	if got.TrackID != id || got.UserID != uid {
		t.Fatalf("got %+v", got)
	}
	if _, err := Verify(key, tok+"x"); err == nil {
		t.Fatal("expected fail")
	}
}

func TestSignRejectsNilUser(t *testing.T) {
	if Sign([]byte("k"), uuid.Nil, uuid.New(), time.Minute, "") != "" {
		t.Fatal("nil user must not sign")
	}
}

func TestVerifyRejectsV1(t *testing.T) {
	id := uuid.New()
	legacy := id.String() + ".9999999999.original.deadbeef"
	if _, err := Verify([]byte("k"), legacy); err == nil {
		t.Fatal("v1 token must fail")
	}
}

func TestVerifyExpiry(t *testing.T) {
	key := []byte("test-key-32-bytes-long-enough!!")
	tok := Sign(key, uuid.New(), uuid.New(), -time.Minute, "original")
	if tok == "" {
		t.Fatal("signed")
	}
	_, err := Verify(key, tok)
	if err == nil {
		t.Fatal("expired token")
	}
	if !strings.Contains(err.Error(), "expired") && err.Error() != "expired" {
		t.Fatalf("err %v", err)
	}
}
