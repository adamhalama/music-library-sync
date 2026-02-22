package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSpotifySyncState(t *testing.T) {
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, "spotify.sync")
	payload := `# udl spotify state v2
1abc234def
 spotify 2abc234def
 https://open.spotify.com/track/3abc234def?si=redacted
4abc234def	title=Regent+-+Permean	path=Regent%2FPermean.mp3
bad-line
`
	if err := os.WriteFile(statePath, []byte(payload), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	state, err := parseSpotifySyncState(statePath)
	if err != nil {
		t.Fatalf("parse state file: %v", err)
	}
	if len(state.KnownIDs) != 4 {
		t.Fatalf("expected 4 known ids, got %d", len(state.KnownIDs))
	}
	entry, ok := state.Entries["4abc234def"]
	if !ok {
		t.Fatalf("expected metadata entry for id 4abc234def")
	}
	if entry.DisplayName != "Regent - Permean" {
		t.Fatalf("unexpected parsed display name: %q", entry.DisplayName)
	}
	if entry.LocalPath != "Regent/Permean.mp3" {
		t.Fatalf("unexpected parsed local path: %q", entry.LocalPath)
	}
}

func TestAppendSpotifySyncStateID(t *testing.T) {
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, "spotify.sync")

	if err := appendSpotifySyncStateID(statePath, "1abc234def"); err != nil {
		t.Fatalf("append id: %v", err)
	}
	if err := appendSpotifySyncStateID(statePath, "https://open.spotify.com/track/2abc234def"); err != nil {
		t.Fatalf("append id from url: %v", err)
	}

	payload, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	text := string(payload)
	if text == "" {
		t.Fatalf("expected state content")
	}
	if text[0] != '#' {
		t.Fatalf("expected state file header, got %q", text)
	}

	state, err := parseSpotifySyncState(statePath)
	if err != nil {
		t.Fatalf("parse state: %v", err)
	}
	if len(state.KnownIDs) != 2 {
		t.Fatalf("expected 2 known ids, got %d", len(state.KnownIDs))
	}
}

func TestAppendSpotifySyncStateEntry(t *testing.T) {
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, "spotify.sync")

	if err := appendSpotifySyncStateEntry(statePath, "41gXFhitx4whS6PsoXREzy", "Regent - Permean", "spotify/Regent - Permean.mp3"); err != nil {
		t.Fatalf("append entry: %v", err)
	}

	state, err := parseSpotifySyncState(statePath)
	if err != nil {
		t.Fatalf("parse state: %v", err)
	}
	if _, ok := state.KnownIDs["41gXFhitx4whS6PsoXREzy"]; !ok {
		t.Fatalf("expected known id entry")
	}
	entry, ok := state.Entries["41gXFhitx4whS6PsoXREzy"]
	if !ok {
		t.Fatalf("expected metadata entry")
	}
	if entry.DisplayName != "Regent - Permean" {
		t.Fatalf("unexpected display name %q", entry.DisplayName)
	}
	if entry.LocalPath != "spotify/Regent - Permean.mp3" {
		t.Fatalf("unexpected local path %q", entry.LocalPath)
	}
}
