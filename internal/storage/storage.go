package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"time"
)

var (
	ErrEscape     = errors.New("path escapes storage root")
	ErrNotFound   = errors.New("object not found")
	ErrReadOnly   = errors.New("storage is read-only")
	ErrUnsupported = errors.New("unsupported storage operation")
)

type Caps struct {
	Read  bool
	Write bool
	Watch bool
	Seek  bool
}

type ObjectInfo struct {
	Key       string
	Size      int64
	ModTime   time.Time
	ETag      string
	ContentType string
}

type WriteInfo struct {
	Size        int64
	ContentType string
}

type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
	io.ReaderAt
}

type StorageProvider interface {
	ID() string
	Type() string
	Capabilities() Caps
	Open(ctx context.Context, key string) (ReadSeekCloser, *ObjectInfo, error)
	Stat(ctx context.Context, key string) (*ObjectInfo, error)
	List(ctx context.Context, prefix string) (Iterator, error)
	Write(ctx context.Context, key string, r io.Reader, info WriteInfo) error
	Delete(ctx context.Context, key string) error
}

type FFmpegSource struct {
	Path   string
	Reader io.ReadCloser
	Close  func() error
}

type FFmpegSourcer interface {
	FFmpegSource(ctx context.Context, key string) (FFmpegSource, error)
}

type Entry struct {
	Key     string
	IsDir   bool
	Size    int64
	ModTime time.Time
	ETag    string
}

type Iterator interface {
	Next() bool
	Entry() Entry
	Err() error
	Close() error
}

func FileReadSeekCloser(f *os.File) ReadSeekCloser { return f }
