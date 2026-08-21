package waveform

import (
	"encoding/binary"
	"testing"
)

func TestJobNameFrozen(t *testing.T) {
	if JobName != "waveform.generate" || !JobTypeOK(JobName) {
		t.Fatal(JobName)
	}
}

func TestDownsamplePeaks(t *testing.T) {
	pcm := make([]byte, 16)
	binary.LittleEndian.PutUint16(pcm[0:], 32767)
	binary.LittleEndian.PutUint16(pcm[8:], 0)
	out := DownsamplePeaks(pcm, 2)
	if len(out) != 2 {
		t.Fatalf("%v", out)
	}
	if out[0] < 200 {
		t.Fatalf("expected loud bucket, got %v", out)
	}
}

func TestGenerateNoFFmpegNoPanic(t *testing.T) {
	peaks, err := Generate(t.Context(), "", 16)
	if err != nil || peaks != nil {
		t.Fatalf("%v %v", peaks, err)
	}
}
