package scapex

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/jobs"
)

// QuotaFunc is Server.CheckQuota. Fail the job if extra bytes would exceed the cap.
type QuotaFunc func(ctx context.Context, userID, libraryID uuid.UUID, extraBytes int64) error

type FetchOpts struct {
	JobID       uuid.UUID
	URLs        []string
	DestLibrary uuid.UUID
	Policy      string
	UserID      uuid.UUID
	Quota       QuotaFunc
	Player      Player
	// Cancel is polled before the commit fence (moveDir). After commit, success wins.
	Cancel func(context.Context) bool
}

func (d *Dock) JobInbox(jobID uuid.UUID) string {
	if d == nil {
		return ""
	}
	id := jobID.String()
	if jobID == uuid.Nil {
		id = uuid.NewString()
	}
	return filepath.Join(d.inbox, "jobs", id)
}

func (d *Dock) JobWork(jobID uuid.UUID) string {
	if d == nil {
		return ""
	}
	id := jobID.String()
	if jobID == uuid.Nil {
		id = uuid.NewString()
	}
	return filepath.Join(filepath.Dir(d.inbox), ".scapex-work", id)
}

func (s *Service) download(ctx context.Context, src, dest, policy string) ([]LocalTrack, error) {
	if p, ok := s.yt.(interface {
		FetchPolicy(context.Context, string, string, string) ([]LocalTrack, error)
	}); ok {
		return p.FetchPolicy(ctx, src, dest, policy)
	}
	return s.yt.Fetch(ctx, src, dest)
}

func (s *Service) RunFetchJob(ctx context.Context, opts FetchOpts) ([]uuid.UUID, error) {
	if s.dock == nil {
		return nil, fmt.Errorf("SoundDock volume/database is not attached")
	}
	jobID := opts.JobID
	if jobID == uuid.Nil {
		jobID = uuid.New()
	}
	policy := NormalizePolicy(opts.Policy)
	lib := opts.DestLibrary
	if lib == uuid.Nil {
		var err error
		lib, err = s.dock.LibraryID(ctx)
		if err != nil {
			return nil, fmt.Errorf("no writable SoundDock library: %w", err)
		}
	}
	work := s.dock.JobWork(jobID)
	commit := s.dock.JobInbox(jobID)
	defer os.RemoveAll(work)

	fail := func(err error) ([]uuid.UUID, error) {
		os.RemoveAll(work)
		os.RemoveAll(commit)
		wrapped := WrapFetchError(err)
		switch ClassifyFetchError(wrapped) {
		case FetchCancelled:
			_ = SetIntentsStatus(ctx, s.dock.pool, jobID, StatusCancelled, nil)
			DropUncommitted(ctx, s.dock.pool, opts.Player, jobID)
		case FetchTerminal:
			_ = FailJobIntents(ctx, s.dock.pool, jobID, wrapped.Error())
			DropUncommitted(ctx, s.dock.pool, opts.Player, jobID)
		}
		return nil, wrapped
	}

	if ids, ok := s.dock.ReadyOriginals(ctx, jobID, lib); ok {
		_ = SetIntentsStatus(ctx, s.dock.pool, jobID, StatusReady, []string{StatusQueued, StatusDownloading, StatusProcessing, StatusScanning})
		if err := ApplyReadyIntents(ctx, s.dock.pool, opts.Player, jobID); err != nil {
			return ids, WrapFetchError(err)
		}
		return ids, nil
	}

	_ = SetIntentsStatus(ctx, s.dock.pool, jobID, StatusDownloading, []string{StatusQueued})

	var locals []LocalTrack
	for _, raw := range opts.URLs {
		src := WatchURL(raw)
		if src == "" || VideoID(src) == "" {
			return fail(fmt.Errorf("not an allowlisted YouTube watch URL or video id"))
		}
		if s.dock.HasOriginal(ctx, lib, VideoID(src)) {
			continue
		}
		got, err := s.download(ctx, src, work, policy)
		if err != nil {
			return fail(err)
		}
		locals = append(locals, got...)
	}
	if len(locals) == 0 {
		if ids, ok := s.dock.ReadyOriginals(ctx, jobID, lib); ok {
			_ = SetIntentsStatus(ctx, s.dock.pool, jobID, StatusReady, []string{StatusQueued, StatusDownloading, StatusProcessing, StatusScanning})
			_ = ApplyReadyIntents(ctx, s.dock.pool, opts.Player, jobID)
			return ids, nil
		}
		return fail(fmt.Errorf("yt-dlp produced no audio"))
	}

	var extra int64
	for _, loc := range locals {
		if st, err := os.Stat(loc.Path); err == nil {
			extra += st.Size()
		}
	}
	if opts.Quota != nil {
		if err := opts.Quota(ctx, opts.UserID, lib, extra); err != nil {
			return fail(err)
		}
	}

	if cancelled(ctx, opts) {
		return fail(jobsCancelled())
	}

	_ = SetIntentsStatus(ctx, s.dock.pool, jobID, StatusProcessing, []string{StatusDownloading, StatusQueued})
	if err := os.RemoveAll(commit); err != nil && !os.IsNotExist(err) {
		return fail(err)
	}
	if cancelled(ctx, opts) {
		return fail(jobsCancelled())
	}
	if err := moveDir(work, commit); err != nil {
		return fail(err)
	}
	for i := range locals {
		locals[i].Path = filepath.Join(commit, filepath.Base(locals[i].Path))
	}

	_ = SetIntentsStatus(ctx, s.dock.pool, jobID, StatusScanning, []string{StatusProcessing, StatusDownloading})
	ids, err := s.dock.AttachLocals(ctx, lib, jobID, locals)
	if err != nil {
		return fail(err)
	}
	for i, loc := range locals {
		id := uuid.Nil
		if i < len(ids) {
			id = ids[i]
		}
		if vid := VideoID(loc.VideoID); vid != "" && id != uuid.Nil {
			_ = RecordTrackSource(ctx, s.dock.pool, id, ProviderYouTube, vid, jobID)
		}
	}

	_ = SetIntentsStatus(ctx, s.dock.pool, jobID, StatusReady, []string{StatusScanning, StatusProcessing, StatusDownloading, StatusQueued})
	if err := ApplyReadyIntents(ctx, s.dock.pool, opts.Player, jobID); err != nil {
		return ids, WrapFetchError(err)
	}
	return ids, nil
}

func cancelled(ctx context.Context, opts FetchOpts) bool {
	if ctx.Err() != nil {
		return true
	}
	if opts.Cancel != nil && opts.Cancel(ctx) {
		return true
	}
	return false
}

func jobsCancelled() error {
	return fmt.Errorf("%w: cancel requested before commit", jobs.ErrCancelled)
}


func moveDir(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o775); err != nil {
		return err
	}
	if err := os.Rename(src, dest); err == nil {
		return nil
	}
	if err := os.MkdirAll(dest, 0o775); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		from := filepath.Join(src, e.Name())
		to := filepath.Join(dest, e.Name())
		if e.IsDir() {
			if err := moveDir(from, to); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(from, to); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o664)
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

func SharedInboxName(inbox, videoID, ext string) string {
	return filepath.Join(inbox, strings.TrimSpace(videoID)+ext)
}
