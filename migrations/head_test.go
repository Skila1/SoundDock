package migrations

import "testing"

func TestHead(t *testing.T) {
	if Head() < 25 {
		t.Fatalf("head %d, want at least 23", Head())
	}
}
