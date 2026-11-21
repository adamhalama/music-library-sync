package config

import (
	"strings"
	"testing"
)

func TestSoundCloudSyncPoliciesSurviveCanonicalSave(t *testing.T) {
	payload := []byte(`version: 1
sources:
  - id: sample
    type: soundcloud
    enabled: true
    target_dir: /tmp/sample
    url: https://soundcloud.com/example/sets/sample
    state_file: source.scdl
    sync:
      append_only: true
      download_order: oldest_first
    adapter:
      kind: scdl
`)
	cfg, err := ParseSingleFile("/tmp/config.yaml", payload)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Sources[0].Sync.AppendOnly || cfg.Sources[0].Sync.DownloadOrder != "oldest_first" {
		t.Fatalf("settings not loaded: %+v", cfg.Sources[0].Sync)
	}
	saved, err := MarshalCanonical(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range []string{"append_only: true", "download_order: oldest_first"} {
		if !strings.Contains(string(saved), setting) {
			t.Fatalf("setting %q lost on save: %s", setting, saved)
		}
	}
	reloaded, err := ParseSingleFile("/tmp/config.yaml", saved)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Sources[0].Sync.AppendOnly || reloaded.Sources[0].Sync.DownloadOrder != "oldest_first" {
		t.Fatal("settings lost on reload")
	}
}
