package httpapi

import (
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
			m[c] = vals[i]
		}
		out = append(out, m)
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out
}
