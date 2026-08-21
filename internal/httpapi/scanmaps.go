package httpapi

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func scanMaps(rows pgx.Rows, cols ...string) []map[string]any {
	var out []map[string]any
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		m := map[string]any{}
		for i, c := range cols {
			m[c] = jsonCell(vals[i])
		}
		out = append(out, m)
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out
}

func jsonCell(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case uuid.UUID:
		return t.String()
	case [16]byte:
		return uuid.UUID(t).String()
	case []byte:
		if len(t) == 16 {
			var u uuid.UUID
			copy(u[:], t)
			return u.String()
		}
		return string(t)
	default:
		return v
	}
}
