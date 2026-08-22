package discordx

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os/exec"
)

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
