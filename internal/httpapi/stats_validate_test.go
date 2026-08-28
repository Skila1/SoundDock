package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestListenCompareRouteRegistered(t *testing.T) {
	h := (&Server{}).Router()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/listen-compare", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatal("GET /api/v1/admin/listen-compare is not registered")
	}
}

func TestAdminListenCompareNilPool(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/listen-compare", nil)
	rec := httptest.NewRecorder()
	s.adminListenCompare(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	raw := rec.Body.Bytes()
	var body listenCompareResponse
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.Ready {
		t.Fatal("expected ready=false without a database")
	}
	if body.Message == "" {
		t.Fatal("expected a message when tables/db are unavailable")
	}
	if body.Diffs != nil {
		t.Fatal("diffs must be omitted when not ready")
	}
	assertNoCombinedListenTotal(t, raw)
}

func TestAdminListenCompareInvalidPeriod(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/listen-compare?from=not-a-date", nil)
	rec := httptest.NewRecorder()
	s.adminListenCompare(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestParseListenComparePeriod(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

	t.Run("default last 30 days", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		p, err := parseListenComparePeriod(req, now)
		if err != nil {
			t.Fatal(err)
		}
		if p.Preset != "last_30_days" || p.From == nil || p.To == nil {
			t.Fatalf("%+v", p)
		}
		if p.From.After(now.AddDate(0, 0, -29)) || p.From.Before(now.AddDate(0, 0, -31)) {
			t.Fatalf("from %s", p.From)
		}
	})

	t.Run("all", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/x?period=all", nil)
		p, err := parseListenComparePeriod(req, now)
		if err != nil {
			t.Fatal(err)
		}
		if p.Preset != "all" || p.From != nil {
			t.Fatalf("%+v", p)
		}
	})

	t.Run("rfc3339", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/x?from=2026-01-01T00:00:00Z&to=2026-02-01T00:00:00Z", nil)
		p, err := parseListenComparePeriod(req, now)
		if err != nil {
			t.Fatal(err)
		}
		if p.Preset != "custom" || p.From == nil || p.To == nil {
			t.Fatalf("%+v", p)
		}
		if !p.From.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("from %s", p.From)
		}
	})
}

func TestListenCompareResponseNeverSumsIntoCanonicalTotal(t *testing.T) {
	resp := listenCompareResponse{
		Ready: true,
		Note:  listenComparePurpose,
		Period: listenComparePeriod{
			Preset: "last_30_days",
			Note:   "test",
		},
		History: listenCompareHistory{
			RowCount:                        111,
			RowCountExcludingImport:         100,
			ImportRowCount:                  11,
			DistinctUsers:                   7,
			DistinctUsersExcludingImport:    6,
			DistinctTracks:                  40,
			DistinctTracksExcludingImport:   38,
			DurationMsSum:                   6000000,
			DurationMsSumExcludingImport:    5400000,
			EstimatedMinutes:                100,
			EstimatedMinutesExcludingImport: 90,
			EstimatedMinutesImport:          10,
			EstimatedMinutesSource:          "sum(listen_history.duration_ms) / 60000",
		},
		Events: listenCompareEvents{
			RowCount:                  222,
			QualifiedPlay:             222,
			QualifiedPlayLive:         50,
			QualifiedPlayBackfill:     172,
			Skipped:                   8,
			KindSkip:                  8,
			KindSkipUnqualified:       5,
			LegacyBackfill:            172,
			Live:                      50,
			DistinctUsers:             7,
			DistinctTracks:            39,
			ListenedMsSum:             120000,
			ListenedMsIncomplete:      true,
			NullListenedMsCount:       172,
			ListenedMinutesIncomplete: 2,
			OutputSegmentCount:        3,
			ListenedMsNote:            "incomplete",
		},
		Diffs: &listenCompareDiffs{
			MatchKey:     listenCompareMatchKey,
			MatchKeyNote: listenCompareMatchNote,
			DeltaMeaning: "delta is history minus events. It is a gap, not a combined listen count.",
			HistoryPlaysVsQualifiesLive: listenComparePair{
				History: 100, Events: 50, Delta: 50,
			},
			HistoryPlaysVsQualifiesIncludingBackfill: listenComparePair{
				History: 111, Events: 222, Delta: -111,
			},
			HistoryRowsWithNoMatchingEvent:  4,
			LiveEventsWithNoMatchingHistory: 2,
			PlayCountsSkipCount:             9,
			SkipEvents:                      8,
			SkipEventsUnqualified:           5,
			SkipDelta:                       4,
			SkipNote:                        "lifetime vs period",
		},
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	assertNoCombinedListenTotal(t, raw)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if containsNumber(m, 333) {
		t.Fatal("response must not expose history.row_count + events.qualified_play as a number")
	}
	if containsNumber(m, 150) {
		t.Fatal("response must not expose excluding-import history + live qualifies as a number")
	}
}

func TestListenCompareSourceDoesNotCombineTables(t *testing.T) {
	b, err := os.ReadFile("stats_validate.go")
	if err != nil {
		t.Fatal(err)
	}
	upper := strings.ToUpper(string(b))
	if strings.Contains(upper, " UNION ") || strings.Contains(upper, "\tUNION ") || strings.Contains(upper, "UNION ALL") {
		t.Fatal("stats_validate.go must not combine listen_history with listen_events")
	}
	if strings.Contains(string(b), "total_listens") || strings.Contains(string(b), "combined_total") {
		t.Fatal("must not define a combined listen total field")
	}
}

func TestAdminListenCompareDB(t *testing.T) {
	pool := testPool(t)
	s := &Server{Pool: pool}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/listen-compare?period=all", nil)
	rec := httptest.NewRecorder()
	s.adminListenCompare(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	raw := rec.Body.Bytes()
	var body listenCompareResponse
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	assertNoCombinedListenTotal(t, raw)
	if body.Note == "" {
		t.Fatal("expected validation note")
	}
	if body.Ready {
		if body.Diffs == nil {
			t.Fatal("ready report must include diffs")
		}
	} else if body.Message == "" {
		t.Fatal("not ready must explain missing 0015 tables")
	}
}

func assertNoCombinedListenTotal(t *testing.T, raw []byte) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	forbidden := []string{
		"total_listens", "combined_listens", "combined_total", "history_plus_events",
		"canonical_total", "merged_total", "grand_total", "total_plays",
	}
	var found []string
	collectForbiddenKeys(m, forbidden, &found)
	if len(found) > 0 {
		t.Fatalf("forbidden combined-total fields: %s", strings.Join(found, ", "))
	}
}

func collectForbiddenKeys(v any, forbidden []string, found *[]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			lk := strings.ToLower(k)
			for _, f := range forbidden {
				if lk == f {
					*found = append(*found, k)
				}
			}
			collectForbiddenKeys(child, forbidden, found)
		}
	case []any:
		for _, child := range t {
			collectForbiddenKeys(child, forbidden, found)
		}
	}
}

func containsNumber(v any, want float64) bool {
	switch t := v.(type) {
	case float64:
		return t == want
	case json.Number:
		n, err := t.Float64()
		return err == nil && n == want
	case map[string]any:
		for _, child := range t {
			if containsNumber(child, want) {
				return true
			}
		}
	case []any:
		for _, child := range t {
			if containsNumber(child, want) {
				return true
			}
		}
	}
	return false
}
