package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/sounddock/sounddock/internal/jobs"
	"github.com/sounddock/sounddock/internal/storage"
)

func TestMigrateHandlerDeletesCopiedKeysWhenDestFails(t *testing.T) {
	srcDir := t.TempDir()
	managedDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "track.flac"), []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := storage.NewLocal("src", srcDir, false)
	if err != nil {
		t.Fatal(err)
	}
	managed, err := storage.NewLocal("managed", managedDir, false)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(nil, managed, nil, t.TempDir(), 1<<20)
	srcID, destID := uuid.New(), uuid.New()
	h := svc.MigrateHandler(func(_ context.Context, id uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error) {
		if id == destID {
			return nil, uuid.Nil, "", errors.New("dest unavailable")
		}
		return src, srcID, "", nil
	})
	raw, _ := json.Marshal(MigratePayload{Source: srcID, Dest: destID, Mode: "copy"})
	err = h(context.Background(), jobs.Job{ID: uuid.New(), Payload: raw})
	if err == nil {
		t.Fatal("expected dest failure")
	}
	if _, err := os.Stat(filepath.Join(managedDir, "migrated", "track.flac")); err == nil {
		t.Fatal("migrated object must be deleted after dest failure")
	}
}

func TestMigrateMoveDoesNotDeleteSourceWhenDestFails(t *testing.T) {
	srcDir := t.TempDir()
	managedDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "track.flac")
	if err := os.WriteFile(srcFile, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := storage.NewLocal("src", srcDir, false)
	if err != nil {
		t.Fatal(err)
	}
	managed, err := storage.NewLocal("managed", managedDir, false)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(nil, managed, nil, t.TempDir(), 1<<20)
	srcID, destID := uuid.New(), uuid.New()
	h := svc.MigrateHandler(func(_ context.Context, id uuid.UUID) (storage.StorageProvider, uuid.UUID, string, error) {
		if id == destID {
			return nil, uuid.Nil, "", errors.New("dest unavailable")
		}
		return src, srcID, "", nil
	})
	raw, _ := json.Marshal(MigratePayload{Source: srcID, Dest: destID, Mode: "move"})
	if err := h(context.Background(), jobs.Job{ID: uuid.New(), Payload: raw}); err == nil {
		t.Fatal("expected dest failure")
	}
	if _, err := os.Stat(srcFile); err != nil {
		t.Fatal("NAS/local source must remain after failed move")
	}
}

func TestResolveMigrateModesCoercesExternalMove(t *testing.T) {
	svc := New(nil, nil, nil, t.TempDir(), 1)
	req, eff, reason := svc.ResolveMigrateModes(context.Background(), "move", uuid.New())
	if req != "move" || eff != "copy" || reason != "source_not_managed" {
		t.Fatalf("requested=%s effective=%s reason=%s", req, eff, reason)
	}
	req, eff, reason = svc.ResolveMigrateModes(context.Background(), "copy", uuid.New())
	if req != "copy" || eff != "copy" || reason != "" {
		t.Fatalf("copy requested=%s effective=%s reason=%s", req, eff, reason)
	}
}
