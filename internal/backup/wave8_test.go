package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sounddock/sounddock/internal/config"
	cryptox "github.com/sounddock/sounddock/internal/crypto"
	"github.com/sounddock/sounddock/internal/db"
	"github.com/sounddock/sounddock/internal/testdb"
)

func testPool(t *testing.T) *pgxpool.Pool {
	return testdb.Open(t)
}

func testService(t *testing.T, pool *pgxpool.Pool) *Service {
	t.Helper()
	dir := t.TempDir()
	dsn := os.Getenv("SD_TEST_DATABASE_URL")
	if dsn == "" && pool != nil {
		dsn = "postgres://unused"
	}
	box, err := cryptox.New("wave8-master-key-test")
	if err != nil {
		t.Fatal(err)
	}
	s := New(pool, dir, dsn)
	s.Attach(filepath.Join(dir, "managed"), box)
	s.Configure(config.Config{
		MasterKey:    "wave8-master-key-test",
		DataDir:      dir,
		CacheDir:     filepath.Join(dir, "cache"),
		InstanceName: "Wave8Test",
	})
	s.Restart = func() {}
	s.lookPath = func(string) (string, error) { return "pg_dump", nil }
	s.dumpFn = func(ctx context.Context, dest string) error {
		return os.WriteFile(dest, []byte("-- SoundDock test dump\nCREATE TABLE wave8_ok (id int);\n"), 0o644)
	}
	if pool != nil {
		_, _ = pool.Exec(context.Background(), `DELETE FROM server_settings WHERE key=$1`, SettingKey)
	}
	return s
}

func TestNoDumpFailsWithoutRow(t *testing.T) {
	s := &Service{
		lookPath: func(string) (string, error) { return "", exec.ErrNotFound },
	}
	if err := s.requireDumpTool(); err == nil {
		t.Fatal("expected dump-or-fail")
	}
	pool := optionalPool(t)
	if pool == nil {
		_, err := s.Run(context.Background())
		if err == nil {
			t.Fatal("expected fail")
		}
		return
	}
	s.pool = pool
	s.dir = t.TempDir()
	before, _ := s.countBackups(context.Background())
	_, err := s.Run(context.Background())
	if err == nil {
		t.Fatal("expected fail")
	}
	after, _ := s.countBackups(context.Background())
	if after != before {
		t.Fatalf("backup row inserted: %d -> %d", before, after)
	}
}

func TestScheduledCannotEnableWithoutPassphrase(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	err := s.SaveSettings(context.Background(), Settings{LocalEnabled: true, ScheduledEnabled: true})
	if err == nil || !strings.Contains(err.Error(), "passphrase") {
		t.Fatalf("err=%v", err)
	}
}

func TestNoPassphraseRefusesBackup(t *testing.T) {
	s := &Service{
		lookPath: func(string) (string, error) { return "pg_dump", nil },
		dumpFn: func(ctx context.Context, dest string) error {
			t.Fatal("dump should not run")
			return nil
		},
	}
	_, err := s.Run(context.Background())
	if err != ErrPassphraseRequired {
		t.Fatalf("err=%v", err)
	}
}

func TestScheduledBackupNoPassphraseInRequest(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	ctx := context.Background()
	if _, err := s.SetPassphrase(ctx, "recovery-pass-12", ""); err != nil {
		t.Fatal(err)
	}
	id, err := s.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if id == uuid.Nil {
		t.Fatal("empty id")
	}
	rec, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Kind != "full" {
		t.Fatalf("kind=%s", rec.Kind)
	}
	if !isEncryptedArchive(rec.Path) {
		t.Fatal("archive should be encrypted")
	}
}

func TestWrongPassphraseNoWipe(t *testing.T) {
	s := testService(t, optionalPool(t))
	path := mustTinyArchive(t, s, "recovery-pass-12")
	s.wiped = false
	s.WipeFn = func(ctx context.Context) error { return nil }
	_, _, err := s.unlockArchive(path, "wrong-passphrase-xx")
	if err == nil {
		t.Fatal("expected wrong passphrase")
	}
	if s.pool != nil {
		if _, err := restorePath(t, s, path, "wrong-passphrase-xx"); err == nil {
			t.Fatal("expected restore fail")
		}
	}
	if s.Wiped() {
		t.Fatal("wipe ran")
	}
}

func TestCorruptionNoWipe(t *testing.T) {
	s := testService(t, optionalPool(t))
	path := mustTinyArchive(t, s, "recovery-pass-12")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 80 {
		t.Fatal("archive too small")
	}
	b[len(b)-8] ^= 0xff
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	s.wiped = false
	s.WipeFn = func(ctx context.Context) error { return nil }
	dek, _, err := s.unlockArchive(path, "recovery-pass-12")
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.VerifyArchive(path, dek); ok {
		t.Fatal("corrupt archive verified")
	}
	if s.pool != nil {
		if _, err := restorePath(t, s, path, "recovery-pass-12"); err == nil {
			t.Fatal("expected restore fail")
		}
	}
	if s.Wiped() {
		t.Fatal("wipe ran")
	}
}

func TestWipeAndApplyTestdb(t *testing.T) {
	dsn := os.Getenv("SD_TEST_MIGRATE_URL")
	if dsn == "" {
		t.Skip("SD_TEST_MIGRATE_URL not set")
	}
	if _, err := exec.LookPath("psql"); err != nil {
		t.Skip("psql not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	s := New(pool, dir, dsn)
	s.Restart = func() {}
	sqlPath := filepath.Join(dir, "probe.sql")
	if err := os.WriteFile(sqlPath, []byte("CREATE TABLE wave8_probe (id int);\nINSERT INTO wave8_probe VALUES (7);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Migrate(dsn)
	})
	if err := s.wipeDatabase(ctx); err != nil {
		t.Fatal(err)
	}
	if !s.Wiped() {
		t.Fatal("expected wipe")
	}
	if err := s.applySQL(ctx, sqlPath); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT id FROM wave8_probe`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 7 {
		t.Fatalf("id=%d", n)
	}
}

func TestNASNotPacked(t *testing.T) {
	dir := t.TempDir()
	managed := filepath.Join(dir, "managed")
	nas := filepath.Join(dir, "nas")
	_ = os.MkdirAll(managed, 0o755)
	_ = os.MkdirAll(nas, 0o755)
	if err := os.WriteFile(filepath.Join(managed, "song.bin"), []byte("managed-audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nas, "nas-song.bin"), []byte("nas-audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Service{media: managed}
	work := filepath.Join(dir, "work")
	_ = os.MkdirAll(work, 0o755)
	sql := filepath.Join(dir, "db.sql")
	if err := os.WriteFile(sql, []byte("CREATE TABLE t(id int);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.stageInner(work, sql, true, RestoreRequirements{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(work, "managed", "song.bin")); err != nil {
		t.Fatal("managed file missing")
	}
	if _, err := os.Stat(filepath.Join(work, "managed", "nas-song.bin")); err == nil {
		t.Fatal("NAS file was packed")
	}
	found := false
	_ = filepath.Walk(work, func(p string, info os.FileInfo, err error) error {
		if info != nil && info.Name() == "nas-song.bin" {
			found = true
		}
		return nil
	})
	if found {
		t.Fatal("NAS file present in staged tree")
	}
}

func TestSecretsAbsentFromCiphertextAndPublic(t *testing.T) {
	pass := "recovery-pass-12"
	master := "super-secret-master-key"
	dir := t.TempDir()
	managed := filepath.Join(dir, "managed")
	_ = os.MkdirAll(managed, 0o755)
	if err := os.WriteFile(filepath.Join(managed, ".env"), []byte("SD_MASTER_KEY="+master+"\nPASSPHRASE="+pass+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(managed, "master.key"), []byte(master), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Service{media: managed, master: master, instance: "Peek"}
	work := filepath.Join(dir, "work")
	_ = os.MkdirAll(work, 0o755)
	sql := filepath.Join(dir, "db.sql")
	if err := os.WriteFile(sql, []byte("CREATE TABLE t(id int);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.stageInner(work, sql, true, RestoreRequirements{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(work, "managed", ".env")); err == nil {
		t.Fatal(".env was staged")
	}
	if _, err := os.Stat(filepath.Join(work, "managed", "master.key")); err == nil {
		t.Fatal("master.key was staged")
	}
	inner := filepath.Join(dir, "inner.tar.gz")
	if err := packInnerArchive(inner, work, true, "Peek", ImageSchemaHead()); err != nil {
		t.Fatal(err)
	}
	dek, err := newDEK()
	if err != nil {
		t.Fatal(err)
	}
	kdf := defaultKDF()
	box, err := wrapRecovery(pass, dek, []byte(master), kdf)
	if err != nil {
		t.Fatal(err)
	}
	arch := filepath.Join(dir, "out.sdar")
	if err := encryptArchiveFile(arch, inner, dek, box, kdf); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(arch)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(arch)
	if err != nil {
		t.Fatal(err)
	}
	hdr, err := readClearHeader(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	ct := raw[len(raw)-len(hdr.Box)-32:]
	for _, needle := range []string{pass, master, "SD_MASTER_KEY=", "PASSPHRASE="} {
		if bytes.Contains(ct, []byte(needle)) || bytes.Contains(raw[64:], []byte(needle)) {
			t.Fatalf("plaintext %q found in ciphertext peek", needle)
		}
	}
	pub := Settings{
		RestorePassphraseSet: true,
		SecretKey:            "must-strip",
	}.Public()
	b, _ := json.Marshal(pub)
	js := string(b)
	if strings.Contains(js, "must-strip") || strings.Contains(js, "dek_enc") || strings.Contains(js, "recovery_box") || strings.Contains(js, pass) {
		t.Fatalf("Public leaked secrets: %s", js)
	}
}

func TestRestoreRequirementsListsEnvOnly(t *testing.T) {
	t.Setenv("SD_PUBLIC_URL", "https://music.example.test")
	t.Setenv("SD_LIBRARY_HOST", "/nas/music")
	req := ClassifyRestoreRequirements(config.Config{InstanceName: "ReqTest", MasterKey: "k"})
	var sawURL, sawLib bool
	for _, it := range req.Items {
		if it.Key == "SD_PUBLIC_URL" {
			sawURL = true
			if it.Class != ClassMustReenter {
				t.Fatalf("SD_PUBLIC_URL class=%s", it.Class)
			}
		}
		if it.Key == "SD_LIBRARY_HOST" {
			sawLib = true
			if it.Class != ClassMustReenter {
				t.Fatalf("SD_LIBRARY_HOST class=%s", it.Class)
			}
		}
		if it.Key == "SD_MASTER_KEY" && strings.Contains(strings.ToLower(it.Note+it.Source), "passphrase") {
			t.Fatal("requirements must not mention the passphrase secret")
		}
	}
	if !sawURL || !sawLib {
		t.Fatalf("missing env-only keys url=%v lib=%v items=%v", sawURL, sawLib, req.Items)
	}
}

func TestStreamEncryptRoundTrip(t *testing.T) {
	dek, err := newDEK()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	ew, err := newEncryptWriter(&buf, dek)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("wave8-chunk-"), 9000)
	if _, err := ew.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := ew.Close(); err != nil {
		t.Fatal(err)
	}
	dr, err := newDecryptReader(bytes.NewReader(buf.Bytes()), dek)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(dr)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("round trip mismatch")
	}
}

func TestPublicSettingsOmitsWrapMaterial(t *testing.T) {
	pool := testPool(t)
	s := testService(t, pool)
	ctx := context.Background()
	if _, err := s.SetPassphrase(ctx, "recovery-pass-12", ""); err != nil {
		t.Fatal(err)
	}
	pub := s.LoadSettings(ctx).Public()
	b, _ := json.Marshal(pub)
	js := string(b)
	for _, needle := range []string{"dek_enc", "recovery_box", "recovery-pass-12", "wave8-master-key-test"} {
		if strings.Contains(js, needle) {
			t.Fatalf("Public leaked %q: %s", needle, js)
		}
	}
	if !pub.RestorePassphraseSet {
		t.Fatal("expected restore_passphrase_set")
	}
}

func optionalPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("SD_TEST_DATABASE_URL")
	if dsn == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil
	}
	t.Cleanup(pool.Close)
	return pool
}

func mustTinyArchive(t *testing.T, s *Service, pass string) string {
	t.Helper()
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	_ = os.MkdirAll(work, 0o755)
	sql := filepath.Join(dir, "db.sql")
	if err := os.WriteFile(sql, []byte("CREATE TABLE t(id int);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.stageInner(work, sql, false, RestoreRequirements{SchemaVersion: ImageSchemaHead()}); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(dir, "inner.tar.gz")
	if err := packInnerArchive(inner, work, false, "t", ImageSchemaHead()); err != nil {
		t.Fatal(err)
	}
	dek, err := newDEK()
	if err != nil {
		t.Fatal(err)
	}
	kdf := defaultKDF()
	box, err := wrapRecovery(pass, dek, []byte(s.master), kdf)
	if err != nil {
		t.Fatal(err)
	}
	arch := filepath.Join(s.dir, "tiny.sdar")
	if err := encryptArchiveFile(arch, inner, dek, box, kdf); err != nil {
		t.Fatal(err)
	}
	return arch
}

func restorePath(t *testing.T, s *Service, path, pass string) (RestoreRequirements, error) {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO backups (path, size_bytes, checksum, status, destination, kind, remote_key)
		VALUES ($1, 1, '', 'created', 'local', 'full', '') RETURNING id`, path).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM backups WHERE id=$1`, id)
	})
	return s.Restore(ctx, id, pass)
}
