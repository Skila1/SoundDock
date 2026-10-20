// Package dupes finds likely duplicate songs by title. Results are a
// throwaway snapshot kept in a JSON file in the data folder: a scan replaces
// it, and nothing is written to the database.
package dupes

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/text/unicode/norm"
)

type Track struct {
	ID         uuid.UUID `json:"id"`
	Title      string    `json:"title"`
	Artist     string    `json:"artist"`
	Album      string    `json:"album"`
	DurationMS int       `json:"duration_ms"`
	Codec      string    `json:"codec"`
	Bitrate    int       `json:"bitrate"`
	SizeBytes  int64     `json:"size_bytes"`
	Hash       string    `json:"-"`
	Plays      int       `json:"plays"`
	CreatedAt  time.Time `json:"created_at"`
}

type Group struct {
	Key     string   `json:"key"`
	Title   string   `json:"title"`
	Artist  string   `json:"artist"`
	Score   int      `json:"score"`
	Reasons []string `json:"reasons"`
	Tracks  []Track  `json:"tracks"`
	BestID  string   `json:"best_id"`
}

type Result struct {
	ScannedAt time.Time `json:"scanned_at"`
	Tracks    int       `json:"tracks"`
	Groups    []Group   `json:"groups"`
}

type Store struct {
	dir string
	mu  sync.Mutex
}

func NewStore(dataDir string) *Store { return &Store{dir: dataDir} }

func (s *Store) resultPath() string  { return filepath.Join(s.dir, "duplicates.json") }
func (s *Store) ignoredPath() string { return filepath.Join(s.dir, "duplicates-ignored.json") }

// Load returns the last scan, minus ignored groups and tracks that no longer
// exist. A missing file is an empty result.
func (s *Store) Load(ctx context.Context, pool *pgxpool.Pool) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var res Result
	b, err := os.ReadFile(s.resultPath())
	if errors.Is(err, os.ErrNotExist) {
		return Result{Groups: []Group{}}, nil
	}
	if err != nil {
		return res, err
	}
	if err := json.Unmarshal(b, &res); err != nil {
		return Result{Groups: []Group{}}, nil
	}
	alive := map[uuid.UUID]bool{}
	var ids []uuid.UUID
	for _, g := range res.Groups {
		for _, t := range g.Tracks {
			ids = append(ids, t.ID)
		}
	}
	if pool != nil && len(ids) > 0 {
		rows, err := pool.Query(ctx, `SELECT id FROM tracks WHERE id = ANY($1)`, ids)
		if err == nil {
			for rows.Next() {
				var id uuid.UUID
				if rows.Scan(&id) == nil {
					alive[id] = true
				}
			}
			rows.Close()
		}
	}
	ignored := s.ignored()
	out := make([]Group, 0, len(res.Groups))
	for _, g := range res.Groups {
		if ignored[g.Key] {
			continue
		}
		var keep []Track
		for _, t := range g.Tracks {
			if pool == nil || alive[t.ID] {
				keep = append(keep, t)
			}
		}
		if len(keep) < 2 {
			continue
		}
		g.Tracks = keep
		g.Key = groupKey(keep)
		g.BestID = best(keep).String()
		out = append(out, g)
	}
	res.Groups = out
	return res, nil
}

func (s *Store) Save(res Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(s.resultPath(), res)
}

// Ignore remembers a set of tracks as "not duplicates" so rescans skip it.
func (s *Store) Ignore(ids []uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ig := s.ignored()
	ig[keyOf(ids)] = true
	list := make([]string, 0, len(ig))
	for k := range ig {
		list = append(list, k)
	}
	sort.Strings(list)
	return writeJSON(s.ignoredPath(), list)
}

func (s *Store) ignored() map[string]bool {
	out := map[string]bool{}
	b, err := os.ReadFile(s.ignoredPath())
	if err != nil {
		return out
	}
	var list []string
	_ = json.Unmarshal(b, &list)
	for _, k := range list {
		out[k] = true
	}
	return out
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Scan reads every track (optionally one library) and groups likely
// duplicates. It only reads the database.
func Scan(ctx context.Context, pool *pgxpool.Pool, libraryID *uuid.UUID) (Result, error) {
	rows, err := pool.Query(ctx, `
		SELECT t.id, t.title,
		       coalesce((SELECT string_agg(ar.name, ', ' ORDER BY ta.position) FROM track_artists ta
		                 JOIN artists ar ON ar.id=ta.artist_id
		                 WHERE ta.track_id=t.id AND ta.role IN ('primary','featured')), ''),
		       coalesce(al.title, ''), coalesce(t.duration_ms, 0),
		       coalesce(f.codec, ''), coalesce(f.bitrate, 0), coalesce(f.size_bytes, 0), coalesce(f.content_hash, ''),
		       coalesce((SELECT count(*) FROM listen_events le WHERE le.track_id=t.id), 0),
		       t.created_at
		FROM tracks t
		LEFT JOIN albums al ON al.id=t.album_id
		LEFT JOIN LATERAL (
		  SELECT codec, bitrate, size_bytes, content_hash FROM track_files tf
		  WHERE tf.track_id=t.id AND tf.quality='original' ORDER BY tf.size_bytes DESC LIMIT 1
		) f ON true
		WHERE ($1::uuid IS NULL OR t.library_id=$1)`, libraryID)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	var tracks []Track
	for rows.Next() {
		var t Track
		if err := rows.Scan(&t.ID, &t.Title, &t.Artist, &t.Album, &t.DurationMS, &t.Codec, &t.Bitrate, &t.SizeBytes, &t.Hash, &t.Plays, &t.CreatedAt); err != nil {
			return Result{}, err
		}
		tracks = append(tracks, t)
	}
	if err := rows.Err(); err != nil {
		return Result{}, err
	}
	return Result{ScannedAt: time.Now().UTC(), Tracks: len(tracks), Groups: Find(tracks)}, nil
}

// Find groups tracks whose cleaned title words match, after working out
// which side of "A - B" is the artist.
func Find(tracks []Track) []Group {
	known := map[string]bool{}
	for _, t := range tracks {
		for _, a := range splitArtists(t.Artist) {
			if k := strings.Join(Words(a), " "); k != "" {
				known[k] = true
			}
		}
	}
	buckets := map[string][]int{}
	parsed := make([]parsedTitle, len(tracks))
	for i, t := range tracks {
		p := parse(t, known)
		parsed[i] = p
		if p.key == "" {
			continue
		}
		buckets[p.key] = append(buckets[p.key], i)
	}
	// Exact file copies always group, even when titles differ.
	byHash := map[string][]int{}
	for i, t := range tracks {
		if t.Hash != "" {
			byHash[t.Hash] = append(byHash[t.Hash], i)
		}
	}
	seen := map[string]bool{}
	var groups []Group
	add := func(idx []int, sameHash bool) {
		if len(idx) < 2 {
			return
		}
		members := make([]Track, 0, len(idx))
		for _, i := range idx {
			members = append(members, tracks[i])
		}
		key := groupKey(members)
		if seen[key] {
			return
		}
		seen[key] = true
		score, reasons := scoreGroup(members, idx, parsed, sameHash)
		if score < 3 {
			return
		}
		g := Group{
			Key:     key,
			Title:   members[0].Title,
			Artist:  parsed[idx[0]].artist,
			Score:   score,
			Reasons: reasons,
			Tracks:  members,
			BestID:  best(members).String(),
		}
		if g.Artist == "" {
			g.Artist = members[0].Artist
		}
		groups = append(groups, g)
	}
	for _, idx := range byHash {
		add(idx, true)
	}
	for _, idx := range buckets {
		add(idx, false)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].Score != groups[j].Score {
			return groups[i].Score > groups[j].Score
		}
		return strings.ToLower(groups[i].Title) < strings.ToLower(groups[j].Title)
	})
	if groups == nil {
		groups = []Group{}
	}
	return groups
}

type parsedTitle struct {
	key    string // sorted title words
	artist string // artist words as found in the title or tags
}

func parse(t Track, known map[string]bool) parsedTitle {
	title := t.Title
	tagArtists := map[string]bool{}
	for _, a := range splitArtists(t.Artist) {
		if k := strings.Join(Words(a), " "); k != "" {
			tagArtists[k] = true
		}
	}
	artist := strings.Join(Words(firstArtist(t.Artist)), " ")
	if left, right, ok := splitDash(title); ok {
		l := strings.Join(Words(left), " ")
		r := strings.Join(Words(right), " ")
		switch {
		case tagArtists[l] || (known[l] && !known[r]):
			title, artist = right, l
		case tagArtists[r] || (known[r] && !known[l]):
			title, artist = left, r
		}
	}
	words := Words(stripExtras(title))
	// Drop the artist's words if they were glued onto the title.
	if artist != "" {
		aw := map[string]bool{}
		for _, w := range strings.Fields(artist) {
			aw[w] = true
		}
		var keep []string
		for _, w := range words {
			if !aw[w] {
				keep = append(keep, w)
			}
		}
		if len(keep) > 0 {
			words = keep
		}
	}
	sort.Strings(words)
	return parsedTitle{key: strings.Join(words, " "), artist: artist}
}

func scoreGroup(members []Track, idx []int, parsed []parsedTitle, sameHash bool) (int, []string) {
	if sameHash {
		return 10, []string{"Identical file"}
	}
	score := 6
	reasons := []string{"Same title words"}
	artists := map[string]bool{}
	for _, i := range idx {
		if a := parsed[i].artist; a != "" {
			artists[a] = true
		}
	}
	switch {
	case len(artists) <= 1:
		score += 2
		reasons = append(reasons, "Same artist")
	default:
		score -= 2
		reasons = append(reasons, "Different artists")
	}
	minD, maxD := 1<<31-1, 0
	for _, t := range members {
		if t.DurationMS <= 0 {
			continue
		}
		minD = min(minD, t.DurationMS)
		maxD = max(maxD, t.DurationMS)
	}
	if maxD > 0 {
		switch diff := maxD - minD; {
		case diff <= 3000:
			score += 2
			reasons = append(reasons, "Same length")
		case diff <= 10000:
			score++
			reasons = append(reasons, "Similar length")
		case diff > 30000:
			score -= 3
			reasons = append(reasons, "Very different length")
		}
	}
	return max(0, min(10, score)), reasons
}

// best picks the copy to keep: highest bitrate, then most plays, then oldest.
func best(ts []Track) uuid.UUID {
	b := ts[0]
	for _, t := range ts[1:] {
		switch {
		case t.Bitrate != b.Bitrate:
			if t.Bitrate > b.Bitrate {
				b = t
			}
		case t.Plays != b.Plays:
			if t.Plays > b.Plays {
				b = t
			}
		case t.CreatedAt.Before(b.CreatedAt):
			b = t
		}
	}
	return b.ID
}

func groupKey(ts []Track) string {
	ids := make([]uuid.UUID, len(ts))
	for i, t := range ts {
		ids[i] = t.ID
	}
	return keyOf(ids)
}

func keyOf(ids []uuid.UUID) string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = id.String()
	}
	sort.Strings(s)
	return strings.Join(s, ",")
}

func splitArtists(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '&' || r == ';' || r == '/' })
}

func firstArtist(s string) string {
	if a := splitArtists(s); len(a) > 0 {
		return a[0]
	}
	return ""
}

// splitDash splits "A - B" on the first spaced dash (also en/em dashes).
func splitDash(s string) (string, string, bool) {
	for _, sep := range []string{" - ", " " + string(rune(0x2013)) + " ", " " + string(rune(0x2014)) + " "} {
		if i := strings.Index(s, sep); i > 0 {
			return s[:i], s[i+len(sep):], true
		}
	}
	return "", "", false
}

var extraWords = map[string]bool{
	"official": true, "video": true, "audio": true, "lyrics": true, "lyric": true, "hd": true, "hq": true,
	"music": true, "visualizer": true, "visualiser": true, "remaster": true, "remastered": true, "version": true,
	"feat": true, "ft": true, "featuring": true, "explicit": true, "clean": true, "mv": true, "4k": true,
}

// stripExtras drops bracketed tags like "(Official Video)" and anything after
// feat./ft. so "Song (feat. X) [HD]" matches "Song".
func stripExtras(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '(', '[', '{':
			depth++
			continue
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth == 0 {
			b.WriteRune(r)
		}
	}
	out := " " + strings.ToLower(b.String()) + " "
	for _, m := range []string{" feat. ", " feat ", " ft. ", " ft ", " featuring "} {
		if i := strings.Index(out, m); i >= 0 {
			out = out[:i]
		}
	}
	return out
}

// Words lowercases, folds accents, turns "&" into "and", drops punctuation
// and filler words, and returns what is left.
func Words(s string) []string {
	s = strings.ReplaceAll(s, "&", " and ")
	var b strings.Builder
	for _, r := range norm.NFKD.String(strings.ToLower(s)) {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case r == '\'' || r == '’':
			continue // "don't" -> "dont"
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}
	var out []string
	for _, w := range strings.Fields(b.String()) {
		if !extraWords[w] {
			out = append(out, w)
		}
	}
	return out
}
