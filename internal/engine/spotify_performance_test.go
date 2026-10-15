package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSpotifyMediaSnapshotTrackerUsesOneInitialAndOneWalkPerInvocation(t *testing.T) {
	original := snapshotSpotifyMediaFilesFn
	t.Cleanup(func() { snapshotSpotifyMediaFilesFn = original })

	sequence := []map[string]mediaFileSnapshot{
		{},
		{"a.mp3": {Size: 1, ModTime: time.Unix(1, 0)}},
		{"a.mp3": {Size: 1, ModTime: time.Unix(1, 0)}},
		{
			"a.mp3": {Size: 1, ModTime: time.Unix(1, 0)},
			"b.mp3": {Size: 2, ModTime: time.Unix(2, 0)},
		},
	}
	walks := 0
	snapshotSpotifyMediaFilesFn = func(string) (map[string]mediaFileSnapshot, error) {
		if walks >= len(sequence) {
			t.Fatalf("unexpected media walk %d", walks+1)
		}
		result := sequence[walks]
		walks++
		return result, nil
	}

	tracker, err := newSpotifyMediaSnapshotTracker("/music")
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := tracker.Advance(); err != nil || changed != "a.mp3" {
		t.Fatalf("first invocation changed=%q err=%v", changed, err)
	}
	// Represents an unavailable/failed invocation: its baseline still advances
	// even though the caller intentionally ignores the empty attribution.
	if changed, err := tracker.Advance(); err != nil || changed != "" {
		t.Fatalf("failed invocation changed=%q err=%v", changed, err)
	}
	if changed, err := tracker.Advance(); err != nil || changed != "b.mp3" {
		t.Fatalf("third invocation changed=%q err=%v", changed, err)
	}
	if walks != 4 {
		t.Fatalf("walk count = %d, want one initial + three post-invocation", walks)
	}
}

func TestSpotifyMediaSnapshotTrackerDisablesAttributionAfterWalkFailure(t *testing.T) {
	original := snapshotSpotifyMediaFilesFn
	t.Cleanup(func() { snapshotSpotifyMediaFilesFn = original })
	walks := 0
	snapshotSpotifyMediaFilesFn = func(string) (map[string]mediaFileSnapshot, error) {
		walks++
		if walks == 2 {
			return nil, errors.New("walk failed")
		}
		return map[string]mediaFileSnapshot{}, nil
	}
	tracker, err := newSpotifyMediaSnapshotTracker("/music")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.Advance(); err == nil {
		t.Fatal("expected post-invocation walk failure")
	}
	if changed, err := tracker.Advance(); err != nil || changed != "" {
		t.Fatalf("disabled tracker changed=%q err=%v", changed, err)
	}
	if walks != 2 {
		t.Fatalf("disabled tracker performed %d walks, want 2", walks)
	}
}

func TestSpotifyStateWriterReadsOnceAndPersistsEveryTrackAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spotify.sync.spotify")
	initial := strings.Join([]string{
		"# user comment",
		"1abc234def",
		"unknown directive stays",
		"1abc234def\ttitle=duplicate",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(initial), 0o640); err != nil {
		t.Fatal(err)
	}

	originalRead := readSpotifyStateWriterFileFn
	t.Cleanup(func() { readSpotifyStateWriterFileFn = originalRead })
	reads := 0
	readSpotifyStateWriterFileFn = func(path string) ([]byte, error) {
		reads++
		return os.ReadFile(path)
	}
	writer, err := newSpotifyStateWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []spotifyStateBackfillEntry{
		{ID: "1abc234def", DisplayName: "One", LocalPath: "/music/one.mp3"},
		{ID: "2abc234def", DisplayName: "Two", LocalPath: "/music/two.mp3"},
		{ID: "3abc234def", DisplayName: "Three", LocalPath: "/music/three.mp3"},
	} {
		if err := writer.Upsert(entry.ID, entry.DisplayName, entry.LocalPath); err != nil {
			t.Fatal(err)
		}
		state, err := parseSpotifySyncState(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := state.KnownIDs[entry.ID]; !exists {
			t.Fatalf("track %s was not durable immediately after Upsert", entry.ID)
		}
	}
	if reads != 1 {
		t.Fatalf("writer read state %d times, want exactly once", reads)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	if !strings.Contains(text, "# user comment") || !strings.Contains(text, "unknown directive stays") {
		t.Fatalf("comments/unknown lines were not preserved:\n%s", text)
	}
	if strings.Count(text, "1abc234def") != 1 {
		t.Fatalf("duplicate handling drifted:\n%s", text)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o, want 640", info.Mode().Perm())
	}
}

func TestSpotifyStateWriterKeepsNewFileHeaderAcrossWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.sync.spotify")
	writer, err := newSpotifyStateWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"1abc234def", "2abc234def"} {
		if err := writer.Upsert(id, "Track "+id, ""); err != nil {
			t.Fatal(err)
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(payload), "# udl spotify state v2") != 1 {
			t.Fatalf("v2 header drifted after writing %s:\n%s", id, payload)
		}
	}
}

func TestSpotifyStateWriterPreservesSpotDLJSONPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hybrid.sync.spotdl")
	prefix := `{"songs":[{"song_id":"1abc234def","name":"One","artist":"Artist"}]}`
	if err := os.WriteFile(path, []byte(prefix+"\n# trailing udl state\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writer, err := newSpotifyStateWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Upsert("2abc234def", "Two", "/music/two.mp3"); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(payload), prefix) || !strings.Contains(string(payload), "# trailing udl state") {
		t.Fatalf("hybrid prefix/layout drifted:\n%s", payload)
	}
	state, err := parseSpotifySyncState(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"1abc234def", "2abc234def"} {
		if _, ok := state.KnownIDs[id]; !ok {
			t.Fatalf("hybrid state lost %s: %+v", id, state.KnownIDs)
		}
	}
}
