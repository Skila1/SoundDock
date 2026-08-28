package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func fileSHA(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type checksumFile struct {
	Files map[string]string `json:"files"`
}

// VerifyArchive decrypts (if needed) and compares inner checksums.
// path may be a plaintext inner tree or an encrypted archive (dek required).
func (s *Service) VerifyArchive(path string, dek []byte) (bool, string) {
	if isEncryptedArchive(path) {
		if len(dek) == 0 {
			return false, "encrypted archive needs a DEK to verify"
		}
		tmp, err := os.MkdirTemp("", "sounddock-verify-*")
		if err != nil {
			return false, err.Error()
		}
		defer os.RemoveAll(tmp)
		inner, err := decryptToFile(path, dek, filepath.Join(tmp, "inner.tar.gz"))
		if err != nil {
			return false, err.Error()
		}
		work := filepath.Join(tmp, "unpacked")
		if _, _, err := unpackFullArchive(inner, work); err != nil {
			return false, err.Error()
		}
		return compareChecksums(work)
	}
	if isGzip(path) {
		tmp, err := os.MkdirTemp("", "sounddock-verify-*")
		if err != nil {
			return false, err.Error()
		}
		defer os.RemoveAll(tmp)
		if _, _, err := unpackFullArchive(path, tmp); err != nil {
			return false, err.Error()
		}
		return compareChecksums(tmp)
	}
	return s.VerifyFile(path)
}

func compareChecksums(root string) (bool, string) {
	raw, err := os.ReadFile(filepath.Join(root, "checksums.json"))
	if err != nil {
		return false, "checksums.json is missing"
	}
	var sum checksumFile
	if json.Unmarshal(raw, &sum) != nil || sum.Files == nil {
		return false, "checksums.json is invalid"
	}
	for rel, want := range sum.Files {
		clean := filepath.Clean(rel)
		if strings.Contains(clean, "..") {
			return false, "checksums.json has an unsafe path"
		}
		got, err := fileSHA(filepath.Join(root, filepath.FromSlash(clean)))
		if err != nil {
			return false, "missing " + rel
		}
		if !strings.EqualFold(got, want) {
			return false, "hash mismatch: " + rel
		}
	}
	return true, fmt.Sprintf("files=%d checksum_ok=true", len(sum.Files))
}

func writeChecksums(root string) error {
	files := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if name == "checksums.json" {
			return nil
		}
		sum, err := fileSHA(path)
		if err != nil {
			return err
		}
		files[name] = sum
		return nil
	})
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(checksumFile{Files: files}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "checksums.json"), b, 0o644)
}

func decryptToFile(src string, dek []byte, dest string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	if _, err := readClearHeader(in); err != nil {
		return "", err
	}
	dr, err := newDecryptReader(in, dek)
	if err != nil {
		return "", err
	}
	out, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, dr); err != nil {
		return "", err
	}
	return dest, nil
}
