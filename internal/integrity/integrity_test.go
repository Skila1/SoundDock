package integrity

import (
	"os"
	"testing"

	"github.com/google/uuid"
)

func TestJobNameFrozen(t *testing.T) {
	if JobName != "integrity.scan" || !JobTypeOK(JobName) {
		t.Fatal(JobName)
	}
}

func TestTrashKeyRoundTrip(t *testing.T) {
	id := uuid.MustParse("00000000-0000-4000-8000-000000000060")
	orig := "uploads/aa/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.flac"
	got := TrashKey(id, orig)
	want := "trash/" + id.String() + "/" + orig
	if got != want {
		t.Fatal(got)
	}
	back, err := OriginalKeyFromTrash(got, id)
	if err != nil || back != orig {
		t.Fatalf("%q %v", back, err)
	}
	if _, err := OriginalKeyFromTrash(orig, id); err == nil {
		t.Fatal("hash key is not trash")
	}
}

func TestSkipLivePostgres(t *testing.T) {
	if os.Getenv("SD_TEST_DATABASE_URL") == "" && os.Getenv("SD_DATABASE_URL") == "" {
		t.Skip("live postgres unset")
	}
}
