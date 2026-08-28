package backup

import (
	"bufio"
	"context"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var createTableRe = regexp.MustCompile(`(?i)^\s*CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-zA-Z0-9_."]+)`)

// Preview describes a backup archive without restoring it.
type Preview struct {
	ID          string   `json:"id"`
	Path        string   `json:"path"`
	SizeBytes   int64    `json:"size_bytes"`
	Checksum    string   `json:"checksum"`
	Status      string   `json:"status"`
	CreatedAt   string   `json:"created_at"`
	Verified    bool     `json:"verified"`
	Readable    bool     `json:"readable"`
	Empty       bool     `json:"empty"`
	Logical     bool     `json:"logical"`
	Header      string   `json:"header"`
	Tables      []string `json:"tables"`
	Statements  int      `json:"statements"`
	Warnings    []string `json:"warnings"`
	RestoreKind string   `json:"restore_kind"`
}

func (s *Service) Preview(ctx context.Context, id uuid.UUID) (Preview, error) {
	rec, err := s.Get(ctx, id)
	if err != nil {
		return Preview{}, err
	}
	p := Preview{
		ID:          rec.ID.String(),
		Path:        rec.Path,
		SizeBytes:   rec.SizeBytes,
		Checksum:    rec.Checksum,
		Status:      rec.Status,
		CreatedAt:   rec.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		Tables:      []string{},
		Warnings:    []string{},
		RestoreKind: "unknown",
	}
	ok, detail := s.VerifyFile(rec.Path)
	p.Verified = ok
	if !ok {
		p.Warnings = append(p.Warnings, detail)
	}
	st, err := os.Stat(rec.Path)
	if err != nil {
		p.Warnings = append(p.Warnings, err.Error())
		return p, nil
	}
	p.Empty = st.Size() == 0
	p.SizeBytes = st.Size()
	if p.Empty {
		p.Warnings = append(p.Warnings, "empty archive")
		return p, nil
	}
	f, err := os.Open(rec.Path)
	if err != nil {
		p.Warnings = append(p.Warnings, err.Error())
		return p, nil
	}
	defer f.Close()
	p.Readable = true
	if isEncryptedArchive(rec.Path) {
		p.RestoreKind = "full"
		p.Logical = true
		p.Header = "Encrypted SoundDock archive. Restore requires the recovery passphrase."
		return p, nil
	}
	if isGzip(rec.Path) || rec.Kind == "full" || strings.HasSuffix(strings.ToLower(rec.Path), ".tar.gz") {
		p.RestoreKind = "full"
		p.Logical = true
		p.Header = "Full SoundDock archive (database + media)"
		return p, nil
	}
	info := parseSQLDump(io.LimitReader(f, 1<<20))
	p.Header = info.header
	p.Tables = info.tables
	p.Statements = info.statements
	p.Logical = info.logical
	if info.incomplete {
		p.Warnings = append(p.Warnings, "logical backup is incomplete (pg_dump was unavailable)")
	}
	if info.logical {
		p.RestoreKind = "sql"
	}
	if len(p.Tables) == 0 && !info.incomplete {
		p.Warnings = append(p.Warnings, "no CREATE TABLE statements found in the first megabyte")
	}
	return p, nil
}

type dumpInfo struct {
	header     string
	tables     []string
	statements int
	logical    bool
	incomplete bool
}

func parseSQLDump(r io.Reader) dumpInfo {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	seen := map[string]bool{}
	info := dumpInfo{tables: []string{}}
	var headerLines []string
	for sc.Scan() {
		line := sc.Text()
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "--") && len(headerLines) < 6 {
			headerLines = append(headerLines, trim)
		}
		if strings.Contains(trim, "pg_dump unavailable") {
			info.incomplete = true
		}
		if strings.HasPrefix(strings.ToUpper(trim), "CREATE ") || strings.HasPrefix(strings.ToUpper(trim), "COPY ") || strings.HasPrefix(strings.ToUpper(trim), "INSERT ") {
			info.logical = true
		}
		if m := createTableRe.FindStringSubmatch(line); len(m) == 2 {
			name := strings.Trim(m[1], `"`)
			if !seen[name] {
				seen[name] = true
				info.tables = append(info.tables, name)
			}
		}
		info.statements += strings.Count(line, ";")
	}
	info.header = strings.Join(headerLines, "\n")
	if info.incomplete && !info.logical {
		info.logical = true
	}
	return info
}
