package discordx

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

func TestOpusNextYieldsEveryPacketOnAPage(t *testing.T) {
	var buf bytes.Buffer
	writeOggPage(&buf, [][]byte{
		[]byte("OpusHead-skip-me"),
		[]byte("frame-one"),
		[]byte("frame-two"),
		[]byte("frame-three"),
	})
	enc := &opusEncoder{r: bufio.NewReader(&buf)}

	got := make([]string, 0, 3)
	for {
		pkt, err := enc.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, string(pkt))
	}
	if len(got) != 3 || got[0] != "frame-one" || got[1] != "frame-two" || got[2] != "frame-three" {
		t.Fatalf("got %q, want all three audio frames", got)
	}
}

func writeOggPage(w *bytes.Buffer, packets [][]byte) {
	w.WriteString("OggS")
	hdr := make([]byte, 23)
	var table []byte
	var body []byte
	for _, pkt := range packets {
		for len(pkt) >= 255 {
			table = append(table, 255)
			body = append(body, pkt[:255]...)
			pkt = pkt[255:]
		}
		table = append(table, byte(len(pkt)))
		body = append(body, pkt...)
	}
	hdr[22] = byte(len(table))
	w.Write(hdr)
	w.Write(table)
	w.Write(body)
}

func int16LE(samples []int16) []byte {
	out := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(s))
	}
	return out
}

func leInt16(buf []byte) []int16 {
	n := len(buf) / 2
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		out[i] = int16(binary.LittleEndian.Uint16(buf[i*2:]))
	}
	return out
}

func TestApplyPCMGainScalesMuteAndClips(t *testing.T) {
	in := int16LE([]int16{1000, -1000, 20000, -20000})

	half := append([]byte(nil), in...)
	applyPCMGain(half, 0.5)
	got := leInt16(half)
	if got[0] != 500 || got[1] != -500 {
		t.Fatalf("0.5: %v", got)
	}

	muted := append([]byte(nil), in...)
	applyPCMGain(muted, 0)
	for i, s := range leInt16(muted) {
		if s != 0 {
			t.Fatalf("mute sample %d = %d", i, s)
		}
	}

	clipped := append([]byte(nil), in...)
	applyPCMGain(clipped, 2)
	got = leInt16(clipped)
	if got[0] != 2000 || got[1] != -2000 {
		t.Fatalf("2x linear: %v", got[:2])
	}
	if got[2] != 32767 || got[3] != -32768 {
		t.Fatalf("clip: %v", got[2:])
	}

	unity := append([]byte(nil), in...)
	applyPCMGain(unity, 1)
	if !bytes.Equal(unity, in) {
		t.Fatal("unity must be a no-op")
	}
}

func TestPCMGainReaderLiveMultiplier(t *testing.T) {
	gain := newPCMGain(1)
	src := int16LE([]int16{1000, 2000, 3000, 4000})
	r := newPCMGainReader(bytes.NewReader(src), gain)

	buf := make([]byte, 4)
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Fatal(err)
	}
	got := leInt16(buf)
	if got[0] != 1000 || got[1] != 2000 {
		t.Fatalf("unity: %v", got)
	}

	gain.Set(0.5)
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Fatal(err)
	}
	got = leInt16(buf)
	if got[0] != 1500 || got[1] != 2000 {
		t.Fatalf("live 0.5: %v", got)
	}

	gain.Set(0)
	r = newPCMGainReader(bytes.NewReader(int16LE([]int16{9, -9})), gain)
	if _, err := io.ReadFull(r, buf[:4]); err != nil {
		t.Fatal(err)
	}
	got = leInt16(buf[:4])
	if got[0] != 0 || got[1] != 0 {
		t.Fatalf("live mute: %v", got)
	}
}
