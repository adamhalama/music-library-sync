package engine

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/output"
)

func TestSyncPlanModeRunsSelectorPerSupportedSourceInOrderAndSkipsUnsupported(t *testing.T) {
	tmp := t.TempDir()
	targetA, stateA := syncerTestDirs(t, tmp, "a")
	targetB, _ := syncerTestDirs(t, tmp, "b")
	targetC, _ := syncerTestDirs(t, tmp, "c")

	cfg := config.Config{
		Version: 1,
		Defaults: config.Defaults{
			StateDir:              stateA,
			ArchiveFile:           "archive.txt",
			ContinueOnError:       true,
			CommandTimeoutSeconds: 900,
		},
		Sources: []config.Source{
			{
				ID:        "scdl-a",
				Type:      config.SourceTypeSoundCloud,
				Enabled:   true,
				TargetDir: targetA,
				URL:       "https://soundcloud.com/a",
				StateFile: "scdl-a.sync.scdl",
				Adapter:   config.AdapterSpec{Kind: "scdl"},
			},
			{
				ID:        "spotify-legacy",
				Type:      config.SourceTypeSpotify,
				Enabled:   true,
				TargetDir: targetC,
				URL:       "https://open.spotify.com/playlist/test",
				StateFile: "spotify-legacy.sync.spotdl",
				Adapter:   config.AdapterSpec{Kind: "spotdl"},
			},
			{
				ID:        "scdl-b",
				Type:      config.SourceTypeSoundCloud,
				Enabled:   true,
				TargetDir: targetB,
				URL:       "https://soundcloud.com/b",
				StateFile: "scdl-b.sync.scdl",
				Adapter:   config.AdapterSpec{Kind: "scdl"},
			},
		},
	}
	if err := writePlanStateFixtures(cfg, stateA); err != nil {
		t.Fatalf("write fixtures: %v", err)
	}

	origEnumerateLimited := enumerateSoundCloudTracksWithLimitFn
	t.Cleanup(func() {
		enumerateSoundCloudTracksWithLimitFn = origEnumerateLimited
	})
	enumerateSoundCloudTracksWithLimitFn = func(ctx context.Context, source config.Source, limit int) ([]soundCloudRemoteTrack, error) {
		return []soundCloudRemoteTrack{{ID: "track-new", Title: "Track New"}}, nil
	}

	selectorOrder := []string{}
	stdout := &bytes.Buffer{}
	syncer := NewSyncer(
		map[string]Adapter{
			"scdl":   fakeAdapter{},
			"spotdl": fakeSpotifyAdapter{},
		},
		noOpRunner{},
		output.NewJSONEmitter(stdout),
	)

	result, err := syncer.Sync(context.Background(), cfg, SyncOptions{
		Plan:      true,
		PlanLimit: 10,
		DryRun:    true,
		SelectPlanRows: func(sourceID string, rows []PlanRow) (PlanSelectionResult, error) {
			selectorOrder = append(selectorOrder, sourceID)
			return PlanSelectionResult{
				Manifest: testExecutionManifest(t, sourceID, rows, []int{1}, DownloadOrderNewestFirst),
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if want := []string{"scdl-a", "scdl-b"}; strings.Join(selectorOrder, ",") != strings.Join(want, ",") {
		t.Fatalf("expected selector order %v, got %v", want, selectorOrder)
	}
	if result.Succeeded != 2 || result.Skipped != 1 || result.Failed != 0 {
		t.Fatalf("unexpected sync result: %+v", result)
	}
	if !strings.Contains(stdout.String(), "--plan only supports adapter.kind=scdl") {
		t.Fatalf("expected unsupported-source warning in output, got %s", stdout.String())
	}
}

func TestSyncPlanModeCancelReturnsInterrupted(t *testing.T) {
	tmp := t.TempDir()
	targetDir, stateDir := syncerTestDirs(t, tmp, "cancel")

	cfg := config.Config{
		Version: 1,
		Defaults: config.Defaults{
			StateDir:              stateDir,
			ArchiveFile:           "archive.txt",
			ContinueOnError:       true,
			CommandTimeoutSeconds: 900,
		},
		Sources: []config.Source{
			{
				ID:        "scdl-cancel",
				Type:      config.SourceTypeSoundCloud,
				Enabled:   true,
				TargetDir: targetDir,
				URL:       "https://soundcloud.com/cancel",
				StateFile: "scdl-cancel.sync.scdl",
				Adapter:   config.AdapterSpec{Kind: "scdl"},
			},
		},
	}
	if err := writePlanStateFixtures(cfg, stateDir); err != nil {
		t.Fatalf("write fixtures: %v", err)
	}

	origEnumerateLimited := enumerateSoundCloudTracksWithLimitFn
	t.Cleanup(func() {
		enumerateSoundCloudTracksWithLimitFn = origEnumerateLimited
	})
	enumerateSoundCloudTracksWithLimitFn = func(ctx context.Context, source config.Source, limit int) ([]soundCloudRemoteTrack, error) {
		return []soundCloudRemoteTrack{{ID: "track-new", Title: "Track New"}}, nil
	}

	syncer := NewSyncer(map[string]Adapter{"scdl": fakeAdapter{}}, noOpRunner{}, output.NewHumanEmitter(&bytes.Buffer{}, &bytes.Buffer{}, false, true))
	result, err := syncer.Sync(context.Background(), cfg, SyncOptions{
		Plan:      true,
		PlanLimit: 10,
		SelectPlanRows: func(sourceID string, rows []PlanRow) (PlanSelectionResult, error) {
			return PlanSelectionResult{Canceled: true}, nil
		},
	})
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("expected ErrInterrupted, got %v", err)
	}
	if !result.Interrupted {
		t.Fatalf("expected interrupted result, got %+v", result)
	}
	if result.Attempted != 0 {
		t.Fatalf("expected no attempted sources after cancel, got %+v", result)
	}
}

func TestSyncPlanModeDoesNotDeleteSelectedKnownGapIfAdapterDoesNotRewriteState(t *testing.T) {
	tmp := t.TempDir()
	targetDir, stateDir := syncerTestDirs(t, tmp, "plan-merge")

	cfg := config.Config{
		Version: 1,
		Defaults: config.Defaults{
			StateDir:              stateDir,
			ArchiveFile:           "archive.txt",
			ContinueOnError:       true,
			CommandTimeoutSeconds: 900,
		},
		Sources: []config.Source{
			{
				ID:        "scdl-plan",
				Type:      config.SourceTypeSoundCloud,
				Enabled:   true,
				TargetDir: targetDir,
				URL:       "https://soundcloud.com/plan",
				StateFile: "scdl-plan.sync.scdl",
				Adapter:   config.AdapterSpec{Kind: "scdl"},
			},
		},
	}

	statePath := filepath.Join(stateDir, "scdl-plan.sync.scdl")
	if err := os.WriteFile(statePath, []byte("soundcloud gap-a missing-a.m4a\n"), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	archivePath := filepath.Join(stateDir, "scdl-plan.archive.txt")
	if err := os.WriteFile(archivePath, []byte("soundcloud gap-a\n"), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	origEnumerateLimited := enumerateSoundCloudTracksWithLimitFn
	t.Cleanup(func() {
		enumerateSoundCloudTracksWithLimitFn = origEnumerateLimited
	})
	enumerateSoundCloudTracksWithLimitFn = func(ctx context.Context, source config.Source, limit int) ([]soundCloudRemoteTrack, error) {
		return []soundCloudRemoteTrack{{ID: "gap-a", Title: "Gap A"}}, nil
	}

	syncer := NewSyncer(
		map[string]Adapter{"scdl": fakeAdapter{}},
		noOpRunner{},
		output.NewHumanEmitter(&bytes.Buffer{}, &bytes.Buffer{}, false, true),
	)

	result, err := syncer.Sync(context.Background(), cfg, SyncOptions{
		Plan:      true,
		PlanLimit: 10,
		SelectPlanRows: func(sourceID string, rows []PlanRow) (PlanSelectionResult, error) {
			return PlanSelectionResult{
				Manifest: testExecutionManifest(t, sourceID, rows, []int{1}, DownloadOrderNewestFirst),
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Succeeded != 1 || result.Failed != 0 {
		t.Fatalf("unexpected sync result: %+v", result)
	}

	state, err := parseSoundCloudSyncState(statePath)
	if err != nil {
		t.Fatalf("parse state: %v", err)
	}
	if _, ok := state.ByID["gap-a"]; !ok {
		t.Fatalf("expected selected known gap to remain in state, got %+v", state.ByID)
	}
	archive, err := parseSoundCloudArchive(archivePath)
	if err != nil {
		t.Fatalf("parse archive: %v", err)
	}
	if _, ok := archive["gap-a"]; !ok {
		t.Fatalf("expected selected known gap to remain in archive, got %+v", archive)
	}
}

func syncerTestDirs(t *testing.T, root, suffix string) (targetDir, stateDir string) {
	t.Helper()
	targetDir = root + "/target-" + suffix
	stateDir = root + "/state-" + suffix
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	return targetDir, stateDir
}

func writePlanStateFixtures(cfg config.Config, stateDir string) error {
	for _, source := range cfg.Sources {
		if source.Type != config.SourceTypeSoundCloud {
			continue
		}
		statePath := filepath.Join(stateDir, source.StateFile)
		if err := os.WriteFile(statePath, []byte(""), 0o644); err != nil {
			return err
		}
		archivePath := filepath.Join(stateDir, source.ID+".archive.txt")
		if err := os.WriteFile(archivePath, []byte(""), 0o644); err != nil {
			return err
		}
	}
	return nil
}
