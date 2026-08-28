package httpapi

import (
	"strings"
	"testing"
)

func TestHomeContinueSQLLimit15(t *testing.T) {
	hist := homeContinueSQL(false)
	ev := homeContinueSQL(true)
	for _, sql := range []string{hist, ev} {
		if !strings.Contains(sql, "LIMIT 15") {
			t.Fatal("home continue LIMIT 15")
		}
		if !strings.Contains(sql, "DISTINCT ON (h.track_id)") {
			t.Fatal("home continue DISTINCT ON track_id")
		}
	}
	if !strings.Contains(hist, "listen_history") || strings.Contains(hist, "listen_events") {
		t.Fatal("default continue reads listen_history")
	}
	if !strings.Contains(ev, "listen_events") || !strings.Contains(ev, "qualified_play") {
		t.Fatal("events continue reads listen_events")
	}
	if strings.Contains(strings.ToUpper(hist+ev), "UNION") {
		t.Fatal("home continue must not UNION")
	}
}

func TestExportHistoryLimit5000(t *testing.T) {
	sql := historyListSQL(false, 5000)
	if !strings.Contains(sql, "LIMIT 5000") {
		t.Fatal(sql)
	}
	if !strings.Contains(sql, "listen_history") {
		t.Fatal("export history default is listen_history")
	}
}
