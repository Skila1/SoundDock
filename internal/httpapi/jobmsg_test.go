package httpapi

import "testing"

func TestExplainJobErrorSpotify403(t *testing.T) {
	raw := `403 Forbidden: {"error": {"status": 403, "message": "Forbidden" }}`
	got := explainJobError("external.playlist.import", raw)
	if got == raw || !containsFold(got, "Spotify") {
		t.Fatalf("got %q", got)
	}
}

func TestExplainJobErrorQueueFull(t *testing.T) {
	got := explainJobError("external.playlist.import", "workload queue is full: Sync")
	if !containsFold(got, "queue") || containsFold(got, "workload") {
		t.Fatalf("got %q", got)
	}
}

func TestExplainJobErrorKeepsPlainText(t *testing.T) {
	got := explainJobError("library.scan", "path does not exist")
	if got != "path does not exist" {
		t.Fatalf("got %q", got)
	}
}

func containsFold(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(func() bool {
			ls, lsub := []rune(s), []rune(sub)
			for i := 0; i+len(lsub) <= len(ls); i++ {
				ok := true
				for j := range lsub {
					a, b := ls[i+j], lsub[j]
					if a >= 'A' && a <= 'Z' {
						a += 32
					}
					if b >= 'A' && b <= 'Z' {
						b += 32
					}
					if a != b {
						ok = false
						break
					}
				}
				if ok {
					return true
				}
			}
			return false
		})())
}
