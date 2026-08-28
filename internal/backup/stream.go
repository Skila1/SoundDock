package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	archiveMagic    = "SDAR"
	archiveVersion  = 2
	streamChunkSize = 64 * 1024
	streamNonceSize = 12
)

var ErrNotArchive = errors.New("not a SoundDock encrypted archive")

// ClearHeader is the only plaintext prefix of an archive.
type ClearHeader struct {
	Version uint16
	KDF     KDFParams
	Box     []byte
}

func writeClearHeader(w io.Writer, h ClearHeader) error {
	if len(h.KDF.Salt) == 0 || len(h.KDF.Salt) > 255 {
		return fmt.Errorf("invalid kdf salt")
	}
	if len(h.Box) == 0 {
		return fmt.Errorf("recovery.box is required")
	}
	var hdr [4 + 2 + 4 + 4 + 1 + 1]byte
	copy(hdr[0:4], archiveMagic)
	binary.BigEndian.PutUint16(hdr[4:6], archiveVersion)
	binary.BigEndian.PutUint32(hdr[6:10], h.KDF.Time)
	binary.BigEndian.PutUint32(hdr[10:14], h.KDF.Memory)
	hdr[14] = h.KDF.Threads
	hdr[15] = byte(len(h.KDF.Salt))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if _, err := w.Write(h.KDF.Salt); err != nil {
		return err
	}
	var boxLen [4]byte
	binary.BigEndian.PutUint32(boxLen[:], uint32(len(h.Box)))
	if _, err := w.Write(boxLen[:]); err != nil {
		return err
	}
	_, err := w.Write(h.Box)
	return err
}

func readClearHeader(r io.Reader) (ClearHeader, error) {
	var fixed [16]byte
	if _, err := io.ReadFull(r, fixed[:]); err != nil {
		return ClearHeader{}, err
	}
	if string(fixed[0:4]) != archiveMagic {
		return ClearHeader{}, ErrNotArchive
	}
	ver := binary.BigEndian.Uint16(fixed[4:6])
	if ver != archiveVersion {
		return ClearHeader{}, fmt.Errorf("unsupported archive version %d", ver)
	}
	h := ClearHeader{
		Version: ver,
		KDF: KDFParams{
			Time:    binary.BigEndian.Uint32(fixed[6:10]),
			Memory:  binary.BigEndian.Uint32(fixed[10:14]),
			Threads: fixed[14],
			Salt:    make([]byte, int(fixed[15])),
		},
	}
	if _, err := io.ReadFull(r, h.KDF.Salt); err != nil {
		return ClearHeader{}, err
	}
	var boxLen [4]byte
	if _, err := io.ReadFull(r, boxLen[:]); err != nil {
		return ClearHeader{}, err
	}
	n := binary.BigEndian.Uint32(boxLen[:])
	if n == 0 || n > 1<<20 {
		return ClearHeader{}, ErrWrapCorrupt
	}
	h.Box = make([]byte, n)
	if _, err := io.ReadFull(r, h.Box); err != nil {
		return ClearHeader{}, err
	}
	return h, nil
}

type encryptWriter struct {
	w     io.Writer
	aead  cipher.AEAD
	buf   []byte
	index uint32
	err   error
}

func newEncryptWriter(w io.Writer, dek []byte) (*encryptWriter, error) {
	aead, err := gcmFor(dek)
	if err != nil {
		return nil, err
	}
	return &encryptWriter{w: w, aead: aead, buf: make([]byte, 0, streamChunkSize)}, nil
}

func (e *encryptWriter) Write(p []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	n := 0
	for len(p) > 0 {
		need := streamChunkSize - len(e.buf)
		if need > len(p) {
			e.buf = append(e.buf, p...)
			n += len(p)
			return n, nil
		}
		e.buf = append(e.buf, p[:need]...)
		p = p[need:]
		n += need
		if err := e.flush(false); err != nil {
			e.err = err
			return n, err
		}
	}
	return n, nil
}

func (e *encryptWriter) Close() error {
	if e.err != nil {
		return e.err
	}
	if err := e.flush(true); err != nil {
		e.err = err
		return err
	}
	return nil
}

func (e *encryptWriter) flush(last bool) error {
	if len(e.buf) == 0 && !last {
		return nil
	}
	if len(e.buf) == 0 && last && e.index > 0 {
		return writeChunk(e.w, e.aead, e.index, nil, true)
	}
	if len(e.buf) == 0 && last {
		return writeChunk(e.w, e.aead, e.index, nil, true)
	}
	plain := e.buf
	e.buf = e.buf[:0]
	if err := writeChunk(e.w, e.aead, e.index, plain, last); err != nil {
		return err
	}
	e.index++
	return nil
}

func writeChunk(w io.Writer, aead cipher.AEAD, index uint32, plain []byte, last bool) error {
	nonce := make([]byte, streamNonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	aad := make([]byte, 5)
	binary.BigEndian.PutUint32(aad[0:4], index)
	if last {
		aad[4] = 1
	}
	sealed := aead.Seal(nil, nonce, plain, aad)
	var hdr [5]byte
	binary.BigEndian.PutUint32(hdr[0:4], uint32(len(nonce)+len(sealed)))
	if last {
		hdr[4] = 1
	}
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if _, err := w.Write(nonce); err != nil {
		return err
	}
	_, err := w.Write(sealed)
	return err
}

type decryptReader struct {
	r     io.Reader
	aead  cipher.AEAD
	plain []byte
	off   int
	index uint32
	eof   bool
}

func newDecryptReader(r io.Reader, dek []byte) (*decryptReader, error) {
	aead, err := gcmFor(dek)
	if err != nil {
		return nil, err
	}
	return &decryptReader{r: r, aead: aead}, nil
}

func (d *decryptReader) Read(p []byte) (int, error) {
	if d.off < len(d.plain) {
		n := copy(p, d.plain[d.off:])
		d.off += n
		return n, nil
	}
	if d.eof {
		return 0, io.EOF
	}
	var hdr [5]byte
	if _, err := io.ReadFull(d.r, hdr[:]); err != nil {
		if err == io.EOF {
			return 0, io.ErrUnexpectedEOF
		}
		return 0, err
	}
	nlen := binary.BigEndian.Uint32(hdr[0:4])
	last := hdr[4] == 1
	if nlen < streamNonceSize || nlen > streamChunkSize+64 {
		return 0, fmt.Errorf("corrupt archive chunk")
	}
	buf := make([]byte, nlen)
	if _, err := io.ReadFull(d.r, buf); err != nil {
		return 0, err
	}
	nonce, sealed := buf[:streamNonceSize], buf[streamNonceSize:]
	aad := make([]byte, 5)
	binary.BigEndian.PutUint32(aad[0:4], d.index)
	if last {
		aad[4] = 1
	}
	plain, err := d.aead.Open(nil, nonce, sealed, aad)
	if err != nil {
		return 0, fmt.Errorf("archive decrypt failed: %w", err)
	}
	d.index++
	d.plain = plain
	d.off = 0
	if last {
		d.eof = true
	}
	if len(plain) == 0 {
		if last {
			return 0, io.EOF
		}
		return d.Read(p)
	}
	n := copy(p, d.plain)
	d.off = n
	return n, nil
}

func gcmFor(dek []byte) (cipher.AEAD, error) {
	if len(dek) != dekSize {
		return nil, fmt.Errorf("dek must be %d bytes", dekSize)
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func openRead(path string) (*os.File, error) {
	return os.Open(path)
}

func isEncryptedArchive(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return false
	}
	return string(magic[:]) == archiveMagic
}
