package migrations

import "testing"

func TestHead(t *testing.T) {
	if Head() < 23 {
		t.Fatalf("head %d, want at least 23", Head())
	}
}
