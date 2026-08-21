package auth

import "testing"

func TestPasswordHash(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(h, "correct horse") {
		t.Fatal("verify failed")
	}
	if VerifyPassword(h, "wrong") {
		t.Fatal("accepted wrong password")
	}
}
