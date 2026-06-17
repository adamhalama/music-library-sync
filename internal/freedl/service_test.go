package freedl

import (
	"os"
	"path/filepath"
	"reflect"
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

func TestBuildPromotionAssignmentsUsesCaptureIdentityForRenamedDownload(t *testing.T) {
	dir := t.TempDir()
	libraryPath := filepath.Join(dir, "library", "Paolo Doldo X Muso - FOTOGRAFiA (FREE DOWNLOAD).m4a")
	freePath := filepath.Join(dir, "buffer", "downloads", "fotografia (master).wav")
	library := []mediaFile{{
		Path: libraryPath,
		Rel:  "Paolo Doldo X Muso - FOTOGRAFiA (FREE DOWNLOAD).m4a",
		Key:  "paolo doldo x muso fotografia free download",
	}}
	freeFiles := []mediaFile{{
		Path: freePath,
		Rel:  "fotografia (master).wav",
		Key:  "fotografia master",
	}}
	plan := CapturePlan{Rows: []PlanRow{{
		RemoteID:  "2186972503",
		LocalPath: libraryPath,
	}}}
	state := []captureStateEntry{{
		TrackID: "2186972503",
		Path:    "fotografia (master).wav",
	}}

	assignments := buildPromotionAssignments(library, freeFiles, plan, state, filepath.Join(dir, "buffer", "downloads"), 72, 8)
	if len(assignments) != 1 {
		t.Fatalf("expected one identity assignment, got %+v", assignments)
	}
	if assignments[0].library.Path != libraryPath || assignments[0].free.Path != freePath {
		t.Fatalf("expected identity paths to be matched, got %+v", assignments[0])
	}
	if assignments[0].score != 100 {
		t.Fatalf("expected identity score 100, got %d", assignments[0].score)
	}
}

func TestBuildPromotionAssignmentsIdentityWinsOverSemanticMatch(t *testing.T) {
	dir := t.TempDir()
	intendedPath := filepath.Join(dir, "library", "intended.m4a")
	decoyPath := filepath.Join(dir, "library", "track 1 master.m4a")
	freePath := filepath.Join(dir, "buffer", "downloads", "track 1 master.wav")
	library := []mediaFile{
		{Path: intendedPath, Rel: "intended.m4a", Key: "intended track"},
		{Path: decoyPath, Rel: "track 1 master.m4a", Key: "track 1 master"},
	}
	freeFiles := []mediaFile{{Path: freePath, Rel: "track 1 master.wav", Key: "track 1 master"}}
	plan := CapturePlan{Rows: []PlanRow{{
		RemoteID:  "track-1",
		LocalPath: intendedPath,
	}}}
	state := []captureStateEntry{{TrackID: "track-1", Path: "track 1 master.wav"}}

	assignments := buildPromotionAssignments(library, freeFiles, plan, state, filepath.Join(dir, "buffer", "downloads"), 72, 8)
	if len(assignments) != 1 {
		t.Fatalf("expected only the identity assignment, got %+v", assignments)
	}
	if assignments[0].library.Path != intendedPath {
		t.Fatalf("expected captured download to stay assigned to intended track, got %+v", assignments[0])
	}
}

func TestBuildPromotionAssignmentsFallsBackToSemanticMatching(t *testing.T) {
	assignments := buildPromotionAssignments(
		[]mediaFile{{Path: "/library/song.mp3", Rel: "song.mp3", Key: "artist song"}},
		[]mediaFile{{Path: "/buffer/song.wav", Rel: "song.wav", Key: "artist song"}},
		CapturePlan{},
		nil,
		"/buffer",
		72,
		8,
	)
	if len(assignments) != 1 {
		t.Fatalf("expected fallback semantic assignment, got %+v", assignments)
	}
	if assignments[0].score != 90 {
		t.Fatalf("expected semantic match score 90, got %d", assignments[0].score)
	}
}

func TestReadCaptureStateKeepsPathsWithSpaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capture.sync.scdl")
	payload := "# comment\nsoundcloud 123 track 1 master.wav\nsoundcloud 456 nested/other file.aiff\n"
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := readCaptureState(path)
	if err != nil {
		t.Fatalf("read capture state: %v", err)
	}
	want := []captureStateEntry{
		{TrackID: "123", Path: "track 1 master.wav"},
		{TrackID: "456", Path: "nested/other file.aiff"},
	}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("unexpected entries\nwant: %+v\n got: %+v", want, entries)
	}
}
