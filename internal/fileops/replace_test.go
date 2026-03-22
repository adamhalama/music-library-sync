package fileops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceFileSafelyReplacesExistingTarget(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "track.m4a")
	replacement := filepath.Join(tmp, ".tmp-track.m4a")

	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.WriteFile(replacement, []byte("new"), 0o644); err != nil {
		t.Fatalf("write replacement: %v", err)
	}

	if err := ReplaceFileSafely(replacement, target); err != nil {
		t.Fatalf("replace file safely: %v", err)
	}

	payload, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(payload) != "new" {
		t.Fatalf("expected replaced payload, got %q", string(payload))
	}
	if _, err := os.Stat(replacement); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected replacement file to be moved, stat err: %v", err)
	}
	if _, err := os.Stat(target + ".udl.bak"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected backup cleanup, stat err: %v", err)
	}
}

func TestReplaceFileSafelyRollbackRestoresOriginalTarget(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "track.m4a")
	replacement := filepath.Join(tmp, ".tmp-track.m4a")

	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.WriteFile(replacement, []byte("new"), 0o644); err != nil {
		t.Fatalf("write replacement: %v", err)
	}

	origRename := renameFile
	renameFile = func(oldpath string, newpath string) error {
		if oldpath == replacement && newpath == target {
			return errors.New("injected rename failure")
		}
		return os.Rename(oldpath, newpath)
	}
	t.Cleanup(func() {
		renameFile = origRename
	})

	err := ReplaceFileSafely(replacement, target)
	if err == nil {
		t.Fatalf("expected replacement failure")
	}

	payload, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("read restored target: %v", readErr)
	}
	if string(payload) != "old" {
		t.Fatalf("expected rollback to restore original payload, got %q", string(payload))
	}
	if _, statErr := os.Stat(target + ".udl.bak"); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected backup to be restored, stat err: %v", statErr)
	}
}

func TestReplaceFileSafelyRejectsDirectoryTarget(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "config")
	replacement := filepath.Join(tmp, ".tmp-config")

	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.WriteFile(replacement, []byte("new"), 0o644); err != nil {
		t.Fatalf("write replacement: %v", err)
	}

	err := ReplaceFileSafely(replacement, target)
	if err == nil {
		t.Fatalf("expected directory target rejection")
	}

	info, statErr := os.Stat(target)
	if statErr != nil {
		t.Fatalf("stat target: %v", statErr)
	}
	if !info.IsDir() {
		t.Fatalf("expected target to remain a directory")
	}
	if _, statErr := os.Stat(target + ".udl.bak"); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected no backup to be created, stat err: %v", statErr)
	}
}
