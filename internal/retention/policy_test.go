package retention

import "testing"

func TestNormalizeDisabled(t *testing.T) {
	p := Normalize(Policy{Enabled: true, Mode: "nope", BatchSize: 0, IntervalMinutes: -1})
	if p.Mode != ModeAge {
		t.Fatalf("mode %s", p.Mode)
	}
	if p.BatchSize != 50 || p.IntervalMinutes != 60 {
		t.Fatalf("defaults %+v", p)
	}
	p = Normalize(Policy{Mode: ModeDisabled, Enabled: true})
	if p.Enabled {
		t.Fatal("disabled must clear enabled")
	}
}

func TestLowWaterHysteresis(t *testing.T) {
	p := Normalize(Policy{MaxManagedBytes: 500 << 30, PruneDownToBytes: 450 << 30})
	if p.HighWater() != 500<<30 {
		t.Fatal("high")
	}
	if p.LowWater() != 450<<30 {
		t.Fatal("low")
	}
	p = Normalize(Policy{MaxManagedBytes: 100 << 30})
	if p.LowWater() != 90<<30 {
		t.Fatalf("default 90%% got %d", p.LowWater())
	}
	p = Normalize(Policy{MaxManagedBytes: 100, PruneDownToBytes: 200})
	if p.PruneDownToBytes >= p.MaxManagedBytes {
		t.Fatal("low water must sit below high water")
	}
}

func TestFreeTarget(t *testing.T) {
	p := Policy{MinFreeBytes: 20 << 30}
	if p.FreeTarget() <= p.MinFreeBytes {
		t.Fatal("target should add headroom")
	}
	p.FreeSpaceTargetBytes = 40 << 30
	if p.FreeTarget() != 40<<30 {
		t.Fatal("explicit target")
	}
}

func TestYouTubeIDFromKey(t *testing.T) {
	if got := YouTubeIDFromKey("inbox/dQw4w9WgXcQ.m4a"); got != "dQw4w9WgXcQ" {
		t.Fatalf("got %q", got)
	}
	if YouTubeIDFromKey("uploads/aa/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.flac") != "" {
		t.Fatal("hash key")
	}
	if YouTubeIDFromKey("inbox/not-an-id.mp3") != "" {
		t.Fatal("short")
	}
	if !IsYouTubeID("abcdefghijk") {
		t.Fatal("11 chars")
	}
}

func TestModeFlags(t *testing.T) {
	p := Normalize(Policy{Enabled: true, Mode: ModeHybrid, MaxManagedBytes: 10, MinFreeBytes: 1, AgeDays: 7})
	if !p.UsesAge() || !p.UsesStorage() || !p.UsesFreeSpace() {
		t.Fatalf("%+v", p)
	}
	p = Normalize(Policy{Enabled: false, Mode: ModeAge, MaxManagedBytes: 10})
	if p.UsesAge() || p.UsesStorage() {
		t.Fatal("disabled")
	}
}
