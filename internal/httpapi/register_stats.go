package httpapi

import (
	"context"
	"log/slog"

	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/stats"
)

func (s *Server) RegisterStatsJobs() {
	if s.Jobs == nil {
		return
	}
	s.Jobs.Register("stats.rebuild", s.jobStatsRebuild)
}

// listenReaderEvents is true when recap readers must query listen_events only.
// A missing server_settings.listen_reader key defaults to listen_history.
func (s *Server) listenReaderEvents(ctx context.Context) bool {
	if s == nil || s.Pool == nil {
		return false
	}
	return stats.ReaderIsEvents(ctx, s.Pool)
}

// setListenReader stores "events" or "history". Rebuild flips to events last.
func (s *Server) setListenReader(ctx context.Context, mode string) error {
	if s == nil || s.Pool == nil {
		return errString("listen reader: no database")
	}
	return stats.SetReader(ctx, s.Pool, mode)
}

// rebuildAbortOnCancel refuses to abort once the play_counts swap has started.
func rebuildAbortOnCancel(swapStarted, cancelRequested bool) bool {
	if swapStarted {
		return false
	}
	return cancelRequested
}

func (s *Server) jobStatsRebuild(ctx context.Context, job jobs.Job) error {
	if s.Pool == nil {
		return errString("stats.rebuild: no database")
	}
	cancelRequested := s.Jobs != nil && s.Jobs.Cancelled(ctx, job.ID)
	if rebuildAbortOnCancel(false, cancelRequested) {
		return context.Canceled
	}

	histN, evN, cmpErr := queryListenPlayCountsSeparate(ctx, s.Pool)
	if s.Log != nil && cmpErr == nil {
		s.Log.Info("stats.rebuild dual-read",
			slog.Int64("history_plays", histN),
			slog.Int64("events_qualified_plays", evN))
	}
	if s.Jobs != nil {
		s.Jobs.SetProgress(ctx, job.ID, 15)
	}

	// Swap started: ignore RequestCancel. Flag flip is last inside CutoverToEvents.
	res, err := stats.CutoverToEvents(ctx, s.Pool)
	if err != nil {
		return err
	}
	if s.Jobs != nil {
		s.Jobs.SetProgress(ctx, job.ID, 100)
		s.Jobs.SetResult(ctx, job.ID, map[string]any{
			"listen_reader":          res.Reader,
			"play_counts_rows":       res.PlayCountRows,
			"history_plays":          histN,
			"events_qualified_plays": evN,
			"history_kept":           true,
		})
	}
	return nil
}
