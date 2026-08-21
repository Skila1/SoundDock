package matcher

import (
	"testing"

	"github.com/google/uuid"
)

func TestAutoAcceptExactHighOnly(t *testing.T) {
	id := uuid.New()
	cases := []struct {
		r    Result
		want bool
	}{
		{Result{TrackID: &id, Status: "exact"}, true},
		{Result{TrackID: &id, Status: "high"}, true},
		{Result{TrackID: &id, Status: "possible"}, false},
		{Result{Status: "ambiguous"}, false},
		{Result{Status: "unmatched"}, false},
	}
	for _, c := range cases {
		got := c.r.TrackID != nil && (c.r.Status == "exact" || c.r.Status == "high")
		if got != c.want {
			t.Fatalf("%+v got %v", c.r, got)
		}
	}
}
