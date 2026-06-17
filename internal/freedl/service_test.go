package freedl

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupOriginalCopiesOriginalAndRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "library", "track.mp3")
	backup := filepath.Join(dir, "backups", "run-1", "track.mp3")
	if err := os.MkdirAll(filepath.Dir(original), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(original, []byte("original audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	sum, err := backupOriginal(original, backup)
	if err != nil {
		t.Fatalf("backup original: %v", err)
	}
	if sum == "" {
		t.Fatal("expected backup checksum")
	}
	payload, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(payload) != "original audio" {
		t.Fatalf("unexpected backup payload %q", payload)
	}

	if err := os.WriteFile(original, []byte("changed audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := backupOriginal(original, backup); err == nil {
		t.Fatal("expected second backup to fail instead of overwriting")
	}
	payload, err = os.ReadFile(backup)
	if err != nil {
		t.Fatalf("read backup after failed overwrite: %v", err)
	}
	if string(payload) != "original audio" {
		t.Fatalf("backup was overwritten: %q", payload)
	}
}

func TestBuildAssignmentsRejectsAmbiguousMatches(t *testing.T) {
	library := []mediaFile{{
		Rel: "song.mp3",
		Key: "artist song",
	}}
	freeFiles := []mediaFile{
		{Rel: "song-a.wav", Key: "artist song"},
		{Rel: "song-b.wav", Key: "artist song"},
	}

	assignments := buildAssignments(library, freeFiles, 70, 8)
	if len(assignments) != 0 {
		t.Fatalf("expected ambiguous candidates to be skipped, got %+v", assignments)
	}
}
