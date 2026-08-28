package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/auth"
)

func TestAcquisitionPolicyRoutesRegistered(t *testing.T) {
	h := (&Server{}).Router()
	for _, path := range []struct{ method, url string }{
		{http.MethodGet, "/api/v1/admin/acquisition-policy"},
		{http.MethodPut, "/api/v1/admin/acquisition-policy"},
	} {
		req := httptest.NewRequest(path.method, path.url, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			t.Fatalf("%s %s is not registered", path.method, path.url)
		}
	}
}

func TestAdminAcquisitionPolicyForbiddenWithoutPerm(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "user"}
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		req := authedJSON(u, method, "/api/v1/admin/acquisition-policy", map[string]string{})
		rec := httptest.NewRecorder()
		if method == http.MethodGet {
			s.adminGetAcquisitionPolicy(rec, req)
		} else {
			s.adminPutAcquisitionPolicy(rec, req)
		}
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s without perm: status %d body %s", method, rec.Code, rec.Body.String())
		}
	}
}

func TestAdminAcquisitionPolicyHasPermViaAdminFlag(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "admin", IsAdmin: true}
	req := authedJSON(u, http.MethodGet, "/api/v1/admin/acquisition-policy", nil)
	rec := httptest.NewRecorder()
	s.adminGetAcquisitionPolicy(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	out := decodeMap(t, rec)
	if out["format_profile"] != defaultFormatProfile {
		t.Fatalf("default format %v", out["format_profile"])
	}
	if out["media_policy_id"] != defaultFormatProfile {
		t.Fatalf("default policy %v", out["media_policy_id"])
	}
}

func TestAdminAcquisitionPolicyHasPermViaNamedPermission(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "ops", Permissions: []string{acquirePerm}}
	req := authedJSON(u, http.MethodGet, "/api/v1/admin/acquisition-policy", nil)
	rec := httptest.NewRecorder()
	s.adminGetAcquisitionPolicy(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("named perm should pass HasPerm, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestAdminAcquisitionPolicyRejectsYtdlpArgs(t *testing.T) {
	s := &Server{}
	u := &auth.User{ID: uuid.New(), Username: "admin", IsAdmin: true}
	req := authedJSON(u, http.MethodPut, "/api/v1/admin/acquisition-policy", map[string]any{
		"format_profile": "m4a-0",
		"ytdlp_args":     "-f bestaudio",
	})
	rec := httptest.NewRecorder()
	s.adminPutAcquisitionPolicy(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "yt-dlp") {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestNormalizeAcquisitionPolicy(t *testing.T) {
	got, err := normalizeAcquisitionPolicy(AcquisitionPolicy{FormatProfile: "m4a-0"})
	if err != nil {
		t.Fatal(err)
	}
	if got.FormatProfile != "m4a-0" || got.MediaPolicyID != "m4a-0" {
		t.Fatalf("%+v", got)
	}
	if _, err := normalizeAcquisitionPolicy(AcquisitionPolicy{FormatProfile: "-f bestaudio"}); err == nil {
		t.Fatal("expected reject")
	}
	if msg := rejectRawYtdlpArgs(map[string]any{"extractor_args": "youtube:player"}); msg == "" {
		t.Fatal("expected reject extractor_args")
	}
}

func TestAdminAcquisitionPolicyDB(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := &Server{Pool: pool}
	admin := &auth.User{ID: uuid.New(), Username: "admin", IsAdmin: true}

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM server_settings WHERE key=$1`, acquisitionPolicySetting)
	})

	put := authedJSON(admin, http.MethodPut, "/api/v1/admin/acquisition-policy", map[string]string{
		"media_policy_id": "opus-0",
		"format_profile":  "opus-0",
	})
	prec := httptest.NewRecorder()
	s.adminPutAcquisitionPolicy(prec, put)
	if prec.Code != http.StatusOK {
		t.Fatalf("PUT status %d %s", prec.Code, prec.Body.String())
	}
	out := decodeMap(t, prec)
	if out["format_profile"] != "opus-0" {
		t.Fatalf("saved %v", out)
	}

	var perm int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM permissions WHERE name=$1`, acquirePerm).Scan(&perm); err != nil || perm != 1 {
		t.Fatalf("permissions seed: count=%d err=%v", perm, err)
	}
	var attached int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM role_permissions rp
		JOIN roles r ON r.id=rp.role_id
		JOIN permissions p ON p.id=rp.permission_id
		WHERE r.name='Administrator' AND p.name=$1`, acquirePerm).Scan(&attached); err != nil || attached < 1 {
		t.Fatalf("Administrator role_permissions seed: count=%d err=%v", attached, err)
	}

	grec := httptest.NewRecorder()
	s.adminGetAcquisitionPolicy(grec, authedJSON(admin, http.MethodGet, "/api/v1/admin/acquisition-policy", nil))
	got := decodeMap(t, grec)
	if got["media_policy_id"] != "opus-0" {
		t.Fatalf("GET %v", got)
	}

	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "ytdlp") {
		t.Fatalf("must not leak yt-dlp args: %s", raw)
	}
}
