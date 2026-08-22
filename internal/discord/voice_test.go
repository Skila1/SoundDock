package discordx

import "testing"

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
