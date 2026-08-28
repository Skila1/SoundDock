package scapex

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
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

	_ = SetIntentsStatus(ctx, s.dock.pool, jobID, StatusDownloading, []string{StatusQueued})

	var locals []LocalTrack
	for _, raw := range opts.URLs {
		src := WatchURL(raw)
		if src == "" || VideoID(src) == "" {
			os.RemoveAll(commit)
			err := fmt.Errorf("not an allowlisted YouTube watch URL or video id")
			_ = FailJobIntents(ctx, s.dock.pool, jobID, err.Error())
			return nil, err
		}
		got, err := s.download(ctx, src, work, policy)
		if err != nil {
			os.RemoveAll(commit)
			_ = FailJobIntents(ctx, s.dock.pool, jobID, err.Error())
			return nil, err
		}
		locals = append(locals, got...)
	}
	if len(locals) == 0 {
		os.RemoveAll(commit)
		err := fmt.Errorf("yt-dlp produced no audio")
		_ = FailJobIntents(ctx, s.dock.pool, jobID, err.Error())
		return nil, err
	}

	var extra int64
	for _, loc := range locals {
		if st, err := os.Stat(loc.Path); err == nil {
			extra += st.Size()
		}
	}
	if opts.Quota != nil {
		if err := opts.Quota(ctx, opts.UserID, lib, extra); err != nil {
			os.RemoveAll(commit)
			_ = FailJobIntents(ctx, s.dock.pool, jobID, err.Error())
			return nil, err
		}
	}

	_ = SetIntentsStatus(ctx, s.dock.pool, jobID, StatusProcessing, []string{StatusDownloading, StatusQueued})
	if err := os.RemoveAll(commit); err != nil && !os.IsNotExist(err) {
		_ = FailJobIntents(ctx, s.dock.pool, jobID, err.Error())
		return nil, err
	}
	if err := moveDir(work, commit); err != nil {
		os.RemoveAll(commit)
		_ = FailJobIntents(ctx, s.dock.pool, jobID, err.Error())
		return nil, err
	}
	for i := range locals {
		locals[i].Path = filepath.Join(commit, filepath.Base(locals[i].Path))
	}

	_ = SetIntentsStatus(ctx, s.dock.pool, jobID, StatusScanning, []string{StatusProcessing, StatusDownloading})
	if err := s.dock.EnqueueInboxScan(ctx, lib); err != nil {
		os.RemoveAll(commit)
		_ = FailJobIntents(ctx, s.dock.pool, jobID, err.Error())
		return nil, err
	}

	var ids []uuid.UUID
	for _, loc := range locals {
		id, err := s.dock.WaitTrack(ctx, lib, loc.VideoID, loc.Title)
		if err != nil {
			_ = FailJobIntents(ctx, s.dock.pool, jobID, err.Error())
			return ids, err
		}
		ids = append(ids, id)
		_ = RecordTrackSource(ctx, s.dock.pool, id, ProviderYouTube, loc.VideoID, jobID)
	}

	_ = SetIntentsStatus(ctx, s.dock.pool, jobID, StatusReady, []string{StatusScanning, StatusProcessing, StatusDownloading, StatusQueued})
	if err := ApplyReadyIntents(ctx, s.dock.pool, opts.Player, jobID); err != nil {
		_ = FailJobIntents(ctx, s.dock.pool, jobID, err.Error())
		return ids, err
	}
	return ids, nil
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
