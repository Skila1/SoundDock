package oplog

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/testdb"
)

func TestRedactDetailsMasksCredentialKeysAndNestedValues(t *testing.T) {
	in := map[string]any{
		"password":      "hunter2",
		"client_secret": "shh",
		"Authorization": "Bearer abc.def",
		"nested": map[string]any{
			"bot_token": "MTIz.secret",
			"note":      "token=abc123 and postgres://u:pw@db/x",
			"list":      []any{"password=letmein", map[string]any{"api_key": "k"}},
		},
		"headers": map[string]string{"Cookie": "sd_session=zzz", "Accept": "json"},
		"key":     "sd_ABCDEFGHIJKLMNOPQRSTUV",
		"count":   3,
	}
	out := RedactDetails(in)
	raw, _ := json.Marshal(out)
	for _, leak := range []string{"hunter2", "shh", "abc.def", "MTIz.secret", "abc123", ":pw@", "letmein", `"k"`, "zzz", "ABCDEFGHIJKLMNOPQRSTUV"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("leaked %q in %s", leak, raw)
		}
	}
	if out["count"] != 3 {
		t.Fatalf("non-secret value changed: %v", out["count"])
	}
	if !strings.Contains(string(raw), `"Accept":"json"`) {
		t.Fatalf("harmless header dropped: %s", raw)
	}
}

func TestRedactMasksAPIKeysAndPassphrases(t *testing.T) {
	out := Redact("used sdp_abcdefghijklmnopqrstuvwx with passphrase=correct-horse client_secret: s3cr3t")
	for _, leak := range []string{"abcdefghijklmnopqrstuvwx", "correct-horse", "s3cr3t"} {
		if strings.Contains(out, leak) {
			t.Fatalf("leaked %q in %q", leak, out)
		}
	}
}

func TestEmitFillsRequestContext(t *testing.T) {
	w := NewWriter(nil, nil)
	SetDefault(w)
	t.Cleanup(func() { SetDefault(nil) })
	uid := uuid.New()
	req := &Request{ID: "req-1", IP: "203.0.113.9"}
	req.SetActor(uid, "alice", "session")
	got := fillFromContext(WithRequest(context.Background(), req), Entry{Message: "x"})
	if got.RequestID != "req-1" || got.IP != "203.0.113.9" || got.ActorName != "alice" || got.ActorID == nil || *got.ActorID != uid {
		t.Fatalf("context not applied: %+v", got)
	}
	explicit := fillFromContext(WithRequest(context.Background(), req), Entry{ActorName: "bob", IP: "198.51.100.1"})
	if explicit.ActorName != "bob" || explicit.IP != "198.51.100.1" {
		t.Fatalf("explicit fields overwritten: %+v", explicit)
	}
}

func TestRequestNilSafe(t *testing.T) {
	var r *Request
	r.SetActor(uuid.New(), "x", "y")
	r.Suppress()
	r.Fail("x")
	r.Describe("a", "b", "c")
	r.Annotate("k", "v")
	r.MarkAudited()
	if r.Audited() || r.Failure() != "" || len(r.Details()) != 0 {
		t.Fatal("nil request should be inert")
	}
	if RequestFrom(context.Background()) != nil {
		t.Fatal("no request expected")
	}
}

func TestPrepareNormalisesAndClips(t *testing.T) {
	e, raw := prepare(Entry{Level: "WARNING", Result: "bogus", Message: strings.Repeat("a", maxText+50), Error: "password=x", Type: "job.t"})
	if e.Level != "warn" || e.Result != "" {
		t.Fatalf("level/result: %q %q", e.Level, e.Result)
	}
	if len(e.Message) > maxText+4 {
		t.Fatalf("message not clipped: %d", len(e.Message))
	}
	if strings.Contains(string(raw), "password=x") || !strings.Contains(string(raw), `"type":"job.t"`) {
		t.Fatalf("details: %s", raw)
	}
}

func TestTeeHandlerCopiesWarningsAndHonoursNoActivity(t *testing.T) {
	probe := NewWriter(nil, nil)
	SetDefault(probe)
	t.Cleanup(func() { SetDefault(nil) })
	req := &Request{ID: "req-tee", IP: "192.0.2.1"}
	ctx := WithRequest(context.Background(), req)
	log := slog.New(NewTeeHandler(slog.DiscardHandler, slog.LevelWarn)).With("category", "discord")
	log.InfoContext(ctx, "not copied")
	log.WarnContext(ctx, "gateway dropped", "err", "token=abc", "guild", "1")
	log.ErrorContext(ctx, "already logged", NoActivity)
	if n := len(probe.ch); n != 1 {
		t.Fatalf("teed %d entries, want 1", n)
	}
	e := <-probe.ch
	if e.Category != "discord" || e.Level != "warn" || e.Message != "gateway dropped" || e.Details["guild"] != "1" {
		t.Fatalf("entry: %+v", e)
	}
	if e.RequestID != "req-tee" || e.IP != "192.0.2.1" {
		t.Fatalf("request context missing: %+v", e)
	}
	if _, raw := prepare(e); strings.Contains(string(raw), "abc") {
		t.Fatalf("error text not redacted: %s", raw)
	}
}

func TestWriterStoresBatchesAndListFilters(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	marker := "act-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM operational_logs WHERE request_id LIKE $1`, marker+"%")
	})
	w := NewWriter(pool, nil)
	w.Start(ctx)
	defer w.Close()
	uid := uuid.New()
	status := 201
	ms := 12
	for i := 0; i < 5; i++ {
		w.Enqueue(Entry{Level: "info", Category: "access", Message: "Create user", RequestID: marker + "-ok", IP: "203.0.113.7",
			ActorID: &uid, ActorName: "alice", Method: "POST", Route: "/api/v1/admin/users", Status: &status, DurationMs: &ms,
			Result: ResultSuccess, Details: map[string]any{"password": "nope"}})
	}
	fail := 403
	w.Enqueue(Entry{Level: "warn", Category: "access", Message: "Delete user failed: forbidden", RequestID: marker + "-bad",
		IP: "198.51.100.4", ActorName: "mallory", Status: &fail, Result: ResultFailure})
	w.Flush(ctx)
	if w.Written() < 6 {
		t.Fatalf("written %d", w.Written())
	}

	items, next, err := List(ctx, pool, Filter{Q: marker, Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || next == "" {
		t.Fatalf("page 1: %d next=%q", len(items), next)
	}
	more, next2, err := List(ctx, pool, Filter{Q: marker, Limit: 3, Cursor: next})
	if err != nil {
		t.Fatal(err)
	}
	if len(more) != 3 || next2 != "" {
		t.Fatalf("page 2: %d next=%q", len(more), next2)
	}
	seen := map[uuid.UUID]bool{}
	for _, e := range append(items, more...) {
		if seen[e.ID] {
			t.Fatal("duplicate across pages")
		}
		seen[e.ID] = true
	}

	failed, _, _ := List(ctx, pool, Filter{Q: marker, Result: "failure"})
	if len(failed) != 1 || failed[0].ActorName != "mallory" || failed[0].Status == nil || *failed[0].Status != 403 {
		t.Fatalf("result filter: %+v", failed)
	}
	byIP, _, _ := List(ctx, pool, Filter{Q: marker, IP: "203.0.113"})
	if len(byIP) != 5 {
		t.Fatalf("ip prefix filter: %d", len(byIP))
	}
	byActor, _, _ := List(ctx, pool, Filter{Q: marker, Actor: uid.String()})
	if len(byActor) != 5 {
		t.Fatalf("actor id filter: %d", len(byActor))
	}
	byName, _, _ := List(ctx, pool, Filter{Q: marker, Actor: "mall"})
	if len(byName) != 1 {
		t.Fatalf("actor name filter: %d", len(byName))
	}
	byReq, _, _ := List(ctx, pool, Filter{RequestID: marker + "-bad"})
	if len(byReq) != 1 {
		t.Fatalf("request filter: %d", len(byReq))
	}
	byLevels, _, _ := List(ctx, pool, Filter{Q: marker, Level: "warn,error"})
	if len(byLevels) != 1 {
		t.Fatalf("level list filter: %d", len(byLevels))
	}
	future, _, _ := List(ctx, pool, Filter{Q: marker, Since: time.Now().Add(time.Hour)})
	if len(future) != 0 {
		t.Fatalf("since filter: %d", len(future))
	}
	for _, e := range byIP {
		if e.Details["password"] != "[redacted]" {
			t.Fatalf("details not redacted: %v", e.Details)
		}
		if e.Method != "POST" || e.Route != "/api/v1/admin/users" || e.DurationMs == nil {
			t.Fatalf("request columns: %+v", e)
		}
	}
	wild, _, _ := List(ctx, pool, Filter{Q: marker + "%"})
	if len(wild) != 0 {
		t.Fatalf("LIKE wildcards in search must be literal, got %d", len(wild))
	}
	cats := Categories(ctx, pool)
	found := false
	for _, c := range cats {
		if c == "access" {
			found = true
		}
	}
	if !found {
		t.Fatalf("categories: %v", cats)
	}
}

func TestWriterDropsWhenFull(t *testing.T) {
	w := NewWriter(nil, nil)
	w.ch = make(chan Entry, 2)
	for i := 0; i < 5; i++ {
		w.Enqueue(Entry{Message: "x"})
	}
	if w.Dropped() != 3 {
		t.Fatalf("dropped %d, want 3", w.Dropped())
	}
	e := <-w.ch
	if e.CreatedAt.IsZero() {
		t.Fatal("enqueue should stamp the time")
	}
}
