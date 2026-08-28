package update

import (
	"fmt"
)

type TxResult struct {
	Status        string
	Kind          Kind
	StartOldImage bool
	CurrentDigest string
	AppliedDigest string
	BackupPath    string
	NeedsRecovery bool
	Dumped        bool
	Err           error
}

type TxHooks struct {
	SchemaBefore   int64
	TargetHead     int64
	OldImageHead   int64
	PreviousDigest string
	NewDigest      string
	Dump           func() (string, error)
	Pull           func() error
	StartNew       func() error
	Health         func() error
	SchemaAfter    func() (int64, error)
	StartOld       func() error
}

// RunTransaction is the apply path used by tests and the socket helper.
// SQL dump runs before pull when the target schema is ahead.
func RunTransaction(h TxHooks) TxResult {
	kind := Classify(h.SchemaBefore, h.TargetHead)
	res := TxResult{Kind: kind, CurrentDigest: h.PreviousDigest}

	if kind == KindSchemaForward {
		if h.Dump == nil {
			res.Status = "error"
			res.Err = fmt.Errorf("schema-forward update requires a SQL backup")
			return res
		}
		path, err := h.Dump()
		if err != nil {
			res.Status = "error"
			res.Err = err
			return res
		}
		res.BackupPath = path
		res.Dumped = true
	}

	if h.Pull != nil {
		if err := h.Pull(); err != nil {
			res.Status = "error"
			res.Err = err
			return res
		}
	}

	if h.StartNew != nil {
		if err := h.StartNew(); err != nil {
			return failApply(h, res, h.SchemaBefore, err)
		}
	}

	schemaAfter := h.SchemaBefore
	if h.SchemaAfter != nil {
		if v, err := h.SchemaAfter(); err == nil {
			schemaAfter = v
		}
	}

	if h.Health != nil {
		if err := h.Health(); err != nil {
			return failApply(h, res, schemaAfter, err)
		}
	}

	res.Status = "ok"
	res.AppliedDigest = h.NewDigest
	res.CurrentDigest = h.NewDigest
	return res
}

func failApply(h TxHooks, res TxResult, schemaAfter int64, cause error) TxResult {
	d := DecideAfterFailure(h.SchemaBefore, schemaAfter, h.OldImageHead)
	res.Status = d.Status
	res.StartOldImage = d.StartOldImage
	res.NeedsRecovery = d.Status == "needs_recovery"
	res.Err = cause
	if d.StartOldImage {
		if h.StartOld != nil {
			_ = h.StartOld()
		}
		res.CurrentDigest = h.PreviousDigest
		res.AppliedDigest = ""
		return res
	}
	res.CurrentDigest = h.NewDigest
	return res
}
