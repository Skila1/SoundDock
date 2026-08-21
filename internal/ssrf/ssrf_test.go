package ssrf

import (
	"net"
	"testing"
)

func TestIPBlocked(t *testing.T) {
	blocked := []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "172.16.0.5", "169.254.169.254", "::1", "fc00::1", "fe80::1", "0.0.0.0"}
	for _, s := range blocked {
		if !ipBlocked(net.ParseIP(s)) {
			t.Fatalf("expected blocked %s", s)
		}
	}
	if ipBlocked(net.ParseIP("8.8.8.8")) {
		t.Fatal("8.8.8.8 should be allowed")
	}
}

func TestHostAllowed(t *testing.T) {
	if err := hostAllowed("localhost", nil, nil); err == nil {
		t.Fatal("localhost")
	}
	if err := hostAllowed("metadata.google.internal", nil, nil); err == nil {
		t.Fatal("metadata")
	}
	if err := hostAllowed("evil.example", nil, []string{"example"}); err == nil {
		t.Fatal("blocklist")
	}
	if err := hostAllowed("files.example.com", []string{"example.com"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := hostAllowed("files.other.com", []string{"example.com"}, nil); err == nil {
		t.Fatal("allowlist")
	}
}
