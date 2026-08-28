package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sounddock/sounddock/internal/auth"
)

func TestRejectIfDisabled(t *testing.T) {
	rec := httptest.NewRecorder()
	if rejectIfDisabled(rec, nil) {
		t.Fatal("nil user")
	}
	if rec.Code != 200 {
		t.Fatalf("code %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	if !rejectIfDisabled(rec, &auth.User{Disabled: true}) {
		t.Fatal("disabled")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	if rejectIfDisabled(rec, &auth.User{Disabled: false}) {
		t.Fatal("enabled")
	}
}
