package diag

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbeFailIsFAIL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	c := ProbeHTTP(ctx, client, "http://127.0.0.1:1/health")
	if c.Status != Fail {
		t.Fatalf("status=%s detail=%s", c.Status, c.Detail)
	}
}

func TestProbeHTTPPass(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := ProbeHTTP(context.Background(), srv.Client(), srv.URL+"/health")
	if c.Status != Pass {
		t.Fatalf("status=%s detail=%s", c.Status, c.Detail)
	}
}

func TestRedisUnconfiguredIsSkip(t *testing.T) {
	c := probeRedis(context.Background(), "")
	if c.Status != Skip {
		t.Fatalf("status=%s", c.Status)
	}
}

func TestConfigExistsIsNotOK(t *testing.T) {
	c := probeMeili(context.Background(), &http.Client{Timeout: 400 * time.Millisecond}, "http://127.0.0.1:1", "")
	if c.Status != Fail {
		t.Fatalf("configured-but-down meili must be FAIL, got %s", c.Status)
	}
}
