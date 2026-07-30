package freedl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/config"
)

func TestMarshalCanonicalFeatureConfig(t *testing.T) {
	main := config.DefaultConfig()
	cfg := Config{
		Version: 1,
		Defaults: Defaults{
			PlanLimit:         25,
			DownloadOrder:     DefaultDownloadOrder,
			TargetFormat:      TargetMP3320,
			MinMatchScore:     80,
			AmbiguityGap:      9,
			CommandTimeoutSec: 60,
		},
		Jobs: []Job{{
			ID:            "upgrades",
			Enabled:       true,
			SourceURL:     "https://soundcloud.com/example/likes",
			LibraryDir:    "/tmp/library",
			BufferDir:     "/tmp/buffer",
			BackupDir:     "/tmp/backups",
			LogDir:        "/tmp/logs",
			StateFile:     "upgrades.sync.scdl",
			PlanLimit:     25,
			DownloadOrder: DefaultDownloadOrder,
			TargetFormat:  TargetMP3320,
			MinMatchScore: 80,
			AmbiguityGap:  9,
		}},
	}

	payload, err := MarshalCanonical(cfg, main)
	if err != nil {
		t.Fatalf("marshal canonical: %v", err)
	}
	text := string(payload)
	for _, token := range []string{
		"version: 1",
		"plan_limit: 25",
		"target_format: mp3-320",
		"source_url: https://soundcloud.com/example/likes",
		"backup_dir: /tmp/backups",
	} {
		if !strings.Contains(text, token) {
			t.Fatalf("canonical yaml missing %q:\n%s", token, text)
		}
	}
}

func TestSaveAndLoadSingleFeatureConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "freedl.yaml")
	main := config.DefaultConfig()
	main.Defaults.StateDir = filepath.Join(dir, "state")
	cfg := Config{
		Version: 1,
		Defaults: Defaults{
			PlanLimit:     10,
			DownloadOrder: DefaultDownloadOrder,
			TargetFormat:  DefaultTargetFormat,
			MinMatchScore: DefaultMinMatchScore,
			AmbiguityGap:  DefaultAmbiguityGap,
		},
		Jobs: []Job{{
			ID:            "upgrades",
			Enabled:       true,
			SourceURL:     "https://soundcloud.com/example/likes",
			LibraryDir:    "/tmp/library",
			BufferDir:     "/tmp/buffer",
			BackupDir:     "/tmp/backups",
			LogDir:        "/tmp/logs",
			StateFile:     "upgrades.sync.scdl",
			PlanLimit:     10,
			DownloadOrder: DefaultDownloadOrder,
			TargetFormat:  DefaultTargetFormat,
			MinMatchScore: DefaultMinMatchScore,
			AmbiguityGap:  DefaultAmbiguityGap,
		}},
	}

	if err := SaveSingleFile(path, cfg, main); err != nil {
		t.Fatalf("save single feature config: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected saved file: %v", err)
	}
	loaded, err := LoadSingleFile(path, main)
	if err != nil {
		t.Fatalf("load single feature config: %v", err)
	}
	if len(loaded.Jobs) != 1 || loaded.Jobs[0].ID != "upgrades" {
		t.Fatalf("unexpected loaded jobs: %+v", loaded.Jobs)
	}
}
