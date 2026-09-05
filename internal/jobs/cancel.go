package jobs

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
)

var ErrNotCancellable = errors.New("job cannot be cancelled")

// Stage values for scapex.fetch (acquisition_intents.status).
const (
	StageQueued      = "queued"
	StageDownloading = "downloading"
	StageProcessing  = "processing"
	StageScanning    = "scanning"
	StageReady       = "ready"
	StageApplied     = "applied"
)

// Extra is optional runtime state for cancel decisions.
type Extra struct {
	FetchStage        string // acquisition_intents status; empty if unknown
	RetentionDeleted  int    // tracks already deleted this run
	StatsSwapStarted  bool   // play_counts rebuild / reader flip in progress
}

func typeAllowlisted(typ string) bool {
	switch strings.TrimSpace(typ) {
	case "scapex.fetch", "tracks.metadata", "library.scan", "scan.duplicates", "lyrics.fetch",
		"maintenance.retention", "stats.rebuild", "metadata.refresh":
		return true
	default:
		return false
	}
}

func fetchCommitted(stage string) bool {
	switch strings.ToLower(strings.TrimSpace(stage)) {
	case StageProcessing, StageScanning, StageReady, StageApplied:
		return true
	default:
		return false
	}
}

// AllowCancel is the server-side allowlist. Unsupported types and unsafe
// stages return false even if the Workers UI would have shown a button.
func AllowCancel(typ, status string, progress int, extra Extra) bool {
	typ = strings.TrimSpace(typ)
	status = strings.ToLower(strings.TrimSpace(status))
	if status != "queued" && status != "retry" && status != "running" {
		return false
	}
	if !typeAllowlisted(typ) {
		return false
	}
	switch typ {
	case "scapex.fetch":
		if fetchCommitted(extra.FetchStage) {
			return false
		}
		if status == "running" && extra.FetchStage == "" && progress >= 50 {
			return false
		}
		return true
	case "library.scan", "tracks.metadata", "scan.duplicates", "lyrics.fetch", "metadata.refresh":
		return true
	case "maintenance.retention":
		if status == "queued" || status == "retry" {
			return true
		}
		return extra.RetentionDeleted == 0 && progress == 0
	case "stats.rebuild":
		if status == "running" || extra.StatsSwapStarted {
			return false
		}
		return true
	default:
		return false
	}
}

// MayCancel is a listing hint without extra DB round-trips.
func MayCancel(typ, status string, progress int) bool {
	return AllowCancel(typ, status, progress, Extra{})
}

func (r *Runner) fetchStage(ctx context.Context, id uuid.UUID) string {
	if r == nil || r.db == nil {
		return ""
	}
	var stage string
	_ = r.db.QueryRow(ctx, `
		SELECT status FROM acquisition_intents
		WHERE job_id=$1
		ORDER BY CASE status
			WHEN 'applied' THEN 0 WHEN 'ready' THEN 1 WHEN 'scanning' THEN 2
			WHEN 'processing' THEN 3 WHEN 'downloading' THEN 4
			ELSE 5 END
		LIMIT 1`, id).Scan(&stage)
	return stage
}

func (r *Runner) retentionDeleted(ctx context.Context, id uuid.UUID) int {
	if r == nil || r.db == nil {
		return 0
	}
	var n int
	_ = r.db.QueryRow(ctx, `
		SELECT coalesce((
			SELECT deleted_count FROM retention_runs WHERE job_id=$1 ORDER BY started_at DESC LIMIT 1
		), 0)`, id).Scan(&n)
	return n
}

func (r *Runner) cancelExtra(ctx context.Context, typ string, id uuid.UUID) Extra {
	ex := Extra{}
	switch typ {
	case "scapex.fetch":
		ex.FetchStage = r.fetchStage(ctx, id)
	case "maintenance.retention":
		ex.RetentionDeleted = r.retentionDeleted(ctx, id)
	}
	return ex
}

