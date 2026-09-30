package migrations

import "testing"

func TestHead(t *testing.T) {
	if Head() < 26 {
		t.Fatalf("head %d, want at least 26", Head())
	}
}
