package storage

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Local struct {
	id   string
	root string
	ro   bool
}

func NewLocal(id, root string, readOnly bool) (*Local, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil && !readOnly {
		return nil, err
	}
	return &Local{id: id, root: abs, ro: readOnly}, nil
}

func (l *Local) ID() string   { return l.id }
func (l *Local) Type() string { return "local" }
func (l *Local) Root() string { return l.root }
func (l *Local) Capabilities() Caps {
	return Caps{Read: true, Write: !l.ro, Watch: true, Seek: true}
}

func (l *Local) resolve(key string) (string, error) { return ResolveUnder(l.root, key) }

func (l *Local) Open(_ context.Context, key string) (ReadSeekCloser, *ObjectInfo, error) {
	p, err := l.resolve(key)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		f.Close()
		return nil, nil, ErrEscape
	}
	info := &ObjectInfo{Key: key, Size: st.Size(), ModTime: st.ModTime()}
	return f, info, nil
}

func (l *Local) Stat(_ context.Context, key string) (*ObjectInfo, error) {
	p, err := l.resolve(key)
	if err != nil {
		return nil, err
	}
	st, err := os.Lstat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return nil, ErrEscape
	}
	return &ObjectInfo{Key: key, Size: st.Size(), ModTime: st.ModTime()}, nil
}

func (l *Local) List(_ context.Context, prefix string) (Iterator, error) {
	clean, err := SanitizeKey(prefix)
	if err != nil {
		return nil, err
	}
	start := l.root
	if clean != "" {
		start, err = l.resolve(clean)
		if err != nil {
			return nil, err
		}
	}
	var entries []Entry
	err = filepath.WalkDir(start, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(l.root, path)
		if err != nil || strings.HasPrefix(rel, "..") {
			return ErrEscape
		}
		if rel == "." {
			return nil
		}
		key := filepath.ToSlash(rel)
		info, _ := d.Info()
		e := Entry{Key: key, IsDir: d.IsDir()}
		if info != nil {
			e.Size = info.Size()
			e.ModTime = info.ModTime()
		}
		entries = append(entries, e)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &sliceIter{items: entries}, nil
}

func (l *Local) Write(_ context.Context, key string, r io.Reader, _ WriteInfo) error {
	if l.ro {
		return ErrReadOnly
	}
	p, err := l.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func (l *Local) Delete(_ context.Context, key string) error {
	if l.ro {
		return ErrReadOnly
	}
	p, err := l.resolve(key)
	if err != nil {
		return err
	}
	err = os.Remove(p)
	if os.IsNotExist(err) {
		return ErrNotFound
	}
	return err
}

func (l *Local) FFmpegSource(_ context.Context, key string) (FFmpegSource, error) {
	p, err := l.resolve(key)
	if err != nil {
		return FFmpegSource{}, err
	}
	if _, err := os.Lstat(p); err != nil {
		return FFmpegSource{}, err
	}
	return FFmpegSource{Path: p, Close: func() error { return nil }}, nil
}

type sliceIter struct {
	items []Entry
	i     int
	cur   Entry
}

func (s *sliceIter) Next() bool {
	if s.i >= len(s.items) {
		return false
	}
	s.cur = s.items[s.i]
	s.i++
	return true
}
func (s *sliceIter) Entry() Entry { return s.cur }
func (s *sliceIter) Err() error   { return nil }
func (s *sliceIter) Close() error { return nil }

func IsNotExist(err error) bool {
	return errors.Is(err, ErrNotFound) || os.IsNotExist(err)
}
