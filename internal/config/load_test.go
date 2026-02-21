package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPrecedence(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg"))

	userConfigPath, err := UserConfigPath()
	if err != nil {
		t.Fatalf("user config path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(userConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir user config dir: %v", err)
	}

	userConfig := `version: 1
defaults:
  threads: 2
  archive_file: "user-archive.txt"
sources:
  - id: "user-source"
    type: "spotify"
    enabled: true
    target_dir: "/tmp/user-target"
    url: "https://open.spotify.com/playlist/user"
    state_file: "user.sync.spotdl"
    adapter:
      kind: "spotdl"
`
	if err := os.WriteFile(userConfigPath, []byte(userConfig), 0o644); err != nil {
		t.Fatalf("write user config: %v", err)
	}

	projectDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}
	projectConfigPath := filepath.Join(projectDir, "udl.yaml")
	projectConfig := `version: 1
defaults:
  archive_file: "project-archive.txt"
sources:
  - id: "project-source"
    type: "spotify"
    enabled: true
    target_dir: "/tmp/project-target"
    url: "https://open.spotify.com/playlist/project"
    state_file: "project.sync.spotdl"
    adapter:
      kind: "spotdl"
`
	if err := os.WriteFile(projectConfigPath, []byte(projectConfig), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	cfg, err := Load(LoadOptions{
		WorkingDir: projectDir,
		Env: map[string]string{
			"UDL_THREADS": "7",
		},
	})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Defaults.Threads != 7 {
		t.Fatalf("expected env override threads=7, got %d", cfg.Defaults.Threads)
	}
	if cfg.Defaults.ArchiveFile != "project-archive.txt" {
		t.Fatalf("expected project archive file, got %q", cfg.Defaults.ArchiveFile)
	}
	if len(cfg.Sources) != 1 || cfg.Sources[0].ID != "project-source" {
		t.Fatalf("expected project sources to override user sources, got %+v", cfg.Sources)
	}
}

func TestLoadExplicitPathRequired(t *testing.T) {
	_, err := Load(LoadOptions{ExplicitPath: "/path/does/not/exist.yaml"})
	if err == nil {
		t.Fatalf("expected error for missing explicit config path")
	}
}
