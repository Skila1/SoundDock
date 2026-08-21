package ingest

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/scan"
	"github.com/sounddock/sounddock/internal/storage"
	"github.com/sounddock/sounddock/internal/transcode"
)

const (
	maxZipFiles = 10000
	maxZipBytes = 32 << 30
)

var ErrZipQueued = errors.New("zip extract queued")

type ZipPayload struct {
	SessionID uuid.UUID `json:"session_id"`
}

func zipSafeName(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimPrefix(name, "/")
	if name == "" || strings.Contains(name, "..") {
		return "", fmt.Errorf("invalid path in zip")
	}
	return name, nil
}

func skipZipEntry(rel string) bool {
	base := path.Base(rel)
	if base == ".DS_Store" || base == "Thumbs.db" {
		return true
	}
	if strings.HasPrefix(rel, "__MACOSX/") || strings.Contains(rel, "/__MACOSX/") {
		return true
	}
	return false
}

func (s *Service) ZipHandler(getProv func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error)) jobs.Handler {
	return func(ctx context.Context, job jobs.Job) error {
		var p ZipPayload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return err
		}
		var staging, filename string
		var lib uuid.UUID
		if err := s.pool.QueryRow(ctx, `SELECT staging_key, filename, library_id FROM upload_sessions WHERE id=$1`, p.SessionID).Scan(&staging, &filename, &lib); err != nil {
			return err
		}
		return s.finishZip(ctx, p.SessionID, staging, lib, getProv, job.ID)
	}
}

func (s *Service) finishZip(ctx context.Context, sessionID uuid.UUID, staging string, lib uuid.UUID, getProv func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error), jobID uuid.UUID) error {
	n, root, libID, prov, err := s.extractZip(ctx, staging, sessionID, lib, getProv)
	if err != nil {
		return err
	}
	if s.pool != nil {
		_, _ = s.pool.Exec(ctx, `UPDATE upload_sessions SET state='complete' WHERE id=$1`, sessionID)
	}
	if n == 0 {
		return fmt.Errorf("zip contained no audio files")
	}
	if s.scanner == nil {
		return nil
	}
	return s.scanner.ScanLibrary(ctx, libID, prov, root, "upload", jobID)
}

func (s *Service) extractZip(ctx context.Context, staging string, sessionID uuid.UUID, lib uuid.UUID, getProv func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error)) (int, string, uuid.UUID, storage.StorageProvider, error) {
	zr, err := zip.OpenReader(staging)
	if err != nil {
		return 0, "", uuid.Nil, nil, fmt.Errorf("not a valid zip")
	}
	prov, libID, prefix, err := getProv(ctx, lib)
	if err != nil {
		zr.Close()
		return 0, "", uuid.Nil, nil, err
	}
	var files int
	var bytes int64
	root := path.Join(prefix, "uploads", "zip", sessionID.String())
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rel, err := zipSafeName(f.Name)
		if err != nil || skipZipEntry(rel) {
			continue
		}
		base := path.Base(rel)
		if !scan.IsAudioName(base) {
			continue
		}
		files++
		if files > maxZipFiles {
			zr.Close()
			return 0, "", uuid.Nil, nil, fmt.Errorf("zip has more than %d audio files", maxZipFiles)
		}
		if int64(f.UncompressedSize64) > 1<<30 {
			zr.Close()
			return 0, "", uuid.Nil, nil, fmt.Errorf("%s is larger than 1 GiB", base)
		}
		bytes += int64(f.UncompressedSize64)
		if bytes > maxZipBytes {
			zr.Close()
			return 0, "", uuid.Nil, nil, fmt.Errorf("zip is larger than 32 GiB uncompressed")
		}
		rc, err := f.Open()
		if err != nil {
			zr.Close()
			return 0, "", uuid.Nil, nil, err
		}
		tmp, err := os.CreateTemp("", "sd-zip-*"+path.Ext(base))
		if err != nil {
			rc.Close()
			zr.Close()
			return 0, "", uuid.Nil, nil, err
		}
		_, err = io.Copy(tmp, rc)
		tmp.Close()
		rc.Close()
		if err != nil {
			os.Remove(tmp.Name())
			zr.Close()
			return 0, "", uuid.Nil, nil, err
		}
		out, storeName := transcode.PrepareStore(ctx, tmp.Name(), rel, "")
		if out != tmp.Name() {
			os.Remove(tmp.Name())
		}
		key := path.Join(root, storeName)
		sf, err := os.Open(out)
		if err != nil {
			os.Remove(out)
			zr.Close()
			return 0, "", uuid.Nil, nil, err
		}
		st, _ := sf.Stat()
		err = prov.Write(ctx, key, sf, storage.WriteInfo{Size: st.Size()})
		sf.Close()
		os.Remove(out)
		if err != nil {
			zr.Close()
			return 0, "", uuid.Nil, nil, err
		}
	}
	zr.Close()
	os.Remove(staging)
	return files, root, libID, prov, nil
}
