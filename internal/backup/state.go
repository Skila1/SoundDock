package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const (
	PhaseDecrypt   = "decrypt"
	PhaseVerify    = "verify"
	PhaseWipe      = "wipe"
	PhasePSQL      = "psql"
	PhaseFiles     = "files"
	PhaseMasterKey = "masterkey"
	PhaseDone      = "done"
)

// RestoreState is written to restore-state.json so a crash can resume.
type RestoreState struct {
	Phase         string              `json:"phase"`
	Archive       string              `json:"archive"`
	WorkDir       string              `json:"work_dir,omitempty"`
	StartedAt     string              `json:"started_at"`
	UpdatedAt     string              `json:"updated_at"`
	SchemaVersion int                 `json:"schema_version"`
	Requirements  RestoreRequirements `json:"requirements,omitempty"`
	Dismissed     bool                `json:"dismissed,omitempty"`
	Error         string              `json:"error,omitempty"`
}

func statePath(dataDir string) string {
	if dataDir == "" {
		dataDir = "."
	}
	return filepath.Join(dataDir, "restore-state.json")
}

func LoadState(dataDir string) (RestoreState, error) {
	var st RestoreState
	b, err := os.ReadFile(statePath(dataDir))
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return st, err
	}
	return st, nil
}

func (s *Service) dataRoot() string {
	if s != nil && s.dataDir != "" {
		return s.dataDir
	}
	if s != nil && s.dir != "" {
		return filepath.Dir(s.dir)
	}
	return "."
}

func (s *Service) loadState() (RestoreState, error) {
	return LoadState(s.dataRoot())
}

func (s *Service) writeState(st RestoreState) error {
	st.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if st.StartedAt == "" {
		st.StartedAt = st.UpdatedAt
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	p := statePath(s.dataRoot())
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func (s *Service) setPhase(st *RestoreState, phase string) error {
	st.Phase = phase
	st.Error = ""
	return s.writeState(*st)
}

func (st RestoreState) InProgress() bool {
	return st.Phase != "" && st.Phase != PhaseDone && !st.Dismissed
}

func (s *Service) DismissRequirements() error {
	st, err := s.loadState()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	st.Dismissed = true
	return s.writeState(st)
}
