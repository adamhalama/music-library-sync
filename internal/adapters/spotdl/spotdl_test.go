package spotdl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/config"
)

func TestBuildExecSpecWithExistingStateFile(t *testing.T) {
	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	stateDir := filepath.Join(tmp, "state")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}

	stateFile := filepath.Join(stateDir, "playlist.sync.spotdl")
	if err := os.WriteFile(stateFile, []byte("data"), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	adapter := New()
	spec, err := adapter.BuildExecSpec(config.Source{
		ID:        "spotify-a",
		Type:      config.SourceTypeSpotify,
		TargetDir: targetDir,
		URL:       "https://open.spotify.com/playlist/a",
		StateFile: "playlist.sync.spotdl",
		Adapter:   config.AdapterSpec{Kind: "spotdl", ExtraArgs: []string{"--headless"}},
	}, config.Defaults{StateDir: stateDir, ArchiveFile: "archive.txt", Threads: 1}, 2*time.Minute)
	if err != nil {
		t.Fatalf("build exec spec: %v", err)
	}

	if len(spec.Args) < 2 || spec.Args[0] != "sync" || spec.Args[1] != stateFile {
		t.Fatalf("expected sync command with state file, got %v", spec.Args)
	}
	if strings.Contains(strings.Join(spec.Args, " "), "--save-file") {
		t.Fatalf("did not expect --save-file when state exists")
	}
}

func TestBuildExecSpecWithMissingStateFile(t *testing.T) {
	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	stateDir := filepath.Join(tmp, "state")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}

	adapter := New()
	spec, err := adapter.BuildExecSpec(config.Source{
		ID:        "spotify-b",
		Type:      config.SourceTypeSpotify,
		TargetDir: targetDir,
		URL:       "https://open.spotify.com/playlist/b",
		StateFile: "playlist.sync.spotdl",
		Adapter:   config.AdapterSpec{Kind: "spotdl"},
	}, config.Defaults{StateDir: stateDir, ArchiveFile: "archive.txt", Threads: 1}, 2*time.Minute)
	if err != nil {
		t.Fatalf("build exec spec: %v", err)
	}

	joined := strings.Join(spec.Args, " ")
	if !strings.Contains(joined, "--save-file") {
		t.Fatalf("expected --save-file when state is missing, got %v", spec.Args)
	}
}
