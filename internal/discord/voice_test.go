package discordx

import (
	"testing"
	"time"
)

func TestSkipControlExtraEnded(t *testing.T) {
	got := skipControlExtra(true)
	if got["ended"] != true {
		t.Fatalf("natural EOF extra %v", got)
	}
	if skipControlExtra(false) != nil {
		t.Fatal("user skip / decode error must send ended=false (nil extra)")
	}
}

func TestFFmpegSeekArgs(t *testing.T) {
	if ffmpegSeekArgs(0) != nil || ffmpegSeekArgs(250) != nil {
		t.Fatal("small offsets should be ignored")
	}
	got := ffmpegSeekArgs(1500)
	if len(got) != 2 || got[0] != "-ss" || got[1] != "1.500" {
		t.Fatalf("got %v", got)
	}
}

func TestSessionPositionMS(t *testing.T) {
	if sessionPositionMS(nil) != 0 {
		t.Fatal("nil")
	}
	if sessionPositionMS(map[string]any{"position_ms": 1200}) != 1200 {
		t.Fatal("int")
	}
	if sessionPositionMS(map[string]any{"position_ms": float64(900)}) != 900 {
		t.Fatal("float")
	}
}

func TestSessionLivePositionMS(t *testing.T) {
	now := time.Date(2026, 10, 21, 17, 0, 0, 0, time.UTC)
	at := now.Add(-7 * time.Second)
	playing := map[string]any{"status": "playing", "position_ms": 60000, "checkpoint_at": &at, "duration_ms": 200000}
	if got := sessionLivePositionMS(playing, now); got != 67000 {
		t.Fatalf("playing from a browser checkpoint 7s ago should start at 67000, got %d", got)
	}
	paused := map[string]any{"status": "paused", "position_ms": 60000, "checkpoint_at": &at}
	if got := sessionLivePositionMS(paused, now); got != 60000 {
		t.Fatalf("paused must not move, got %d", got)
	}
	old := now.Add(-10 * time.Minute)
	stale := map[string]any{"status": "playing", "position_ms": 60000, "checkpoint_at": &old}
	if got := sessionLivePositionMS(stale, now); got != 60000 {
		t.Fatalf("a checkpoint nobody kept up is not extrapolated, got %d", got)
	}
	nearEnd := map[string]any{"status": "playing", "position_ms": 198000, "checkpoint_at": at, "duration_ms": 200000}
	if got := sessionLivePositionMS(nearEnd, now); got != 200000 {
		t.Fatalf("clamped to duration, got %d", got)
	}
	fast := map[string]any{"status": "playing", "position_ms": 1000, "checkpoint_at": &at, "playback_rate": 1.5}
	if got := sessionLivePositionMS(fast, now); got != 11500 {
		t.Fatalf("rate applies, got %d", got)
	}
	if got := sessionLivePositionMS(map[string]any{"status": "playing", "position_ms": 500}, now); got != 500 {
		t.Fatalf("no checkpoint, got %d", got)
	}
}

func TestSessionVolumeZeroIsNotRewritten(t *testing.T) {
	if got := sessionVolume(map[string]any{"volume": float64(0)}); got != 0 {
		t.Fatalf("explicit 0 rewritten to %v", got)
	}
	if got := sessionVolume(nil); got != 1 {
		t.Fatalf("missing volume: got %v want 1", got)
	}
	if got := sessionVolume(map[string]any{}); got != 1 {
		t.Fatalf("absent key: got %v want 1", got)
	}
	if got := sessionVolume(map[string]any{"volume": 0.5}); got != 0.5 {
		t.Fatalf("0.5: got %v", got)
	}
}

func TestPCMGainDBReplayGainOnly(t *testing.T) {
	if got := pcmGainDB("off", nil, nil); got != 0 {
		t.Fatalf("ReplayGain off should be 0dB, got %v", got)
	}
	g := 6.0
	if got := pcmGainDB("track", &g, nil); got < 5.9 || got > 6.1 {
		t.Fatalf("track +6dB ReplayGain: got %v", got)
	}
}

func TestLiveVolumeMultiplierMuteAndZero(t *testing.T) {
	if got := liveVolumeMultiplier(map[string]any{"volume": float64(0)}); got != 0 {
		t.Fatalf("volume 0: got %v", got)
	}
	if got := liveVolumeMultiplier(map[string]any{"volume": 0.5, "muted": true}); got != 0 {
		t.Fatalf("muted: got %v", got)
	}
	if got := liveVolumeMultiplier(map[string]any{"volume": 0.4}); got != 0.4 {
		t.Fatalf("0.4: got %v", got)
	}
	if got := liveVolumeMultiplier(nil); got != 1 {
		t.Fatalf("missing: got %v", got)
	}
	if got := liveVolumeMultiplier(map[string]any{"volume": 1.5}); got != 1 {
		t.Fatalf("clamp >1: got %v", got)
	}
}
