package httpapi

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/backup"
)

func (s *Server) adminBackupSettingsGet(w http.ResponseWriter, r *http.Request) {
	if s.Backup == nil {
		writeErr(w, 500, "backup", "backup service unavailable")
		return
	}
	writeJSON(w, 200, s.Backup.LoadSettings(r.Context()).Public())
}

func (s *Server) adminBackupSettingsPut(w http.ResponseWriter, r *http.Request) {
	if s.Backup == nil {
		writeErr(w, 500, "backup", "backup service unavailable")
		return
	}
	var body backup.Settings
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid", "invalid body")
		return
	}
	if body.R2Enabled && (strings.TrimSpace(body.Endpoint) == "" || strings.TrimSpace(body.Bucket) == "") {
		writeErr(w, 400, "invalid", "R2 endpoint and bucket are required when R2 is on")
		return
	}
	if err := s.Backup.SaveSettings(r.Context(), body); err != nil {
		writeErr(w, 400, "backup", err.Error())
		return
	}
	if currentUser(r) != nil {
		s.Audit.Event(r.Context(), &currentUser(r).ID, "backup.settings", "", r.RemoteAddr, nil)
	}
	writeJSON(w, 200, s.Backup.LoadSettings(r.Context()).Public())
}

func (s *Server) adminBackupPassphrase(w http.ResponseWriter, r *http.Request) {
	if s.Backup == nil {
		writeErr(w, 500, "backup", "backup service unavailable")
		return
	}
	var body struct {
		Passphrase        string `json:"passphrase"`
		CurrentPassphrase string `json:"current_passphrase"`
	}
	if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.Passphrase) == "" {
		writeErr(w, 400, "invalid", "passphrase required")
		return
	}
	out, err := s.Backup.SetPassphrase(r.Context(), body.Passphrase, body.CurrentPassphrase)
	if err != nil {
		writeErr(w, 400, "passphrase", err.Error())
		return
	}
	s.Audit.Event(r.Context(), &currentUser(r).ID, "backup.passphrase", "", r.RemoteAddr, nil)
	writeJSON(w, 200, out)
}

func (s *Server) adminBackupReminderGet(w http.ResponseWriter, r *http.Request) {
	if s.Backup == nil {
		writeErr(w, 500, "backup", "backup service unavailable")
		return
	}
	text, err := s.Backup.ConsumeReminder(r.Context())
	if err != nil {
		writeErr(w, 404, "reminder", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="sounddock-recovery-reminder.txt"`)
	w.WriteHeader(200)
	_, _ = w.Write([]byte(text))
}

func (s *Server) adminBackupReminderDismiss(w http.ResponseWriter, r *http.Request) {
	if s.Backup == nil {
		writeErr(w, 500, "backup", "backup service unavailable")
		return
	}
	if err := s.Backup.DeclineReminder(r.Context()); err != nil {
		writeErr(w, 500, "reminder", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) adminBackupRequirements(w http.ResponseWriter, r *http.Request) {
	if s.Backup == nil {
		writeErr(w, 500, "backup", "backup service unavailable")
		return
	}
	req, err := s.Backup.Requirements()
	if err != nil {
		writeJSON(w, 200, map[string]any{"items": []any{}})
		return
	}
	writeJSON(w, 200, req)
}

func (s *Server) adminBackupRequirementsDismiss(w http.ResponseWriter, r *http.Request) {
	if s.Backup == nil {
		writeErr(w, 500, "backup", "backup service unavailable")
		return
	}
	_ = s.Backup.DismissRequirements()
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) adminBackupRemote(w http.ResponseWriter, r *http.Request) {
	if s.Backup == nil {
		writeErr(w, 500, "backup", "backup service unavailable")
		return
	}
	if !s.setupAllowsBackupRemote(r) {
		writeErr(w, 409, "setup", "R2 list is available during first setup or after you sign in as admin")
		return
	}
	st := s.Backup.LoadSettings(r.Context())
	if !st.R2Enabled || st.Bucket == "" {
		writeJSON(w, 200, []any{})
		return
	}
	list, err := s.Backup.ListRemote(r.Context(), st)
	if err != nil {
		writeErr(w, 502, "r2", err.Error())
		return
	}
	if list == nil {
		list = []backup.RemoteObject{}
	}
	writeJSON(w, 200, list)
}

func (s *Server) adminBackupImportRemote(w http.ResponseWriter, r *http.Request) {
	if s.Backup == nil {
		writeErr(w, 500, "backup", "backup service unavailable")
		return
	}
	if !s.setupAllowsBackupRemote(r) {
		writeErr(w, 409, "setup", "R2 import is available during first setup or after you sign in as admin")
		return
	}
	var body struct {
		Key        string `json:"key"`
		Restore    bool   `json:"restore"`
		Confirm    bool   `json:"confirm"`
		Passphrase string `json:"passphrase"`
	}
	if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.Key) == "" {
		writeErr(w, 400, "invalid", "key required")
		return
	}
	id, err := s.Backup.ImportRemote(r.Context(), body.Key)
	if err != nil {
		writeErr(w, 502, "r2", err.Error())
		return
	}
	var req backup.RestoreRequirements
	if body.Restore {
		if !body.Confirm {
			writeErr(w, 400, "confirm", "restore requires confirm=true")
			return
		}
		req, err = s.Backup.Restore(r.Context(), id, body.Passphrase)
		if err != nil {
			writeErr(w, 500, "restore", err.Error())
			return
		}
	}
	if u := currentUser(r); u != nil {
		s.Audit.Event(r.Context(), &u.ID, "backup.import_r2", body.Key, r.RemoteAddr, nil)
	}
	writeJSON(w, 200, map[string]any{"ok": true, "id": id, "restored": body.Restore, "requirements": req})
}

func (s *Server) setupAllowsBackupRemote(r *http.Request) bool {
	if currentUser(r) != nil {
		return true
	}
	if s.Auth == nil {
		return false
	}
	needed, err := s.Auth.SetupNeeded(r.Context())
	return err == nil && needed
}

func (s *Server) requireSetupNeeded(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Auth == nil {
			writeErr(w, 500, "setup", "auth unavailable")
			return
		}
		needed, err := s.Auth.SetupNeeded(r.Context())
		if err != nil || !needed {
			writeErr(w, 409, "setup", "setup is already complete")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) adminBackupDoRestore(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	var body struct {
		Confirm    bool   `json:"confirm"`
		Passphrase string `json:"passphrase"`
	}
	_ = decodeJSON(r, &body)
	if !body.Confirm {
		writeErr(w, 400, "confirm", "restore requires confirm=true")
		return
	}
	req, err := s.Backup.Restore(r.Context(), id, body.Passphrase)
	if err != nil {
		writeErr(w, 500, "restore", err.Error())
		return
	}
	if u := currentUser(r); u != nil {
		s.Audit.Event(r.Context(), &u.ID, "backup.restore", id.String(), r.RemoteAddr, nil)
	}
	writeJSON(w, 200, map[string]any{"ok": true, "id": id, "requirements": req, "restarting": true})
}
