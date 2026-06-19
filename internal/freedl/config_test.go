package freedl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/config"
)

func TestLoadExplicitFeatureConfigOverridesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "freedl.yaml")
	payload := `
version: 1
defaults:
  plan_limit: 12
  download_order: newest_first
  target_format: mp3-320
jobs:
  - id: sc-upgrades
    enabled: true
    source_url: https://soundcloud.com/example/likes
    library_dir: /tmp/library
    buffer_dir: /tmp/buffer
    backup_dir: /tmp/backups
    log_dir: /tmp/logs
    state_file: sc-upgrades.sync.scdl
`
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(LoadOptions{
		ExplicitPath: path,
		MainConfig: config.Config{
			Defaults: config.Defaults{StateDir: dir, CommandTimeoutSeconds: 30},
		},
	})
	if err != nil {
		t.Fatalf("load feature config: %v", err)
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("validate feature config: %v", err)
	}
	jobs := EnabledJobs(cfg)
	if len(jobs) != 1 {
		t.Fatalf("expected one enabled job, got %d", len(jobs))
	}
	job := jobs[0]
	if job.ID != "sc-upgrades" || job.PlanLimit != 12 || job.DownloadOrder != "newest_first" || job.TargetFormat != TargetMP3320 {
		t.Fatalf("unexpected job: %+v", job)
	}
}

func TestLoadFallsBackToLegacySCDLFreeDLSource(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	main := config.DefaultConfig()
	main.Defaults.StateDir = dir
	main.Sources = []config.Source{{
		ID:        "legacy",
		Type:      config.SourceTypeSoundCloud,
		Enabled:   true,
		URL:       "https://soundcloud.com/example/likes",
		TargetDir: "/tmp/library",
		StateFile: "legacy.sync.scdl",
		Adapter:   config.AdapterSpec{Kind: "scdl-freedl"},
	}}

	cfg, err := Load(LoadOptions{
		WorkingDir: dir,
		Env:        map[string]string{},
		MainConfig: main,
	})
	if err != nil {
		t.Fatalf("load legacy config: %v", err)
	}
	jobs := EnabledJobs(cfg)
	if len(jobs) != 1 {
		t.Fatalf("expected one legacy job, got %d", len(jobs))
	}
	if jobs[0].ID != "legacy" || jobs[0].LibraryDir != "/tmp/library" {
		t.Fatalf("unexpected legacy job: %+v", jobs[0])
	}
}

func TestValidateRejectsInvalidFormatAndOrder(t *testing.T) {
	cfg := Config{
		Version: 1,
		Jobs: []Job{{
			ID:            "bad",
			Enabled:       true,
			SourceURL:     "https://soundcloud.com/example/likes",
			LibraryDir:    "/tmp/library",
			BufferDir:     "/tmp/buffer",
			BackupDir:     "/tmp/backups",
			LogDir:        "/tmp/logs",
			StateFile:     "bad.sync.scdl",
			PlanLimit:     1,
			DownloadOrder: "sideways",
			TargetFormat:  "flac",
			MinMatchScore: 80,
			AmbiguityGap:  4,
		}},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation error")
	}
	message := err.Error()
	if !strings.Contains(message, "download_order") || !strings.Contains(message, "target_format") {
		t.Fatalf("expected format and order problems, got %q", message)
	}
}
