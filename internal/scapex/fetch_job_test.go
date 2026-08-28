package scapex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestNormalizePolicyNeverPassesThroughRaw(t *testing.T) {
	raw := "m4a-0; rm -rf /"
	got := FormatArgs(raw)
	joined := strings.Join(got, " ")
	if strings.Contains(joined, raw) || strings.Contains(joined, "rm -rf") || strings.Contains(joined, ";") {
		t.Fatalf("interpolated policy: %q", got)
	}
	if len(got) < 3 || got[2] != "m4a" {
		t.Fatalf("default m4a args: %q", got)
	}
	if NormalizePolicy("opus-0") != "opus-0" || FormatArgs("opus")[2] != "opus" {
		t.Fatal("opus")
	}
	if BestAllowed([]string{"mp3-0", "opus-0"}) != "opus-0" {
		t.Fatal("rank")
	}
}

func TestCoalesceKeyIncludesDestAndPolicy(t *testing.T) {
	lib := uuid.MustParse("00000000-0000-4000-8000-000000000020")
	a := CoalesceKey("youtube", "dQw4w9WgXcQ", lib.String(), "m4a-0")
	b := CoalesceKey("youtube", "dQw4w9WgXcQ", lib.String(), "opus-0")
	c := CoalesceKey("youtube", "dQw4w9WgXcQ", uuid.New().String(), "m4a-0")
	if a == b || a == c {
		t.Fatal(a, b, c)
	}
}

func TestFinalizeDownloadDoesNotUseSharedInboxName(t *testing.T) {
	inbox := t.TempDir()
	d := NewDockWithPool(nil, inbox)
	srcDir := d.JobInbox(uuid.New())
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(srcDir, "dQw4w9WgXcQ.m4a")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := d.FinalizeDownload(LocalTrack{Path: p, VideoID: "dQw4w9WgXcQ"})
	if err != nil {
		t.Fatal(err)
	}
	shared := SharedInboxName(inbox, "dQw4w9WgXcQ", ".m4a")
	if filepath.Clean(got.Path) == filepath.Clean(shared) {
		t.Fatal("copied to shared inbox filename")
	}
	if _, err := os.Stat(shared); err == nil {
		t.Fatal("shared inbox file exists")
	}
	if !strings.Contains(filepath.ToSlash(got.Path), "/jobs/") {
		t.Fatalf("not job-scoped: %s", got.Path)
	}
}

func TestJobInboxNotSharedFilename(t *testing.T) {
	inbox := t.TempDir()
	d := NewDockWithPool(nil, inbox)
	job := uuid.New()
	dir := d.JobInbox(job)
	if filepath.Dir(dir) != filepath.Join(inbox, "jobs") {
		t.Fatalf("job inbox %s", dir)
	}
	shared := SharedInboxName(inbox, "dQw4w9WgXcQ", ".m4a")
	if dir == shared || filepath.Base(dir) == "dQw4w9WgXcQ.m4a" {
		t.Fatal(dir)
	}
}

func TestQuotaFailDoesNotCommitFiles(t *testing.T) {
	inbox := t.TempDir()
	d := NewDockWithPool(nil, inbox)
	src := filepath.Join(t.TempDir(), "dQw4w9WgXcQ.m4a")
	if err := os.WriteFile(src, []byte("audio-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	yt := quotaYT{stubYT: stubYT{tracks: []LocalTrack{{
		Path: src, VideoID: "dQw4w9WgXcQ", Title: "Numb",
	}}}, destCopy: true}
	svc := NewService(yt, d)
	jobID := uuid.New()
	_, err := svc.RunFetchJob(context.Background(), FetchOpts{
		JobID:       jobID,
		URLs:        []string{"dQw4w9WgXcQ"},
		DestLibrary: uuid.New(),
		Policy:      DefaultMediaPolicy,
		Quota: func(context.Context, uuid.UUID, uuid.UUID, int64) error {
			return errString("library storage quota exceeded")
		},
	})
	if err == nil {
		t.Fatal("expected quota error")
	}
	if _, err := os.Stat(d.JobInbox(jobID)); err == nil {
		t.Fatal("committed job inbox despite quota fail")
	}
	if _, err := os.Stat(SharedInboxName(inbox, "dQw4w9WgXcQ", ".m4a")); err == nil {
		t.Fatal("shared inbox file committed")
	}
}

func TestRunFetchJobRejectsNonYouTube(t *testing.T) {
	d := NewDockWithPool(nil, t.TempDir())
	svc := NewService(stubYT{}, d)
	_, err := svc.RunFetchJob(context.Background(), FetchOpts{
		URLs:        []string{"https://example.com/a.mp3"},
		DestLibrary: uuid.New(),
	})
	if err == nil {
		t.Fatal("expected reject")
	}
}

type quotaYT struct {
	stubYT
	destCopy bool
}

func (q quotaYT) FetchPolicy(ctx context.Context, mediaURL, destDir, policy string) ([]LocalTrack, error) {
	return q.Fetch(ctx, mediaURL, destDir)
}

func (q quotaYT) Fetch(_ context.Context, _, destDir string) ([]LocalTrack, error) {
	if !q.destCopy {
		return q.tracks, nil
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}
	var out []LocalTrack
	for _, tr := range q.tracks {
		dest := filepath.Join(destDir, tr.VideoID+filepath.Ext(tr.Path))
		b, err := os.ReadFile(tr.Path)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(dest, b, 0o644); err != nil {
			return nil, err
		}
		tr.Path = dest
		out = append(out, tr)
	}
	return out, nil
}

type errString string

func (e errString) Error() string { return string(e) }
