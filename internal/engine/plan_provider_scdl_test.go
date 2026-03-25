package engine

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/config"
)

func testExecutionManifest(t *testing.T, sourceID string, rows []PlanRow, selected []int, order DownloadOrder) ExecutionManifest {
	t.Helper()
	manifest, err := BuildExecutionManifest(sourceID, rows, selected, order)
	if err != nil {
		t.Fatalf("build execution manifest: %v", err)
	}
	return manifest
}

func TestSCDLPlanProviderBuildClassifiesRowsAndDefaultSelection(t *testing.T) {
	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	stateDir := filepath.Join(tmp, "state")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}

	source := config.Source{
		ID:        "sc-plan",
		Type:      config.SourceTypeSoundCloud,
		Enabled:   true,
		TargetDir: targetDir,
		URL:       "https://soundcloud.com/user",
		StateFile: "sc-plan.sync.scdl",
		Adapter:   config.AdapterSpec{Kind: "scdl"},
	}
	cfg := config.Config{
		Version: 1,
		Defaults: config.Defaults{
			StateDir:    stateDir,
			ArchiveFile: "archive.txt",
		},
		Sources: []config.Source{source},
	}

	statePath := filepath.Join(stateDir, source.StateFile)
	statePayload := strings.Join([]string{
		"soundcloud track-known known.mp3",
		"soundcloud track-gap missing.mp3",
	}, "\n") + "\n"
	if err := os.WriteFile(statePath, []byte(statePayload), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "known.mp3"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write local known file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, source.ID+".archive.txt"), []byte("soundcloud track-known\nsoundcloud track-gap\n"), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	origEnumerateLimited := enumerateSoundCloudTracksWithLimitFn
	t.Cleanup(func() {
		enumerateSoundCloudTracksWithLimitFn = origEnumerateLimited
	})
	gotLimit := -1
	enumerateSoundCloudTracksWithLimitFn = func(ctx context.Context, source config.Source, limit int) ([]soundCloudRemoteTrack, error) {
		gotLimit = limit
		return []soundCloudRemoteTrack{
			{ID: "track-known", Title: "Known"},
			{ID: "track-gap", Title: "Gap"},
			{ID: "track-new", Title: "New"},
		}, nil
	}

	provider := NewSCDLPlanProvider()
	plan, err := provider.Build(context.Background(), cfg, source, SyncOptions{Plan: true, PlanLimit: 10})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}

	rows := plan.Rows()
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if gotLimit != 10 {
		t.Fatalf("expected plan enumeration limit=10, got %d", gotLimit)
	}
	if rows[0].Status != PlanRowAlreadyDownloaded || rows[0].Toggleable || rows[0].SelectedByDefault {
		t.Fatalf("unexpected first row classification: %+v", rows[0])
	}
	if rows[1].Status != PlanRowMissingKnownGap || !rows[1].Toggleable || !rows[1].SelectedByDefault {
		t.Fatalf("unexpected second row classification: %+v", rows[1])
	}
	if rows[2].Status != PlanRowMissingNew || !rows[2].Toggleable || !rows[2].SelectedByDefault {
		t.Fatalf("unexpected third row classification: %+v", rows[2])
	}
}

func TestSCDLPlanProviderAppliesSelectionUsesTempSyncAndFiltersSelectedKnownGap(t *testing.T) {
	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	stateDir := filepath.Join(tmp, "state")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}

	source := config.Source{
		ID:        "sc-plan",
		Type:      config.SourceTypeSoundCloud,
		Enabled:   true,
		TargetDir: targetDir,
		URL:       "https://soundcloud.com/user",
		StateFile: "sc-plan.sync.scdl",
		Adapter:   config.AdapterSpec{Kind: "scdl"},
	}
	cfg := config.Config{
		Version: 1,
		Defaults: config.Defaults{
			StateDir:    stateDir,
			ArchiveFile: "archive.txt",
		},
		Sources: []config.Source{source},
	}

	statePath := filepath.Join(stateDir, source.StateFile)
	statePayload := strings.Join([]string{
		"soundcloud gap-a missing-a.mp3",
		"soundcloud gap-b missing-b.mp3",
	}, "\n") + "\n"
	if err := os.WriteFile(statePath, []byte(statePayload), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	archivePayload := strings.Join([]string{
		"soundcloud gap-a",
		"soundcloud gap-b",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(stateDir, source.ID+".archive.txt"), []byte(archivePayload), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	origEnumerateLimited := enumerateSoundCloudTracksWithLimitFn
	t.Cleanup(func() {
		enumerateSoundCloudTracksWithLimitFn = origEnumerateLimited
	})
	enumerateSoundCloudTracksWithLimitFn = func(ctx context.Context, source config.Source, limit int) ([]soundCloudRemoteTrack, error) {
		return []soundCloudRemoteTrack{
			{ID: "gap-a", Title: "Gap A"},
			{ID: "gap-b", Title: "Gap B"},
		}, nil
	}

	provider := NewSCDLPlanProvider()
	plan, err := provider.Build(context.Background(), cfg, source, SyncOptions{Plan: true, PlanLimit: 10})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}

	execPlan, err := plan.ApplySelection(testExecutionManifest(t, source.ID, plan.Rows(), []int{1}, DownloadOrderNewestFirst), PlanApplyOptions{})
	if err != nil {
		t.Fatalf("apply selection: %v", err)
	}
	if got := execPlan.SourceForExec.SelectedPlaylistIDs; len(got) != 1 || got[0] != 1 {
		t.Fatalf("expected selected playlist index [1], got %v", got)
	}
	if execPlan.SourcePreflight == nil || execPlan.SourcePreflight.PlannedDownloadCount != 1 {
		t.Fatalf("expected planned_download_count=1, got %+v", execPlan.SourcePreflight)
	}
	if execPlan.SourceForExec.DisableSyncMode {
		t.Fatalf("did not expect plan subset execution to disable scdl --sync")
	}
	if execPlan.StateSwap.TempSyncPath == "" {
		t.Fatalf("expected temporary sync file for subset execution, got %+v", execPlan.StateSwap)
	}
	if execPlan.StateSwap.TempArchivePath == "" {
		t.Fatalf("expected temporary archive file for selected known-gap replay")
	}

	filteredState, err := os.ReadFile(execPlan.StateSwap.TempSyncPath)
	if err != nil {
		t.Fatalf("read filtered state: %v", err)
	}
	if strings.TrimSpace(string(filteredState)) != "" {
		t.Fatalf("expected temporary sync file to exclude original state entries, got %q", string(filteredState))
	}

	filteredArchive, err := os.ReadFile(execPlan.StateSwap.TempArchivePath)
	if err != nil {
		t.Fatalf("read filtered archive: %v", err)
	}
	if strings.Contains(string(filteredArchive), "gap-a") {
		t.Fatalf("expected selected known gap gap-a removed from temp archive, got %q", string(filteredArchive))
	}
	if !strings.Contains(string(filteredArchive), "gap-b") {
		t.Fatalf("expected unselected known gap gap-b to remain in temp archive, got %q", string(filteredArchive))
	}
}

func TestSCDLPlanProviderAppliesSelectionOrdersExecutionByDownloadOrder(t *testing.T) {
	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	stateDir := filepath.Join(tmp, "state")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}

	source := config.Source{
		ID:        "sc-plan",
		Type:      config.SourceTypeSoundCloud,
		Enabled:   true,
		TargetDir: targetDir,
		URL:       "https://soundcloud.com/user",
		StateFile: "sc-plan.sync.scdl",
		Adapter:   config.AdapterSpec{Kind: "scdl"},
	}
	cfg := config.Config{
		Version: 1,
		Defaults: config.Defaults{
			StateDir:    stateDir,
			ArchiveFile: "archive.txt",
		},
		Sources: []config.Source{source},
	}

	if err := os.WriteFile(filepath.Join(stateDir, source.StateFile), []byte(""), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, source.ID+".archive.txt"), []byte(""), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	origEnumerateLimited := enumerateSoundCloudTracksWithLimitFn
	t.Cleanup(func() {
		enumerateSoundCloudTracksWithLimitFn = origEnumerateLimited
	})
	enumerateSoundCloudTracksWithLimitFn = func(ctx context.Context, source config.Source, limit int) ([]soundCloudRemoteTrack, error) {
		return []soundCloudRemoteTrack{
			{ID: "track-a", Title: "Track A"},
			{ID: "track-b", Title: "Track B"},
			{ID: "track-c", Title: "Track C"},
		}, nil
	}

	provider := NewSCDLPlanProvider()
	plan, err := provider.Build(context.Background(), cfg, source, SyncOptions{Plan: true, PlanLimit: 10})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}

	execPlan, err := plan.ApplySelection(testExecutionManifest(t, source.ID, plan.Rows(), []int{1, 3}, DownloadOrderOldestFirst), PlanApplyOptions{})
	if err != nil {
		t.Fatalf("apply selection: %v", err)
	}
	if got := execPlan.SourceForExec.SelectedPlaylistIDs; !reflect.DeepEqual(got, []int{3, 1}) {
		t.Fatalf("expected oldest_first selection order [3 1], got %v", got)
	}
	if got := []string{execPlan.PlannedSoundCloudTracks[0].ID, execPlan.PlannedSoundCloudTracks[1].ID}; !reflect.DeepEqual(got, []string{"track-c", "track-a"}) {
		t.Fatalf("expected planned track execution order [track-c track-a], got %v", got)
	}
}

func TestSCDLPlanProviderEmptySelectionNoOp(t *testing.T) {
	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	stateDir := filepath.Join(tmp, "state")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}

	source := config.Source{
		ID:        "sc-plan",
		Type:      config.SourceTypeSoundCloud,
		Enabled:   true,
		TargetDir: targetDir,
		URL:       "https://soundcloud.com/user",
		StateFile: "sc-plan.sync.scdl",
		Adapter:   config.AdapterSpec{Kind: "scdl"},
	}
	cfg := config.Config{
		Version: 1,
		Defaults: config.Defaults{
			StateDir:    stateDir,
			ArchiveFile: "archive.txt",
		},
		Sources: []config.Source{source},
	}

	if err := os.WriteFile(filepath.Join(stateDir, source.StateFile), []byte(""), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, source.ID+".archive.txt"), []byte(""), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	origEnumerateLimited := enumerateSoundCloudTracksWithLimitFn
	t.Cleanup(func() {
		enumerateSoundCloudTracksWithLimitFn = origEnumerateLimited
	})
	enumerateSoundCloudTracksWithLimitFn = func(ctx context.Context, source config.Source, limit int) ([]soundCloudRemoteTrack, error) {
		return []soundCloudRemoteTrack{
			{ID: "track-a", Title: "Track A"},
		}, nil
	}

	provider := NewSCDLPlanProvider()
	plan, err := provider.Build(context.Background(), cfg, source, SyncOptions{Plan: true, PlanLimit: 10})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}

	execPlan, err := plan.ApplySelection(testExecutionManifest(t, source.ID, plan.Rows(), nil, DownloadOrderNewestFirst), PlanApplyOptions{})
	if err != nil {
		t.Fatalf("apply empty selection: %v", err)
	}
	if execPlan.SourcePreflight == nil || execPlan.SourcePreflight.PlannedDownloadCount != 0 {
		t.Fatalf("expected no-op preflight planned_download_count=0, got %+v", execPlan.SourcePreflight)
	}
	if len(execPlan.SourceForExec.SelectedPlaylistIDs) != 0 {
		t.Fatalf("expected no selected playlist ids, got %v", execPlan.SourceForExec.SelectedPlaylistIDs)
	}
	if execPlan.StateSwap.TempSyncPath != "" || execPlan.StateSwap.TempArchivePath != "" {
		t.Fatalf("expected no temp file rewrites on empty selection, got %+v", execPlan.StateSwap)
	}
}

func TestSCDLPlanProviderAppliesSelectionKeepsSyncForNewTracks(t *testing.T) {
	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	stateDir := filepath.Join(tmp, "state")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}

	source := config.Source{
		ID:        "sc-plan",
		Type:      config.SourceTypeSoundCloud,
		Enabled:   true,
		TargetDir: targetDir,
		URL:       "https://soundcloud.com/user",
		StateFile: "sc-plan.sync.scdl",
		Adapter:   config.AdapterSpec{Kind: "scdl"},
	}
	cfg := config.Config{
		Version: 1,
		Defaults: config.Defaults{
			StateDir:    stateDir,
			ArchiveFile: "archive.txt",
		},
		Sources: []config.Source{source},
	}

	statePath := filepath.Join(stateDir, source.StateFile)
	if err := os.WriteFile(statePath, []byte("soundcloud known-a known-a.mp3\n"), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, source.ID+".archive.txt"), []byte("soundcloud known-a\n"), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	origEnumerateLimited := enumerateSoundCloudTracksWithLimitFn
	t.Cleanup(func() {
		enumerateSoundCloudTracksWithLimitFn = origEnumerateLimited
	})
	enumerateSoundCloudTracksWithLimitFn = func(ctx context.Context, source config.Source, limit int) ([]soundCloudRemoteTrack, error) {
		return []soundCloudRemoteTrack{
			{ID: "known-a", Title: "Known A"},
			{ID: "new-a", Title: "New A"},
		}, nil
	}

	provider := NewSCDLPlanProvider()
	plan, err := provider.Build(context.Background(), cfg, source, SyncOptions{Plan: true, PlanLimit: 10})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}

	execPlan, err := plan.ApplySelection(testExecutionManifest(t, source.ID, plan.Rows(), []int{2}, DownloadOrderNewestFirst), PlanApplyOptions{})
	if err != nil {
		t.Fatalf("apply selection: %v", err)
	}
	if execPlan.SourceForExec.DisableSyncMode {
		t.Fatalf("did not expect plan subset execution to disable scdl --sync for new tracks")
	}
	if execPlan.StateSwap.TempSyncPath == "" {
		t.Fatalf("expected temporary sync file for new-track subset execution, got %+v", execPlan.StateSwap)
	}
	if execPlan.StateSwap.TempArchivePath != "" {
		t.Fatalf("did not expect temporary archive file when no known gaps are selected, got %+v", execPlan.StateSwap)
	}
	if execPlan.SourceForExec.DownloadArchivePath != "" {
		t.Fatalf("did not expect archive override when no known gaps are selected, got %q", execPlan.SourceForExec.DownloadArchivePath)
	}

	filteredState, err := os.ReadFile(execPlan.StateSwap.TempSyncPath)
	if err != nil {
		t.Fatalf("read filtered state: %v", err)
	}
	if strings.TrimSpace(string(filteredState)) != "" {
		t.Fatalf("expected temporary sync file to start empty for managed subset execution, got %q", string(filteredState))
	}
}
