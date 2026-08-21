package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSQLDumpTables(t *testing.T) {
	src := `-- SoundDock logical backup
-- Dumped from database
CREATE TABLE users (
  id uuid
);
CREATE TABLE IF NOT EXISTS library_grants (id uuid);
INSERT INTO users VALUES ('x');
COPY tracks (id) FROM stdin;
`
	info := parseSQLDump(strings.NewReader(src))
	if !info.logical {
		t.Fatal("expected logical dump")
	}
	if len(info.tables) != 2 {
		t.Fatalf("tables=%v", info.tables)
	}
	if info.tables[0] != "users" || info.tables[1] != "library_grants" {
		t.Fatalf("tables=%v", info.tables)
	}
	if info.statements < 2 {
		t.Fatalf("statements=%d", info.statements)
	}
}

func TestParseSQLDumpIncomplete(t *testing.T) {
	src := "-- SoundDock logical backup now\n-- pg_dump unavailable: exec: \"pg_dump\": executable file not found\n"
	info := parseSQLDump(strings.NewReader(src))
	if !info.incomplete {
		t.Fatal("expected incomplete flag")
	}
}

func TestVerifyFileEmpty(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "empty.sql")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Service{}
	ok, detail := s.VerifyFile(p)
	if ok {
		t.Fatal("empty file should fail verify")
	}
	if !strings.Contains(detail, "empty") {
		t.Fatalf("detail=%s", detail)
	}
}

func TestVerifyFileOk(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ok.sql")
	if err := os.WriteFile(p, []byte("-- SoundDock\nCREATE TABLE t (id int);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Service{}
	ok, _ := s.VerifyFile(p)
	if !ok {
		t.Fatal("expected ok")
	}
}
