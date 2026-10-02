package oplog

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Entry struct {
	ID         uuid.UUID      `json:"id"`
	CreatedAt  time.Time      `json:"created_at"`
	Level      string         `json:"level"`
	Category   string         `json:"category"`
	Message    string         `json:"message"`
	ActorID    *uuid.UUID     `json:"actor_id,omitempty"`
	ActorName  string         `json:"actor_name,omitempty"`
	JobID      *uuid.UUID     `json:"job_id,omitempty"`
	LibraryID  *uuid.UUID     `json:"library_id,omitempty"`
	TrackID    *uuid.UUID     `json:"track_id,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
	Error      string         `json:"error,omitempty"`
	Summary    string         `json:"summary,omitempty"`
	Type       string         `json:"type,omitempty"`
	RequestID  string         `json:"request_id,omitempty"`
	IP         string         `json:"ip,omitempty"`
	Action     string         `json:"action,omitempty"`
	Method     string         `json:"method,omitempty"`
	Route      string         `json:"route,omitempty"`
	Status     *int           `json:"status,omitempty"`
	DurationMs *int           `json:"duration_ms,omitempty"`
	Result     string         `json:"result,omitempty"`
}

// Filter narrows List. Level, Category and Result accept comma-separated
// values. Actor matches a username/display name fragment or an exact user id.
type Filter struct {
	Level     string
	Category  string
	Q         string
	Actor     string
	IP        string
	Result    string
	RequestID string
	Since     time.Time
	Until     time.Time
	Limit     int
	Cursor    string
}

const (
	ResultSuccess = "success"
	ResultFailure = "failure"
)

var (
	rePostgresURL = regexp.MustCompile(`(?i)(postgres(?:ql)?://[^:]+:)[^@\s]+@`)
	reKVSecret    = regexp.MustCompile(`(?i)\b(password|passwd|passphrase|secret|token|api[_-]?key|access[_-]?key|secret[_-]?key|client[_-]?secret|authorization|cookie|bearer|sd_master_key)\s*[=:]\s*([^\s,;&]+)`)
	reBearer      = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._\-+/=]+`)
	rePEM         = regexp.MustCompile(`(?is)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
	reAPIKey      = regexp.MustCompile(`\b(sdp_|sd_)[A-Za-z0-9_\-]{16,}`)
)

// Redact removes credentials from free text before it is stored or shown.
func Redact(s string) string {
	if s == "" {
		return s
	}
	out := rePEM.ReplaceAllString(s, "[redacted-key]")
	out = rePostgresURL.ReplaceAllString(out, "${1}[redacted]@")
	out = reBearer.ReplaceAllString(out, "Bearer [redacted]")
	out = reKVSecret.ReplaceAllString(out, "${1}=[redacted]")
	out = reAPIKey.ReplaceAllString(out, "${1}[redacted]")
	return out
}

// sensitiveKey reports whether a detail key names a credential. Such values
// are replaced wholesale, whatever their type.
func sensitiveKey(k string) bool {
	k = strings.ToLower(strings.TrimSpace(k))
	k = strings.NewReplacer("-", "_", " ", "_").Replace(k)
	switch k {
	case "password", "passwd", "new_password", "current_password", "passphrase", "current_passphrase",
		"secret", "client_secret", "secret_key", "access_key", "api_key", "apikey", "token", "bot_token",
		"access_token", "refresh_token", "id_token", "authorization", "cookie", "set_cookie", "sd_session",
		"sd_csrf", "csrf", "x_csrf_token", "developer_token", "master_key", "sd_master_key", "private_key", "signature":
		return true
	}
	return strings.HasSuffix(k, "_password") || strings.HasSuffix(k, "_secret") || strings.HasSuffix(k, "_token") ||
		strings.HasSuffix(k, "_passphrase") || strings.HasSuffix(k, "_api_key")
}

func normalizeLevel(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug", "info", "warn", "error":
		return strings.ToLower(strings.TrimSpace(s))
	case "warning":
		return "warn"
	default:
		return "info"
	}
}

func normalizeResult(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case ResultSuccess:
		return ResultSuccess
	case ResultFailure:
		return ResultFailure
	default:
		return ""
	}
}

const maxText = 4000

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// prepare normalises and redacts an entry before it is stored.
func prepare(e Entry) (Entry, []byte) {
	e.Level = normalizeLevel(e.Level)
	e.Result = normalizeResult(e.Result)
	e.Category = clip(strings.TrimSpace(e.Category), 64)
	e.Message = clip(Redact(strings.TrimSpace(e.Message)), maxText)
	e.Action = clip(strings.TrimSpace(e.Action), 128)
	e.ActorName = clip(strings.TrimSpace(e.ActorName), 128)
	e.IP = clip(strings.TrimSpace(e.IP), 64)
	e.RequestID = clip(strings.TrimSpace(e.RequestID), 128)
	e.Method = clip(strings.TrimSpace(e.Method), 16)
	e.Route = clip(Redact(strings.TrimSpace(e.Route)), 256)
	details := e.Details
	if details == nil {
		details = map[string]any{}
	}
	if e.Error != "" {
		details["error"] = clip(e.Error, maxText)
	}
	if e.Type != "" {
		if _, ok := details["type"]; !ok {
			details["type"] = e.Type
		}
	}
	raw, err := json.Marshal(RedactDetails(details))
	if err != nil {
		raw = []byte("{}")
	}
	return e, raw
}

const insertSQL = `
	INSERT INTO operational_logs (created_at, level, category, message, actor_id, job_id, library_id, track_id, details,
		request_id, ip, actor_name, action, method, route, status, duration_ms, result)
	VALUES (coalesce($1, now()),$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,$12,$13,$14,$15,$16,$17,$18)`

func insertArgs(e Entry, raw []byte) []any {
	var at *time.Time
	if !e.CreatedAt.IsZero() {
		t := e.CreatedAt
		at = &t
	}
	return []any{at, e.Level, e.Category, e.Message, e.ActorID, e.JobID, e.LibraryID, e.TrackID, raw,
		e.RequestID, e.IP, e.ActorName, e.Action, e.Method, e.Route, e.Status, e.DurationMs, e.Result}
}

// Write stores one entry synchronously. Request context (id, ip, actor) is
// filled from ctx when the caller did not set it.
func Write(ctx context.Context, pool *pgxpool.Pool, e Entry) error {
	if pool == nil {
		return nil
	}
	e = fillFromContext(ctx, e)
	e, raw := prepare(e)
	_, err := pool.Exec(ctx, insertSQL, insertArgs(e, raw)...)
	if err != nil && isUndefinedTable(err) {
		return nil
	}
	return err
}

// RedactDetails returns a copy of in with credential-named keys masked and
// every string value scrubbed. Nested maps and slices are walked.
func RedactDetails(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		if sensitiveKey(k) {
			out[k] = "[redacted]"
			continue
		}
		out[k] = redactValue(v, 0)
	}
	return out
}

func redactValue(v any, depth int) any {
	if depth > 6 {
		return "[truncated]"
	}
	switch t := v.(type) {
	case string:
		return clip(Redact(t), maxText)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			if sensitiveKey(k) {
				out[k] = "[redacted]"
				continue
			}
			out[k] = redactValue(vv, depth+1)
		}
		return out
	case map[string]string:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			if sensitiveKey(k) {
				out[k] = "[redacted]"
				continue
			}
			out[k] = clip(Redact(vv), maxText)
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for _, vv := range t {
			out = append(out, redactValue(vv, depth+1))
		}
		return out
	case []string:
		out := make([]string, 0, len(t))
		for _, vv := range t {
			out = append(out, clip(Redact(vv), maxText))
		}
		return out
	case error:
		return clip(Redact(t.Error()), maxText)
	default:
		return v
	}
}

func isUndefinedTable(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "operational_logs") && (strings.Contains(s, "does not exist") || strings.Contains(s, "undefined"))
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// escapeLike escapes LIKE wildcards in user input.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func buildWhere(f Filter) ([]string, []any) {
	args := []any{}
	where := []string{"1=1"}
	add := func(v any) string {
		args = append(args, v)
		return "$" + itoa(len(args))
	}
	if levels := splitList(f.Level); len(levels) > 0 {
		norm := make([]string, 0, len(levels))
		for _, l := range levels {
			norm = append(norm, normalizeLevel(l))
		}
		where = append(where, "level = ANY("+add(norm)+"::text[])")
	}
	if cats := splitList(f.Category); len(cats) > 0 {
		where = append(where, "category = ANY("+add(cats)+"::text[])")
	}
	if res := normalizeResult(f.Result); res != "" {
		where = append(where, "result = "+add(res))
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		p := add("%" + escapeLike(q) + "%")
		where = append(where, "(message ILIKE "+p+" OR category ILIKE "+p+" OR action ILIKE "+p+" OR route ILIKE "+p+
			" OR actor_name ILIKE "+p+" OR request_id ILIKE "+p+" OR details::text ILIKE "+p+")")
	}
	if a := strings.TrimSpace(f.Actor); a != "" {
		if id, err := uuid.Parse(a); err == nil {
			where = append(where, "actor_id = "+add(id))
		} else {
			where = append(where, "actor_name ILIKE "+add("%"+escapeLike(a)+"%"))
		}
	}
	if ip := strings.TrimSpace(f.IP); ip != "" {
		where = append(where, "ip LIKE "+add(escapeLike(ip)+"%"))
	}
	if rid := strings.TrimSpace(f.RequestID); rid != "" {
		where = append(where, "request_id = "+add(rid))
	}
	if !f.Since.IsZero() {
		where = append(where, "created_at >= "+add(f.Since))
	}
	if !f.Until.IsZero() {
		where = append(where, "created_at < "+add(f.Until))
	}
	if c := strings.TrimSpace(f.Cursor); c != "" {
		if ts, id, ok := parseCursor(c); ok {
			a, b := add(ts), add(id)
			where = append(where, "(created_at, id) < ("+a+","+b+")")
		}
	}
	return where, args
}

func List(ctx context.Context, pool *pgxpool.Pool, f Filter) ([]Entry, string, error) {
	if pool == nil {
		return []Entry{}, "", nil
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	where, args := buildWhere(f)
	args = append(args, limit+1)
	q := `SELECT id, created_at, level, category, message, actor_id, job_id, library_id, track_id, details,
			request_id, ip, actor_name, action, method, route, status, duration_ms, result
		FROM operational_logs
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY created_at DESC, id DESC
		LIMIT $` + itoa(len(args))
	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		if isUndefinedTable(err) {
			return []Entry{}, "", nil
		}
		return nil, "", err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		var details []byte
		if err := rows.Scan(&e.ID, &e.CreatedAt, &e.Level, &e.Category, &e.Message, &e.ActorID, &e.JobID, &e.LibraryID, &e.TrackID, &details,
			&e.RequestID, &e.IP, &e.ActorName, &e.Action, &e.Method, &e.Route, &e.Status, &e.DurationMs, &e.Result); err != nil {
			continue
		}
		e.Message = Redact(e.Message)
		if len(details) > 0 {
			_ = json.Unmarshal(details, &e.Details)
		}
		if e.Details == nil {
			e.Details = map[string]any{}
		}
		e.Details = RedactDetails(e.Details)
		if t, ok := e.Details["type"].(string); ok {
			e.Type = t
		}
		if t, ok := e.Details["error"].(string); ok {
			e.Error = t
		}
		out = append(out, e)
	}
	next := ""
	if len(out) > limit {
		last := out[limit-1]
		next = last.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + last.ID.String()
		out = out[:limit]
	}
	return out, next, rows.Err()
}

// Categories returns the categories seen recently, for filter menus.
func Categories(ctx context.Context, pool *pgxpool.Pool) []string {
	out := []string{}
	if pool == nil {
		return out
	}
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT category FROM operational_logs
		WHERE created_at > now() - interval '30 days' AND category <> ''
		ORDER BY category LIMIT 100`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var c string
		if rows.Scan(&c) == nil {
			out = append(out, c)
		}
	}
	return out
}

func parseCursor(s string) (time.Time, uuid.UUID, bool) {
	i := strings.LastIndex(s, "|")
	if i < 0 {
		return time.Time{}, uuid.Nil, false
	}
	ts, err := time.Parse(time.RFC3339Nano, s[:i])
	if err != nil {
		return time.Time{}, uuid.Nil, false
	}
	id, err := uuid.Parse(s[i+1:])
	if err != nil {
		return time.Time{}, uuid.Nil, false
	}
	return ts, id, true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
