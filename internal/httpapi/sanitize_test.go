package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteInternalNoPQ(t *testing.T) {
	rec := httptest.NewRecorder()
	writeErr(rec, http.StatusInternalServerError, "db", "pq: password authentication failed for user \"sounddock\"")
	if rec.Code != 500 {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "pq:") {
		t.Fatalf("leaked pq: %s", body)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	msg, _ := out["message"].(string)
	if strings.Contains(strings.ToLower(msg), "password") {
		t.Fatalf("leaked password text: %s", msg)
	}
}

func TestWriteErrSanitizes502(t *testing.T) {
	rec := httptest.NewRecorder()
	writeErr(rec, http.StatusBadGateway, "db", "SQLSTATE 28P01")
	if strings.Contains(rec.Body.String(), "SQLSTATE") {
		t.Fatalf("leaked sqlstate: %s", rec.Body.String())
	}
}

func TestWriteErrKeeps400ClientMessage(t *testing.T) {
	rec := httptest.NewRecorder()
	writeErr(rec, 400, "invalid", "track_id required")
	if !strings.Contains(rec.Body.String(), "track_id required") {
		t.Fatalf("400 should keep the message: %s", rec.Body.String())
	}
}
