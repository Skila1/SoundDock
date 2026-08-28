package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
)

type flushRecorder struct {
	*httptest.ResponseRecorder
}

func (f *flushRecorder) Flush() {}

func TestWriteSSEHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	writeSSEHeaders(rec)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content-type %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("X-Accel-Buffering") != "no" {
		t.Fatalf("x-accel %q", rec.Header().Get("X-Accel-Buffering"))
	}
}

func TestWriteSSECommentPing(t *testing.T) {
	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	writeSSEComment(rec, "ping")
	if rec.Body.String() != ": ping\n\n" {
		t.Fatalf("body %q", rec.Body.String())
	}
}

func TestRejectSSEQueryAuth(t *testing.T) {
	ok := (&Server{}).rejectSSEQueryAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	ok.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/me/queue/sse?access_token=secret", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("access_token status %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "query token") {
		t.Fatalf("body %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	ok.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/me/queue/sse?token=secret", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("token status %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	ok.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/me/queue/sse", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("cookie path status %d", rec.Code)
	}
}

func TestSessionStateOmitsPlayheadTick(t *testing.T) {
	q := map[string]any{
		"id":             uuid.MustParse("00000000-0000-4000-8000-000000000001"),
		"state_revision": int64(4), "status": "playing", "volume": 0.5, "muted": false,
		"position_ms": 1234, "playhead_sequence": int64(9), "items": []map[string]any{},
	}
	st := sessionStatePayload(q)
	if _, ok := st["position_ms"]; ok {
		t.Fatal("session.state must not include 1s playhead position_ms")
	}
	if st["state_revision"] != int64(4) {
		t.Fatalf("revision %v", st["state_revision"])
	}
	ph := sessionPlayheadPayload(q)
	if ph["checkpoint_position_ms"] != 1234 {
		t.Fatalf("checkpoint %v", ph["checkpoint_position_ms"])
	}
	if _, ok := ph["position_ms"]; ok {
		t.Fatal("playhead event uses checkpoint_position_ms")
	}
}

func TestPresenceMultiTabOneUser(t *testing.T) {
	h := newSessionHub()
	sid := uuid.New()
	uid := uuid.New()
	h.touch(sid, uid, "tab-a", "Ada", "")
	h.touch(sid, uid, "tab-b", "Ada", "")
	list := h.listeners(sid)
	if len(list) != 1 {
		t.Fatalf("listeners %d", len(list))
	}
	if list[0].Source != "web" || derefStr(list[0].UserID) != uid.String() {
		t.Fatalf("%+v", list[0])
	}
}

func TestPresenceSSEDisconnectAndExpire(t *testing.T) {
	h := newSessionHub()
	sid := uuid.New()
	uid := uuid.New()
	if !h.addSSE(sid, uid, "tab-1", "Bea", "") {
		t.Fatal("first join")
	}
	if !h.dropSSE(sid, uid, "tab-1") {
		t.Fatal("last SSE should expire presence")
	}
	if len(h.listeners(sid)) != 0 {
		t.Fatal("expected empty after disconnect")
	}

	h.addSSE(sid, uid, "tab-1", "Bea", "")
	h.addSSE(sid, uid, "tab-1", "Bea", "")
	if h.dropSSE(sid, uid, "tab-1") {
		t.Fatal("refcount should keep the user")
	}
	if len(h.listeners(sid)) != 1 {
		t.Fatal("still present")
	}
	if !h.dropSSE(sid, uid, "tab-1") {
		t.Fatal("second drop removes user")
	}

	base := time.Date(2026, 8, 29, 1, 0, 0, 0, time.UTC)
	nowFn = func() time.Time { return base }
	t.Cleanup(func() { nowFn = time.Now })
	h.touch(sid, uid, "hb", "Bea", "")
	nowFn = func() time.Time { return base.Add(46 * time.Second) }
	if n := len(h.expire(nowFn())); n != 1 {
		t.Fatalf("expire sessions %d", n)
	}
	if len(h.listeners(sid)) != 0 {
		t.Fatal("expired")
	}
}

func TestHubFanoutFromMemory(t *testing.T) {
	h := newSessionHub()
	sid := uuid.New()
	a := h.subscribe(sid)
	b := h.subscribe(sid)
	h.publish(sid, sseEventState, map[string]any{"state_revision": 3, "status": "playing"})
	gotA := <-a.ch
	gotB := <-b.ch
	if gotA.name != sseEventState || gotB.name != sseEventState {
		t.Fatalf("names %q %q", gotA.name, gotB.name)
	}
	if string(gotA.data) != string(gotB.data) {
		t.Fatalf("payloads differ %s vs %s", gotA.data, gotB.data)
	}
	if !strings.Contains(string(gotA.data), `"state_revision":3`) {
		t.Fatalf("data %s", gotA.data)
	}
}

func TestAcquisitionStatusStub(t *testing.T) {
	b, err := json.Marshal(emptyAcquisitionStatus)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"intents":[]}` {
		t.Fatalf("stub %s", b)
	}
}

func TestSortListenersCurrentUserFirst(t *testing.T) {
	cur := uuid.New()
	curID := cur.String()
	other := uuid.New().String()
	list := []QueueListener{
		{UserID: &other, DisplayName: "aaa", Source: "web"},
		{UserID: &curID, DisplayName: "zzz", Source: "web"},
	}
	sortListeners(list, cur)
	if derefStr(list[0].UserID) != curID {
		t.Fatalf("want current first, got %+v", list)
	}
}

func TestRequestClientID(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/me/queue/sse?client_id=from-query", nil)
	if got := requestClientID(r, nil); got != "from-query" {
		t.Fatalf("query %q", got)
	}
	r = httptest.NewRequest(http.MethodGet, "/api/v1/me/queue/sse", nil)
	r.Header.Set("X-Device-ID", "dev")
	if got := requestClientID(r, map[string]any{"client_id": "extra"}); got != "extra" {
		t.Fatalf("extra %q", got)
	}
}

func TestQueueGetListenersAndServerTime(t *testing.T) {
	s, pool := wave1HTTPServer(t)
	u := seedQueueUser(t, pool, "")
	u.DisplayName = "Wave3"
	rec := httptest.NewRecorder()
	s.getQueue(rec, authedJSON(u, http.MethodGet, "/api/v1/me/queue", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	got := decodeMap(t, rec)
	if _, ok := got["server_time"]; !ok {
		t.Fatal("missing server_time")
	}
	raw, ok := got["listeners"].([]any)
	if !ok {
		t.Fatalf("listeners %T", got["listeners"])
	}
	if len(raw) != 1 {
		t.Fatalf("listeners len %d %v", len(raw), raw)
	}
	row, _ := raw[0].(map[string]any)
	if row["display_name"] != "Wave3" {
		t.Fatalf("display %v", row["display_name"])
	}
	if row["source"] != "web" {
		t.Fatalf("source %v", row["source"])
	}
	if _, ok := row["plays"]; ok {
		t.Fatal("presence must not include listen stats")
	}
}

func TestQueueHeartbeatTouchesPresence(t *testing.T) {
	s, pool := wave1HTTPServer(t)
	u := seedQueueUser(t, pool, "")
	rec := httptest.NewRecorder()
	s.queueHeartbeat(rec, authedJSON(u, http.MethodPost, "/api/v1/me/queue/heartbeat", map[string]any{"client_id": "tab-1"}))
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	got := decodeMap(t, rec)
	if got["ok"] != true {
		t.Fatalf("%v", got)
	}
	if _, ok := got["server_time"]; !ok {
		t.Fatal("server_time")
	}
}

func TestQueueSSETypedEventsAndPing(t *testing.T) {
	s, pool := wave1HTTPServer(t)
	u := seedQueueUser(t, pool, "")
	prev := ssePingInterval
	ssePingInterval = 20 * time.Millisecond
	t.Cleanup(func() { ssePingInterval = prev })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.queueSSE(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/me/queue/sse", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatal("missing X-Accel-Buffering")
	}
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("ct %q", res.Header.Get("Content-Type"))
	}
	sc := bufio.NewReader(res.Body)
	var body strings.Builder
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		line, err := sc.ReadString('\n')
		if err != nil && err != io.EOF {
			break
		}
		body.WriteString(line)
		got := body.String()
		if strings.Contains(got, "event: session.presence") &&
			strings.Contains(got, "event: acquisition.status") &&
			strings.Contains(got, ": ping") &&
			strings.Contains(got, `"intents":[]`) {
			cancel()
			return
		}
		if err == io.EOF {
			break
		}
	}
	t.Fatalf("sse body %q", body.String())
}

func TestQueueSSEFanoutStateWithoutDBPerSub(t *testing.T) {
	s, pool := wave1HTTPServer(t)
	u := seedQueueUser(t, pool, "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.queueSSE(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/me/queue/sse", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	sc := bufio.NewReader(res.Body)
	var body strings.Builder
	gotPresence := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !gotPresence {
		line, err := sc.ReadString('\n')
		if err != nil && err != io.EOF {
			t.Fatal(err)
		}
		body.WriteString(line)
		if strings.Contains(body.String(), "event: session.presence") {
			gotPresence = true
		}
		if err == io.EOF {
			break
		}
	}
	if !gotPresence {
		t.Fatalf("no presence %q", body.String())
	}
	sid, err := s.Play.WebSession(context.Background(), u.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	s.sessionHub().publish(sid, sseEventPlayhead, map[string]any{
		"checkpoint_position_ms": 50, "playhead_sequence": 2, "status": "playing", "playback_rate": 1, "duration_ms": 1000,
	})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		line, err := sc.ReadString('\n')
		if err != nil && err != io.EOF {
			break
		}
		body.WriteString(line)
		if strings.Contains(body.String(), "event: session.playhead") {
			cancel()
			return
		}
		if err == io.EOF {
			break
		}
	}
	t.Fatalf("missing playhead in %q", body.String())
}

func TestDiscordAvatarURL(t *testing.T) {
	if discordAvatarURL("", "") != "" {
		t.Fatal("empty")
	}
	if !strings.Contains(discordAvatarURL("123", ""), "cdn.discordapp.com/embed/avatars/") {
		t.Fatal(discordAvatarURL("123", ""))
	}
	got := discordAvatarURL("123456789012345678", "abc123def")
	if !strings.Contains(got, "/avatars/123456789012345678/abc123def.png") {
		t.Fatal(got)
	}
	anim := discordAvatarURL("123456789012345678", "a_deadbeef")
	if !strings.Contains(anim, "/avatars/123456789012345678/a_deadbeef.gif") {
		t.Fatal(anim)
	}
}

func TestPresenceDisplay(t *testing.T) {
	if presenceDisplay(&auth.User{DisplayName: "A", Username: "b"}) != "A" {
		t.Fatal("display")
	}
	if presenceDisplay(&auth.User{Username: "b"}) != "b" {
		t.Fatal("username")
	}
}

func TestQueueSSERejectsQueryTokenOnHandler(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "x"}
	r := authedJSON(u, http.MethodGet, "/api/v1/me/queue/sse?access_token=nope", nil)
	rec := httptest.NewRecorder()
	s.queueSSE(rec, r)
	if rec.Code != 401 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
}

func TestRespondQueuePublishesStateNotPlayheadTick(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "x", DisplayName: "X"}
	sid := uuid.New()
	sub := s.sessionHub().subscribe(sid)
	q := map[string]any{"id": sid, "status": "playing", "state_revision": int64(2), "position_ms": 99}
	rec := httptest.NewRecorder()
	s.respondQueue(rec, authedJSON(u, http.MethodPut, "/api/v1/me/queue", nil), sid, q, "state")
	select {
	case ev := <-sub.ch:
		if ev.name != sseEventState {
			t.Fatalf("first event %s", ev.name)
		}
		if strings.Contains(string(ev.data), "position_ms") {
			t.Fatalf("state included playhead tick %s", ev.data)
		}
	case <-time.After(time.Second):
		t.Fatal("no state event")
	}
}

func TestRespondQueueDoesNotPublishOnGET(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "x", DisplayName: "X"}
	sid := uuid.New()
	sub := s.sessionHub().subscribe(sid)
	q := map[string]any{"id": sid, "status": "stopped", "state_revision": int64(1)}
	rec := httptest.NewRecorder()
	s.respondQueue(rec, authedJSON(u, http.MethodGet, "/api/v1/me/queue", nil), sid, q, "")
	select {
	case ev := <-sub.ch:
		if ev.name == sseEventState {
			t.Fatal("GET must not fan-out session.state")
		}
	default:
	}
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
}
