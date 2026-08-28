package update

import (
	"strconv"
	"strings"

	"github.com/sounddock/sounddock/migrations"
)

type Kind string

const (
	KindImageOnly     Kind = "image_only"
	KindSchemaForward Kind = "schema_forward"
)

func EmbeddedHead() int64 {
	return migrations.Head()
}

func Classify(current, target int64) Kind {
	if target > current {
		return KindSchemaForward
	}
	return KindImageOnly
}

// OldImageCompatible is false when the live schema is ahead of what the old
// image can migrate or run. Starting that image would brick the instance.
func OldImageCompatible(currentSchema, oldImageHead int64) bool {
	if oldImageHead <= 0 {
		return currentSchema <= 0
	}
	return currentSchema <= oldImageHead
}

type Decision struct {
	Status        string
	StartOldImage bool
}

func DecideAfterFailure(schemaBefore, schemaAfter, oldImageHead int64) Decision {
	if schemaAfter == schemaBefore && OldImageCompatible(schemaAfter, oldImageHead) {
		return Decision{Status: "rolled_back", StartOldImage: true}
	}
	return Decision{Status: "needs_recovery", StartOldImage: false}
}

func confirmApply(st stored, applied string, healthy bool) (stored, bool) {
	if st.LastStatus == "needs_recovery" {
		return st, false
	}
	if st.LastStatus != "updating" {
		return st, false
	}
	expected := strings.TrimSpace(st.ExpectedDigest)
	if expected == "" {
		expected = strings.TrimSpace(st.LatestDigest)
	}
	if expected == "" || applied == "" || !digestEqual(applied, expected) {
		return st, false
	}
	if !healthy {
		return st, false
	}
	now := st.LastAppliedAt
	st.CurrentDigest = applied
	st.LastStatus = "ok"
	st.LastError = ""
	st.Available = st.LatestDigest != "" && !digestEqual(applied, st.LatestDigest)
	st.NeedsRecovery = false
	_ = now
	return st, true
}

func parseSchemaHead(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func InferTargetHead(currentSchema, labelHead int64, known bool) int64 {
	if labelHead > 0 {
		return labelHead
	}
	if known {
		return currentSchema
	}
	if currentSchema <= 0 {
		return EmbeddedHead()
	}
	return currentSchema + 1
}

const schemaHeadLabel = "org.sounddock.schema-head"
