package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/sounddock/sounddock/internal/oplog"
)

// activityLog records meaningful requests in the Activity log: every
// mutation, every server error, permission and rate-limit denials, and a
// short list of sensitive reads (exports, sign-in callbacks). Successful
// reads, static assets, health probes, media byte ranges, SSE and playback
// heartbeats are skipped so the log stays useful. Request bodies, cookies,
// headers and credential-like query values are never recorded.
func (s *Server) activityLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := middleware.GetReqID(r.Context())
		if reqID != "" {
			w.Header().Set("X-Request-Id", reqID)
		}
		st := &oplog.Request{
			ID:        reqID,
			IP:        clientIP(r),
			Method:    r.Method,
			Path:      r.URL.Path,
			UserAgent: clipUA(r.UserAgent()),
		}
		r = r.WithContext(oplog.WithRequest(r.Context(), st))
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		capture := &errorCapture{status: ww.Status}
		ww.Tee(capture)

		var panicked any
		var stack []byte
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					if rec == http.ErrAbortHandler {
						panic(rec)
					}
					panicked = rec
					stack = debug.Stack()
					if ww.Status() == 0 {
						writeErr(ww, http.StatusInternalServerError, "internal", "An internal error occurred")
					}
				}
			}()
			next.ServeHTTP(ww, r)
		}()

		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}
		route := ""
		if rc := chi.RouteContext(r.Context()); rc != nil {
			route = rc.RoutePattern()
		}
		if panicked == nil && !shouldLogRequest(st, r.Method, r.URL.Path, route, status) {
			return
		}
		s.emitRequest(r, st, route, status, time.Since(start), capture, panicked, stack)
	})
}

func (s *Server) emitRequest(r *http.Request, st *oplog.Request, route string, status int, took time.Duration, capture *errorCapture, panicked any, stack []byte) {
	category, label := describeRoute(r.Method, r.URL.Path, route)
	cat, action, msg := st.Description()
	if cat != "" {
		category = cat
	}
	if msg != "" {
		label = msg
	}
	if action == "" {
		action = strings.ToLower(r.Method) + " " + firstNonEmpty(route, r.URL.Path)
	}
	level := "info"
	result := oplog.ResultSuccess
	switch {
	case status >= 500 || panicked != nil:
		level, result = "error", oplog.ResultFailure
	case status >= 400 || st.Failure() != "":
		level, result = "warn", oplog.ResultFailure
	}
	details := st.Details()
	details["path"] = r.URL.Path
	if q := safeQuery(r.URL.Query()); len(q) > 0 {
		details["query"] = q
	}
	if st.UserAgent != "" {
		details["user_agent"] = st.UserAgent
	}
	if _, _, m := st.Actor(); m != "" {
		details["auth"] = m
	}
	if rc := chi.RouteContext(r.Context()); rc != nil {
		params := map[string]any{}
		for i, k := range rc.URLParams.Keys {
			if k == "*" || i >= len(rc.URLParams.Values) {
				continue
			}
			params[k] = rc.URLParams.Values[i]
		}
		if len(params) > 0 {
			details["params"] = params
		}
	}
	errText := ""
	if code, m := capture.parse(); code != "" || m != "" {
		if code != "" {
			details["code"] = code
		}
		errText = m
	}
	if f := st.Failure(); f != "" && errText == "" {
		errText = f
	}
	if panicked != nil {
		errText = fmt.Sprintf("panic: %v", panicked)
		details["stack"] = clipStack(stack)
	}
	message := label
	if result == oplog.ResultFailure {
		message = label + " failed"
		if errText != "" {
			message += ": " + errText
		}
	}
	id, name, _ := st.Actor()
	ms := int(took / time.Millisecond)
	e := oplog.Entry{
		Level:      level,
		Category:   category,
		Message:    message,
		ActorID:    id,
		ActorName:  name,
		RequestID:  st.ID,
		IP:         st.IP,
		Action:     action,
		Method:     r.Method,
		Route:      firstNonEmpty(route, r.URL.Path),
		Status:     &status,
		DurationMs: &ms,
		Result:     result,
		Details:    details,
		Error:      errText,
	}
	if w := oplog.Default(); w != nil {
		w.Enqueue(e)
	}
	if panicked != nil && s.Log != nil {
		s.Log.Error("request panic", "category", category, "route", e.Route, "request_id", st.ID, "err", errText)
	}
	s.autoAudit(r, st, e)
}

// autoAudit adds an audit row for successful admin changes that did not
// record their own, so the Audit tab covers every configuration change.
func (s *Server) autoAudit(r *http.Request, st *oplog.Request, e oplog.Entry) {
	if s.Audit == nil || e.Result != oplog.ResultSuccess || st.Audited() {
		return
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/v1/admin/") {
		return
	}
	meta := map[string]any{"method": r.Method, "route": e.Route, "status": e.Status}
	if p, ok := e.Details["params"]; ok {
		meta["params"] = p
	}
	target := ""
	if p, ok := e.Details["params"].(map[string]any); ok {
		for _, k := range []string{"id", "provider", "grantID", "discordID"} {
			if v, ok := p[k].(string); ok && v != "" {
				target = v
				break
			}
		}
	}
	action := strings.TrimSuffix(e.Message, ".")
	go s.Audit.Event(context.WithoutCancel(r.Context()), e.ActorID, action, target, e.IP, meta)
}

// shouldLogRequest decides whether a finished request is worth an Activity
// entry. Kept pure so the policy is testable.
func shouldLogRequest(st *oplog.Request, method, path, route string, status int) bool {
	suppress, force := st.Flags()
	if force {
		return true
	}
	if method == http.MethodOptions || method == http.MethodHead {
		return false
	}
	failed := status >= 400
	if suppress && !failed {
		return false
	}
	api := strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/rest/")
	if !api {
		// Static assets, SPA routes, /healthz, /readyz, /metrics.
		return status >= 500
	}
	if isNoisyRoute(method, path, route) {
		return status >= 500 || (failed && status != http.StatusNotFound && status != http.StatusUnauthorized && status != http.StatusRequestedRangeNotSatisfiable && method != http.MethodGet)
	}
	if strings.HasPrefix(path, "/rest/") {
		// OpenSubsonic clients poll and stream through GET; keep failures.
		return status >= 500 || status == http.StatusForbidden || status == http.StatusTooManyRequests
	}
	if method == http.MethodGet {
		switch {
		case status >= 500:
			return true
		case status == http.StatusForbidden, status == http.StatusTooManyRequests:
			return true
		case status == http.StatusUnauthorized:
			// Expired sessions poll; only record sign-in related reads.
			return strings.HasPrefix(path, "/api/v1/auth/") || strings.HasPrefix(path, "/api/v1/oauth/")
		}
		return isSensitiveRead(path, route)
	}
	return true
}

func isNoisyRoute(method, path, route string) bool {
	switch {
	case strings.HasSuffix(path, "/stream") && strings.HasPrefix(path, "/api/v1/tracks/"):
		return true
	case strings.HasSuffix(path, "/artwork") && method == http.MethodGet:
		return true
	case strings.HasSuffix(path, "/waveform") && method == http.MethodGet:
		return true
	case path == "/api/v1/me/queue/sse", path == "/api/v1/me/queue/events":
		return true
	case path == "/api/v1/me/queue/heartbeat":
		return true
	case path == "/api/v1/me/listen":
		return true
	case path == "/api/v1/auth/csrf":
		return true
	case strings.HasPrefix(path, "/api/v1/uploads/") && method == http.MethodPatch:
		// Resumable upload chunks; the create and complete calls are kept.
		return true
	}
	return false
}

func isSensitiveRead(path, route string) bool {
	switch route {
	case "/api/v1/me/export", "/api/v1/admin/backups/reminder", "/api/v1/auth/discord", "/api/v1/auth/discord/callback",
		"/api/v1/oauth/{provider}/callback", "/api/v1/admin/integrations/discord/invite", "/api/v1/playlists/{id}/export.m3u",
		"/api/v1/admin/users/{id}/library", "/api/v1/admin/discord-users/{discordID}/library":
		return true
	}
	return false
}

type routeLabel struct{ category, label string }

// routeLabels gives plain-language names to the routes admins care most
// about. Anything else falls back to a category from the path and a generic
// "METHOD route" label.
var routeLabels = map[string]routeLabel{
	"POST /api/v1/setup":                                             {"auth", "Initial setup"},
	"POST /api/v1/auth/login":                                        {"auth", "Sign in"},
	"POST /api/v1/auth/logout":                                       {"auth", "Sign out"},
	"POST /api/v1/auth/logout-all":                                   {"auth", "Sign out everywhere"},
	"GET /api/v1/auth/discord":                                       {"auth", "Discord sign-in started"},
	"GET /api/v1/auth/discord/callback":                              {"auth", "Discord sign-in"},
	"POST /api/v1/me/password":                                       {"auth", "Change password"},
	"DELETE /api/v1/me/sessions/{id}":                                {"auth", "End session"},
	"POST /api/v1/me/tokens":                                         {"auth", "Create personal token"},
	"DELETE /api/v1/me/tokens/{id}":                                  {"auth", "Revoke personal token"},
	"PATCH /api/v1/me":                                               {"account", "Update profile"},
	"GET /api/v1/me/export":                                          {"account", "Export account data"},
	"POST /api/v1/admin/users":                                       {"access", "Create user"},
	"PATCH /api/v1/admin/users/{id}":                                 {"access", "Update user access"},
	"DELETE /api/v1/admin/users/{id}":                                {"access", "Delete user"},
	"DELETE /api/v1/admin/users/{id}/identities/discord":             {"access", "Unlink Discord from user"},
	"POST /api/v1/admin/roles":                                       {"access", "Create group"},
	"PATCH /api/v1/admin/roles/{id}":                                 {"access", "Update group"},
	"DELETE /api/v1/admin/roles/{id}":                                {"access", "Delete group"},
	"POST /api/v1/admin/roles/{id}/members":                          {"access", "Add group members"},
	"DELETE /api/v1/admin/roles/{id}/members":                        {"access", "Remove group members"},
	"PUT /api/v1/admin/roles/{id}/discord":                           {"access", "Link group to Discord roles"},
	"POST /api/v1/admin/roles/sync-discord":                          {"access", "Sync Discord roles"},
	"POST /api/v1/admin/libraries/{id}/grants":                       {"access", "Add library grant"},
	"PATCH /api/v1/admin/libraries/{id}/grants/{grantID}":            {"access", "Update library grant"},
	"DELETE /api/v1/admin/libraries/{id}/grants/{grantID}":           {"access", "Remove library grant"},
	"PUT /api/v1/admin/library-grants-strict":                        {"access", "Change library grant mode"},
	"PUT /api/v1/admin/quotas":                                       {"config", "Update quotas"},
	"PUT /api/v1/admin/maintenance":                                  {"config", "Change maintenance mode"},
	"PUT /api/v1/admin/announcement":                                 {"config", "Update announcement"},
	"PUT /api/v1/admin/metadata":                                     {"config", "Change metadata settings"},
	"POST /api/v1/admin/metadata/refresh":                            {"library", "Start library metadata update"},
	"PUT /api/v1/admin/lyrics":                                       {"config", "Change lyrics settings"},
	"PUT /api/v1/admin/acquisition-policy":                           {"config", "Change download format"},
	"PUT /api/v1/admin/stream-policy":                                {"config", "Change stream policy"},
	"DELETE /api/v1/admin/transcode/cache":                           {"config", "Clear transcode cache"},
	"PUT /api/v1/admin/workers":                                      {"jobs", "Update worker pools"},
	"POST /api/v1/admin/jobs/{id}/cancel":                            {"jobs", "Cancel job"},
	"POST /api/v1/admin/jobs/{id}/retry":                             {"jobs", "Retry job"},
	"POST /api/v1/admin/storage":                                     {"library", "Add storage"},
	"PATCH /api/v1/admin/storage/{id}":                               {"library", "Update storage"},
	"DELETE /api/v1/admin/storage/{id}":                              {"library", "Remove storage"},
	"POST /api/v1/admin/libraries":                                   {"library", "Create library"},
	"PATCH /api/v1/admin/libraries/{id}":                             {"library", "Update library"},
	"DELETE /api/v1/admin/libraries/{id}":                            {"library", "Delete library"},
	"POST /api/v1/admin/libraries/{id}/scan":                         {"library", "Start library scan"},
	"POST /api/v1/admin/libraries/{id}/migrate":                      {"library", "Start library migration"},
	"POST /api/v1/admin/libraries/{id}/merge":                        {"library", "Merge libraries"},
	"POST /api/v1/admin/libraries/{id}/default":                      {"library", "Set default library"},
	"POST /api/v1/admin/library/integrity/scan":                      {"library", "Start integrity scan"},
	"POST /api/v1/admin/backups":                                     {"backup", "Run backup"},
	"PUT /api/v1/admin/backups/settings":                             {"backup", "Update backup settings"},
	"POST /api/v1/admin/backups/passphrase":                          {"backup", "Set recovery passphrase"},
	"POST /api/v1/admin/backups/{id}/restore":                        {"backup", "Restore backup"},
	"POST /api/v1/admin/backups/import-remote":                       {"backup", "Import remote backup"},
	"GET /api/v1/admin/backups/reminder":                             {"backup", "Download recovery reminder"},
	"PUT /api/v1/admin/retention":                                    {"config", "Update retention"},
	"POST /api/v1/admin/retention/run":                               {"library", "Run media prune"},
	"POST /api/v1/admin/retention/preview":                           {"library", "Preview media prune"},
	"POST /api/v1/admin/webhooks":                                    {"integrations", "Add webhook"},
	"DELETE /api/v1/admin/webhooks/{id}":                             {"integrations", "Delete webhook"},
	"POST /api/v1/admin/integrations":                                {"integrations", "Create API key"},
	"DELETE /api/v1/admin/integrations/{id}":                         {"integrations", "Revoke API key"},
	"PUT /api/v1/admin/integrations/external-providers/{provider}":   {"integrations", "Update playlist provider"},
	"PUT /api/v1/admin/integrations/discord":                         {"discord", "Update Discord settings"},
	"POST /api/v1/admin/integrations/discord/test":                   {"discord", "Test Discord bot"},
	"POST /api/v1/admin/integrations/discord/commands/sync":          {"discord", "Sync Discord commands"},
	"PATCH /api/v1/admin/integrations/discord/guilds/{id}":           {"discord", "Update Discord server"},
	"POST /api/v1/admin/integrations/discord/guilds/{id}/disconnect": {"discord", "Disconnect Discord voice"},
	"GET /api/v1/admin/integrations/discord/invite":                  {"discord", "Open Discord bot invite"},
	"PUT /api/v1/admin/updates":                                      {"update", "Change automatic updates"},
	"POST /api/v1/admin/updates/check":                               {"update", "Check for updates"},
	"POST /api/v1/admin/updates/apply":                               {"update", "Apply update"},
	"POST /api/v1/admin/stats/rebuild":                               {"jobs", "Start stats rebuild"},
	"POST /api/v1/admin/demo":                                        {"config", "Seed demo library"},
	"DELETE /api/v1/admin/demo":                                      {"config", "Remove demo library"},
	"POST /api/v1/me/discord/join":                                   {"discord", "Join Discord voice"},
	"POST /api/v1/me/discord/play":                                   {"discord", "Play in Discord"},
	"POST /api/v1/me/discord/link":                                   {"discord", "Link Discord account"},
	"POST /api/v1/me/providers/{provider}/connect":                   {"integrations", "Connect playlist provider"},
	"DELETE /api/v1/me/providers/{provider}":                         {"integrations", "Disconnect playlist provider"},
	"GET /api/v1/oauth/{provider}/callback":                          {"integrations", "Playlist provider sign-in"},
	"POST /api/v1/providers/{provider}/playlists/{id}/import":        {"integrations", "Import playlist"},
	"POST /api/v1/providers/{provider}/import-all":                   {"integrations", "Import all playlists"},
	"POST /api/v1/providers/import-url":                              {"integrations", "Import playlist from URL"},
	"PUT /api/v1/me/scrobble":                                        {"integrations", "Update scrobbling"},
	"POST /api/v1/me/scrobble/import":                                {"integrations", "Import scrobble history"},
	"PUT /api/v1/me/queue":                                           {"playback", "Replace queue"},
	"POST /api/v1/me/queue/add":                                      {"playback", "Add to queue"},
	"POST /api/v1/me/queue/control":                                  {"playback", "Playback control"},
	"POST /api/v1/me/queue/renderer/acquire":                         {"playback", "Switch playback device"},
	"POST /api/v1/me/party":                                          {"playback", "Update listening party"},
	"POST /api/v1/me/party/votes":                                    {"playback", "Vote in listening party"},
	"POST /api/v1/stream-tokens":                                     {"playback", "Issue stream token"},
	"POST /api/v1/me/offline/tokens":                                 {"playback", "Issue offline token"},
	"DELETE /api/v1/me/offline/tokens":                               {"playback", "Revoke offline tokens"},
	"POST /api/v1/imports/url":                                       {"media", "Import from URL"},
	"POST /api/v1/uploads":                                           {"media", "Start upload"},
	"POST /api/v1/uploads/finalize":                                  {"media", "Finalize uploads"},
	"POST /api/v1/uploads/{id}/complete":                             {"media", "Complete upload"},
	"POST /api/v1/tracks/bulk":                                       {"media", "Bulk track change"},
	"DELETE /api/v1/tracks/bulk":                                     {"media", "Bulk track delete"},
	"PATCH /api/v1/tracks/{id}":                                      {"media", "Edit track"},
	"PATCH /api/v1/tracks/{id}/metadata":                             {"media", "Edit track metadata"},
	"POST /api/v1/tracks/bulk/metadata":                              {"media", "Bulk metadata edit"},
	"POST /api/v1/albums":                                            {"media", "Create album"},
	"PATCH /api/v1/albums/{id}":                                      {"media", "Edit album"},
	"DELETE /api/v1/albums/{id}":                                     {"media", "Delete album"},
	"POST /api/v1/albums/merge":                                      {"media", "Merge albums"},
	"POST /api/v1/artists/merge":                                     {"media", "Merge artists"},
	"POST /api/v1/playlists":                                         {"playlists", "Create playlist"},
	"PUT /api/v1/playlists/{id}":                                     {"playlists", "Update playlist"},
	"DELETE /api/v1/playlists/{id}":                                  {"playlists", "Delete playlist"},
}

func describeRoute(method, path, route string) (category, label string) {
	key := method + " " + route
	if l, ok := routeLabels[key]; ok {
		return l.category, l.label
	}
	return routeCategory(path), method + " " + firstNonEmpty(route, path)
}

func routeCategory(path string) string {
	p := strings.TrimPrefix(path, "/api/v1")
	switch {
	case strings.HasPrefix(path, "/rest/"):
		return "subsonic"
	case strings.HasPrefix(p, "/auth/"), p == "/setup", strings.HasPrefix(p, "/setup/"), strings.HasPrefix(p, "/me/sessions"),
		strings.HasPrefix(p, "/me/tokens"), p == "/me/password", p == "/me/identities":
		return "auth"
	case strings.HasPrefix(p, "/admin/integrations/discord"), strings.HasPrefix(p, "/me/discord"), strings.HasPrefix(p, "/admin/discord"):
		return "discord"
	case strings.HasPrefix(p, "/admin/integrations"), strings.HasPrefix(p, "/admin/webhooks"), strings.HasPrefix(p, "/admin/external"),
		strings.HasPrefix(p, "/providers"), strings.HasPrefix(p, "/me/providers"), strings.HasPrefix(p, "/oauth/"), strings.HasPrefix(p, "/me/scrobble"):
		return "integrations"
	case strings.HasPrefix(p, "/admin/users"), strings.HasPrefix(p, "/admin/roles"), strings.HasPrefix(p, "/admin/permissions"),
		strings.HasPrefix(p, "/admin/library-grants"), strings.Contains(p, "/grants"):
		return "access"
	case strings.HasPrefix(p, "/admin/libraries"), strings.HasPrefix(p, "/admin/library"), strings.HasPrefix(p, "/admin/storage"),
		strings.HasPrefix(p, "/admin/scans"), strings.HasPrefix(p, "/admin/duplicate"), strings.HasPrefix(p, "/duplicates"),
		strings.HasPrefix(p, "/admin/retention"), strings.HasPrefix(p, "/admin/media"):
		return "library"
	case strings.HasPrefix(p, "/admin/backups"):
		return "backup"
	case strings.HasPrefix(p, "/admin/updates"):
		return "update"
	case strings.HasPrefix(p, "/admin/jobs"), strings.HasPrefix(p, "/admin/workers"), strings.HasPrefix(p, "/admin/stats"):
		return "jobs"
	case strings.HasPrefix(p, "/admin/playback"):
		return "playback"
	case strings.HasPrefix(p, "/admin/"):
		return "config"
	case strings.HasPrefix(p, "/me/queue"), strings.HasPrefix(p, "/me/party"), strings.HasPrefix(p, "/me/listen"),
		strings.HasPrefix(p, "/me/offline"), strings.HasPrefix(p, "/radio"), strings.HasPrefix(p, "/stream-tokens"),
		strings.HasSuffix(p, "/stream"):
		return "playback"
	case strings.HasPrefix(p, "/playlists"):
		return "playlists"
	case strings.HasPrefix(p, "/tracks"), strings.HasPrefix(p, "/albums"), strings.HasPrefix(p, "/artists"),
		strings.HasPrefix(p, "/uploads"), strings.HasPrefix(p, "/imports"), strings.HasPrefix(p, "/genres"),
		strings.HasPrefix(p, "/search"), strings.HasPrefix(p, "/replace-source"):
		return "media"
	case strings.HasPrefix(p, "/me"), strings.HasPrefix(p, "/users"), strings.HasPrefix(p, "/favourites"), strings.HasPrefix(p, "/history"):
		return "account"
	}
	return "api"
}

// safeQuery keeps query parameters useful for debugging and masks anything
// that could carry a credential.
func safeQuery(q url.Values) map[string]string {
	if len(q) == 0 {
		return nil
	}
	out := map[string]string{}
	n := 0
	for k, v := range q {
		if n >= 20 {
			break
		}
		n++
		lk := strings.ToLower(k)
		if lk == "code" || lk == "state" || lk == "t" || lk == "p" || lk == "s" || lk == "u" ||
			strings.Contains(lk, "token") || strings.Contains(lk, "key") || strings.Contains(lk, "secret") ||
			strings.Contains(lk, "pass") || strings.Contains(lk, "auth") || strings.Contains(lk, "sig") || strings.Contains(lk, "session") {
			out[k] = "[redacted]"
			continue
		}
		val := strings.Join(v, ",")
		if len(val) > 200 {
			val = val[:200] + "…"
		}
		out[k] = oplog.Redact(val)
	}
	return out
}

func clientIP(r *http.Request) string {
	raw := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(raw); err == nil {
		return host
	}
	return raw
}

func clipUA(ua string) string {
	if len(ua) > 200 {
		return ua[:200]
	}
	return ua
}

func clipStack(b []byte) string {
	s := oplog.Redact(string(b))
	if len(s) > 3000 {
		s = s[:3000] + "…"
	}
	return s
}

// errorCapture keeps the first bytes of an error response so the Activity
// entry can say why a request failed. Successful bodies are ignored.
type errorCapture struct {
	status func() int
	buf    bytes.Buffer
}

const errorCaptureMax = 2048

func (c *errorCapture) Write(p []byte) (int, error) {
	if c.status() >= 400 && c.buf.Len() < errorCaptureMax {
		room := errorCaptureMax - c.buf.Len()
		if len(p) < room {
			room = len(p)
		}
		c.buf.Write(p[:room])
	}
	return len(p), nil
}

func (c *errorCapture) parse() (code, message string) {
	if c == nil || c.buf.Len() == 0 {
		return "", ""
	}
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(c.buf.Bytes(), &body); err == nil {
		return body.Code, oplog.Redact(firstNonEmpty(body.Message, body.Error))
	}
	text := strings.TrimSpace(c.buf.String())
	if len(text) > 300 {
		text = text[:300]
	}
	return "", oplog.Redact(text)
}
