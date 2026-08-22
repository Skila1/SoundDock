package httpapi

import "testing"

func TestPATPrefix(t *testing.T) {
	if got := patPrefix("sdp_abcdefghijk"); got != "sdp_abcdef" {
		t.Fatalf("got %q", got)
	}
	if got := patPrefix("short"); got != "short" {
		t.Fatalf("got %q", got)
	}
	if got := patPrefix("exactlyten"); got != "exactlyten" {
		t.Fatalf("got %q", got)
	}
}

func TestPATTokenPrefix(t *testing.T) {
	if patTokenPrefix != "sdp_" {
		t.Fatalf("unexpected prefix %q", patTokenPrefix)
	}
}

func TestNormalizeAPIKeyScopes(t *testing.T) {
	got, err := normalizeAPIKeyScopes([]string{" admin ", "tracks.read", "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "admin" || got[1] != "tracks.read" {
		t.Fatalf("%v", got)
	}
	if _, err := normalizeAPIKeyScopes(nil); err == nil {
		t.Fatal("empty scopes should fail")
	}
	if _, err := normalizeAPIKeyScopes([]string{"read", "stream"}); err == nil {
		t.Fatal("legacy read/stream scopes should fail")
	}
}

func TestIsAPIToken(t *testing.T) {
	if !isAPIToken("sdp_secret") {
		t.Fatal("personal access tokens must authenticate as API keys")
	}
	if !isAPIToken("sd_secret") {
		t.Fatal("integration keys must authenticate as API keys")
	}
	if isAPIToken("session-cookie-value") {
		t.Fatal("session values are not API keys")
	}
}
