package backup

import (
	"context"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/sounddock/sounddock/internal/storage"
)

type RemoteObject struct {
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	SizeBytes int64     `json:"size_bytes"`
	ModTime   time.Time `json:"mod_time"`
}

func (s Settings) s3() storage.S3Config {
	return storage.S3Config{
		Endpoint:  s.Endpoint,
		Region:    s.Region,
		Bucket:    s.Bucket,
		AccessKey: s.AccessKey,
		SecretKey: s.SecretKey,
		UseSSL:    s.UseSSL,
		Prefix:    s.Prefix,
	}
}

func (s *Service) r2Client(st Settings) (*storage.S3, error) {
	return storage.NewS3("backup-r2", st.s3())
}

func (s *Service) UploadRemote(ctx context.Context, st Settings, localPath, key string) error {
	cli, err := s.r2Client(st)
	if err != nil {
		return err
	}
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	ct := "application/octet-stream"
	if isGzip(localPath) {
		ct = "application/gzip"
	}
	return cli.Write(ctx, key, f, storage.WriteInfo{Size: info.Size(), ContentType: ct})
}

func (s *Service) ListRemote(ctx context.Context, st Settings) ([]RemoteObject, error) {
	cli, err := s.r2Client(st)
	if err != nil {
		return nil, err
	}
	it, err := cli.List(ctx, "")
	if err != nil {
		return nil, err
	}
	defer it.Close()
	var out []RemoteObject
	for it.Next() {
		e := it.Entry()
		if e.IsDir {
			continue
		}
		name := path.Base(e.Key)
		if !strings.Contains(name, "sounddock-") {
			continue
		}
		out = append(out, RemoteObject{Key: e.Key, Name: name, SizeBytes: e.Size, ModTime: e.ModTime})
	}
	return out, it.Err()
}

func (s *Service) DownloadRemote(ctx context.Context, st Settings, key, dest string) error {
	cli, err := s.r2Client(st)
	if err != nil {
		return err
	}
	rc, _, err := cli.Open(ctx, key)
	if err != nil {
		return err
	}
	defer rc.Close()
	if err := os.MkdirAll(filepathDir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, rc)
	return err
}

func filepathDir(p string) string {
	i := strings.LastIndexAny(p, `/\`)
	if i <= 0 {
		return "."
	}
	return p[:i]
}

func remoteKey(prefix, filename string) string {
	p := strings.Trim(strings.ReplaceAll(prefix, "\\", "/"), "/")
	if p == "" {
		return filename
	}
	return p + "/" + filename
}
