package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	listenCompareMatchKey  = "user_id + track_id + UTC calendar day of listen_history.played_at vs listen_events.started_at"
	listenCompareMatchNote = "There is no shared primary key. listen_history.id and listen_events.id are independent UUIDs; backfill inserts new event rows and does not preserve history ids. Day-bucket EXISTS is not 1:1 — multiple plays of the same track on the same UTC day collapse to a single match."
	listenComparePurpose   = "Validation only: listen_history and listen_events are compared in parallel. This is not a merged listen statistic. Production Home/Stats/Wrapped still read listen_history."
)

// listenCompareResponse is the admin dual-read report. It must never expose a
// single canonical total that adds history rows to event rows.
type listenCompareResponse struct {
	Ready   bool                 `json:"ready"`
	Message string               `json:"message,omitempty"`
	Note    string               `json:"note"`
	Period  listenComparePeriod  `json:"period"`
	History listenCompareHistory `json:"history"`
	Events  listenCompareEvents  `json:"events"`
	Diffs   *listenCompareDiffs  `json:"diffs"`
}

type listenComparePeriod struct {
	From   *time.Time `json:"from"`
	To     *time.Time `json:"to"`
	Preset string     `json:"preset"`
	Note   string     `json:"note"`
}

type listenCompareHistory struct {
	RowCount                        int64   `json:"row_count"`
	RowCountExcludingImport         int64   `json:"row_count_excluding_import"`
	ImportRowCount                  int64   `json:"import_row_count"`
	DistinctUsers                   int64   `json:"distinct_users"`
	DistinctUsersExcludingImport    int64   `json:"distinct_users_excluding_import"`
	DistinctTracks                  int64   `json:"distinct_tracks"`
	DistinctTracksExcludingImport   int64   `json:"distinct_tracks_excluding_import"`
	DurationMsSum                   int64   `json:"duration_ms_sum"`
	DurationMsSumExcludingImport    int64   `json:"duration_ms_sum_excluding_import"`
	EstimatedMinutes                float64 `json:"estimated_minutes"`
	EstimatedMinutesExcludingImport float64 `json:"estimated_minutes_excluding_import"`
	EstimatedMinutesImport          float64 `json:"estimated_minutes_import"`
	EstimatedMinutesSource          string  `json:"estimated_minutes_source"`
}

type listenCompareEvents struct {
	RowCount                  int64   `json:"row_count"`
	QualifiedPlay             int64   `json:"qualified_play"`
	QualifiedPlayLive         int64   `json:"qualified_play_live"`
	QualifiedPlayBackfill     int64   `json:"qualified_play_backfill"`
	Skipped                   int64   `json:"skipped"`
	KindSkip                  int64   `json:"kind_skip"`
	KindSkipUnqualified       int64   `json:"kind_skip_unqualified"`
	LegacyBackfill            int64   `json:"legacy_backfill"`
	Live                      int64   `json:"live"`
	DistinctUsers             int64   `json:"distinct_users"`
	DistinctTracks            int64   `json:"distinct_tracks"`
	ListenedMsSum             int64   `json:"listened_ms_sum"`
	ListenedMsIncomplete      bool    `json:"listened_ms_incomplete"`
	NullListenedMsCount       int64   `json:"null_listened_ms_count"`
	ListenedMinutesIncomplete float64 `json:"listened_minutes_incomplete"`
	OutputSegmentCount        int64   `json:"output_segment_count"`
	ListenedMsNote            string  `json:"listened_ms_note"`
}

type listenComparePair struct {
	History int64 `json:"history"`
	Events  int64 `json:"events"`
	Delta   int64 `json:"delta"`
}

type listenCompareDiffs struct {
	MatchKey     string `json:"match_key"`
	MatchKeyNote string `json:"match_key_note"`
	DeltaMeaning string `json:"delta_meaning"`

	HistoryPlaysVsQualifiesLive              listenComparePair `json:"history_plays_vs_qualifies_live"`
	HistoryPlaysVsQualifiesIncludingBackfill listenComparePair `json:"history_plays_vs_qualifies_including_backfill"`

	HistoryRowsWithNoMatchingEvent  int64 `json:"history_rows_with_no_matching_event"`
	LiveEventsWithNoMatchingHistory int64 `json:"live_events_with_no_matching_history"`

	PlayCountsSkipCount   int64  `json:"play_counts_skip_count"`
	SkipEvents            int64  `json:"skip_events"`
	SkipEventsUnqualified int64  `json:"skip_events_unqualified"`
	SkipDelta             int64  `json:"skip_delta"`
	SkipNote              string `json:"skip_note"`
}

func (s *Server) adminListenCompare(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	period, err := parseListenComparePeriod(r, now)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_period", err.Error())
		return
	}
	if s.Pool == nil {
		writeJSON(w, 200, listenCompareNotReady(period, "database unavailable"))
		return
	}
	resp, err := buildListenCompare(r.Context(), s.Pool, period)
	if err != nil {
		if isMissingListenSchema(err) {
			hist, _ := queryListenCompareHistory(r.Context(), s.Pool, period)
			writeJSON(w, 200, listenCompareNotReadyWithHistory(period, "listen_events tables are not present (migration 0015 pending)", hist))
			return
		}
		writeErr(w, http.StatusInternalServerError, "listen_compare_failed", err.Error())
		return
	}
	writeJSON(w, 200, resp)
}

func listenCompareNotReady(period listenComparePeriod, msg string) listenCompareResponse {
	return listenCompareNotReadyWithHistory(period, msg, listenCompareHistory{
		EstimatedMinutesSource: "sum(listen_history.duration_ms) / 60000",
	})
}

func listenCompareNotReadyWithHistory(period listenComparePeriod, msg string, hist listenCompareHistory) listenCompareResponse {
	if hist.EstimatedMinutesSource == "" {
		hist.EstimatedMinutesSource = "sum(listen_history.duration_ms) / 60000"
	}
	return listenCompareResponse{
		Ready:   false,
		Message: msg,
		Note:    listenComparePurpose,
		Period:  period,
		History: hist,
		Events: listenCompareEvents{
			ListenedMsIncomplete: true,
			ListenedMsNote:       "listen_events is not available. listened_ms is NULL on backfill rows and must not be treated as duration.",
		},
		Diffs: nil,
	}
}

func parseListenComparePeriod(r *http.Request, now time.Time) (listenComparePeriod, error) {
	q := r.URL.Query()
	fromRaw := strings.TrimSpace(q.Get("from"))
	toRaw := strings.TrimSpace(q.Get("to"))
	preset := strings.ToLower(strings.TrimSpace(q.Get("period")))

	if fromRaw != "" || toRaw != "" {
		var from, to time.Time
		var err error
		if fromRaw != "" {
			from, err = parseListenCompareTime(fromRaw)
			if err != nil {
				return listenComparePeriod{}, err
			}
		}
		if toRaw != "" {
			to, err = parseListenCompareTime(toRaw)
			if err != nil {
				return listenComparePeriod{}, err
			}
		} else {
			to = now.Add(time.Second)
		}
		if !from.IsZero() && !from.Before(to) {
			return listenComparePeriod{}, fmt.Errorf("from must be before to")
		}
		p := listenComparePeriod{
			Preset: "custom",
			Note:   "Custom RFC3339 window. Production readers still use listen_history only.",
		}
		if !from.IsZero() {
			f := from
			p.From = &f
		}
		t := to
		p.To = &t
		return p, nil
	}

	if preset == "all" {
		return listenComparePeriod{
			Preset: "all",
			Note:   "All-time window. Default without period=all is the last 30 days.",
		}, nil
	}

	from := now.AddDate(0, 0, -30)
	to := now.Add(time.Second)
	return listenComparePeriod{
		From:   &from,
		To:     &to,
		Preset: "last_30_days",
		Note:   "Default window is the last 30 days. Pass from and to as RFC3339, or period=all.",
	}, nil
}

func parseListenCompareTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("invalid time %q (use RFC3339)", s)
}

func (p listenComparePeriod) bounds() (from, to any) {
	if p.From != nil && !p.From.IsZero() {
		from = *p.From
	}
	if p.To != nil && !p.To.IsZero() {
		to = *p.To
	}
	return from, to
}

func buildListenCompare(ctx context.Context, pool *pgxpool.Pool, period listenComparePeriod) (listenCompareResponse, error) {
	ready, err := listenEventsTablesReady(ctx, pool)
	if err != nil {
		return listenCompareResponse{}, err
	}
	hist, err := queryListenCompareHistory(ctx, pool, period)
	if err != nil {
		return listenCompareResponse{}, err
	}
	if !ready {
		return listenCompareNotReadyWithHistory(period, "listen_events tables are not present (migration 0015 pending)", hist), nil
	}
	ev, err := queryListenCompareEvents(ctx, pool, period)
	if err != nil {
		if isMissingListenSchema(err) {
			return listenCompareNotReadyWithHistory(period, "listen_events tables are not present (migration 0015 pending)", hist), nil
		}
		return listenCompareResponse{}, err
	}
	diffs, err := queryListenCompareDiffs(ctx, pool, period, hist, ev)
	if err != nil {
		if isMissingListenSchema(err) {
			return listenCompareNotReadyWithHistory(period, "listen_events tables are not present (migration 0015 pending)", hist), nil
		}
		return listenCompareResponse{}, err
	}
	return listenCompareResponse{
		Ready:   true,
		Note:    listenComparePurpose,
		Period:  period,
		History: hist,
		Events:  ev,
		Diffs:   &diffs,
	}, nil
}

func listenEventsTablesReady(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	var events, segs bool
	err := pool.QueryRow(ctx, `
		SELECT
			to_regclass('public.listen_events') IS NOT NULL,
			to_regclass('public.listen_output_segments') IS NOT NULL`).Scan(&events, &segs)
	if err != nil {
		return false, err
	}
	return events && segs, nil
}

func queryListenCompareHistory(ctx context.Context, pool *pgxpool.Pool, period listenComparePeriod) (listenCompareHistory, error) {
	from, to := period.bounds()
	var h listenCompareHistory
	var importMS int64
	err := pool.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE source <> 'import'),
			count(*) FILTER (WHERE source = 'import'),
			count(DISTINCT user_id),
			count(DISTINCT user_id) FILTER (WHERE source <> 'import'),
			count(DISTINCT track_id),
			count(DISTINCT track_id) FILTER (WHERE source <> 'import'),
			coalesce(sum(duration_ms), 0),
			coalesce(sum(duration_ms) FILTER (WHERE source <> 'import'), 0),
			coalesce(sum(duration_ms) FILTER (WHERE source = 'import'), 0)
		FROM listen_history
		WHERE ($1::timestamptz IS NULL OR played_at >= $1)
			AND ($2::timestamptz IS NULL OR played_at < $2)`, from, to).Scan(
		&h.RowCount,
		&h.RowCountExcludingImport,
		&h.ImportRowCount,
		&h.DistinctUsers,
		&h.DistinctUsersExcludingImport,
		&h.DistinctTracks,
		&h.DistinctTracksExcludingImport,
		&h.DurationMsSum,
		&h.DurationMsSumExcludingImport,
		&importMS,
	)
	if err != nil {
		return listenCompareHistory{}, err
	}
	h.EstimatedMinutes = estimatedMinutes(h.DurationMsSum)
	h.EstimatedMinutesExcludingImport = estimatedMinutes(h.DurationMsSumExcludingImport)
	h.EstimatedMinutesImport = estimatedMinutes(importMS)
	h.EstimatedMinutesSource = "sum(listen_history.duration_ms) / 60000"
	return h, nil
}

func queryListenCompareEvents(ctx context.Context, pool *pgxpool.Pool, period listenComparePeriod) (listenCompareEvents, error) {
	from, to := period.bounds()
	var e listenCompareEvents
	err := pool.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE qualified_play),
			count(*) FILTER (WHERE qualified_play AND NOT legacy_backfill),
			count(*) FILTER (WHERE qualified_play AND legacy_backfill),
			count(*) FILTER (WHERE skipped),
			count(*) FILTER (WHERE kind = 'skip'),
			count(*) FILTER (WHERE kind = 'skip' AND NOT qualified_play),
			count(*) FILTER (WHERE legacy_backfill),
			count(*) FILTER (WHERE NOT legacy_backfill),
			count(DISTINCT user_id),
			count(DISTINCT track_id),
			coalesce(sum(listened_ms) FILTER (WHERE listened_ms IS NOT NULL), 0),
			count(*) FILTER (WHERE listened_ms IS NULL)
		FROM listen_events
		WHERE ($1::timestamptz IS NULL OR started_at >= $1)
			AND ($2::timestamptz IS NULL OR started_at < $2)`, from, to).Scan(
		&e.RowCount,
		&e.QualifiedPlay,
		&e.QualifiedPlayLive,
		&e.QualifiedPlayBackfill,
		&e.Skipped,
		&e.KindSkip,
		&e.KindSkipUnqualified,
		&e.LegacyBackfill,
		&e.Live,
		&e.DistinctUsers,
		&e.DistinctTracks,
		&e.ListenedMsSum,
		&e.NullListenedMsCount,
	)
	if err != nil {
		return listenCompareEvents{}, err
	}
	_ = pool.QueryRow(ctx, `
		SELECT count(*)
		FROM listen_output_segments
		WHERE ($1::timestamptz IS NULL OR started_at >= $1)
			AND ($2::timestamptz IS NULL OR started_at < $2)`, from, to).Scan(&e.OutputSegmentCount)
	e.ListenedMsIncomplete = e.NullListenedMsCount > 0 || e.LegacyBackfill > 0
	e.ListenedMinutesIncomplete = estimatedMinutes(e.ListenedMsSum)
	e.ListenedMsNote = "sum(listened_ms) includes only non-null values. Backfill rows have listened_ms NULL and must not be treated as full-track duration."
	return e, nil
}

func queryListenCompareDiffs(ctx context.Context, pool *pgxpool.Pool, period listenComparePeriod, hist listenCompareHistory, ev listenCompareEvents) (listenCompareDiffs, error) {
	from, to := period.bounds()
	var unmatchedHistory, unmatchedLive int64
	err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM listen_history h
		WHERE ($1::timestamptz IS NULL OR h.played_at >= $1)
			AND ($2::timestamptz IS NULL OR h.played_at < $2)
			AND NOT EXISTS (
				SELECT 1 FROM listen_events e
				WHERE e.user_id = h.user_id
					AND e.track_id = h.track_id
					AND date_trunc('day', e.started_at AT TIME ZONE 'UTC')
					  = date_trunc('day', h.played_at AT TIME ZONE 'UTC')
			)`, from, to).Scan(&unmatchedHistory)
	if err != nil {
		return listenCompareDiffs{}, err
	}
	err = pool.QueryRow(ctx, `
		SELECT count(*)
		FROM listen_events e
		WHERE e.legacy_backfill = false
			AND e.qualified_play = true
			AND ($1::timestamptz IS NULL OR e.started_at >= $1)
			AND ($2::timestamptz IS NULL OR e.started_at < $2)
			AND NOT EXISTS (
				SELECT 1 FROM listen_history h
				WHERE h.user_id = e.user_id
					AND h.track_id = e.track_id
					AND date_trunc('day', h.played_at AT TIME ZONE 'UTC')
					  = date_trunc('day', e.started_at AT TIME ZONE 'UTC')
			)`, from, to).Scan(&unmatchedLive)
	if err != nil {
		return listenCompareDiffs{}, err
	}
	var playSkip int64
	if err := pool.QueryRow(ctx, `SELECT coalesce(sum(skip_count), 0) FROM play_counts`).Scan(&playSkip); err != nil {
		return listenCompareDiffs{}, err
	}
	livePair := listenComparePair{
		History: hist.RowCountExcludingImport,
		Events:  ev.QualifiedPlayLive,
	}
	livePair.Delta = livePair.History - livePair.Events
	allPair := listenComparePair{
		History: hist.RowCount,
		Events:  ev.QualifiedPlay,
	}
	allPair.Delta = allPair.History - allPair.Events
	return listenCompareDiffs{
		MatchKey:                                 listenCompareMatchKey,
		MatchKeyNote:                             listenCompareMatchNote,
		DeltaMeaning:                             "delta is history minus events. It is a gap, not a combined listen count.",
		HistoryPlaysVsQualifiesLive:              livePair,
		HistoryPlaysVsQualifiesIncludingBackfill: allPair,
		HistoryRowsWithNoMatchingEvent:           unmatchedHistory,
		LiveEventsWithNoMatchingHistory:          unmatchedLive,
		PlayCountsSkipCount:                      playSkip,
		SkipEvents:                               ev.KindSkip,
		SkipEventsUnqualified:                    ev.KindSkipUnqualified,
		SkipDelta:                                playSkip - ev.KindSkipUnqualified,
		SkipNote:                                 "play_counts.skip_count is lifetime and is not filtered by from/to. Skip events are filtered to the selected period. Backfill copies listen_history qualifies only — it does not create skip events from play_counts.",
	}, nil
}

func estimatedMinutes(ms int64) float64 {
	return float64(ms) / 60000.0
}

func isMissingListenSchema(err error) bool {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code == "42P01" || pe.Code == "42703"
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "listen_events") && strings.Contains(msg, "does not exist")
}
