package navidrome

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func backupResolved(t *testing.T) Resolved {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := DefaultConfig()
	resolved, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := os.MkdirAll(resolved.BackupDir, 0o755); err != nil {
		t.Fatalf("create backup dir: %v", err)
	}
	return resolved
}

func TestBackupsOnMissingDirectoryIsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	resolved, err := Resolve(DefaultConfig())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	items, err := Backups(resolved)
	if err != nil {
		t.Fatalf("Backups: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("items = %+v", items)
	}
}

func TestBackupsAreNewestFirst(t *testing.T) {
	resolved := backupResolved(t)
	older := filepath.Join(resolved.BackupDir, "navidrome_backup_old.db")
	newer := filepath.Join(resolved.BackupDir, "navidrome_backup_new.db")
	for _, path := range []string{older, newer} {
		if err := os.WriteFile(path, []byte("db"), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(older, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	items, err := Backups(resolved)
	if err != nil {
		t.Fatalf("Backups: %v", err)
	}
	if len(items) != 2 || items[0].Path != newer {
		t.Fatalf("items = %+v", items)
	}
	latest, ok, err := LatestBackup(resolved)
	if err != nil || !ok || latest.Path != newer {
		t.Fatalf("LatestBackup = %+v %v %v", latest, ok, err)
	}
}

func TestCreateBackupReportsTheNewFile(t *testing.T) {
	resolved := backupResolved(t)
	created := filepath.Join(resolved.BackupDir, "navidrome_backup_2026.db")
	run := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("ok"), os.WriteFile(created, []byte("db"), 0o600)
	}
	result, err := CreateBackup(context.Background(), run, "/opt/homebrew/bin/navidrome", resolved)
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	if result.Backup.Path != created {
		t.Fatalf("backup = %+v", result.Backup)
	}
}

func TestCreateBackupFailsWhenNothingAppears(t *testing.T) {
	resolved := backupResolved(t)
	run := func(_ context.Context, _ string, _ ...string) ([]byte, error) { return []byte("ok"), nil }
	_, err := CreateBackup(context.Background(), run, "/opt/homebrew/bin/navidrome", resolved)
	if err == nil || !strings.Contains(err.Error(), "no new backup") {
		t.Fatalf("a silent no-op must be an error, got %v", err)
	}
}

func TestCreateBackupSurfacesCommandFailure(t *testing.T) {
	resolved := backupResolved(t)
	run := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("permission denied"), errors.New("exit status 1")
	}
	_, err := CreateBackup(context.Background(), run, "/opt/homebrew/bin/navidrome", resolved)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("error = %v", err)
	}
}

func TestCreateBackupWithoutBinaryFails(t *testing.T) {
	resolved := backupResolved(t)
	_, err := CreateBackup(context.Background(), nil, "", resolved)
	if err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("error = %v", err)
	}
}
