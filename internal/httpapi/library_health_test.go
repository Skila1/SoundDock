package httpapi

import (
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/fingerprint"
	"github.com/sounddock/sounddock/internal/integrity"
	"github.com/sounddock/sounddock/internal/waveform"
)

func TestFrozenJobNames(t *testing.T) {
	if waveform.JobName != "waveform.generate" || fingerprint.JobName != "fingerprint.generate" || integrity.JobName != "integrity.scan" {
		t.Fatal("frozen job names")
	}
}

func TestHealthFingerprintStatus(t *testing.T) {
	st := fingerprint.Availability()
	if st != fingerprint.Available && st != fingerprint.Missing {
		t.Fatal(st)
	}
}

func TestTrashKeyKeepsUniqueConstraint(t *testing.T) {
	id := uuid.MustParse("00000000-0000-4000-8000-000000000060")
	orig := "uploads/aa/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.flac"
	if integrity.TrashKey(id, orig) == orig {
		t.Fatal("trash must move the unique key")
	}
}

func TestJsonRawOrNil(t *testing.T) {
	if jsonRawOrNil(nil) != nil || jsonRawOrNil([]byte("null")) != nil {
		t.Fatal("empty peaks")
	}
}
