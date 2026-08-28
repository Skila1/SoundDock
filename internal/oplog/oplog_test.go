package oplog

import "testing"

func TestRedactSecrets(t *testing.T) {
	in := "connect postgres://sounddock:super-secret@postgres:5432/sounddock token=abc123xyz password=hunter2 Bearer eyJhbGciOi.secret"
	out := Redact(in)
	for _, leak := range []string{"super-secret", "abc123xyz", "hunter2", "eyJhbGciOi.secret"} {
		if contains(out, leak) {
			t.Fatalf("leaked %q in %q", leak, out)
		}
	}
	if !contains(out, "[redacted]") {
		t.Fatalf("expected redaction markers, got %q", out)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
