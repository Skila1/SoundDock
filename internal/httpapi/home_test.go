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
	if !strings.Contains(ev, "listen_events") || !strings.Contains(ev, "kind = 'qualify'") {
		t.Fatal("events continue reads listen_events")
	}
	if strings.Contains(strings.ToUpper(hist+ev), "UNION") {
		t.Fatal("home continue must not UNION")
	}
}

func TestHomeRecentlyAddedSQL(t *testing.T) {
	sql := homeRecentlyAddedSQL()
	if !strings.Contains(sql, "LIMIT 15") {
		t.Fatal("recently_added LIMIT 15")
	}
	if !strings.Contains(sql, "t.created_at DESC") {
		t.Fatal("recently_added orders by tracks.created_at")
	}
	if !strings.Contains(sql, "library_id = ANY($1)") {
		t.Fatal("recently_added is grant-scoped")
	}
	up := strings.ToUpper(sql)
	if strings.Contains(up, "LISTEN_EVENTS") || strings.Contains(up, "PLAY_COUNTS") {
		t.Fatal("recently_added must not use events or play_counts")
	}
}

func TestHomeMostPlayedSQL(t *testing.T) {
	sql := homeMostPlayedSQL()
	if !strings.Contains(sql, "LIMIT 15") {
		t.Fatal("most_played LIMIT 15")
	}
	if !strings.Contains(sql, "play_counts") {
		t.Fatal("most_played reads play_counts")
	}
	if strings.Contains(sql, "listen_events") || strings.Contains(sql, "listen_history") {
		t.Fatal("most_played must not use events")
	}
	if !strings.Contains(sql, "library_id = ANY($2)") {
		t.Fatal("most_played is grant-scoped")
	}
	if strings.Contains(strings.ToUpper(sql), "UNION") {
		t.Fatal("most_played must not UNION")
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
