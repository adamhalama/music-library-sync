//go:build darwin

package fileops

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRestoreCreationTimePreservesModificationTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "track.m4a")
	if err := os.WriteFile(path, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	modified := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	created := modified.Add(-30 * 24 * time.Hour)

	if err := RestoreCreationTime(path, CreationTime{Time: created, Available: true}); err != nil {
		t.Fatalf("restore creation time: %v", err)
	}
	got, err := ReadCreationTime(path)
	if err != nil {
		t.Fatalf("read creation time: %v", err)
	}
	if !got.Available || !got.Time.Equal(created) {
		t.Fatalf("creation time mismatch: want %s, got %+v", created, got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(modified) {
		t.Fatalf("modification time changed: want %s, got %s", modified, info.ModTime())
	}
}
