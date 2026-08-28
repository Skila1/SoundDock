package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
)

const (
	sseEventState       = "session.state"
	sseEventPlayhead    = "session.playhead"
	sseEventPresence    = "session.presence"
	sseEventAcquisition = "acquisition.status"
)

var (
	ssePingInterval        = 15 * time.Second
	presenceTTL            = 45 * time.Second
	nowFn                  = time.Now
	emptyAcquisitionStatus = map[string]any{"intents": []any{}}
)

// QueueListener is a presence row for GET /me/queue and session.presence.
// Source is web, discord, or both. This is avatars only — not listen stats.
type QueueListener struct {
	UserID      *string `json:"user_id"`
	DisplayName string  `json:"display_name"`
	AvatarURL   *string `json:"avatar_url,omitempty"`
	Source      string  `json:"source"`
}

type sseEvent struct {
	name string
	data []byte
}

type sseSub struct {
	ch chan sseEvent
}

type presenceUser struct {
	clientRefs map[string]int
	lastSeen   time.Time
	display    string
	avatar     string
}

type sessionLive struct {
	presence     map[uuid.UUID]*presenceUser
	subs         map[*sseSub]struct{}
	lastState    []byte
	lastPlayhead []byte
}

type sessionHub struct {
	mu       sync.Mutex
	sessions map[uuid.UUID]*sessionLive
}

func newSessionHub() *sessionHub {
	return &sessionHub{sessions: map[uuid.UUID]*sessionLive{}}
}

func (s *Server) sessionHub() *sessionHub {
	s.hubOnce.Do(func() {
		s.hub = newSessionHub()
	})
	return s.hub
}

func (h *sessionHub) liveLocked(sid uuid.UUID) *sessionLive {
	live := h.sessions[sid]
	if live == nil {
		live = &sessionLive{
			presence: map[uuid.UUID]*presenceUser{},
			subs:     map[*sseSub]struct{}{},
		}
		h.sessions[sid] = live
	}
	return live
}

func (h *sessionHub) expireAndPublish(now time.Time) {
	changed := h.expire(now)
	for _, sid := range changed {
		h.publishPresence(sid)
	}
}

func (h *sessionHub) expire(now time.Time) []uuid.UUID {
	h.mu.Lock()
	defer h.mu.Unlock()
	var changed []uuid.UUID
	for sid, live := range h.sessions {
		removed := false
		for uid, p := range live.presence {
			if now.Sub(p.lastSeen) >= presenceTTL {
				delete(live.presence, uid)
				removed = true
			}
		}
		if removed {
			changed = append(changed, sid)
		}
		if len(live.presence) == 0 && len(live.subs) == 0 {
			delete(h.sessions, sid)
		}
	}
	return changed
}

func (h *sessionHub) subscribe(sid uuid.UUID) *sseSub {
	sub := &sseSub{ch: make(chan sseEvent, 16)}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.liveLocked(sid).subs[sub] = struct{}{}
	return sub
}

func (h *sessionHub) unsubscribe(sid uuid.UUID, sub *sseSub) {
	if sub == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	live := h.sessions[sid]
	if live == nil {
		return
	}
	delete(live.subs, sub)
	if len(live.presence) == 0 && len(live.subs) == 0 {
		delete(h.sessions, sid)
	}
}

func (h *sessionHub) publish(sid uuid.UUID, name string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	live := h.sessions[sid]
	if live == nil {
		if name != sseEventState && name != sseEventPlayhead {
			return
		}
		live = h.liveLocked(sid)
	}
	switch name {
	case sseEventState:
		live.lastState = data
	case sseEventPlayhead:
		live.lastPlayhead = data
	}
	ev := sseEvent{name: name, data: data}
	for sub := range live.subs {
		select {
		case sub.ch <- ev:
		default:
		}
	}
}

func (h *sessionHub) publishPresence(sid uuid.UUID) {
	h.mu.Lock()
	payload := map[string]any{"listeners": h.listenersLocked(sid)}
	h.mu.Unlock()
	h.publish(sid, sseEventPresence, payload)
}

func (h *sessionHub) listeners(sid uuid.UUID) []QueueListener {
	h.expire(nowFn())
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.listenersLocked(sid)
}

func (h *sessionHub) listenersLocked(sid uuid.UUID) []QueueListener {
	live := h.sessions[sid]
	if live == nil || len(live.presence) == 0 {
		return []QueueListener{}
	}
	out := make([]QueueListener, 0, len(live.presence))
	for uid, p := range live.presence {
		id := uid.String()
		row := QueueListener{UserID: &id, DisplayName: p.display, Source: "web"}
		if p.avatar != "" {
			av := p.avatar
			row.AvatarURL = &av
		}
		out = append(out, row)
	}
	return out
}

func (h *sessionHub) touch(sid, userID uuid.UUID, clientID, display, avatar string) bool {
	if sid == uuid.Nil || userID == uuid.Nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.touchLocked(h.liveLocked(sid), userID, clientID, display, avatar, false)
}

func (h *sessionHub) touchLocked(live *sessionLive, userID uuid.UUID, clientID, display, avatar string, sse bool) bool {
	if clientID == "" {
		clientID = "_"
	}
	p := live.presence[userID]
	joined := p == nil
	if p == nil {
		p = &presenceUser{clientRefs: map[string]int{}}
		live.presence[userID] = p
	}
	if _, ok := p.clientRefs[clientID]; !ok {
		p.clientRefs[clientID] = 0
	}
	if sse {
		p.clientRefs[clientID]++
	}
	p.lastSeen = nowFn()
	if display != "" {
		p.display = display
	}
	if avatar != "" {
		p.avatar = avatar
	}
	return joined
}

func (h *sessionHub) addSSE(sid, userID uuid.UUID, clientID, display, avatar string) bool {
	if sid == uuid.Nil || userID == uuid.Nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.touchLocked(h.liveLocked(sid), userID, clientID, display, avatar, true)
}

func (h *sessionHub) dropSSE(sid, userID uuid.UUID, clientID string) bool {
	if sid == uuid.Nil || userID == uuid.Nil {
		return false
	}
	if clientID == "" {
		clientID = "_"
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	live := h.sessions[sid]
	if live == nil {
		return false
	}
	p := live.presence[userID]
	if p == nil {
		return false
	}
	p.clientRefs[clientID]--
	if p.clientRefs[clientID] <= 0 {
		delete(p.clientRefs, clientID)
	}
	if len(p.clientRefs) == 0 {
		delete(live.presence, userID)
		if len(live.presence) == 0 && len(live.subs) == 0 {
			delete(h.sessions, sid)
		}
		return true
	}
	return false
}

func requestClientID(r *http.Request, extra map[string]any) string {
	if extra != nil {
		if s, ok := extra["client_id"].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	if q := strings.TrimSpace(r.URL.Query().Get("client_id")); q != "" {
		return q
	}
	return requestDeviceID(r, extra)
}

func presenceDisplay(u *auth.User) string {
	if u == nil {
		return ""
	}
	if strings.TrimSpace(u.DisplayName) != "" {
		return u.DisplayName
	}
	return u.Username
}

func discordAvatarURL(discordUserID string) string {
	discordUserID = strings.TrimSpace(discordUserID)
	if discordUserID == "" {
		return ""
	}
	n, err := strconv.ParseUint(discordUserID, 10, 64)
	if err != nil {
		return "https://cdn.discordapp.com/embed/avatars/0.png"
	}
	return fmt.Sprintf("https://cdn.discordapp.com/embed/avatars/%d.png", (n>>22)%6)
}

func (s *Server) presenceAvatar(r *http.Request, u *auth.User) string {
	if u == nil || s.Pool == nil {
		return ""
	}
	var did string
	_ = s.Pool.QueryRow(r.Context(), `SELECT provider_user_id FROM user_identities WHERE user_id=$1 AND provider='discord' LIMIT 1`, u.ID).Scan(&did)
	return discordAvatarURL(did)
}

func (s *Server) touchPresenceFromRequest(r *http.Request, sid uuid.UUID) bool {
	u := currentUser(r)
	if u == nil || sid == uuid.Nil {
		return false
	}
	joined := s.sessionHub().touch(sid, u.ID, requestClientID(r, nil), presenceDisplay(u), s.presenceAvatar(r, u))
	if joined {
		s.sessionHub().publishPresence(sid)
	}
	return joined
}

func (s *Server) rejectSSEQueryAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("access_token") != "" || r.URL.Query().Get("token") != "" {
			writeErr(w, http.StatusUnauthorized, "unauthorized", "query token not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeSSEHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "private, no-store, no-cache, must-revalidate")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
}

func flushSSE(w http.ResponseWriter) {
	_ = http.NewResponseController(w).Flush()
}

func writeSSEComment(w http.ResponseWriter, comment string) {
	_, _ = fmt.Fprintf(w, ": %s\n\n", comment)
	flushSSE(w)
}

func writeSSEEvent(w http.ResponseWriter, name string, data []byte) {
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data)
	flushSSE(w)
}

func writeSSEJSON(w http.ResponseWriter, name string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	writeSSEEvent(w, name, data)
}

func serverTimeNow() string {
	return nowFn().UTC().Format(time.RFC3339Nano)
}

var sessionStateKeys = []string{
	"id", "kind", "owner_key", "state_revision", "status", "volume", "muted",
	"output_pref", "playback_instance_id", "repeat", "shuffle", "autoplay",
	"current_index", "current_track_id", "items", "renderer_kind", "renderer_id",
	"renderer_generation", "binding_revision", "stop_after_current", "shuffle_mode",
	"crossfade_seconds", "replaygain_mode", "device_id", "generation",
}

func sessionStatePayload(q map[string]any) map[string]any {
	out := map[string]any{}
	if q == nil {
		return out
	}
	for _, k := range sessionStateKeys {
		if v, ok := q[k]; ok {
			out[k] = v
		}
	}
	return out
}

func sessionPlayheadPayload(q map[string]any) map[string]any {
	if q == nil {
		q = map[string]any{}
	}
	return map[string]any{
		"playback_instance_id":   q["playback_instance_id"],
		"checkpoint_position_ms": q["position_ms"],
		"checkpoint_at":          q["checkpoint_at"],
		"status":                 q["status"],
		"playhead_sequence":      q["playhead_sequence"],
		"playback_rate":          q["playback_rate"],
		"duration_ms":            q["duration_ms"],
	}
}

func sortListeners(list []QueueListener, current uuid.UUID) {
	sort.SliceStable(list, func(i, j int) bool {
		iu, ju := listenerUser(list[i]), listenerUser(list[j])
		if iu == current && ju != current {
			return true
		}
		if ju == current && iu != current {
			return false
		}
		di := strings.ToLower(list[i].DisplayName)
		dj := strings.ToLower(list[j].DisplayName)
		if di != dj {
			return di < dj
		}
		return strings.Compare(derefStr(list[i].UserID), derefStr(list[j].UserID)) < 0
	})
}

func listenerUser(l QueueListener) uuid.UUID {
	if l.UserID == nil {
		return uuid.Nil
	}
	id, err := uuid.Parse(*l.UserID)
	if err != nil {
		return uuid.Nil
	}
	return id
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (s *Server) snapshotListeners(r *http.Request, sid uuid.UUID) []QueueListener {
	web := s.sessionHub().listeners(sid)
	list := s.mergeDiscordListeners(r, sid, web)
	cur := uuid.Nil
	if u := currentUser(r); u != nil {
		cur = u.ID
	}
	sortListeners(list, cur)
	if list == nil {
		list = []QueueListener{}
	}
	return list
}

func (s *Server) mergeDiscordListeners(r *http.Request, sid uuid.UUID, web []QueueListener) []QueueListener {
	if s.Pool == nil || sid == uuid.Nil {
		return append([]QueueListener{}, web...)
	}
	rows, err := s.Pool.Query(r.Context(), `
		SELECT v.discord_user_id, i.user_id, COALESCE(NULLIF(u.display_name, ''), NULLIF(i.provider_username, ''), v.discord_user_id)
		FROM discord_voice_runtime r
		JOIN discord_user_voice v ON v.guild_id = r.guild_id AND v.channel_id = r.voice_channel_id
		LEFT JOIN user_identities i ON i.provider = 'discord' AND i.provider_user_id = v.discord_user_id
		LEFT JOIN users u ON u.id = i.user_id
		WHERE r.session_id = $1 AND v.channel_id IS NOT NULL AND v.channel_id <> ''`, sid)
	if err != nil {
		return append([]QueueListener{}, web...)
	}
	defer rows.Close()

	byUser := map[string]QueueListener{}
	var unlinked []QueueListener
	for _, l := range web {
		if l.UserID != nil {
			byUser[*l.UserID] = l
		}
	}
	for rows.Next() {
		var did string
		var uid *uuid.UUID
		var display string
		if err := rows.Scan(&did, &uid, &display); err != nil {
			continue
		}
		av := discordAvatarURL(did)
		if uid != nil && *uid != uuid.Nil {
			id := uid.String()
			if existing, ok := byUser[id]; ok {
				existing.Source = "both"
				if existing.AvatarURL == nil && av != "" {
					existing.AvatarURL = &av
				}
				if existing.DisplayName == "" {
					existing.DisplayName = display
				}
				byUser[id] = existing
				continue
			}
			row := QueueListener{UserID: &id, DisplayName: display, Source: "discord"}
			if av != "" {
				row.AvatarURL = &av
			}
			byUser[id] = row
			continue
		}
		row := QueueListener{DisplayName: display, Source: "discord"}
		if av != "" {
			row.AvatarURL = &av
		}
		unlinked = append(unlinked, row)
	}
	out := make([]QueueListener, 0, len(byUser)+len(unlinked))
	for _, l := range byUser {
		out = append(out, l)
	}
	out = append(out, unlinked...)
	return out
}

func (s *Server) annotateQueueSnapshot(r *http.Request, sid uuid.UUID, q map[string]any) {
	if q == nil {
		return
	}
	s.touchPresenceFromRequest(r, sid)
	q["server_time"] = serverTimeNow()
	q["listeners"] = s.snapshotListeners(r, sid)
}

func (s *Server) publishQueueSSE(sid uuid.UUID, q map[string]any, playhead bool) {
	if sid == uuid.Nil || q == nil {
		return
	}
	h := s.sessionHub()
	h.publish(sid, sseEventState, sessionStatePayload(q))
	if playhead {
		h.publish(sid, sseEventPlayhead, sessionPlayheadPayload(q))
	}
}

func (s *Server) respondQueue(w http.ResponseWriter, r *http.Request, sid uuid.UUID, q map[string]any, emit string) {
	if q == nil {
		q = map[string]any{}
	}
	switch emit {
	case "state":
		s.publishQueueSSE(sid, q, false)
	case "state+playhead":
		s.publishQueueSSE(sid, q, true)
	}
	s.annotateQueueSnapshot(r, sid, q)
	writeJSON(w, 200, q)
}

func (s *Server) queueHeartbeat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClientID string `json:"client_id"`
		DeviceID string `json:"device_id"`
	}
	_ = decodeJSON(r, &body)
	extra := map[string]any{}
	if body.ClientID != "" {
		extra["client_id"] = body.ClientID
	}
	if body.DeviceID != "" {
		extra["device_id"] = body.DeviceID
	}
	sid, err := s.webPlaySession(r, extra)
	if s.writePlaySessionErr(w, err) {
		return
	}
	u := currentUser(r)
	joined := s.sessionHub().touch(sid, u.ID, requestClientID(r, extra), presenceDisplay(u), s.presenceAvatar(r, u))
	if joined {
		s.sessionHub().publishPresence(sid)
	}
	writeJSON(w, 200, map[string]any{"ok": true, "server_time": serverTimeNow()})
}

func (s *Server) queueSSE(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("access_token") != "" || r.URL.Query().Get("token") != "" {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "query token not allowed")
		return
	}
	u := currentUser(r)
	if u == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	sid, err := s.webPlaySession(r, nil)
	if s.writePlaySessionErr(w, err) {
		return
	}
	writeSSEHeaders(w)
	flushSSE(w)

	clientID := requestClientID(r, nil)
	h := s.sessionHub()
	joined := h.addSSE(sid, u.ID, clientID, presenceDisplay(u), s.presenceAvatar(r, u))
	sub := h.subscribe(sid)
	defer func() {
		h.unsubscribe(sid, sub)
		if h.dropSSE(sid, u.ID, clientID) {
			h.publishPresence(sid)
		}
	}()
	if joined {
		h.publishPresence(sid)
	}

	writeSSEJSON(w, sseEventPresence, map[string]any{"listeners": s.snapshotListeners(r, sid)})
	writeSSEJSON(w, sseEventAcquisition, emptyAcquisitionStatus)

	ping := time.NewTicker(ssePingInterval)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			h.expireAndPublish(nowFn())
			writeSSEComment(w, "ping")
		case ev, ok := <-sub.ch:
			if !ok {
				return
			}
			writeSSEEvent(w, ev.name, ev.data)
		}
	}
}
