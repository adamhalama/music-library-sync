package cli

import (
	"path/filepath"
	"testing"

	"github.com/jaa/update-downloads/internal/config"
)

func TestBuildPlanSourceDetails(t *testing.T) {
	tmp := t.TempDir()
	stateDir := filepath.Join(tmp, "state")

	source := config.Source{
		ID:        "scdl-source",
		Type:      config.SourceTypeSoundCloud,
		TargetDir: "~/Music",
		URL:       "https://soundcloud.com/user/likes?utm_source=test",
		StateFile: "scdl-source.sync.scdl",
		Adapter:   config.AdapterSpec{Kind: "scdl"},
	}
	details := buildPlanSourceDetails(source, config.Defaults{StateDir: stateDir}, 10, true)

	if details.SourceID != "scdl-source" {
		t.Fatalf("expected source id, got %q", details.SourceID)
	}
	if details.SourceType != "soundcloud" {
		t.Fatalf("expected source type soundcloud, got %q", details.SourceType)
	}
	if details.Adapter != "scdl" {
		t.Fatalf("expected adapter scdl, got %q", details.Adapter)
	}
	if details.URL != "https://soundcloud.com/user/likes" {
		t.Fatalf("expected sanitized url without query, got %q", details.URL)
	}
	if details.StateFile == "" || filepath.Base(details.StateFile) != "scdl-source.sync.scdl" {
		t.Fatalf("expected resolved state file path, got %q", details.StateFile)
	}
	if details.PlanLimit != 10 || !details.DryRun {
		t.Fatalf("unexpected plan metadata: %+v", details)
	}
}
