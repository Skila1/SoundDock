package jobs

import "testing"

func TestAllowCancelAllowlist(t *testing.T) {
	t.Parallel()
	cases := []struct {
		typ, status string
		progress    int
		extra       Extra
		want        bool
	}{
		{"scapex.fetch", "queued", 0, Extra{}, true},
		{"scapex.fetch", "running", 10, Extra{FetchStage: StageDownloading}, true},
		{"scapex.fetch", "running", 10, Extra{FetchStage: StageProcessing}, false},
		{"scapex.fetch", "running", 80, Extra{}, false},
		{"tracks.metadata", "running", 0, Extra{}, true},
		{"library.scan", "queued", 0, Extra{}, true},
		{"scan.duplicates", "running", 0, Extra{}, true},
		{"lyrics.fetch", "queued", 0, Extra{}, true},
		{"library.merge", "queued", 0, Extra{}, false},
		{"library.migrate", "queued", 0, Extra{}, false},
		{"tracks.bulk_delete", "queued", 0, Extra{}, false},
		{"maintenance.retention", "queued", 0, Extra{}, true},
		{"maintenance.retention", "running", 0, Extra{}, true},
		{"maintenance.retention", "running", 10, Extra{RetentionDeleted: 1}, false},
		{"stats.rebuild", "queued", 0, Extra{}, true},
		{"stats.rebuild", "running", 50, Extra{StatsSwapStarted: true}, false},
		{"backup.run", "queued", 0, Extra{}, false},
		{"scapex.fetch", "completed", 100, Extra{}, false},
	}
	for _, c := range cases {
		t.Run(c.typ+"/"+c.status, func(t *testing.T) {
			if got := AllowCancel(c.typ, c.status, c.progress, c.extra); got != c.want {
				t.Fatalf("AllowCancel(%s,%s)=%v want %v", c.typ, c.status, got, c.want)
			}
		})
	}
}

func TestMayCancelMatchesAllowWithoutExtra(t *testing.T) {
	if !MayCancel("library.scan", "queued", 0) {
		t.Fatal("scan queued")
	}
	if MayCancel("library.merge", "queued", 0) {
		t.Fatal("merge must not be cancellable")
	}
}
