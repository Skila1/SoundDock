package discordx

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math"
	"os/exec"
	"sync/atomic"
)

// pcmGain is the live volume multiplier applied to s16le samples between
// decode and Opus stdin. streamLoop updates it when session volume/mute changes.
type pcmGain struct {
	bits atomic.Uint64
}

func newPCMGain(mult float64) *pcmGain {
	g := &pcmGain{}
	g.Set(mult)
	return g
}

func (g *pcmGain) Set(mult float64) {
	if g == nil {
		return
	}
	if mult < 0 || math.IsNaN(mult) {
		mult = 0
	}
	g.bits.Store(math.Float64bits(mult))
}

func (g *pcmGain) Get() float64 {
	if g == nil {
		return 1
	}
	return math.Float64frombits(g.bits.Load())
}

// applyPCMGain scales packed little-endian int16 samples in place and clips.
func applyPCMGain(buf []byte, mult float64) {
	if mult == 1 {
		return
	}
	n := len(buf) &^ 1
	if n == 0 {
		return
	}
	if mult == 0 {
		for i := 0; i < n; i++ {
			buf[i] = 0
		}
		return
	}
	for i := 0; i < n; i += 2 {
		s := int16(binary.LittleEndian.Uint16(buf[i : i+2]))
		v := math.Round(float64(s) * mult)
		if v > math.MaxInt16 {
			v = math.MaxInt16
		} else if v < math.MinInt16 {
			v = math.MinInt16
		}
		binary.LittleEndian.PutUint16(buf[i:i+2], uint16(int16(v)))
	}
}

type pcmGainReader struct {
	r    io.Reader
	gain *pcmGain
	tail byte
	has  bool
}

func newPCMGainReader(r io.Reader, gain *pcmGain) io.Reader {
	if gain == nil {
		gain = newPCMGain(1)
	}
	return &pcmGainReader{r: r, gain: gain}
}

func (r *pcmGainReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	off := 0
	if r.has {
		p[0] = r.tail
		r.has = false
		off = 1
	}
	var n int
	var err error
	if off < len(p) {
		n, err = r.r.Read(p[off:])
	}
	n += off
	if n%2 == 1 {
		r.tail = p[n-1]
		r.has = true
		n--
	}
	if n > 0 {
		applyPCMGain(p[:n], r.gain.Get())
	}
	if n == 0 {
		return 0, err
	}
	return n, err
}

type opusEncoder struct {
	cmd     *exec.Cmd
	stdout  io.ReadCloser
	stdin   io.WriteCloser
	r       *bufio.Reader
	accum   []byte
	pending [][]byte
}

func startOpusEncoder(ctx context.Context, pcm io.Reader) (*opusEncoder, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-f", "s16le", "-ar", "48000", "-ac", "2", "-i", "pipe:0",
		"-c:a", "libopus", "-b:a", "96k", "-vbr", "on",
		"-frame_duration", "20", "-application", "audio",
		"-f", "ogg", "pipe:1",
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, err
	}
	go func() {
		_, _ = io.Copy(stdin, pcm)
		stdin.Close()
	}()
	return &opusEncoder{cmd: cmd, stdout: stdout, stdin: stdin, r: bufio.NewReaderSize(stdout, 8192)}, nil
}

func (e *opusEncoder) Close() {
	if e.stdout != nil {
		e.stdout.Close()
	}
	if e.stdin != nil {
		e.stdin.Close()
	}
	if e.cmd != nil && e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
		_ = e.cmd.Wait()
	}
}

func (e *opusEncoder) Next() ([]byte, error) {
	for {
		if len(e.pending) > 0 {
			pkt := e.pending[0]
			e.pending = e.pending[1:]
			return pkt, nil
		}
		page, err := readOggPage(e.r)
		if err != nil {
			return nil, err
		}
		for _, seg := range page {
			if len(seg) == 255 {
				e.accum = append(e.accum, seg...)
				continue
			}
			pkt := append(e.accum, seg...)
			e.accum = nil
			if len(pkt) == 0 || isOpusHeader(pkt) {
				continue
			}
			out := make([]byte, len(pkt))
			copy(out, pkt)
			e.pending = append(e.pending, out)
		}
	}
}

func isOpusHeader(pkt []byte) bool {
	return bytes.HasPrefix(pkt, []byte("OpusHead")) || bytes.HasPrefix(pkt, []byte("OpusTags"))
}

func readOggPage(r *bufio.Reader) ([][]byte, error) {
	for {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if b != 'O' {
			continue
		}
		rest := make([]byte, 3)
		if _, err := io.ReadFull(r, rest); err != nil {
			return nil, err
		}
		if string(append([]byte{'O'}, rest...)) != "OggS" {
			continue
		}
		hdr := make([]byte, 23)
		if _, err := io.ReadFull(r, hdr); err != nil {
			return nil, err
		}
		nseg := int(hdr[22])
		table := make([]byte, nseg)
		if _, err := io.ReadFull(r, table); err != nil {
			return nil, err
		}
		var segs [][]byte
		for _, n := range table {
			seg := make([]byte, int(n))
			if _, err := io.ReadFull(r, seg); err != nil {
				return nil, err
			}
			segs = append(segs, seg)
		}
		_ = binary.LittleEndian.Uint32(hdr[18:22])
		return segs, nil
	}
}
