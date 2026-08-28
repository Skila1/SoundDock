package retention

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

func (e *Engine) busy(ctx context.Context) (tracks []uuid.UUID, libs []uuid.UUID) {
	trackSet := map[uuid.UUID]struct{}{}
	libSet := map[uuid.UUID]struct{}{}
	addTrack := func(id uuid.UUID) {
		if id != uuid.Nil {
			trackSet[id] = struct{}{}
		}
	}
	addLib := func(id uuid.UUID) {
		if id != uuid.Nil {
			libSet[id] = struct{}{}
		}
	}
	if e.pool == nil {
		return nil, nil
	}
	rows, err := e.pool.Query(ctx, `SELECT type, payload FROM jobs WHERE status IN ('queued','running','retry')`)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()
	var refs []string
	for rows.Next() {
		var typ string
		var raw []byte
		if err := rows.Scan(&typ, &raw); err != nil {
			continue
		}
		var p map[string]any
		_ = json.Unmarshal(raw, &p)
		addTrack(uuidFromAny(p["track_id"]))
		for _, key := range []string{"ids", "track_ids"} {
			for _, v := range anySlice(p[key]) {
				addTrack(uuidFromAny(v))
			}
		}
		addLib(uuidFromAny(p["library_id"]))
		addLib(uuidFromAny(p["dest"]))
		addLib(uuidFromAny(p["dest_library_id"]))
		addLib(uuidFromAny(p["source_library_id"]))
		for _, v := range anySlice(p["source_ids"]) {
			addLib(uuidFromAny(v))
		}
		for _, v := range anySlice(p["urls"]) {
			if s, ok := v.(string); ok {
				s = strings.TrimSpace(s)
				if IsYouTubeID(s) {
					refs = append(refs, s)
				}
			}
		}
		switch {
		case typ == "library.scan", typ == "library.migrate", typ == "library.merge", typ == "library.delete",
			strings.HasPrefix(typ, "ingest."), strings.HasPrefix(typ, "external.playlist."):
			addLib(uuidFromAny(p["library_id"]))
		}
	}
	if len(refs) > 0 {
		r2, err := e.pool.Query(ctx, `SELECT id FROM tracks WHERE acquisition_ref = ANY($1)`, refs)
		if err == nil {
			defer r2.Close()
			for r2.Next() {
				var id uuid.UUID
				if r2.Scan(&id) == nil {
					addTrack(id)
				}
			}
		}
	}
	for _, id := range e.live.IDs() {
		addTrack(id)
	}
	for id := range trackSet {
		tracks = append(tracks, id)
	}
	for id := range libSet {
		libs = append(libs, id)
	}
	return tracks, libs
}

func uuidFromAny(v any) uuid.UUID {
	switch t := v.(type) {
	case string:
		id, err := uuid.Parse(t)
		if err == nil {
			return id
		}
	case []byte:
		id, err := uuid.ParseBytes(t)
		if err == nil {
			return id
		}
	}
	return uuid.Nil
}

func anySlice(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	default:
		return nil
	}
}

func emptyUUID(ids []uuid.UUID) []uuid.UUID {
	if ids == nil {
		return []uuid.UUID{}
	}
	return ids
}
