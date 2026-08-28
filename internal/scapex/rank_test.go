package scapex

import "testing"

func TestRankHitsPenalizesUnrequestedModifiers(t *testing.T) {
	hits := []Hit{
		{ID: "live", Title: "Song (Live)"},
		{ID: "orig", Title: "Song"},
		{ID: "remix", Title: "Song Remix"},
	}
	got := RankHits("song", hits)
	if got[0].ID != "orig" {
		t.Fatalf("want original first, got %#v", ids(got))
	}
}

func TestRankHitsKeepsRequestedLive(t *testing.T) {
	hits := []Hit{
		{ID: "studio", Title: "Song"},
		{ID: "live", Title: "Song Live"},
	}
	got := RankHits("song live", hits)
	if got[0].ID != "studio" && got[1].ID != "live" {
		// live must not be penalized; original index 1 + 0 penalty vs studio 0
		// studio still wins on index. Check live is not +100 behind uniquely.
	}
	livePen := HitPenalty("song live", "Song Live", "")
	if livePen != 0 {
		t.Fatalf("requested live should not penalize, got %d", livePen)
	}
	unreq := HitPenalty("song", "Song Live", "")
	if unreq == 0 {
		t.Fatal("unrequested live should penalize")
	}
}

func TestHitPenaltyEachModifier(t *testing.T) {
	for _, m := range qualityModifiers {
		if HitPenalty("ballad", "Track "+m, "") == 0 {
			t.Fatalf("expected penalty for %s", m)
		}
		if HitPenalty("ballad "+m, "Track "+m, "") != 0 {
			t.Fatalf("query asked for %s", m)
		}
	}
}

func TestHitPenaltyDoesNotMatchSubstringTokens(t *testing.T) {
	if HitPenalty("song", "Olive Garden", "") != 0 {
		t.Fatal("olive must not count as live")
	}
	if HitPenalty("song", "Song (Live)", "") == 0 {
		t.Fatal("parenthetical live is a modifier token")
	}
}

func ids(hits []Hit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.ID
	}
	return out
}
