//go:build darwin

package freedl

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/fileops"
)

func TestPromotionOutputsPreserveOriginalCreationTime(t *testing.T) {
	dir := t.TempDir()
	libraryPath := filepath.Join(dir, "library", "track.m4a")
	backupPath := filepath.Join(dir, "backups", "track.m4a")
	if err := os.MkdirAll(filepath.Dir(libraryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(libraryPath, []byte("original audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := time.Now().Add(-180 * 24 * time.Hour).Truncate(time.Second)
	if err := fileops.RestoreCreationTime(libraryPath, fileops.CreationTime{Time: created, Available: true}); err != nil {
		t.Fatal(err)
	}
	creation, err := fileops.ReadCreationTime(libraryPath)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := backupOriginal(libraryPath, backupPath, creation); err != nil {
		t.Fatalf("backup original: %v", err)
	}
	assertCreationTime(t, backupPath, created)

	originalRun := runPromotionFFmpeg
	runPromotionFFmpeg = func(_ context.Context, _ PromotionPlan, _ PromotionRow, outputPath string) error {
		return os.WriteFile(outputPath, []byte("upgraded audio"), 0o644)
	}
	t.Cleanup(func() {
		runPromotionFFmpeg = originalRun
	})
	if err := applyReplacement(
		context.Background(),
		PromotionPlan{},
		PromotionRow{LibraryPath: libraryPath},
		creation,
	); err != nil {
		t.Fatalf("apply replacement: %v", err)
	}
	assertCreationTime(t, libraryPath, created)
}

func assertCreationTime(t *testing.T, path string, want time.Time) {
	t.Helper()
	got, err := fileops.ReadCreationTime(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Available || !got.Time.Equal(want) {
		t.Fatalf("creation time mismatch for %s: want %s, got %+v", path, want, got)
	}
}
