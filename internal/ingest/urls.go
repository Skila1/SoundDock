package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/external"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/scan"
	"github.com/sounddock/sounddock/internal/ssrf"
	"github.com/sounddock/sounddock/internal/storage"
)

const maxImportURLs = 200

func ParseURLList(parts ...string) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range parts {
		for _, line := range strings.Split(part, "\n") {
			for _, piece := range strings.Split(line, ",") {
				u := strings.TrimSpace(piece)
				u = strings.Trim(u, `"'`)
				if u == "" || strings.HasPrefix(u, "#") {
					continue
				}
				if seen[u] {
					continue
				}
				seen[u] = true
				out = append(out, u)
			}
		}
	}
	return out
}

func (p URLPayload) URLs() []string {
	return ParseURLList(append([]string{p.URL}, p.Extra...)...)
}

func (s *Service) URLHandler(getProv func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error)) jobs.Handler {
	return func(ctx context.Context, job jobs.Job) error {
		var p URLPayload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return err
		}
		list := p.URLs()
		if len(list) == 0 {
			return fmt.Errorf("no URLs")
		}
		if len(list) > maxImportURLs {
			return fmt.Errorf("at most %d URLs per import", maxImportURLs)
		}
		var imported, skipped, failed int
		var errs []string
		for i, raw := range list {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			s.setProgress(ctx, job.ID, (i*100)/len(list))
			err := s.importURL(ctx, raw, p.LibraryID, getProv)
			switch {
			case err == nil:
				imported++
			case strings.Contains(err.Error(), "already imported"):
				skipped++
			default:
				failed++
				if len(errs) < 8 {
					errs = append(errs, fmt.Sprintf("%s: %s", raw, err.Error()))
				}
			}
		}
		s.setProgress(ctx, job.ID, 100)
		summary := fmt.Sprintf("%d imported, %d skipped, %d failed", imported, skipped, failed)
		if failed > 0 {
			if imported == 0 && skipped == 0 {
				return fmt.Errorf("%s. %s", summary, strings.Join(errs, "; "))
			}
			_, _ = s.pool.Exec(ctx, `UPDATE jobs SET last_error=$2, updated_at=now() WHERE id=$1`, job.ID, summary+". "+strings.Join(errs, "; "))
		}
		return nil
	}
}

func (s *Service) setProgress(ctx context.Context, id uuid.UUID, p int) {
	if s.pool == nil || id == uuid.Nil {
		return
	}
	_, _ = s.pool.Exec(ctx, `UPDATE jobs SET progress=$2, updated_at=now() WHERE id=$1`, id, p)
}

func (s *Service) importURL(ctx context.Context, raw string, libID uuid.UUID, getProv func(context.Context, uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error)) error {
	if _, err := url.ParseRequestURI(raw); err != nil {
		return fmt.Errorf("invalid URL")
	}
	if external.IsPlaylistURL(raw) {
		return fmt.Errorf("playlist URLs belong in External Playlist Import, not Remote Import")
	}
	opt := ssrf.DefaultOptions()
	opt.MaxBytes = s.maxBytes
	if strings.EqualFold(scan.ExtFromURL(raw), ".zip") {
		opt.MaxBytes = 2 << 30
	}
	rc, ctype, _, err := ssrf.Fetch(ctx, raw, opt)
	if err != nil {
		return err
	}
	defer rc.Close()
	isZip := strings.EqualFold(scan.ExtFromURL(raw), ".zip") || scan.IsZipContentType(ctype)
	ext := scan.ResolveAudioExt("", raw, ctype)
	if !isZip && ext == "" {
		return fmt.Errorf("not a supported audio file")
	}
	tmp, err := os.CreateTemp(s.staging, "url-*")
	if err != nil {
		return err
	}
	hw := sha256.New()
	n, err := io.Copy(tmp, io.TeeReader(rc, hw))
	name := tmp.Name()
	tmp.Close()
	if err != nil {
		os.Remove(name)
		return err
	}
	if isZip {
		id := uuid.New()
		count, root, resolved, prov, err := s.extractZip(ctx, name, id, libID, getProv)
		if err != nil {
			os.Remove(name)
			return err
		}
		if count == 0 {
			return fmt.Errorf("zip contained no audio files")
		}
		if s.scanner == nil {
			return nil
		}
		return s.scanner.ScanLibrary(ctx, resolved, prov, root, "import", uuid.Nil)
	}
	hash := hex.EncodeToString(hw.Sum(nil))
	var existing string
	if err := s.pool.QueryRow(ctx, `SELECT storage_key FROM track_files WHERE content_hash=$1 LIMIT 1`, hash).Scan(&existing); err == nil && existing != "" {
		os.Remove(name)
		return fmt.Errorf("already imported")
	}
	prov, resolved, prefix, err := getProv(ctx, libID)
	if err != nil {
		os.Remove(name)
		return err
	}
	key := path.Join(prefix, "imports", hash[:2], hash+ext)
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	err = prov.Write(ctx, key, f, storage.WriteInfo{Size: n})
	f.Close()
	os.Remove(name)
	if err != nil {
		return err
	}
	return s.scanner.ScanLibrary(ctx, resolved, prov, path.Join(prefix, "imports", hash[:2]), "import", uuid.Nil)
}
