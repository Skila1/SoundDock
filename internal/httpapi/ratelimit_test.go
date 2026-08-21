package httpapi

import (
	"testing"

	"github.com/sounddock/sounddock/internal/httpapi/ratelimit"
)

func TestRateLimitClassesIndependent(t *testing.T) {
	l := ratelimit.New()
	key := "1.2.3.4"
	for i := 0; i < 20; i++ {
		if !l.Allow(ratelimit.ClassAuth, key) && i < 20 {
			// may trip at 20
		}
	}
	if !l.Allow(ratelimit.ClassSearch, key) {
		t.Fatal("search should not share auth bucket")
	}
	if !l.Allow(ratelimit.ClassStreamSlot, key) {
		t.Fatal("stream class must not 429 via Allow")
	}
}
