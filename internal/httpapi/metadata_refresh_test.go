package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
	"github.com/sounddock/sounddock/internal/scan"
)

func TestMetadataRefreshRoutesRegistered(t *testing.T) {
	h := (&Server{}).Router()
	for _, path := range []struct{ method, url string }{
		{http.MethodGet, "/api/v1/admin/metadata"},
		{http.MethodPut, "/api/v1/admin/metadata"},
		{http.MethodPost, "/api/v1/admin/metadata/refresh"},
	} {
		req := httptest.NewRequest(path.method, path.url, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			t.Fatalf("%s %s is not registered", path.method, path.url)
		}
	}
}

func TestAdminMetadataGetWithoutPool(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "admin", IsAdmin: true}
	req := authedJSON(u, http.MethodGet, "/api/v1/admin/metadata", nil)
	rec := httptest.NewRecorder()
	s.adminMetadata(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["busy"] != false {
		t.Fatalf("busy %v", body["busy"])
	}
	if body["track_count"] != float64(0) {
		t.Fatalf("track_count %v", body["track_count"])
	}
	providers, _ := body["providers"].([]any)
	if len(providers) != 2 {
		t.Fatalf("providers %v", body["providers"])
	}
}

func TestAdminRefreshMetadataRequiresJobs(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "admin", IsAdmin: true}
	req := authedJSON(u, http.MethodPost, "/api/v1/admin/metadata/refresh", nil)
	rec := httptest.NewRecorder()
	s.adminRefreshMetadata(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestMetadataRefreshJobType(t *testing.T) {
	if metadataRefreshJobType != scan.JobRefresh || scan.JobRefresh != "metadata.refresh" {
		t.Fatalf("%q", metadataRefreshJobType)
	}
}
