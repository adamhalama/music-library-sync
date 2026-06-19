package cli

import (
	"bytes"
	"os"
	"path/filepath"
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

func TestRekordboxConfigPathCommandUsesExplicitPath(t *testing.T) {
	var out bytes.Buffer
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: &out, ErrOut: &bytes.Buffer{}},
	}
	path := filepath.Join(t.TempDir(), "rb.yaml")
	root := newRootCommand(app)
	root.SetArgs([]string{"--rekordbox-config", path, "rekordbox", "config", "path"})

	if err := root.Execute(); err != nil {
		t.Fatalf("config path failed: %v", err)
	}
	if strings.TrimSpace(out.String()) != path {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestRekordboxConfigShowCommandPrintsFeatureConfig(t *testing.T) {
	tmp := t.TempDir()
	rbPath := filepath.Join(tmp, "rb.yaml")
	if err := os.WriteFile(rbPath, []byte(`
version: 1
sync:
  folders:
    - id: phone
      music_folder: Phone
      rekordbox_folder: Phone RB
`), 0o644); err != nil {
		t.Fatalf("write rb config: %v", err)
	}
	var out bytes.Buffer
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: &out, ErrOut: &bytes.Buffer{}},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{"--rekordbox-config", rbPath, "rekordbox", "config", "show"})

	if err := root.Execute(); err != nil {
		t.Fatalf("config show failed: %v", err)
	}
	text := out.String()
	for _, want := range []string{"Path: " + rbPath, "Folder mappings: 1", "id: phone"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in output:\n%s", want, text)
		}
	}
}
