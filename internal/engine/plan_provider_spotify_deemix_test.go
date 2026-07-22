package engine

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/auth"
	"github.com/jaa/update-downloads/internal/config"
)

func withSpotifyDeemixPlanStubs(t *testing.T, tracks []spotifyRemoteTrack) {
	t.Helper()
	origResolveCreds := resolveSpotifyCredentialsFn
	origResolveARL := resolveDeemixARLFn
	origSaveARL := saveDeemixARLFn
	origEnumerate := enumerateSpotifyTracksFn
	origFetchMetadata := fetchSpotifyTrackMetadataFn
	t.Cleanup(func() {
		resolveSpotifyCredentialsFn = origResolveCreds
		resolveDeemixARLFn = origResolveARL
		saveDeemixARLFn = origSaveARL
		enumerateSpotifyTracksFn = origEnumerate
		fetchSpotifyTrackMetadataFn = origFetchMetadata
	})
	resolveSpotifyCredentialsFn = func() (auth.SpotifyCredentials, error) {
		return auth.SpotifyCredentials{ClientID: "id", ClientSecret: "secret"}, nil
	}
	resolveDeemixARLFn = func() (string, error) { return "arl", nil }
	saveDeemixARLFn = func(string) error { return nil }
	enumerateSpotifyTracksFn = func(ctx context.Context, source config.Source, creds auth.SpotifyCredentials) ([]spotifyRemoteTrack, error) {
		return tracks, nil
	}
	fetchSpotifyTrackMetadataFn = func(ctx context.Context, trackID string) (spotifyTrackMetadata, error) {
		return spotifyTrackMetadata{}, os.ErrNotExist
	}
}

func TestSpotifyDeemixPlanProviderLatestWindowUsesAddedAtBeforeLimit(t *testing.T) {
	cfg, source, _, _ := testSpotifyDeemixPlanSource(t)
	withSpotifyDeemixPlanStubs(t, []spotifyRemoteTrack{
		{ID: "old12345678", Title: "Old", Artist: "Artist", AddedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Position: 1},
		{ID: "new12345678", Title: "New", Artist: "Artist", AddedAt: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), Position: 2},
		{ID: "mid12345678", Title: "Mid", Artist: "Artist", AddedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Position: 3},
	})

	plan, err := NewSpotifyDeemixPlanProvider().Build(context.Background(), cfg, source, SyncOptions{Plan: true, PlanLimit: 2, PlanWindow: PlanWindowLatest})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	rows := plan.Rows()
	if got, want := []string{rows[0].RemoteID, rows[1].RemoteID}, []string{"new12345678", "mid12345678"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected latest rows %v, got %v", want, got)
	}
}

func TestSpotifyDeemixPlanProviderFirstWindowPreservesRemoteOrder(t *testing.T) {
	cfg, source, _, _ := testSpotifyDeemixPlanSource(t)
	withSpotifyDeemixPlanStubs(t, []spotifyRemoteTrack{
		{ID: "old12345678", Title: "Old", Artist: "Artist", AddedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Position: 1},
		{ID: "new12345678", Title: "New", Artist: "Artist", AddedAt: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), Position: 2},
		{ID: "mid12345678", Title: "Mid", Artist: "Artist", AddedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Position: 3},
	})

	plan, err := NewSpotifyDeemixPlanProvider().Build(context.Background(), cfg, source, SyncOptions{Plan: true, PlanLimit: 2, PlanWindow: PlanWindowFirst})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	rows := plan.Rows()
	if got, want := []string{rows[0].RemoteID, rows[1].RemoteID}, []string{"old12345678", "new12345678"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected first rows %v, got %v", want, got)
	}
}

func TestApplySpotifyPlanWindowLatestWithoutAddedAtUsesTailReversed(t *testing.T) {
	tracks := []spotifyRemoteTrack{
		{ID: "one12345678", Position: 1},
		{ID: "two12345678", Position: 2},
		{ID: "three123456", Position: 3},
		{ID: "four1234567", Position: 4},
	}

	selected := applySpotifyPlanWindow(tracks, 2, PlanWindowLatest)
	if got, want := []string{selected[0].ID, selected[1].ID}, []string{"four1234567", "three123456"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected fallback latest tail reversed %v, got %v", want, got)
	}
}

func testSpotifyDeemixPlanSource(t *testing.T) (config.Config, config.Source, string, string) {
	t.Helper()
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
		ID:        "spotify-plan",
		Type:      config.SourceTypeSpotify,
		Enabled:   true,
		TargetDir: targetDir,
		URL:       "https://open.spotify.com/playlist/a",
		StateFile: "spotify-plan.sync.spotify",
		Adapter:   config.AdapterSpec{Kind: "deemix"},
	}
	cfg := config.Config{
		Version: 1,
		Defaults: config.Defaults{
			StateDir:    stateDir,
			ArchiveFile: "archive.txt",
		},
		Sources: []config.Source{source},
	}
	return cfg, source, targetDir, filepath.Join(stateDir, source.StateFile)
}

func TestSpotifyDeemixPlanProviderBuildClassifiesRowsAndDefaultSelection(t *testing.T) {
	cfg, source, targetDir, statePath := testSpotifyDeemixPlanSource(t)
	if err := os.WriteFile(statePath, []byte("2abc234def\n3abc234def\ttitle=Artist 3 - Track 3\n"), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "Artist 2 - Track 2.mp3"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write local file: %v", err)
	}
	withSpotifyDeemixPlanStubs(t, []spotifyRemoteTrack{
		{ID: "1abc234def", Title: "Track 1", Artist: "Artist 1"},
		{ID: "2abc234def", Title: "Track 2", Artist: "Artist 2"},
		{ID: "3abc234def", Title: "Track 3", Artist: "Artist 3"},
	})

	plan, err := NewSpotifyDeemixPlanProvider().Build(context.Background(), cfg, source, SyncOptions{Plan: true, PlanLimit: 10, PlanWindow: PlanWindowFirst})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	rows := plan.Rows()
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if rows[0].Title != "Artist 1 - Track 1" || rows[0].Status != PlanRowMissingNew || !rows[0].Toggleable || !rows[0].SelectedByDefault {
		t.Fatalf("unexpected first row: %+v", rows[0])
	}
	if rows[1].Status != PlanRowAlreadyDownloaded || rows[1].Toggleable || rows[1].SelectedByDefault {
		t.Fatalf("unexpected second row: %+v", rows[1])
	}
	if rows[2].Status != PlanRowMissingKnownGap || !rows[2].Toggleable || rows[2].SelectedByDefault {
		t.Fatalf("unexpected third row: %+v", rows[2])
	}
}

func TestSpotifyDeemixPlanProviderApplySelectionOrdersSpotifyExecution(t *testing.T) {
	cfg, source, _, statePath := testSpotifyDeemixPlanSource(t)
	if err := os.WriteFile(statePath, []byte("3abc234def\ttitle=Artist 3 - Track 3\n"), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	withSpotifyDeemixPlanStubs(t, []spotifyRemoteTrack{
		{ID: "1abc234def", Title: "Track 1", Artist: "Artist 1"},
		{ID: "2abc234def", Title: "Track 2", Artist: "Artist 2"},
		{ID: "3abc234def", Title: "Track 3", Artist: "Artist 3"},
	})

	plan, err := NewSpotifyDeemixPlanProvider().Build(context.Background(), cfg, source, SyncOptions{Plan: true, ScanGaps: true, PlanWindow: PlanWindowFirst})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	execPlan, err := plan.ApplySelection(testExecutionManifest(t, source.ID, plan.Rows(), []int{1, 3}, DownloadOrderOldestFirst), PlanApplyOptions{})
	if err != nil {
		t.Fatalf("apply selection: %v", err)
	}
	if execPlan.SpotifyDeemixPlan == nil {
		t.Fatalf("expected spotify deemix execution plan")
	}
	if got, want := execPlan.SpotifyDeemixPlan.PlannedTrackIDs, []string{"3abc234def", "1abc234def"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected planned IDs %v, got %v", want, got)
	}
	if execPlan.SourcePreflight == nil || execPlan.SourcePreflight.PlannedDownloadCount != 2 {
		t.Fatalf("expected selected planned count 2, got %+v", execPlan.SourcePreflight)
	}
	if execPlan.DownloadOrder != DownloadOrderOldestFirst {
		t.Fatalf("expected oldest_first, got %q", execPlan.DownloadOrder)
	}
}

func TestSpotifyDeemixPlanProviderEnrichesIDOnlyPlaylistRows(t *testing.T) {
	cfg, source, _, _ := testSpotifyDeemixPlanSource(t)
	withSpotifyDeemixPlanStubs(t, []spotifyRemoteTrack{
		{ID: "1abc234def", Title: "1abc234def"},
		{ID: "2abc234def", Title: "2abc234def"},
	})
	fetchSpotifyTrackMetadataFn = func(ctx context.Context, trackID string) (spotifyTrackMetadata, error) {
		switch trackID {
		case "1abc234def":
			return spotifyTrackMetadata{Title: "Track 1", Artist: "Artist 1", Album: "Album 1"}, nil
		case "2abc234def":
			return spotifyTrackMetadata{Title: "Track 2", Artist: "Artist 2", Album: "Album 2"}, nil
		default:
			t.Fatalf("unexpected metadata lookup id: %q", trackID)
			return spotifyTrackMetadata{}, nil
		}
	}

	plan, err := NewSpotifyDeemixPlanProvider().Build(context.Background(), cfg, source, SyncOptions{Plan: true, PlanLimit: 10, PlanWindow: PlanWindowFirst})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	rows := plan.Rows()
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].Title != "Artist 1 - Track 1" || rows[1].Title != "Artist 2 - Track 2" {
		t.Fatalf("expected enriched row titles, got %+v", rows)
	}
}
