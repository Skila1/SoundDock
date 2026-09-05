package radio

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestClampLimit(t *testing.T) {
	if ClampLimit(0) != 20 {
		t.Fatal("default")
	}
	if ClampLimit(-3) != 20 {
		t.Fatal("negative")
	}
	if ClampLimit(7) != 7 {
		t.Fatal("passthrough")
	}
	if ClampLimit(500) != 100 {
		t.Fatal("cap")
	}
}

func TestClampFill(t *testing.T) {
	if ClampFill(0) != 6 {
		t.Fatal("default")
	}
	if ClampFill(1) != 1 {
		t.Fatal("min")
	}
	if ClampFill(10) != 10 {
		t.Fatal("max")
	}
	if ClampFill(50) != 10 {
		t.Fatal("cap")
	}
}

func TestSimilarQuery(t *testing.T) {
	if SimilarQuery("Rock, Metal", nil) != "Rock Metal mix" {
		t.Fatal(SimilarQuery("Rock, Metal", nil))
	}
	if SimilarQuery("Pop", []string{"synth"}) != "Pop synth mix" {
		t.Fatal(SimilarQuery("Pop", []string{"synth"}))
	}
	if SimilarQuery("", nil) != "" {
		t.Fatal("empty must not fall back to a title")
	}
	if SimilarQuery("  ", []string{"", " "}) != "" {
		t.Fatal("whitespace only")
	}
}

func TestGenreTokensDedupe(t *testing.T) {
	got := GenreTokens("Rock, rock", []string{"Metal", "ROCK"})
	if len(got) != 2 || strings.ToLower(got[0]) != "rock" || strings.ToLower(got[1]) != "metal" {
		t.Fatalf("%v", got)
	}
}

func TestSameSong(t *testing.T) {
	if !SameSong("Numb", "numb") {
		t.Fatal("case")
	}
	if !SameSong("Major Tom (Coming Home)", "Major Tom") {
		t.Fatal("parens")
	}
	if !SameSong("Song (ft. Nateman)", "Song") {
		t.Fatal("feat")
	}
	if SameSong("Numb", "In The End") {
		t.Fatal("different")
	}
}

func TestClampRecent(t *testing.T) {
	if ClampRecent(0) != 40 {
		t.Fatal("default")
	}
	if ClampRecent(500) != 200 {
		t.Fatal("cap")
	}
}

func TestValidKind(t *testing.T) {
	for _, k := range Kinds {
		if !ValidKind(k) {
			t.Fatalf("kind %s", k)
		}
	}
	if ValidKind("podcast") {
		t.Fatal("unknown kind")
	}
}

func TestDecadeStart(t *testing.T) {
	if DecadeStart(1994) != 1990 {
		t.Fatal(DecadeStart(1994))
	}
	if ParseDecade("2000s") != 2000 {
		t.Fatal(ParseDecade("2000s"))
	}
	if ParseDecade("nope") != 0 {
		t.Fatal("invalid")
	}
}

func TestUniqueAppend(t *testing.T) {
	a := uuid.MustParse("00000000-0000-4000-8000-000000000050")
	b := uuid.MustParse("00000000-0000-4000-8000-000000000051")
	got := uniqueAppend([]uuid.UUID{a}, []uuid.UUID{a, b}, 2)
	if len(got) != 2 || got[1] != b {
		t.Fatalf("%v", got)
	}
}

func TestInviteRoundTrip(t *testing.T) {
	key := []byte("test-sign-key")
	id := uuid.MustParse("00000000-0000-4000-8000-000000000090")
	tok, exp, err := SignInvite(key, id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if exp.IsZero() {
		t.Fatal("expiry")
	}
	got, err := VerifyInvite(key, tok)
	if err != nil || got != id {
		t.Fatalf("got %s err %v", got, err)
	}
	if _, err := VerifyInvite(key, tok+"x"); err == nil {
		t.Fatal("tamper")
	}
	if _, err := VerifyInvite([]byte("other"), tok); err == nil {
		t.Fatal("wrong key")
	}
}

func TestParseRulesDefaults(t *testing.T) {
	r, err := ParseRules([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if r.Match != "all" || r.Sort != "random" || r.Limit != 50 {
		t.Fatalf("%+v", r)
	}
}

func TestBuildSmartSQL(t *testing.T) {
	owner := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	lib := uuid.MustParse("00000000-0000-4000-8000-000000000020")
	sql, args, err := buildSmartSQL(owner, []uuid.UUID{lib}, Rules{
		Limit: 10,
		Match: "all",
		Sort:  "title",
		Clauses: []Clause{
			{Field: "genre", Op: "eq", Value: "Rock"},
			{Field: "year", Op: "gte", Value: 1990},
			{Field: "year", Op: "lt", Value: 2000},
			{Field: "favourite", Op: "eq", Value: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "genre_text ILIKE") {
		t.Fatal(sql)
	}
	if !strings.Contains(sql, "t.year >=") {
		t.Fatal(sql)
	}
	if !strings.Contains(sql, "favourites") {
		t.Fatal(sql)
	}
	if !strings.Contains(sql, "ORDER BY t.title") {
		t.Fatal(sql)
	}
	if len(args) < 4 {
		t.Fatalf("args %d", len(args))
	}
}

func TestBuildSmartRejectsUnknownField(t *testing.T) {
	_, _, err := buildSmartSQL(uuid.Nil, nil, Rules{Clauses: []Clause{{Field: "drop", Op: "eq", Value: "x"}}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRefreshPayloadShape(t *testing.T) {
	var p RefreshPayload
	if err := json.Unmarshal([]byte(`{"kind":"artist","seed_id":"00000000-0000-4000-8000-000000000030","limit":50}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.Kind != "artist" || p.Limit != 50 {
		t.Fatalf("%+v", p)
	}
	var s SmartPayload
	if err := json.Unmarshal([]byte(`{"playlist_id":"00000000-0000-4000-8000-000000000090"}`), &s); err != nil {
		t.Fatal(err)
	}
	if s.PlaylistID == uuid.Nil {
		t.Fatal("playlist_id")
	}
}

func TestResultJSONContract(t *testing.T) {
	seed := uuid.MustParse("00000000-0000-4000-8000-000000000030")
	tid := uuid.MustParse("00000000-0000-4000-8000-000000000050")
	b, err := json.Marshal(Result{Kind: "artist", SeedID: seed, TrackIDs: []uuid.UUID{tid}})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"kind", "seed_id", "track_ids"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("missing %s", k)
		}
	}
	if m["kind"] != "artist" {
		t.Fatal(m["kind"])
	}
}

func TestSelectUnknownKind(t *testing.T) {
	svc := &Service{}
	_, err := svc.Select(nil, Request{Kind: "podcast"})
	if err != ErrUnknownKind {
		t.Fatalf("err %v", err)
	}
}
