package auth

import "testing"

func TestIsAdminID(t *testing.T) {
	ids := []string{"123", "456"}
	if !isAdminID("123", ids) {
		t.Fatal("expected admin")
	}
	if isAdminID("999", ids) {
		t.Fatal("not admin")
	}
}
