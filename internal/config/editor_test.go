package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSingleFileIgnoresPrecedenceAndEnv(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("UDL_STATE_DIR", filepath.Join(tmp, "env-state"))

	userPath := filepath.Join(tmp, "editor.yaml")
	projectPath := filepath.Join(tmp, "udl.yaml")
	userPayload := `version: 1
defaults:
  state_dir: "` + filepath.Join(tmp, "user-state") + `"
  archive_file: "archive.txt"
  threads: 2
  continue_on_error: true
  command_timeout_seconds: 900
sources:
  - id: "user-source"
    type: "spotify"
    enabled: true
    target_dir: "/tmp/user"
    url: "https://open.spotify.com/playlist/user"
    state_file: "user.sync.spotify"
    adapter:
      kind: "deemix"
`
	projectPayload := `version: 1
defaults:
  state_dir: "` + filepath.Join(tmp, "project-state") + `"
sources:
  - id: "project-source"
    type: "soundcloud"
    enabled: true
    target_dir: "/tmp/project"
    url: "https://soundcloud.com/project"
    state_file: "project.sync.scdl"
    adapter:
      kind: "scdl"
`
	if err := os.WriteFile(userPath, []byte(userPayload), 0o644); err != nil {
		t.Fatalf("write user payload: %v", err)
	}
	if err := os.WriteFile(projectPath, []byte(projectPayload), 0o644); err != nil {
		t.Fatalf("write project payload: %v", err)
	}

	cfg, err := LoadSingleFile(userPath)
	if err != nil {
		t.Fatalf("LoadSingleFile: %v", err)
	}
	if cfg.Defaults.StateDir != filepath.Join(tmp, "user-state") {
		t.Fatalf("expected single-file state dir, got %q", cfg.Defaults.StateDir)
	}
	if len(cfg.Sources) != 1 || cfg.Sources[0].ID != "user-source" {
		t.Fatalf("expected single-file source set, got %+v", cfg.Sources)
	}
}

func TestMarshalCanonicalNormalizesAndFormatsConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Defaults.StateDir = "/tmp/udl-state"
	cfg.Sources = []Source{
		{
			ID:        "soundcloud-likes",
			Type:      SourceTypeSoundCloud,
			Enabled:   true,
			TargetDir: "~/Music/downloaded/sc-likes",
			URL:       "https://soundcloud.com/user",
			Adapter: AdapterSpec{
				Kind: "scdl",
			},
		},
	}

	payload, err := MarshalCanonical(cfg)
	if err != nil {
		t.Fatalf("MarshalCanonical: %v", err)
	}
	text := string(payload)
	if !strings.Contains(text, "state_file: soundcloud-likes.sync.scdl") {
		t.Fatalf("expected normalized state_file, got:\n%s", text)
	}
	if !strings.Contains(text, "break_on_existing: true") || !strings.Contains(text, "ask_on_existing: false") || !strings.Contains(text, "local_index_cache: false") {
		t.Fatalf("expected normalized sync defaults, got:\n%s", text)
	}
	if !strings.Contains(text, "  state_dir: /tmp/udl-state") {
		t.Fatalf("expected two-space indentation, got:\n%s", text)
	}
}

func TestSaveSingleFileWritesConfigAndEnsuresStateDir(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "udl.yaml")
	stateDir := filepath.Join(tmp, "state")

	cfg := DefaultConfig()
	cfg.Defaults.StateDir = stateDir
	cfg.Sources = []Source{
		{
			ID:        "spotify-list",
			Type:      SourceTypeSpotify,
			Enabled:   true,
			TargetDir: "/tmp/music",
			URL:       "https://open.spotify.com/playlist/abc",
			StateFile: "spotify-list.sync.spotify",
			Adapter: AdapterSpec{
				Kind: "deemix",
			},
			Sync: SyncPolicy{
				BreakOnExisting: boolPtr(true),
				AskOnExisting:   boolPtr(false),
			},
		},
	}

	ensuredStateDir, err := SaveSingleFile(target, cfg)
	if err != nil {
		t.Fatalf("SaveSingleFile: %v", err)
	}
	if ensuredStateDir != stateDir {
		t.Fatalf("expected ensured state dir %q, got %q", stateDir, ensuredStateDir)
	}
	if _, err := os.Stat(stateDir); err != nil {
		t.Fatalf("expected state dir to exist: %v", err)
	}
	payload, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if !strings.Contains(string(payload), "spotify-list") {
		t.Fatalf("expected saved source in config, got:\n%s", string(payload))
	}
}

func TestSaveSingleFileRestoresOriginalOnReplaceFailure(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "udl.yaml")
	original := "version: 1\n"
	if err := os.WriteFile(target, []byte(original), 0o644); err != nil {
		t.Fatalf("write original config: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Defaults.StateDir = filepath.Join(tmp, "state")
	cfg.Sources = []Source{
		{
			ID:        "soundcloud-likes",
			Type:      SourceTypeSoundCloud,
			Enabled:   true,
			TargetDir: "/tmp/music",
			URL:       "https://soundcloud.com/user",
			Adapter:   AdapterSpec{Kind: "scdl"},
		},
	}

	origReplace := replaceTempFile
	replaceTempFile = func(tempPath string, targetPath string) error {
		return errors.New("boom")
	}
	t.Cleanup(func() {
		replaceTempFile = origReplace
	})

	if _, err := SaveSingleFile(target, cfg); err == nil {
		t.Fatalf("expected replace failure")
	}
	payload, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read original config: %v", err)
	}
	if string(payload) != original {
		t.Fatalf("expected original config to remain untouched, got:\n%s", string(payload))
	}
}

func TestSaveSingleFileRejectsDirectoryTarget(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "udl")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Defaults.StateDir = filepath.Join(tmp, "state")
	cfg.Sources = []Source{
		{
			ID:        "soundcloud-likes",
			Type:      SourceTypeSoundCloud,
			Enabled:   true,
			TargetDir: "/tmp/music",
			URL:       "https://soundcloud.com/user",
			Adapter:   AdapterSpec{Kind: "scdl"},
		},
	}

	if _, err := SaveSingleFile(target, cfg); err == nil {
		t.Fatalf("expected directory target rejection")
	} else if !strings.Contains(err.Error(), "directory") {
		t.Fatalf("expected directory error, got %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected target to remain a directory")
	}
	if _, err := os.Stat(target + ".udl.bak"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no backup to be created, stat err: %v", err)
	}
}
