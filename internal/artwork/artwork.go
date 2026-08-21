package artwork

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"

	"github.com/disintegration/imaging"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/image/webp"
)

type Size struct {
	Name string
	Px   int
}

var DerivativeSizes = []Size{
	{Name: "thumb", Px: 64},
	{Name: "card", Px: 300},
	{Name: "page", Px: 600},
	{Name: "now", Px: 1200},
}

type Store struct {
	pool     *pgxpool.Pool
	cacheDir string
}

func New(pool *pgxpool.Pool, cacheDir string) *Store {
	return &Store{pool: pool, cacheDir: filepath.Join(cacheDir, "artwork")}
}

func (s *Store) Save(ctx context.Context, ownerType string, ownerID uuid.UUID, source string, r io.Reader) (uuid.UUID, error) {
	raw, err := io.ReadAll(io.LimitReader(r, 20<<20))
	if err != nil {
		return uuid.Nil, err
	}
	img, err := decode(raw)
	if err != nil {
		return uuid.Nil, err
	}
	if err := os.MkdirAll(s.cacheDir, 0o755); err != nil {
		return uuid.Nil, err
	}
	sum := sha256.Sum256(raw)
	key := hex.EncodeToString(sum[:])
	buf := &bytes.Buffer{}
	if err := jpeg.Encode(buf, imaging.Clone(img), &jpeg.Options{Quality: 90}); err != nil {
		return uuid.Nil, err
	}
	orig := filepath.Join(s.cacheDir, key+".orig.jpg")
	if err := os.WriteFile(orig, buf.Bytes(), 0o644); err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	b := img.Bounds()
	err = s.pool.QueryRow(ctx, `
		INSERT INTO artwork_assets (owner_type, owner_id, source, storage_key, mime, width, height)
		VALUES ($1,$2,$3,$4,'image/jpeg',$5,$6) RETURNING id`,
		ownerType, ownerID, source, key+".orig.jpg", b.Dx(), b.Dy()).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	for _, sz := range DerivativeSizes {
		resized := imaging.Fit(img, sz.Px, sz.Px, imaging.Lanczos)
		out := filepath.Join(s.cacheDir, fmt.Sprintf("%s.%s.jpg", key, sz.Name))
		f, err := os.Create(out)
		if err != nil {
			continue
		}
		_ = jpeg.Encode(f, resized, &jpeg.Options{Quality: 82})
		f.Close()
		rb := resized.Bounds()
		_, _ = s.pool.Exec(ctx, `INSERT INTO artwork_derivatives (artwork_id, size_name, storage_key, mime, width, height)
			VALUES ($1,$2,$3,'image/jpeg',$4,$5) ON CONFLICT DO NOTHING`,
			id, sz.Name, fmt.Sprintf("%s.%s.jpg", key, sz.Name), rb.Dx(), rb.Dy())
	}
	return id, nil
}

func (s *Store) File(key string) (string, error) {
	p := filepath.Join(s.cacheDir, filepath.Base(key))
	if _, err := os.Stat(p); err != nil {
		return "", err
	}
	return p, nil
}

func decode(b []byte) (image.Image, error) {
	img, err := imaging.Decode(bytes.NewReader(b))
	if err == nil {
		return img, nil
	}
	if w, err2 := webp.Decode(bytes.NewReader(b)); err2 == nil {
		return w, nil
	}
	return nil, err
}

func FolderArt(dir string) string {
	for _, n := range []string{"cover.jpg", "cover.png", "folder.jpg", "Folder.jpg", "artwork.jpg", "front.jpg"} {
		p := filepath.Join(dir, n)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
