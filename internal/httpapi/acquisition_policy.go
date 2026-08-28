package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/sounddock/sounddock/internal/auth"
)

const (
	acquirePerm              = "library.acquire"
	acquisitionPolicySetting = "acquisition_policy"
	defaultFormatProfile     = "m4a-0"
)

var allowedFormatProfiles = map[string]bool{
	"m4a-0":  true,
	"mp3-0":  true,
	"opus-0": true,
}

var forbiddenPolicyKeys = map[string]bool{
	"ytdlp_args":     true,
	"yt_dlp_args":    true,
	"yt-dlp_args":    true,
	"extra_args":     true,
	"args":           true,
	"command":        true,
	"cookies":        true,
	"cookiefile":     true,
	"proxy":          true,
	"format":         true,
	"extractor_args": true,
}

// AcquisitionPolicy is the admin default dest/profile for ScapeX jobs.
// Raw yt-dlp arguments are never accepted.
type AcquisitionPolicy struct {
	MediaPolicyID string `json:"media_policy_id"`
	FormatProfile string `json:"format_profile"`
}

func defaultAcquisitionPolicy() AcquisitionPolicy {
	return AcquisitionPolicy{
		MediaPolicyID: defaultFormatProfile,
		FormatProfile: defaultFormatProfile,
	}
}

func (s *Server) requireLibraryAcquire(w http.ResponseWriter, r *http.Request) bool {
	if !auth.HasPerm(currentUser(r), acquirePerm) {
		writeErr(w, http.StatusForbidden, "forbidden", "library acquire not permitted")
		return false
	}
	s.ensureAcquirePerm(r.Context())
	return true
}

// ensureAcquirePerm seeds permissions.library.acquire and attaches it to
// Administrator. No numbered migration (0016 is Wave 6 ScapeX).
func (s *Server) ensureAcquirePerm(ctx context.Context) {
	if s.Pool == nil {
		return
	}
	_, _ = s.Pool.Exec(ctx, `
		INSERT INTO permissions (name, description)
		VALUES ('library.acquire', 'Manage acquisition policy and restore managed media')
		ON CONFLICT DO NOTHING`)
	_, _ = s.Pool.Exec(ctx, `
		INSERT INTO role_permissions (role_id, permission_id)
		SELECT r.id, p.id FROM roles r, permissions p
		WHERE r.name = 'Administrator' AND p.name = 'library.acquire'
		ON CONFLICT DO NOTHING`)
}

func (s *Server) adminGetAcquisitionPolicy(w http.ResponseWriter, r *http.Request) {
	if !s.requireLibraryAcquire(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, s.loadAcquisitionPolicy(r.Context()))
}

func (s *Server) adminPutAcquisitionPolicy(w http.ResponseWriter, r *http.Request) {
	if !s.requireLibraryAcquire(w, r) {
		return
	}
	raw, body, err := decodeAcquisitionPolicyBody(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid", "invalid body")
		return
	}
	if msg := rejectRawYtdlpArgs(raw); msg != "" {
		writeErr(w, http.StatusBadRequest, "invalid", msg)
		return
	}
	pol, err := normalizeAcquisitionPolicy(body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	if err := s.storeAcquisitionPolicy(r.Context(), pol); err != nil {
		writeErr(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	if s.Audit != nil {
		s.Audit.Event(r.Context(), &currentUser(r).ID, "acquisition.policy", "", r.RemoteAddr, nil)
	}
	writeJSON(w, http.StatusOK, s.loadAcquisitionPolicy(r.Context()))
}

func decodeAcquisitionPolicyBody(r *http.Request) (map[string]any, AcquisitionPolicy, error) {
	var raw map[string]any
	if err := decodeJSON(r, &raw); err != nil {
		return nil, AcquisitionPolicy{}, err
	}
	if raw == nil {
		raw = map[string]any{}
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return raw, AcquisitionPolicy{}, err
	}
	var body AcquisitionPolicy
	if err := json.Unmarshal(b, &body); err != nil {
		return raw, AcquisitionPolicy{}, err
	}
	return raw, body, nil
}

func rejectRawYtdlpArgs(raw map[string]any) string {
	for k := range raw {
		lk := strings.ToLower(strings.TrimSpace(k))
		if forbiddenPolicyKeys[lk] || strings.Contains(lk, "ytdlp") || strings.Contains(lk, "yt-dlp") || strings.Contains(lk, "yt_dlp") {
			return "raw yt-dlp arguments are not accepted"
		}
		switch lk {
		case "media_policy_id", "format_profile":
			continue
		default:
			return "unknown field " + k
		}
	}
	return ""
}

func normalizeAcquisitionPolicy(in AcquisitionPolicy) (AcquisitionPolicy, error) {
	out := defaultAcquisitionPolicy()
	profile := strings.TrimSpace(in.FormatProfile)
	policyID := strings.TrimSpace(in.MediaPolicyID)
	if profile == "" {
		profile = policyID
	}
	if policyID == "" {
		policyID = profile
	}
	if profile != "" {
		if !allowedFormatProfiles[profile] {
			return AcquisitionPolicy{}, errString("format_profile must be a known profile (e.g. m4a-0)")
		}
		out.FormatProfile = profile
	}
	if policyID != "" {
		if allowedFormatProfiles[policyID] {
			out.MediaPolicyID = policyID
		} else {
			return AcquisitionPolicy{}, errString("media_policy_id must match a known format profile (e.g. m4a-0)")
		}
	}
	if out.MediaPolicyID == "" {
		out.MediaPolicyID = out.FormatProfile
	}
	if out.FormatProfile == "" {
		out.FormatProfile = out.MediaPolicyID
	}
	return out, nil
}

func (s *Server) loadAcquisitionPolicy(ctx context.Context) AcquisitionPolicy {
	def := defaultAcquisitionPolicy()
	if s == nil || s.Pool == nil {
		return def
	}
	var raw []byte
	err := s.Pool.QueryRow(ctx, `SELECT value FROM server_settings WHERE key=$1`, acquisitionPolicySetting).Scan(&raw)
	if err != nil || len(raw) == 0 {
		return def
	}
	var stored AcquisitionPolicy
	if err := json.Unmarshal(raw, &stored); err != nil {
		return def
	}
	out, err := normalizeAcquisitionPolicy(stored)
	if err != nil {
		return def
	}
	return out
}

func (s *Server) storeAcquisitionPolicy(ctx context.Context, pol AcquisitionPolicy) error {
	if s.Pool == nil {
		return errString("database unavailable")
	}
	b, err := json.Marshal(pol)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `
		INSERT INTO server_settings (key, value) VALUES ($1, $2::jsonb)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, acquisitionPolicySetting, b)
	return err
}
