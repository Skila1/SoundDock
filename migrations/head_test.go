package migrations

import "testing"

func TestHead(t *testing.T) {
	if Head() < 27 {
		t.Fatalf("head %d, want at least 27", Head())
	}
}
