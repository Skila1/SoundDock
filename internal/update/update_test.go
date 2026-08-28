package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseImage(t *testing.T) {
	h, r, tag := parseImage("ghcr.io/skila1/sounddock:latest")
	if h != "ghcr.io" || r != "skila1/sounddock" || tag != "latest" {
		t.Fatalf("%s %s %s", h, r, tag)
	}
	h, r, tag = parseImage("postgres:16-alpine")
	if h != "docker.io" || r != "library/postgres" || tag != "16-alpine" {
		t.Fatalf("%s %s %s", h, r, tag)
	}
}

func TestDigestEqual(t *testing.T) {
	if !digestEqual("sha256:ABC", "SHA256:abc") {
		t.Fatal("expected equal")
	}
	if digestEqual("", "") {
		t.Fatal("empty is not a match")
	}
}

func TestParseChangelog(t *testing.T) {
	md := "# Changelog\n\n## 0.0.8\n\n- Host pull progress.\n- Changelog on the Updates page.\n\n## 0.0.7\n\n- Zip uploads.\n"
	latest, notes := ParseChangelog(md, "0.0.7")
	if latest != "0.0.8" {
		t.Fatalf("latest %s", latest)
	}
	if len(notes) != 1 || notes[0].Version != "0.0.8" || len(notes[0].Notes) != 2 {
		t.Fatalf("%#v", notes)
	}
	_, none := ParseChangelog(md, "0.0.8")
	if len(none) != 0 {
		t.Fatalf("expected no newer notes, got %#v", none)
	}
}

func TestInferProgress(t *testing.T) {
	p := inferProgress("----\nsounddock Pulling\na1b2: Downloading 40%\n", true)
	if p.Stage != "pulling" || p.Percent < 10 {
		t.Fatalf("%#v", p)
	}
	p = inferProgress("pulled\nStarted\ndone\n", false)
	if p.Stage != "done" || p.Percent != 100 {
		t.Fatalf("%#v", p)
	}
}

func TestWriteHostRunner(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SD_UPDATE_DIR", dir)
	if err := WriteHostRunner(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "docker compose pull") || !strings.Contains(string(b), "docker compose up -d") {
		t.Fatalf("script %s", b)
	}
}

func TestRequestUpdate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SD_UPDATE_DIR", dir)
	if HelperOK() {
		t.Fatal("writable dir without helper marker is not a host helper")
	}
	if err := os.WriteFile(filepath.Join(dir, "helper"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !HelperOK() {
		t.Fatal("expected helper marker to be enough")
	}
	if err := RequestUpdate("skila"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "request"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "skila") {
		t.Fatalf("got %s", b)
	}
	if err := os.WriteFile(filepath.Join(dir, "applied"), []byte("ghcr.io/skila1/sounddock@sha256:abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	if AppliedDigest() != "sha256:abc" {
		t.Fatalf("digest %s", AppliedDigest())
	}
	old := filepath.Join(dir, "request")
	past := time.Now().Add(-31 * time.Minute)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	if RequestPending() {
		t.Fatal("stale request should not count as in progress")
	}
	if _, err := os.Stat(old); err == nil {
		t.Fatal("stale request should be removed")
	}
}

func TestRequestUpdateReplaces(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SD_UPDATE_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "helper"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RequestUpdate("first"); err != nil {
		t.Fatal(err)
	}
	if err := RequestUpdate("second"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "request"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "second") {
		t.Fatalf("got %s", b)
	}
	if !HelperActive() {
		t.Fatal("queued progress should look active")
	}
	if !RequestPending() {
		t.Fatal("request should still be pending")
	}
	if helperTookOver() {
		t.Fatal("container-written queued progress is not a host takeover")
	}
	_ = os.Remove(filepath.Join(dir, "request"))
	writeProgress(12, "pulling", "Downloading layers")
	if !helperTookOver() {
		t.Fatal("host pull progress after removing request is a takeover")
	}
}

func TestImageRef(t *testing.T) {
	t.Setenv("SD_IMAGE", "")
	if ImageRef() != CanonicalImage+":latest" {
		t.Fatalf("default %s", ImageRef())
	}
	t.Setenv("SD_IMAGE", "evil.example/hack:latest")
	if ImageRef() != CanonicalImage+":latest" {
		t.Fatalf("non-canonical override leaked: %s", ImageRef())
	}
	t.Setenv("SD_IMAGE", CanonicalImage+":0.0.9")
	if ImageRef() != CanonicalImage+":0.0.9" {
		t.Fatalf("canonical tag should stick: %s", ImageRef())
	}
}

func TestSocketOKFalseWithoutFlag(t *testing.T) {
	t.Setenv("SD_ALLOW_DOCKER_SOCK", "")
	if SocketOK() {
		t.Fatal("socket must stay off unless SD_ALLOW_DOCKER_SOCK is set")
	}
	t.Setenv("SD_ALLOW_DOCKER_SOCK", "0")
	if SocketOK() {
		t.Fatal("explicit 0 must not enable the socket")
	}
}

func TestReconcileRequiresExpectedDigest(t *testing.T) {
	st := stored{LastStatus: "updating", LatestDigest: "sha256:expected", CurrentDigest: "sha256:old"}
	out, save := confirmApply(st, "sha256:other", true)
	if save || out.LastStatus == "ok" {
		t.Fatal("must not confirm without the expected digest")
	}
	out, save = confirmApply(st, "sha256:expected", false)
	if save || out.LastStatus == "ok" {
		t.Fatal("must not confirm without health")
	}
	out, save = confirmApply(st, "sha256:expected", true)
	if !save || out.LastStatus != "ok" {
		t.Fatalf("matching digest and health should confirm: save=%v status=%s", save, out.LastStatus)
	}
}

func TestNoSchemaUpdateFailureRollsBack(t *testing.T) {
	oldStarted := false
	res := RunTransaction(TxHooks{
		SchemaBefore:   23,
		TargetHead:     23,
		OldImageHead:   23,
		PreviousDigest: "sha256:old",
		NewDigest:      "sha256:new",
		Dump: func() (string, error) {
			t.Fatal("image_only must not dump")
			return "", nil
		},
		Pull:     func() error { return nil },
		StartNew: func() error { return nil },
		Health:   func() error { return errHealth },
		SchemaAfter: func() (int64, error) {
			return 23, nil
		},
		StartOld: func() error { oldStarted = true; return nil },
	})
	if res.Status != "rolled_back" {
		t.Fatalf("status=%s err=%v", res.Status, res.Err)
	}
	if !oldStarted {
		t.Fatal("no-schema failure must start the previous image")
	}
	if res.CurrentDigest != "sha256:old" {
		t.Fatalf("digest %s", res.CurrentDigest)
	}
	if res.NeedsRecovery {
		t.Fatal("image_only failure is not needs_recovery")
	}
}

func TestSchemaChangingUpdateFailureNeedsRecovery(t *testing.T) {
	oldStarted := false
	res := RunTransaction(TxHooks{
		SchemaBefore:   22,
		TargetHead:     23,
		OldImageHead:   22,
		PreviousDigest: "sha256:old",
		NewDigest:      "sha256:new",
		Dump:           func() (string, error) { return "/tmp/pre.sql", nil },
		Pull:           func() error { return nil },
		StartNew:       func() error { return nil },
		Health:         func() error { return errHealth },
		SchemaAfter:    func() (int64, error) { return 23, nil },
		StartOld:       func() error { oldStarted = true; return nil },
	})
	if res.Status != "needs_recovery" || !res.NeedsRecovery {
		t.Fatalf("status=%s recovery=%v", res.Status, res.NeedsRecovery)
	}
	if oldStarted {
		t.Fatal("schema-changing failure must not start the old image")
	}
	if !res.Dumped {
		t.Fatal("schema_forward must dump SQL first")
	}
}

func TestOldImageIncompatibleWhenSchemaExceedsHead(t *testing.T) {
	if OldImageCompatible(23, 22) {
		t.Fatal("current schema exceeds the old image head")
	}
	if !OldImageCompatible(22, 22) {
		t.Fatal("same head is compatible")
	}
	dec := DecideAfterFailure(22, 23, 22)
	if dec.StartOldImage || dec.Status != "needs_recovery" {
		t.Fatalf("%#v", dec)
	}
}

var errHealth = errString("unhealthy")

type errString string

func (e errString) Error() string { return string(e) }
