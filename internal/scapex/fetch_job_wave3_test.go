package scapex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/jobs"
)

func TestRunFetchJobCancelBeforeCommit(t *testing.T) {
	d := NewDockWithPool(nil, t.TempDir())
	src := filepath.Join(t.TempDir(), "dQw4w9WgXcQ.m4a")
	if err := os.WriteFile(src, []byte("audio-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := NewService(quotaYT{stubYT: stubYT{tracks: []LocalTrack{{
		Path: src, VideoID: "dQw4w9WgXcQ", Title: "Numb",
	}}}, destCopy: true}, d)
	jobID := uuid.New()
	_, err := svc.RunFetchJob(context.Background(), FetchOpts{
		JobID:       jobID,
		URLs:        []string{"dQw4w9WgXcQ"},
		DestLibrary: uuid.New(),
		Cancel:      func(context.Context) bool { return true },
	})
	if ClassifyFetchError(err) != FetchCancelled {
		t.Fatalf("class %v err %v", ClassifyFetchError(err), err)
	}
	if !errors.Is(err, jobs.ErrCancelled) {
		t.Fatalf("want cancelled: %v", err)
	}
	if _, err := os.Stat(d.JobInbox(jobID)); err == nil {
		t.Fatal("commit dir must not remain after cancel")
	}
}

func TestRunFetchJobRetryableKeepsWorkUnfailed(t *testing.T) {
	d := NewDockWithPool(nil, t.TempDir())
	svc := NewService(errYT{err: errors.New("connection reset by peer")}, d)
	_, err := svc.RunFetchJob(context.Background(), FetchOpts{
		JobID:       uuid.New(),
		URLs:        []string{"dQw4w9WgXcQ"},
		DestLibrary: uuid.New(),
	})
	if ClassifyFetchError(err) != FetchRetryable {
		t.Fatalf("class %v err %v", ClassifyFetchError(err), err)
	}
}

func TestRunFetchJobTerminalInvalidURL(t *testing.T) {
	d := NewDockWithPool(nil, t.TempDir())
	svc := NewService(stubYT{}, d)
	_, err := svc.RunFetchJob(context.Background(), FetchOpts{
		URLs:        []string{"https://example.com/a.mp3"},
		DestLibrary: uuid.New(),
	})
	if ClassifyFetchError(err) != FetchTerminal {
		t.Fatalf("class %v err %v", ClassifyFetchError(err), err)
	}
	if !errors.Is(err, jobs.ErrTerminal) {
		t.Fatalf("want terminal: %v", err)
	}
}

type errYT struct{ err error }

func (e errYT) Search(context.Context, string, int) ([]Hit, error) { return nil, e.err }
func (e errYT) Fetch(context.Context, string, string) ([]LocalTrack, error) {
	return nil, e.err
}
