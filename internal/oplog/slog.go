package oplog

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// NoActivity marks a log record that is already recorded in the activity log
// some other way, so the tee does not store it twice.
var NoActivity = slog.Bool(noActivityKey, false)

const noActivityKey = "activity"

// TeeHandler forwards every record to next and copies records at or above
// min into the activity log, so warnings and errors logged anywhere in the
// process show up in Activity without per-call plumbing.
type TeeHandler struct {
	next   slog.Handler
	min    slog.Level
	attrs  []slog.Attr
	groups []string
}

// NewTeeHandler wraps next.
func NewTeeHandler(next slog.Handler, min slog.Level) *TeeHandler {
	return &TeeHandler{next: next, min: min}
}

func (h *TeeHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l) || l >= h.min
}

func (h *TeeHandler) Handle(ctx context.Context, rec slog.Record) error {
	var err error
	if h.next.Enabled(ctx, rec.Level) {
		err = h.next.Handle(ctx, rec)
	}
	if rec.Level >= h.min && Default() != nil && !skipActivity(rec) {
		Emit(ctx, h.entry(rec))
	}
	return err
}

func skipActivity(rec slog.Record) bool {
	skip := false
	rec.Attrs(func(a slog.Attr) bool {
		if a.Key == noActivityKey && a.Value.Kind() == slog.KindBool && !a.Value.Bool() {
			skip = true
			return false
		}
		return true
	})
	return skip
}

func (h *TeeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := *h
	out.next = h.next.WithAttrs(attrs)
	out.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &out
}

func (h *TeeHandler) WithGroup(name string) slog.Handler {
	out := *h
	out.next = h.next.WithGroup(name)
	out.groups = append(append([]string{}, h.groups...), name)
	return &out
}

func (h *TeeHandler) entry(rec slog.Record) Entry {
	details := map[string]any{}
	category := ""
	errText := ""
	visit := func(a slog.Attr) {
		a.Value = a.Value.Resolve()
		key := a.Key
		switch strings.ToLower(key) {
		case noActivityKey:
			return
		case "category", "component":
			if category == "" {
				category = a.Value.String()
			}
			return
		case "err", "error":
			errText = a.Value.String()
			return
		}
		if len(h.groups) > 0 {
			key = strings.Join(h.groups, ".") + "." + key
		}
		details[key] = attrValue(a.Value)
	}
	for _, a := range h.attrs {
		visit(a)
	}
	rec.Attrs(func(a slog.Attr) bool {
		visit(a)
		return true
	})
	if category == "" {
		category = "system"
	}
	level := "info"
	switch {
	case rec.Level >= slog.LevelError:
		level = "error"
	case rec.Level >= slog.LevelWarn:
		level = "warn"
	}
	result := ""
	if level == "error" {
		result = ResultFailure
	}
	return Entry{
		CreatedAt: rec.Time,
		Level:     level,
		Category:  category,
		Message:   rec.Message,
		Error:     errText,
		Details:   details,
		Result:    result,
		Action:    "log." + level,
	}
}

func attrValue(v slog.Value) any {
	switch v.Kind() {
	case slog.KindString:
		return v.String()
	case slog.KindInt64:
		return v.Int64()
	case slog.KindUint64:
		return v.Uint64()
	case slog.KindFloat64:
		return v.Float64()
	case slog.KindBool:
		return v.Bool()
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time()
	case slog.KindGroup:
		m := map[string]any{}
		for _, a := range v.Group() {
			m[a.Key] = attrValue(a.Value.Resolve())
		}
		return m
	default:
		if e, ok := v.Any().(error); ok {
			return e.Error()
		}
		return fmt.Sprint(v.Any())
	}
}
