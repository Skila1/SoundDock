package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestIsLANAddrFailClosed(t *testing.T) {
	lan := []string{
		"127.0.0.1:1234",
		"127.0.0.1",
		"[::1]:8080",
		"::1",
		"10.0.0.8:9",
		"192.168.1.50",
		"172.16.9.1:443",
		"fe80::1",
	}
	for _, a := range lan {
		if !IsLANAddr(a) {
			t.Fatalf("expected LAN %q", a)
		}
	}
	remote := []string{
		"8.8.8.8:443",
		"8.8.8.8",
		"1.1.1.1",
		"",
		"not-an-ip",
		"100.64.0.1:80",
	}
	for _, a := range remote {
		if IsLANAddr(a) {
			t.Fatalf("expected remote (fail closed) %q", a)
		}
	}
}

func TestCapStreamQualityRemoteDefault(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest(http.MethodGet, "/stream", nil)
	r.RemoteAddr = "8.8.8.8:443"
	if g := s.CapStreamQuality(r, "original"); g != "medium" {
		t.Fatalf("remote original cap: %s", g)
	}
	if g := s.CapStreamQuality(r, "low"); g != "low" {
		t.Fatalf("remote low: %s", g)
	}
	r.RemoteAddr = "192.168.0.10:9"
	if g := s.CapStreamQuality(r, "original"); g != "original" {
		t.Fatalf("LAN original: %s", g)
	}
}

func TestOfflineTokenRoundTripAndRevokeIndependence(t *testing.T) {
	s := &Server{SignKey: []byte("test-sign-key-32-bytes-long!!!!")}
	uid := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	tid := uuid.MustParse("00000000-0000-4000-8000-000000000050")
	issued := time.Now()
	tok, err := s.signOffline(uid, "browser-1", tid, issued, issued.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if tok[:8] != "offline." {
		t.Fatalf("prefix %s", tok)
	}
	got, err := s.VerifyOfflineToken(t.Context(), tok)
	if err != nil {
		t.Fatal(err)
	}
	if got.TrackID != tid || got.UserID != uid || got.DeviceID != "browser-1" {
		t.Fatalf("%+v", got)
	}
}

func TestAcquireStreamSlotRemoteLimit(t *testing.T) {
	policySlotMu.Lock()
	policySlotCur = map[string]int{}
	policySlotMu.Unlock()
	s := &Server{}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "9.9.9.9:1"
	for i := 0; i < DefaultRemoteConcurrency; i++ {
		if !s.AcquireStreamSlot(r) {
			t.Fatalf("acquire %d", i)
		}
	}
	if s.AcquireStreamSlot(r) {
		t.Fatal("remote should 429 after cap")
	}
	s.ReleaseStreamSlot(r)
	if !s.AcquireStreamSlot(r) {
		t.Fatal("after release")
	}
	lan := httptest.NewRequest(http.MethodGet, "/", nil)
	lan.RemoteAddr = "10.0.0.2:1"
	if !s.AcquireStreamSlot(lan) {
		t.Fatal("LAN unlimited")
	}
}
