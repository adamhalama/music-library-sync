package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRekordboxPlaylistSyncCommandRegistered(t *testing.T) {
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{"rekordbox", "playlist-sync", "plan", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("help failed: %v", err)
	}
}

func TestRekordboxPlaylistSyncApplyNoInputRequiresForce(t *testing.T) {
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{"rekordbox", "playlist-sync", "apply", "--plan-file", "/tmp/plan.json", "--no-input"})

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected --force error")
	}
	if !strings.Contains(err.Error(), "--force is required with --no-input") {
		t.Fatalf("unexpected error: %v", err)
	}
}
