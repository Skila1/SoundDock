package discordx

import (
	"bufio"
	"bytes"
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
