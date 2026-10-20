package storage

import (
	"context"
	"os"
)

// LocalPath returns a filesystem path ffmpeg can read for key. Providers that
// expose one directly (FFmpegSourcer) are used as is; anything else is copied
// to a temp file named by pattern. The cleanup func is always safe to call.
func LocalPath(ctx context.Context, prov StorageProvider, key, pattern string) (string, func(), error) {
	noop := func() {}
	if fs, ok := prov.(FFmpegSourcer); ok {
		src, err := fs.FFmpegSource(ctx, key)
		if err == nil && src.Path != "" {
			return src.Path, func() {
				if src.Close != nil {
					_ = src.Close()
				}
			}, nil
		}
		if src.Close != nil {
			_ = src.Close()
		}
	}
	rc, _, err := prov.Open(ctx, key)
	if err != nil {
		return "", noop, err
	}
	defer rc.Close()
	tmp, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", noop, err
	}
	name := tmp.Name()
	_, err = tmp.ReadFrom(rc)
	tmp.Close()
	if err != nil {
		os.Remove(name)
		return "", noop, err
	}
	return name, func() { os.Remove(name) }, nil
}
