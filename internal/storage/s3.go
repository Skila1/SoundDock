package storage

import (
	"context"
	"io"
	"path"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type S3Config struct {
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	UseSSL    bool   `json:"use_ssl"`
	PathStyle bool   `json:"path_style"`
	Prefix    string `json:"prefix"`
}

type S3 struct {
	id     string
	cfg    S3Config
	client *minio.Client
}

func NewS3(id string, cfg S3Config) (*S3, error) {
	ep := strings.TrimPrefix(strings.TrimPrefix(cfg.Endpoint, "https://"), "http://")
	cli, err := minio.New(ep, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, err
	}
	return &S3{id: id, cfg: cfg, client: cli}, nil
}

func (s *S3) ID() string   { return s.id }
func (s *S3) Type() string { return "s3" }
func (s *S3) Capabilities() Caps {
	return Caps{Read: true, Write: true, Watch: false, Seek: true}
}

func (s *S3) obj(key string) (string, error) {
	clean, err := SanitizeKey(key)
	if err != nil {
		return "", err
	}
	return path.Join(s.cfg.Prefix, clean), nil
}

func (s *S3) Open(ctx context.Context, key string) (ReadSeekCloser, *ObjectInfo, error) {
	obj, err := s.obj(key)
	if err != nil {
		return nil, nil, err
	}
	o, err := s.client.GetObject(ctx, s.cfg.Bucket, obj, minio.GetObjectOptions{})
	if err != nil {
		return nil, nil, err
	}
	st, err := o.Stat()
	if err != nil {
		o.Close()
		return nil, nil, ErrNotFound
	}
	info := &ObjectInfo{Key: key, Size: st.Size, ModTime: st.LastModified, ETag: st.ETag, ContentType: st.ContentType}
	return wrapMinio(o), info, nil
}

func (s *S3) Stat(ctx context.Context, key string) (*ObjectInfo, error) {
	obj, err := s.obj(key)
	if err != nil {
		return nil, err
	}
	st, err := s.client.StatObject(ctx, s.cfg.Bucket, obj, minio.StatObjectOptions{})
	if err != nil {
		return nil, ErrNotFound
	}
	return &ObjectInfo{Key: key, Size: st.Size, ModTime: st.LastModified, ETag: st.ETag, ContentType: st.ContentType}, nil
}

func (s *S3) List(ctx context.Context, prefix string) (Iterator, error) {
	p, err := s.obj(prefix)
	if err != nil {
		return nil, err
	}
	ch := s.client.ListObjects(ctx, s.cfg.Bucket, minio.ListObjectsOptions{Prefix: p, Recursive: true})
	return &s3Iter{ch: ch, prefix: s.cfg.Prefix}, nil
}

func (s *S3) Write(ctx context.Context, key string, r io.Reader, info WriteInfo) error {
	obj, err := s.obj(key)
	if err != nil {
		return err
	}
	ct := info.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	_, err = s.client.PutObject(ctx, s.cfg.Bucket, obj, r, info.Size, minio.PutObjectOptions{ContentType: ct})
	return err
}

func (s *S3) Delete(ctx context.Context, key string) error {
	obj, err := s.obj(key)
	if err != nil {
		return err
	}
	return s.client.RemoveObject(ctx, s.cfg.Bucket, obj, minio.RemoveObjectOptions{})
}

func (s *S3) FFmpegSource(ctx context.Context, key string) (FFmpegSource, error) {
	rc, _, err := s.Open(ctx, key)
	if err != nil {
		return FFmpegSource{}, err
	}
	return FFmpegSource{Reader: rc, Close: rc.Close}, nil
}

type minioRS struct{ *minio.Object }

func wrapMinio(o *minio.Object) ReadSeekCloser { return minioRS{o} }

func (m minioRS) ReadAt(p []byte, off int64) (int, error) {
	if _, err := m.Seek(off, io.SeekStart); err != nil {
		return 0, err
	}
	return m.Read(p)
}

type s3Iter struct {
	ch     <-chan minio.ObjectInfo
	prefix string
	cur    Entry
	err    error
}

func (s *s3Iter) Next() bool {
	info, ok := <-s.ch
	if !ok {
		return false
	}
	if info.Err != nil {
		s.err = info.Err
		return false
	}
	key := strings.TrimPrefix(info.Key, strings.TrimSuffix(s.prefix, "/")+"/")
	s.cur = Entry{Key: key, Size: info.Size, ModTime: info.LastModified, ETag: info.ETag, IsDir: strings.HasSuffix(info.Key, "/")}
	return true
}
func (s *s3Iter) Entry() Entry { return s.cur }
func (s *s3Iter) Err() error   { return s.err }
func (s *s3Iter) Close() error { return nil }
