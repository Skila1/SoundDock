package backup

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Manifest struct {
	Version       string `json:"version"`
	CreatedAt     string `json:"created_at"`
	IncludeMedia  bool   `json:"include_media"`
	Database      string `json:"database"`
	MediaRoot     string `json:"media_root,omitempty"`
	InstanceName  string `json:"instance_name,omitempty"`
	SchemaVersion int    `json:"schema_version"`
}

var skipPackNames = map[string]bool{
	".env":               true,
	"master.key":         true,
	"restore-state.json": true,
}

func shouldSkipPacked(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	if skipPackNames[base] {
		return true
	}
	if strings.HasPrefix(base, ".env") {
		return true
	}
	return false
}

func (s *Service) stageInner(work, sqlPath string, includeMedia bool, req RestoreRequirements) error {
	if err := copyFile(sqlPath, filepath.Join(work, "database.sql")); err != nil {
		return err
	}
	rb, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(work, "restore-requirements.json"), rb, 0o644); err != nil {
		return err
	}
	if includeMedia && s.media != "" {
		if err := copyDirFiltered(s.media, filepath.Join(work, "managed")); err != nil {
			return err
		}
	}
	if s.artwork != "" {
		if err := copyDirFiltered(s.artwork, filepath.Join(work, "artwork")); err != nil {
			return err
		}
	}
	if s.lyrics != "" {
		if err := copyDirFiltered(s.lyrics, filepath.Join(work, "lyrics")); err != nil {
			return err
		}
	}
	return nil
}

func packFullArchive(dest, sqlPath, mediaRoot string, includeMedia bool) error {
	work, err := os.MkdirTemp("", "sd-bak-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	s := &Service{media: mediaRoot}
	if err := s.stageInner(work, sqlPath, includeMedia, RestoreRequirements{}); err != nil {
		return err
	}
	return packInnerArchive(dest, work, includeMedia, "", 0)
}

func packInnerArchive(dest, work string, includeMedia bool, instance string, schema int) error {
	man := Manifest{
		Version:       "2",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		IncludeMedia:  includeMedia,
		Database:      "database.sql",
		InstanceName:  instance,
		SchemaVersion: schema,
	}
	if includeMedia {
		man.MediaRoot = "managed"
	}
	mb, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(work, "manifest.json"), mb, 0o644); err != nil {
		return err
	}
	if err := writeChecksums(work); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	return tarDir(tw, "", work)
}

func encryptArchiveFile(dest, inner string, dek, recoveryBox []byte, kdf KDFParams) error {
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	if err := writeClearHeader(out, ClearHeader{Version: archiveVersion, KDF: kdf, Box: recoveryBox}); err != nil {
		return err
	}
	ew, err := newEncryptWriter(out, dek)
	if err != nil {
		return err
	}
	in, err := os.Open(inner)
	if err != nil {
		_ = ew.Close()
		return err
	}
	defer in.Close()
	if _, err := io.Copy(ew, in); err != nil {
		_ = ew.Close()
		return err
	}
	return ew.Close()
}

func copyDirFiltered(src, dest string) error {
	st, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !st.IsDir() {
		return nil
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if shouldSkipPacked(path) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return nil
		}
		target := filepath.Join(dest, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func tarWrite(tw *tar.Writer, name string, body []byte) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), ModTime: time.Now()}); err != nil {
		return err
	}
	_, err := tw.Write(body)
	return err
}

func tarFile(tw *tar.Writer, name, src string) error {
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: st.Size(), ModTime: st.ModTime()}); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

func tarDir(tw *tar.Writer, prefix, root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if shouldSkipPacked(path) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		name := filepath.ToSlash(rel)
		if prefix != "" {
			name = filepath.ToSlash(filepath.Join(prefix, rel))
		}
		if info.IsDir() {
			return tw.WriteHeader(&tar.Header{Name: name + "/", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: info.ModTime()})
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return tarFile(tw, name, path)
	})
}

func unpackFullArchive(src, dest string) (sqlPath string, mediaDir string, err error) {
	if err = os.MkdirAll(dest, 0o755); err != nil {
		return "", "", err
	}
	f, err := os.Open(src)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", "", err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", "", err
		}
		clean := filepath.Clean(hdr.Name)
		if strings.Contains(clean, "..") {
			continue
		}
		target := filepath.Join(dest, clean)
		if hdr.Typeflag == tar.TypeDir || strings.HasSuffix(hdr.Name, "/") {
			_ = os.MkdirAll(target, 0o755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", "", err
		}
		out, err := os.Create(target)
		if err != nil {
			return "", "", err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return "", "", err
		}
		out.Close()
	}
	sqlPath = filepath.Join(dest, "database.sql")
	if _, err := os.Stat(sqlPath); err != nil {
		return "", "", fmt.Errorf("archive is missing database.sql")
	}
	for _, cand := range []string{"managed", "media"} {
		media := filepath.Join(dest, cand)
		if st, err := os.Stat(media); err == nil && st.IsDir() {
			mediaDir = media
			break
		}
	}
	return sqlPath, mediaDir, nil
}

func isGzip(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var hdr [2]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return false
	}
	return hdr[0] == 0x1f && hdr[1] == 0x8b
}

func readManifest(root string) (Manifest, error) {
	var man Manifest
	b, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return man, err
	}
	err = json.Unmarshal(b, &man)
	return man, err
}

func readRequirements(root string) (RestoreRequirements, error) {
	var req RestoreRequirements
	b, err := os.ReadFile(filepath.Join(root, "restore-requirements.json"))
	if err != nil {
		return req, err
	}
	err = json.Unmarshal(b, &req)
	return req, err
}
