package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitTempStateFilesPreservesMissingKnownGapEntries(t *testing.T) {
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, "source.sync.scdl")
	archivePath := filepath.Join(tmp, "source.archive.txt")

	originalState := strings.Join([]string{
		"soundcloud keep-a keep-a.m4a",
		"soundcloud replay-a replay-a-old.m4a",
	}, "\n") + "\n"
	if err := os.WriteFile(statePath, []byte(originalState), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	originalArchive := strings.Join([]string{
		"soundcloud keep-a",
		"soundcloud replay-a",
	}, "\n") + "\n"
	if err := os.WriteFile(archivePath, []byte(originalArchive), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	parsedState, err := parseSoundCloudSyncState(statePath)
	if err != nil {
		t.Fatalf("parse state: %v", err)
	}
	removeIDs := map[string]struct{}{"replay-a": {}}
	tempStatePath, err := writeFilteredSyncStateFile(statePath, parsedState, removeIDs)
	if err != nil {
		t.Fatalf("write temp state: %v", err)
	}
	tempArchivePath, err := writeFilteredArchiveFile(archivePath, removeIDs)
	if err != nil {
		t.Fatalf("write temp archive: %v", err)
	}

	if err := commitTempStateFiles(soundCloudStateSwap{
		OriginalSyncPath:    statePath,
		TempSyncPath:        tempStatePath,
		OriginalArchivePath: archivePath,
		TempArchivePath:     tempArchivePath,
	}); err != nil {
		t.Fatalf("commit temp state files: %v", err)
	}

	state, err := parseSoundCloudSyncState(statePath)
	if err != nil {
		t.Fatalf("parse merged state: %v", err)
	}
	if _, ok := state.ByID["keep-a"]; !ok {
		t.Fatalf("expected keep-a to remain in state, got %+v", state.ByID)
	}
	if _, ok := state.ByID["replay-a"]; !ok {
		t.Fatalf("expected replay-a to remain in state, got %+v", state.ByID)
	}

	archive, err := parseSoundCloudArchive(archivePath)
	if err != nil {
		t.Fatalf("parse merged archive: %v", err)
	}
	if _, ok := archive["keep-a"]; !ok {
		t.Fatalf("expected keep-a to remain in archive, got %+v", archive)
	}
	if _, ok := archive["replay-a"]; !ok {
		t.Fatalf("expected replay-a to remain in archive, got %+v", archive)
	}
}

func TestCommitTempStateFilesPrefersUpdatedTempEntriesAndAppendsNewIDs(t *testing.T) {
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, "source.sync.scdl")
	archivePath := filepath.Join(tmp, "source.archive.txt")

	if err := os.WriteFile(statePath, []byte("soundcloud replay-a replay-a-old.m4a\n"), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if err := os.WriteFile(archivePath, []byte("soundcloud replay-a\n"), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	tempState := strings.Join([]string{
		"soundcloud replay-a replay-a-new.m4a",
		"soundcloud new-a new-a.m4a",
	}, "\n") + "\n"
	tempStatePath := filepath.Join(tmp, ".udl-sync-temp.scdl")
	if err := os.WriteFile(tempStatePath, []byte(tempState), 0o644); err != nil {
		t.Fatalf("write temp state: %v", err)
	}
	tempArchive := strings.Join([]string{
		"soundcloud replay-a",
		"soundcloud new-a",
	}, "\n") + "\n"
	tempArchivePath := filepath.Join(tmp, ".udl-archive-temp.txt")
	if err := os.WriteFile(tempArchivePath, []byte(tempArchive), 0o644); err != nil {
		t.Fatalf("write temp archive: %v", err)
	}

	if err := commitTempStateFiles(soundCloudStateSwap{
		OriginalSyncPath:    statePath,
		TempSyncPath:        tempStatePath,
		OriginalArchivePath: archivePath,
		TempArchivePath:     tempArchivePath,
	}); err != nil {
		t.Fatalf("commit temp state files: %v", err)
	}

	state, err := parseSoundCloudSyncState(statePath)
	if err != nil {
		t.Fatalf("parse merged state: %v", err)
	}
	replay := state.ByID["replay-a"]
	if replay.FilePath != "replay-a-new.m4a" {
		t.Fatalf("expected replay-a path to be updated, got %+v", replay)
	}
	if _, ok := state.ByID["new-a"]; !ok {
		t.Fatalf("expected new-a to be appended, got %+v", state.ByID)
	}

	archive, err := parseSoundCloudArchive(archivePath)
	if err != nil {
		t.Fatalf("parse merged archive: %v", err)
	}
	if _, ok := archive["replay-a"]; !ok {
		t.Fatalf("expected replay-a in archive, got %+v", archive)
	}
	if _, ok := archive["new-a"]; !ok {
		t.Fatalf("expected new-a in archive, got %+v", archive)
	}
}
