package fingerprint

import "testing"

func TestAvailabilityAndNoop(t *testing.T) {
	st := Availability()
	if st != Available && st != Missing {
		t.Fatal(st)
	}
	fp, _, err := Calc(t.Context(), "")
	if err != nil || fp != "" {
		t.Fatalf("missing fpcalc must no-op, got %q %v", fp, err)
	}
}

func TestJobNameFrozen(t *testing.T) {
	if JobName != "fingerprint.generate" || !JobTypeOK(JobName) {
		t.Fatal(JobName)
	}
}

func TestEnsureSchemaNilPool(t *testing.T) {
	if err := EnsureSchema(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
}
