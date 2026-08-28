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
	ID        uuid.UUID      `json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	Level     string         `json:"level"`
	Category  string         `json:"category"`
	Message   string         `json:"message"`
	ActorID   *uuid.UUID     `json:"actor_id,omitempty"`
	JobID     *uuid.UUID     `json:"job_id,omitempty"`
	LibraryID *uuid.UUID     `json:"library_id,omitempty"`
	TrackID   *uuid.UUID     `json:"track_id,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
	Error     string         `json:"error,omitempty"`
	Summary   string         `json:"summary,omitempty"`
	Type      string         `json:"type,omitempty"`
}

type Filter struct {
	Level    string
	Category string
	Q        string
	Limit    int
	Cursor   string
}

var (
	rePostgresURL = regexp.MustCompile(`(?i)(postgres(?:ql)?://[^:]+:)[^@\s]+@`)
	reKVSecret    = regexp.MustCompile(`(?i)\b(password|passwd|secret|token|api[_-]?key|access[_-]?key|secret[_-]?key|authorization|bearer|sd_master_key)\s*[=:]\s*([^\s,;]+)`)
	reBearer      = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._\-+/=]+`)
	rePEM         = regexp.MustCompile(`(?is)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
)

func Redact(s string) string {
	if s == "" {
		return s
	}
	out := rePEM.ReplaceAllString(s, "[redacted-key]")
	out = rePostgresURL.ReplaceAllString(out, "${1}[redacted]@")
	out = reBearer.ReplaceAllString(out, "Bearer [redacted]")
	out = reKVSecret.ReplaceAllString(out, "${1}=[redacted]")
	return out
}

func normalizeLevel(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug", "info", "warn", "error":
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return "info"
	}
}

func Write(ctx context.Context, pool *pgxpool.Pool, e Entry) error {
	if pool == nil {
		return nil
	}
	level := normalizeLevel(e.Level)
	msg := Redact(strings.TrimSpace(e.Message))
	details := e.Details
	if details == nil {
		details = map[string]any{}
	}
	raw, err := json.Marshal(redactDetails(details))
	if err != nil {
		raw = []byte("{}")
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO operational_logs (level, category, message, actor_id, job_id, library_id, track_id, details)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb)`,
		level, strings.TrimSpace(e.Category), msg, e.ActorID, e.JobID, e.LibraryID, e.TrackID, raw)
	if err != nil && isUndefinedTable(err) {
		return nil
	}
	return err
}

func redactDetails(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		switch t := v.(type) {
		case string:
			out[k] = Redact(t)
		default:
			out[k] = v
		}
	}
	return out
}

func isUndefinedTable(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "operational_logs") && (strings.Contains(s, "does not exist") || strings.Contains(s, "undefined"))
}

func List(ctx context.Context, pool *pgxpool.Pool, f Filter) ([]Entry, string, error) {
	if pool == nil {
		return []Entry{}, "", nil
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args := []any{}
	where := []string{"1=1"}
	if f.Level != "" {
		args = append(args, normalizeLevel(f.Level))
		where = append(where, "level=$"+itoa(len(args)))
	}
	if f.Category != "" {
		args = append(args, strings.TrimSpace(f.Category))
		where = append(where, "category=$"+itoa(len(args)))
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		args = append(args, "%"+q+"%")
		where = append(where, "(message ILIKE $"+itoa(len(args))+" OR category ILIKE $"+itoa(len(args))+")")
	}
	if c := strings.TrimSpace(f.Cursor); c != "" {
		if ts, id, ok := parseCursor(c); ok {
			args = append(args, ts, id)
			where = append(where, "(created_at, id) < ($"+itoa(len(args)-1)+",$"+itoa(len(args))+")")
		}
	}
	args = append(args, limit+1)
	q := `SELECT id, created_at, level, category, message, actor_id, job_id, library_id, track_id, details
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
		if err := rows.Scan(&e.ID, &e.CreatedAt, &e.Level, &e.Category, &e.Message, &e.ActorID, &e.JobID, &e.LibraryID, &e.TrackID, &details); err != nil {
			continue
		}
		e.Message = Redact(e.Message)
		if len(details) > 0 {
			_ = json.Unmarshal(details, &e.Details)
		}
		if e.Details == nil {
			e.Details = map[string]any{}
		}
		if t, ok := e.Details["type"].(string); ok {
			e.Type = t
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
