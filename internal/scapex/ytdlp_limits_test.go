package scapex

import "testing"

func TestBoundedBufferRejectsOutputAboveLimit(t *testing.T) {
	var out boundedBuffer
	out.limit = 4
	if _, err := out.Write([]byte("1234")); err != nil {
		t.Fatal(err)
	}
	if _, err := out.Write([]byte("5")); err == nil {
		t.Fatal("expected output limit error")
	}
}
