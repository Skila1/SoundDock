package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteErrSanitizes500(t *testing.T) {
	rec := httptest.NewRecorder()
	writeErr(rec, 500, "db", "pq: password authentication failed")
	if rec.Code != 500 {
		t.Fatalf("code %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	msg, _ := body["message"].(string)
	if msg != "An internal error occurred" {
		t.Fatalf("message %q", msg)
	}
}

func TestWriteErrKeeps400(t *testing.T) {
	rec := httptest.NewRecorder()
	writeErr(rec, 400, "invalid", "pq: not leaked on 400")
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["message"] != "pq: not leaked on 400" {
		t.Fatalf("%v", body)
	}
}

func TestSanitizeInternal(t *testing.T) {
	if sanitizeInternal("ok") != "ok" {
		t.Fatal("ok")
	}
	if sanitizeInternal("pq: syntax") != "An internal error occurred" {
		t.Fatal("pq")
	}
	_ = http.StatusInternalServerError
}
