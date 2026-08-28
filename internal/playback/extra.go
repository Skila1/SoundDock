package playback

import (
	"encoding/json"
	"strconv"

	"github.com/google/uuid"
)

func extraInt(extra map[string]any, key string) (int, bool) {
	if extra == nil {
		return 0, false
	}
	v, ok := extra[key]
	if !ok || v == nil {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return int(t), true
	case float32:
		return int(t), true
	case int:
		return t, true
	case int64:
		return int(t), true
	case json.Number:
		i, err := t.Int64()
		return int(i), err == nil
	case string:
		i, err := strconv.Atoi(t)
		return i, err == nil
	default:
		return 0, false
	}
}

// normalizeControlExtra maps UI control payloads onto engine extra keys.
// action=stop_after_current + extra.enabled becomes extra.stop_after_current.
func normalizeControlExtra(action string, extra map[string]any) map[string]any {
	if extra == nil {
		extra = map[string]any{}
	}
	if action == "stop_after_current" {
		if _, ok := extraBool(extra, "stop_after_current"); !ok {
			if v, ok := extraBool(extra, "enabled"); ok {
				extra["stop_after_current"] = v
			}
		}
	}
	return extra
}

func extraUUIDs(extra map[string]any, key string) []uuid.UUID {
	if extra == nil {
		return nil
	}
	raw, ok := extra[key]
	if !ok || raw == nil {
		return nil
	}
	var out []uuid.UUID
	switch t := raw.(type) {
	case []uuid.UUID:
		return t
	case []string:
		for _, s := range t {
			if id, err := uuid.Parse(s); err == nil {
				out = append(out, id)
			}
		}
		return out
	case []any:
		for _, v := range t {
			switch x := v.(type) {
			case uuid.UUID:
				out = append(out, x)
			case string:
				if id, err := uuid.Parse(x); err == nil {
					out = append(out, id)
				}
			}
		}
	}
	return out
}

func extraString(extra map[string]any, key string) string {
	if extra == nil {
		return ""
	}
	v, ok := extra[key]
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func extraBool(extra map[string]any, key string) (bool, bool) {
	if extra == nil {
		return false, false
	}
	v, ok := extra[key]
	if !ok || v == nil {
		return false, false
	}
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		if t == "true" {
			return true, true
		}
		if t == "false" {
			return false, true
		}
	}
	return false, false
}
