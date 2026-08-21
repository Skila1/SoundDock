package watch

import "testing"

func TestDefaultsOff(t *testing.T) {
	if WatchEnabled(t.Context(), nil) || AutoRescanEnabled(t.Context(), nil) || InboxEnabled(t.Context(), nil) {
		t.Fatal("watch/inbox/auto-rescan must default off")
	}
}

func TestRunNilSafe(t *testing.T) {
	(&Watcher{}).Run(t.Context())
}
