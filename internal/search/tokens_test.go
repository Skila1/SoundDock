package search

import (
	"reflect"
	"testing"
)

func TestSignificantTokensDropsArticles(t *testing.T) {
	got := SignificantTokens("dominga la mave")
	want := []string{"dominga", "mave"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestSignificantTokensKeepsShortWhenAlone(t *testing.T) {
	got := SignificantTokens("up")
	if len(got) != 1 || got[0] != "up" {
		t.Fatalf("got %v", got)
	}
}

func TestSignificantTokensMajorTom(t *testing.T) {
	got := SignificantTokens("Major Tom")
	want := []string{"major", "tom"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestLikeContainsEscapes(t *testing.T) {
	if likeContains(`100%_win`) != `%100\%\_win%` {
		t.Fatalf("%q", likeContains(`100%_win`))
	}
}
