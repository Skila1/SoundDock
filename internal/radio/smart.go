package radio

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

type Rules struct {
	Limit   int      `json:"limit"`
	Match   string   `json:"match"`
	Sort    string   `json:"sort"`
	Clauses []Clause `json:"clauses"`
}

type Clause struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

func ParseRules(raw []byte) (Rules, error) {
	var r Rules
	if len(raw) == 0 {
		return Rules{Match: "all", Sort: "random", Limit: 50}, nil
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return Rules{}, err
	}
	if r.Match == "" {
		r.Match = "all"
	}
	if r.Sort == "" {
		r.Sort = "random"
	}
	if r.Limit <= 0 {
		r.Limit = 50
	}
	if r.Limit > 500 {
		r.Limit = 500
	}
	return r, nil
}

func (s *Service) RefreshSmart(ctx context.Context, playlistID uuid.UUID) error {
	var owner uuid.UUID
	var raw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT p.user_id, r.rules
		FROM smart_playlist_rules r
		JOIN playlists p ON p.id=r.playlist_id
		WHERE r.playlist_id=$1`, playlistID).Scan(&owner, &raw)
	if err != nil {
		return err
	}
	rules, err := ParseRules(raw)
	if err != nil {
		return err
	}
	libs := s.userLibraries(ctx, owner)
	ids, err := s.evalSmart(ctx, owner, libs, rules)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM playlist_entries WHERE playlist_id=$1`, playlistID); err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := tx.Exec(ctx, `INSERT INTO playlist_entries (playlist_id, track_id, position, added_by) VALUES ($1,$2,$3,$4)`, playlistID, id, i, owner); err != nil {
			return err
		}
	}
	_, _ = tx.Exec(ctx, `UPDATE playlists SET updated_at=now() WHERE id=$1`, playlistID)
	_, _ = tx.Exec(ctx, `UPDATE smart_playlist_rules SET updated_at=now() WHERE playlist_id=$1`, playlistID)
	return tx.Commit(ctx)
}

func (s *Service) evalSmart(ctx context.Context, owner uuid.UUID, libs []uuid.UUID, rules Rules) ([]uuid.UUID, error) {
	sql, args, err := buildSmartSQL(owner, libs, rules)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return idsOrEmpty(ids), rows.Err()
}

func buildSmartSQL(owner uuid.UUID, libs []uuid.UUID, rules Rules) (string, []any, error) {
	args := []any{idsOrDummy(libs), owner}
	var parts []string
	for _, c := range rules.Clauses {
		frag, extra, err := clauseSQL(c, len(args)+1)
		if err != nil {
			return "", nil, err
		}
		if frag == "" {
			continue
		}
		parts = append(parts, frag)
		args = append(args, extra...)
	}
	where := "t.library_id = ANY($1)"
	if len(parts) > 0 {
		join := " AND "
		if strings.EqualFold(rules.Match, "any") {
			join = " OR "
		}
		where += " AND (" + strings.Join(parts, join) + ")"
	}
	order := "random()"
	switch strings.ToLower(rules.Sort) {
	case "recent":
		order = "t.created_at DESC"
	case "title":
		order = "t.title"
	case "year":
		order = "t.year DESC NULLS LAST"
	case "most_played":
		order = `(SELECT coalesce(pc.count,0) FROM play_counts pc WHERE pc.user_id=$2 AND pc.track_id=t.id) DESC, random()`
	}
	limit := rules.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit)
	sql := fmt.Sprintf(`
		SELECT t.id FROM tracks t
		WHERE %s
		ORDER BY %s
		LIMIT $%d`, where, order, len(args))
	return sql, args, nil
}

func clauseSQL(c Clause, arg int) (string, []any, error) {
	field := strings.ToLower(strings.TrimSpace(c.Field))
	op := strings.ToLower(strings.TrimSpace(c.Op))
	if op == "" {
		op = "eq"
	}
	sqlOp, ok := sqlOps[op]
	if !ok && field != "never_played" && field != "favourite" && field != "favorited" {
		return "", nil, fmt.Errorf("unsupported op %q", c.Op)
	}
	switch field {
	case "genre":
		v := stringify(c.Value)
		if id, err := uuid.Parse(v); err == nil {
			return fmt.Sprintf(`EXISTS (SELECT 1 FROM track_genres tg WHERE tg.track_id=t.id AND tg.genre_id=$%d)`, arg), []any{id}, nil
		}
		return fmt.Sprintf(`(t.genre_text ILIKE $%d OR EXISTS (
			SELECT 1 FROM track_genres tg JOIN genres g ON g.id=tg.genre_id
			WHERE tg.track_id=t.id AND g.name ILIKE $%d))`, arg, arg), []any{v}, nil
	case "year":
		n, err := asInt(c.Value)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf(`t.year %s $%d`, sqlOp, arg), []any{n}, nil
	case "decade":
		n, err := asInt(c.Value)
		if err != nil {
			return "", nil, err
		}
		d := DecadeStart(n)
		return fmt.Sprintf(`(t.year >= $%d AND t.year < $%d)`, arg, arg+1), []any{d, d + 10}, nil
	case "artist":
		v := stringify(c.Value)
		if id, err := uuid.Parse(v); err == nil {
			return fmt.Sprintf(`EXISTS (SELECT 1 FROM track_artists ta WHERE ta.track_id=t.id AND ta.artist_id=$%d)`, arg), []any{id}, nil
		}
		pat := v
		if op == "contains" || !strings.ContainsAny(v, "%") {
			pat = "%" + v + "%"
		}
		return fmt.Sprintf(`EXISTS (
			SELECT 1 FROM track_artists ta JOIN artists a ON a.id=ta.artist_id
			WHERE ta.track_id=t.id AND a.name ILIKE $%d)`, arg), []any{pat}, nil
	case "album":
		v := stringify(c.Value)
		if id, err := uuid.Parse(v); err == nil {
			return fmt.Sprintf(`t.album_id=$%d`, arg), []any{id}, nil
		}
		return fmt.Sprintf(`EXISTS (SELECT 1 FROM albums al WHERE al.id=t.album_id AND al.title ILIKE $%d)`, arg), []any{"%" + v + "%"}, nil
	case "library":
		id, err := uuid.Parse(stringify(c.Value))
		if err != nil {
			return "", nil, fmt.Errorf("library value must be a uuid")
		}
		return fmt.Sprintf(`t.library_id=$%d`, arg), []any{id}, nil
	case "title":
		v := stringify(c.Value)
		if op == "eq" {
			return fmt.Sprintf(`t.title ILIKE $%d`, arg), []any{v}, nil
		}
		return fmt.Sprintf(`t.title ILIKE $%d`, arg), []any{"%" + v + "%"}, nil
	case "play_count", "played":
		n, err := asInt(c.Value)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf(`coalesce((SELECT pc.count FROM play_counts pc WHERE pc.user_id=$2 AND pc.track_id=t.id),0) %s $%d`, sqlOp, arg), []any{n}, nil
	case "never_played":
		on, _ := asBool(c.Value)
		if on {
			return `NOT EXISTS (SELECT 1 FROM play_counts pc WHERE pc.user_id=$2 AND pc.track_id=t.id AND pc.count>0)`, nil, nil
		}
		return `EXISTS (SELECT 1 FROM play_counts pc WHERE pc.user_id=$2 AND pc.track_id=t.id AND pc.count>0)`, nil, nil
	case "favourite", "favorite", "favorited":
		on, _ := asBool(c.Value)
		if !on {
			return `NOT EXISTS (SELECT 1 FROM favourites f WHERE f.user_id=$2 AND f.entity_type='track' AND f.entity_id=t.id)`, nil, nil
		}
		return `EXISTS (SELECT 1 FROM favourites f WHERE f.user_id=$2 AND f.entity_type='track' AND f.entity_id=t.id)`, nil, nil
	default:
		return "", nil, fmt.Errorf("unsupported field %q", c.Field)
	}
}

var sqlOps = map[string]string{
	"eq": "=", "neq": "<>", "gt": ">", "gte": ">=", "lt": "<", "lte": "<=", "contains": "ILIKE",
}

func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		return fmt.Sprint(t)
	}
}

func asInt(v any) (int, error) {
	switch t := v.(type) {
	case int:
		return t, nil
	case int32:
		return int(t), nil
	case int64:
		return int(t), nil
	case float64:
		return int(t), nil
	case json.Number:
		n, err := t.Int64()
		return int(n), err
	case string:
		return strconv.Atoi(strings.TrimSpace(t))
	default:
		return 0, fmt.Errorf("not an integer")
	}
}

func asBool(v any) (bool, error) {
	switch t := v.(type) {
	case bool:
		return t, nil
	case string:
		return strconv.ParseBool(t)
	case float64:
		return t != 0, nil
	default:
		return true, nil
	}
}

// SnapshotEntries is the JSON stored in playlist_snapshots.entries.
type SnapshotEntry struct {
	EntryID  uuid.UUID `json:"entry_id"`
	TrackID  uuid.UUID `json:"track_id"`
	Position int       `json:"position"`
}

func (s *Service) CaptureSnapshot(ctx context.Context, playlistID, userID uuid.UUID) (uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, track_id, position FROM playlist_entries WHERE playlist_id=$1 ORDER BY position`, playlistID)
	if err != nil {
		return uuid.Nil, err
	}
	defer rows.Close()
	var entries []SnapshotEntry
	for rows.Next() {
		var e SnapshotEntry
		if err := rows.Scan(&e.EntryID, &e.TrackID, &e.Position); err != nil {
			return uuid.Nil, err
		}
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []SnapshotEntry{}
	}
	b, _ := json.Marshal(entries)
	var id uuid.UUID
	err = s.pool.QueryRow(ctx, `
		INSERT INTO playlist_snapshots (playlist_id, created_by, entries) VALUES ($1,$2,$3::jsonb) RETURNING id`,
		playlistID, userID, b).Scan(&id)
	return id, err
}

func (s *Service) RestoreSnapshot(ctx context.Context, playlistID, snapshotID, userID uuid.UUID) error {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT entries FROM playlist_snapshots WHERE id=$1 AND playlist_id=$2`, snapshotID, playlistID).Scan(&raw)
	if err != nil {
		return err
	}
	var entries []SnapshotEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM playlist_entries WHERE playlist_id=$1`, playlistID); err != nil {
		return err
	}
	for i, e := range entries {
		pos := e.Position
		if pos < 0 {
			pos = i
		}
		if _, err := tx.Exec(ctx, `INSERT INTO playlist_entries (playlist_id, track_id, position, added_by) VALUES ($1,$2,$3,$4)`, playlistID, e.TrackID, pos, userID); err != nil {
			return err
		}
	}
	_, _ = tx.Exec(ctx, `UPDATE playlists SET updated_at=now() WHERE id=$1`, playlistID)
	return tx.Commit(ctx)
}
