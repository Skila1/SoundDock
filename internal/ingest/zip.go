package ingest

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/scan"
	"github.com/sounddock/sounddock/internal/storage"
)

const (
	maxZipFiles = 500
	maxZipBytes = 2 << 30
)

func zipSafeName(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimPrefix(name, "/")
	if name == "" || strings.Contains(name, "..") {
		return "", fmt.Errorf("invalid path in zip")
	}
	return name, nil
}

func (s *Service) finishZip(ctx context.Context, sessionID uuid.UUID, staging string, lib uuid.UUID, getProv func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error)) error {
	zr, err := zip.OpenReader(staging)
	if err != nil {
		return fmt.Errorf("not a valid zip")
	}
	prov, libID, prefix, err := getProv(ctx, lib)
	if err != nil {
		zr.Close()
		return err
	}
	var files int
	var bytes int64
	root := path.Join(prefix, "uploads", "zip", sessionID.String())
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rel, err := zipSafeName(f.Name)
		if err != nil {
			continue
		}
		base := path.Base(rel)
		if !scan.IsAudioName(base) {
			continue
		}
		files++
		if files > maxZipFiles {
			return fmt.Errorf("zip has more than %d audio files", maxZipFiles)
		}
		if int64(f.UncompressedSize64) > s.maxBytes {
			return fmt.Errorf("%s is larger than the import limit", base)
		}
		bytes += int64(f.UncompressedSize64)
		if bytes > maxZipBytes {
			return fmt.Errorf("zip is larger than 2 GiB uncompressed")
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		key := path.Join(root, rel)
		err = prov.Write(ctx, key, rc, storage.WriteInfo{Size: int64(f.UncompressedSize64)})
		rc.Close()
		if err != nil {
			return err
		}
	}
	zr.Close()
	os.Remove(staging)
	_, _ = s.pool.Exec(ctx, `UPDATE upload_sessions SET state='complete' WHERE id=$1`, sessionID)
	if files == 0 {
		return fmt.Errorf("zip contained no audio files")
	}
	return s.scanner.ScanLibrary(ctx, libID, prov, root, "upload", uuid.Nil)
}
