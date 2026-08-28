package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/stats"
)

func TestListenReaderEventsDefaultHistory(t *testing.T) {
	s := &Server{}
	if s.listenReaderEvents(context.Background()) {
		t.Fatal("missing pool must default to listen_history")
	}
	s = &Server{Pool: nil}
	if s.listenReaderEvents(context.Background()) {
		t.Fatal("nil pool must default to listen_history")
	}
}

func TestRecapSQLHistoryPath(t *testing.T) {
	for _, sql := range recapReaderSQLs(false) {
		assertNoUnion(t, sql)
		if !strings.Contains(sql, "listen_history") {
			t.Fatalf("history path must query listen_history:\n%s", sql)
		}
		if strings.Contains(sql, "listen_events") {
			t.Fatalf("history path must not query listen_events:\n%s", sql)
		}
		if strings.Contains(sql, "qualified_play") {
			t.Fatalf("history path must not filter qualified_play:\n%s", sql)
		}
	}
}

func TestRecapSQLEventsPath(t *testing.T) {
	for _, sql := range recapReaderSQLs(true) {
		assertNoUnion(t, sql)
		if !strings.Contains(sql, "listen_events") {
			t.Fatalf("events path must query listen_events:\n%s", sql)
		}
		if strings.Contains(sql, "listen_history") {
			t.Fatalf("events path must not query listen_history:\n%s", sql)
		}
		if !strings.Contains(sql, "qualified_play") {
			t.Fatalf("events path must filter qualified_play:\n%s", sql)
		}
	}
}

func TestHomeContinueLimit15(t *testing.T) {
	for _, events := range []bool{false, true} {
		sql := homeContinueSQL(events)
		if !strings.Contains(sql, "DISTINCT ON (h.track_id)") {
			t.Fatalf("continue must DISTINCT ON track_id:\n%s", sql)
		}
		if !strings.Contains(sql, "LIMIT 15") {
			t.Fatalf("continue must LIMIT 15:\n%s", sql)
		}
		assertNoUnion(t, sql)
	}
}

func TestNeverPlayedStaysOnPlayCounts(t *testing.T) {
	sql := neverPlayedSQL()
	assertNoUnion(t, sql)
	if !strings.Contains(sql, "play_counts") {
		t.Fatal("neverPlayed uses play_counts (rebuilt from events at cutover)")
	}
	if strings.Contains(sql, "listen_history") || strings.Contains(sql, "listen_events") {
		t.Fatal("neverPlayed must not UNION listen tables")
	}
}

func TestRebuildAbortIgnoresCancelDuringSwap(t *testing.T) {
	if !rebuildAbortOnCancel(false, true) {
		t.Fatal("cancel before swap should abort")
	}
	if rebuildAbortOnCancel(true, true) {
		t.Fatal("cancel during swap must be ignored")
	}
	if rebuildAbortOnCancel(false, false) || rebuildAbortOnCancel(true, false) {
		t.Fatal("no cancel must not abort")
	}
}

func TestJobStatsRebuildNilPool(t *testing.T) {
	s := &Server{}
	err := s.jobStatsRebuild(context.Background(), jobs.Job{})
	if err == nil {
		t.Fatal("expected error without database")
	}
}

func TestRegisterStatsJobsNilSafe(t *testing.T) {
	s := &Server{}
	s.RegisterStatsJobs()
	s.RegisterJobs()
	r := jobs.New(nil, nil)
	s = &Server{Jobs: r}
	s.RegisterJobs()
}

func TestSetListenReaderNilPool(t *testing.T) {
	s := &Server{}
	if err := s.setListenReader(context.Background(), stats.ReaderEvents); err == nil {
		t.Fatal("expected error")
	}
}

func TestListenEventsMinutesDocumented(t *testing.T) {
	sql := listenTotalsSQL(true)
	if !strings.Contains(sql, "coalesce(h.listened_ms, h.track_duration_ms)") {
		t.Fatal("events minutes must coalesce listened_ms with track_duration_ms")
	}
	if !strings.Contains(listenEventsMinutesSource, "estimated") {
		t.Fatal("minutes source must say estimated")
	}
	tots := listenTotalsJSON(listenTotals{Minutes: 3, MinutesEstimated: true, MinutesSource: listenEventsMinutesSource}, nil)
	if tots["minutes_estimated"] != true {
		t.Fatal("totals JSON must label estimated minutes")
	}
}

func TestJobStatsRebuildDB(t *testing.T) {
	if os.Getenv("SD_TEST_DATABASE_URL") == "" {
		t.Skip("SD_TEST_DATABASE_URL not set")
	}
	pool := testPool(t)
	s := &Server{Pool: pool}
	ctx := context.Background()
	prevEvents := s.listenReaderEvents(ctx)
	t.Cleanup(func() {
		mode := stats.ReaderHistory
		if prevEvents {
			mode = stats.ReaderEvents
		}
		_ = s.setListenReader(context.Background(), mode)
	})
	if err := s.jobStatsRebuild(ctx, jobs.Job{}); err != nil {
		t.Fatal(err)
	}
	if !s.listenReaderEvents(ctx) {
		t.Fatal("rebuild must flip listen_reader to events")
	}
}

func assertNoUnion(t *testing.T, sql string) {
	t.Helper()
	upper := strings.ToUpper(sql)
	if strings.Contains(upper, " UNION ") || strings.Contains(upper, "UNION ALL") || strings.Contains(upper, "FULL OUTER JOIN") {
		t.Fatalf("recap SQL must not UNION or full-outer-join listen tables:\n%s", sql)
	}
}
