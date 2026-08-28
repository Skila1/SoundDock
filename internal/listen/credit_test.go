package listen

import (
	"testing"
	"time"
)

func TestCreditAccumulatesWithoutSeek(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	st := FSM{}
	var credited int
	for i := 0; i <= 30; i++ {
		var drop bool
		credited, st, drop = Credit(st, i*1000, int64(i+1), "playing", 1, now.Add(time.Duration(i)*time.Second))
		if drop {
			t.Fatalf("drop at i=%d", i)
		}
	}
	if st.AccumulatedMS < 30000 {
		t.Fatalf("accumulated %d credited last %d", st.AccumulatedMS, credited)
	}
	if ThresholdMs(185000) != 30000 {
		t.Fatal("T")
	}
	if st.AccumulatedMS < int64(ThresholdMs(185000)) {
		t.Fatal("should qualify on accumulated time")
	}
}

func TestCreditSeekJumpNoCredit(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	_, st, _ := Credit(FSM{}, 0, 1, "playing", 1, now)
	credited, st, drop := Credit(st, 40000, 2, "playing", 1, now.Add(time.Second))
	if drop || credited != 0 {
		t.Fatalf("seek credited %d drop %v", credited, drop)
	}
	if st.AccumulatedMS != 0 {
		t.Fatalf("accumulated %d", st.AccumulatedMS)
	}
	if st.LastPositionMS != 40000 {
		t.Fatalf("last pos %d", st.LastPositionMS)
	}
}

func TestCreditSeekBackThenPlay(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	st := FSM{}
	for i := 0; i <= 10; i++ {
		_, st, _ = Credit(st, i*1000, int64(i+1), "playing", 1, now.Add(time.Duration(i)*time.Second))
	}
	before := st.AccumulatedMS
	_, st, _ = Credit(st, 2000, 12, "playing", 1, now.Add(11*time.Second))
	if st.AccumulatedMS != before {
		t.Fatalf("seek back credited: %d -> %d", before, st.AccumulatedMS)
	}
	_, st, _ = Credit(st, 3000, 13, "playing", 1, now.Add(12*time.Second))
	if st.AccumulatedMS <= before {
		t.Fatalf("playback after seek back should credit, got %d", st.AccumulatedMS)
	}
}

func TestCreditStaleSequenceDropped(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	_, st, _ := Credit(FSM{}, 1000, 5, "playing", 1, now)
	credited, next, drop := Credit(st, 2000, 4, "playing", 1, now.Add(time.Second))
	if !drop || credited != 0 || next.AccumulatedMS != st.AccumulatedMS {
		t.Fatalf("stale seq drop=%v credited=%d acc=%d", drop, credited, next.AccumulatedMS)
	}
}

func TestCreditPauseAndGap(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	_, st, _ := Credit(FSM{}, 1000, 1, "playing", 1, now)
	_, st, _ = Credit(st, 2000, 2, "paused", 1, now.Add(time.Second))
	if st.AccumulatedMS != 1000 {
		t.Fatalf("pause credited %d", st.AccumulatedMS)
	}
	credited, st, _ := Credit(st, 8000, 3, "playing", 1, now.Add(20*time.Second))
	if credited != 0 {
		t.Fatalf("reconnect gap credited %d", credited)
	}
}

func TestHoldsBrowserLease(t *testing.T) {
	if HoldsBrowserLease("browser", "tab-a", "tab-a", "") != true {
		t.Fatal("client_id match")
	}
	if HoldsBrowserLease("browser", "dev-1", "other", "dev-1") != true {
		t.Fatal("device_id match")
	}
	if HoldsBrowserLease("browser", "tab-a", "tab-b", "dev-x") {
		t.Fatal("spectator tab")
	}
	if HoldsBrowserLease("discord", "tab-a", "tab-a", "") {
		t.Fatal("discord lease is not browser")
	}
}

func TestIsAudioListenerForcedAndSkip(t *testing.T) {
	yes, no := true, false
	if !IsAudioListener("progress", "discord", "", "", "", &yes) {
		t.Fatal("forced discord")
	}
	if IsAudioListener("progress", "discord", "", "", "", nil) {
		t.Fatal("unforced discord progress is not a browser listener")
	}
	if !IsAudioListener("skip", "none", "", "", "", nil) {
		t.Fatal("skip always applies")
	}
	if IsAudioListener("progress", "browser", "lease", "other", "", &no) {
		t.Fatal("forced false")
	}
}

func TestFirstCheckpointMidTrackNoCredit(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	credited, st, _ := Credit(FSM{}, 40000, 1, "playing", 1, now)
	if credited != 0 || st.AccumulatedMS != 0 {
		t.Fatalf("mid-track start credited %d acc %d", credited, st.AccumulatedMS)
	}
}
