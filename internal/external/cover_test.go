package external

import (
	"strings"
	"testing"
)

func TestCoverURLFillsAppleTemplate(t *testing.T) {
	got := coverURL("https://is1-ssl.mzstatic.com/image/thumb/abc/{w}x{h}bb.{f}")
	if got != "https://is1-ssl.mzstatic.com/image/thumb/abc/1000x1000bb.jpg" {
		t.Fatalf("got %q", got)
	}
	if coverURL("  ") != "" {
		t.Fatal("blank url should stay blank")
	}
	plain := "https://i.scdn.co/image/ab67616d0000b273"
	if coverURL(plain) != plain {
		t.Fatal("plain url changed")
	}
}

func TestCopyImportHasOwnCoalesceKey(t *testing.T) {
	p := ImportPayload{Provider: "spotify", ExternalID: "x"}
	c := p
	c.Copy = true
	if p.CoalesceKey() == c.CoalesceKey() || !strings.HasSuffix(c.CoalesceKey(), "|copy") {
		t.Fatalf("keys %q %q", p.CoalesceKey(), c.CoalesceKey())
	}
}
