package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/playlists"
	"github.com/jaa/update-downloads/internal/rekordbox/bridge"
	"github.com/jaa/update-downloads/internal/rekordbox/music"
	"github.com/jaa/update-downloads/internal/rekordbox/playlistsync"
)

type fakeRekordboxMusicReader struct {
	playlists []music.Playlist
	playlist  music.Playlist
	tracks    []music.Track
}

func (f fakeRekordboxMusicReader) ListPlaylists(ctx context.Context) ([]music.Playlist, error) {
	return f.playlists, nil
}

func (f fakeRekordboxMusicReader) ReadPlaylist(ctx context.Context, selector music.PlaylistSelector) (music.Playlist, []music.Track, error) {
	return f.playlist, f.tracks, nil
}

type fakeRekordboxBridge struct {
	inspect      bridge.InspectResponse
	applyResp    bridge.ApplyResponse
	batchResp    bridge.ApplyBatchResponse
	inspectCalls int
	applyCalls   int
	batchCalls   int
	onApply      func()
}

func (f *fakeRekordboxBridge) Inspect(ctx context.Context, dbDir string) (bridge.InspectResponse, error) {
	f.inspectCalls++
	return f.inspect, nil
}

func (f *fakeRekordboxBridge) Apply(ctx context.Context, req bridge.ApplyRequest) (bridge.ApplyResponse, error) {
	f.applyCalls++
	if f.onApply != nil {
		f.onApply()
	}
	return f.applyResp, nil
}

func (f *fakeRekordboxBridge) ApplyBatch(ctx context.Context, req bridge.ApplyBatchRequest) (bridge.ApplyBatchResponse, error) {
	f.batchCalls++
	return f.batchResp, nil
}

func TestRekordboxPlaylistSyncUseCasePlanBuildsChecksummedPlan(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Defaults.StateDir = filepath.Join(tmp, "state")
	reader := fakeRekordboxMusicReader{
		playlists: []music.Playlist{{Name: "Favourites", PersistentID: "music-favs", Smart: true, TrackCount: 1}},
		playlist:  music.Playlist{Name: "Favourites", PersistentID: "music-favs", Smart: true, TrackCount: 1},
		tracks:    []music.Track{{Index: 1, PersistentID: "track-1", Title: "Track", Artist: "Artist", Path: "/Music/Track.mp3"}},
	}
	rb := &fakeRekordboxBridge{inspect: bridge.InspectResponse{
		Playlists: []bridge.Playlist{{ID: "3150438241", Name: "fav_imports", Attribute: 0}},
		Contents:  []bridge.Content{{ID: "content-1", Title: "Track", FolderPath: "/Music/Track.mp3"}},
	}}
	useCase := RekordboxPlaylistSyncUseCase{
		MusicReader: reader,
		Bridge:      rb,
		Now:         func() time.Time { return time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC) },
		CheckClosed: func(context.Context, string) error { return nil },
	}

	result, err := useCase.Plan(context.Background(), RekordboxPlaylistSyncPlanRequest{Config: cfg})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if result.Plan.Summary.MatchedByPath != 1 || result.Plan.Summary.FinalTargetCount != 1 {
		t.Fatalf("unexpected plan summary: %+v", result.Plan.Summary)
	}
	if result.Plan.ChecksumSHA256 == "" {
		t.Fatalf("expected checksummed plan")
	}
	if result.PlanPath != filepath.Join(tmp, "state", "rekordbox", "playlist-sync", "fav_imports-20260521-120000.plan.json") {
		t.Fatalf("unexpected plan path: %s", result.PlanPath)
	}
	if rb.inspectCalls != 1 {
		t.Fatalf("expected one inspect call, got %d", rb.inspectCalls)
	}
}

func TestRekordboxPlaylistSyncUseCasePlansFromStandaloneSnapshot(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Defaults.StateDir = filepath.Join(tmp, "state")
	if _, err := playlists.WriteSnapshot(cfg.Defaults.StateDir, playlists.Snapshot{
		PlaylistID: "favorites", Name: "Favorites", Provider: playlists.ProviderAppleMusic,
		ProviderPlaylist: "Favourites", RefreshedAt: time.Date(2026, 5, 21, 11, 0, 0, 0, time.UTC),
		Tracks: []playlists.Track{{Index: 1, ProviderID: "track-1", Title: "Track", Artist: "Artist", Path: "/Music/Track.mp3"}},
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := playlists.LoadSnapshot(cfg.Defaults.StateDir, "favorites")
	if err != nil {
		t.Fatal(err)
	}
	rb := &fakeRekordboxBridge{inspect: bridge.InspectResponse{
		Playlists: []bridge.Playlist{{ID: "3150438241", Name: "fav_imports", Attribute: 0}},
		Contents:  []bridge.Content{{ID: "content-1", Title: "Track", FolderPath: "/Music/Track.mp3"}},
	}}
	useCase := RekordboxPlaylistSyncUseCase{
		Bridge: rb,
		Now:    func() time.Time { return time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC) },
		CheckClosed: func(context.Context, string) error {
			return nil
		},
	}

	result, err := useCase.Plan(context.Background(), RekordboxPlaylistSyncPlanRequest{
		Config: cfg, Snapshot: &snapshot,
	})
	if err != nil {
		t.Fatalf("Plan snapshot: %v", err)
	}
	if result.Plan.MusicPlaylist.Name != "Favorites" || result.Plan.Summary.MatchedByPath != 1 {
		t.Fatalf("unexpected snapshot plan: %+v", result.Plan)
	}
}

func TestRekordboxPlaylistSyncUseCaseApplyDryRunSkipsBackupAndWrite(t *testing.T) {
	plan := testRekordboxApplyPlan(t)
	rb := &fakeRekordboxBridge{inspect: bridge.InspectResponse{
		Playlists: []bridge.Playlist{{ID: "3150438241", Name: "fav_imports", Attribute: 0}},
		Contents:  []bridge.Content{{ID: "content-1", Title: "Track", FolderPath: "/Music/Track.mp3"}},
	}}
	backupCalls := 0
	useCase := RekordboxPlaylistSyncUseCase{
		Bridge: rb,
		CheckClosed: func(context.Context, string) error {
			return nil
		},
		CreateBackup: func(context.Context, string, string, time.Time) (string, error) {
			backupCalls++
			return "/backup", nil
		},
	}

	result, err := useCase.Apply(context.Background(), RekordboxPlaylistSyncApplyRequest{
		Config: config.DefaultConfig(),
		Plan:   plan,
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("Apply dry-run: %v", err)
	}
	if !result.DryRun {
		t.Fatalf("expected dry-run result")
	}
	if result.EffectiveBackupDir != plan.BackupDir {
		t.Fatalf("expected plan backup dir %q, got %q", plan.BackupDir, result.EffectiveBackupDir)
	}
	if backupCalls != 0 || rb.applyCalls != 0 {
		t.Fatalf("expected no backup/apply calls, got backup=%d apply=%d", backupCalls, rb.applyCalls)
	}
}

func TestRekordboxPlaylistSyncUseCaseApplyBackupOverrideDoesNotInvalidatePlan(t *testing.T) {
	plan := testRekordboxApplyPlan(t)
	originalChecksum := plan.ChecksumSHA256
	override := filepath.Join(t.TempDir(), "override")
	rb := &fakeRekordboxBridge{inspect: bridge.InspectResponse{
		Playlists: []bridge.Playlist{{ID: "3150438241", Name: "fav_imports", Attribute: 0}},
		Contents:  []bridge.Content{{ID: "content-1", Title: "Track", FolderPath: "/Music/Track.mp3"}},
	}}
	useCase := RekordboxPlaylistSyncUseCase{
		Bridge:      rb,
		CheckClosed: func(context.Context, string) error { return nil },
	}

	result, err := useCase.Apply(context.Background(), RekordboxPlaylistSyncApplyRequest{
		Config:    config.DefaultConfig(),
		Plan:      plan,
		BackupDir: override,
		DryRun:    true,
	})
	if err != nil {
		t.Fatalf("Apply with backup override: %v", err)
	}
	if result.EffectiveBackupDir != override {
		t.Fatalf("expected effective backup dir %q, got %q", override, result.EffectiveBackupDir)
	}
	if plan.BackupDir != "/tmp/backups" || plan.ChecksumSHA256 != originalChecksum {
		t.Fatalf("apply mutated checksummed plan: %+v", plan)
	}
	if err := playlistsync.VerifyPlanChecksum(plan); err != nil {
		t.Fatalf("plan checksum after apply: %v", err)
	}
}

func TestRekordboxPlaylistSyncUseCaseApplyUsesConfiguredBackupWhenPlanOmitsIt(t *testing.T) {
	plan := testRekordboxApplyPlan(t)
	plan.BackupDir = ""
	if err := playlistsync.SignPlan(&plan); err != nil {
		t.Fatalf("SignPlan: %v", err)
	}
	configured := filepath.Join(t.TempDir(), "configured")
	cfg := config.DefaultConfig()
	cfg.Rekordbox = &config.RekordboxConfig{BackupDir: configured}
	rb := &fakeRekordboxBridge{inspect: bridge.InspectResponse{
		Playlists: []bridge.Playlist{{ID: "3150438241", Name: "fav_imports", Attribute: 0}},
		Contents:  []bridge.Content{{ID: "content-1", Title: "Track", FolderPath: "/Music/Track.mp3"}},
	}}
	useCase := RekordboxPlaylistSyncUseCase{
		Bridge:      rb,
		CheckClosed: func(context.Context, string) error { return nil },
	}

	result, err := useCase.Apply(context.Background(), RekordboxPlaylistSyncApplyRequest{
		Config: cfg,
		Plan:   plan,
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("Apply with configured backup: %v", err)
	}
	if result.EffectiveBackupDir != configured {
		t.Fatalf("expected configured backup dir %q, got %q", configured, result.EffectiveBackupDir)
	}
}

func TestRekordboxPlaylistSyncUseCaseApplyBacksUpBeforeWrite(t *testing.T) {
	plan := testRekordboxApplyPlan(t)
	override := filepath.Join(t.TempDir(), "override")
	sequence := []string{}
	rb := &fakeRekordboxBridge{inspect: bridge.InspectResponse{
		Playlists: []bridge.Playlist{{ID: "3150438241", Name: "fav_imports", Attribute: 0}},
		Contents:  []bridge.Content{{ID: "content-1", Title: "Track", FolderPath: "/Music/Track.mp3"}},
	}, applyResp: bridge.ApplyResponse{
		PlaylistID:      "3150438241",
		PlaylistName:    "fav_imports",
		FinalContentIDs: []string{"content-1"},
		FinalTrackCount: 1,
	}}
	rb.onApply = func() {
		sequence = append(sequence, "apply")
	}
	useCase := RekordboxPlaylistSyncUseCase{
		Bridge: rb,
		CheckClosed: func(context.Context, string) error {
			return nil
		},
		CreateBackup: func(_ context.Context, _ string, backupRoot string, _ time.Time) (string, error) {
			if backupRoot != override {
				t.Fatalf("expected override backup root %q, got %q", override, backupRoot)
			}
			sequence = append(sequence, "backup")
			return "/backup", nil
		},
	}
	rb.applyResp.FinalContentIDs = []string{"content-1"}

	result, err := useCase.Apply(context.Background(), RekordboxPlaylistSyncApplyRequest{
		Config:    config.DefaultConfig(),
		Plan:      plan,
		BackupDir: override,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.BackupPath != "/backup" || rb.applyCalls != 1 {
		t.Fatalf("unexpected apply result: %+v applyCalls=%d", result, rb.applyCalls)
	}
	if len(sequence) != 2 || sequence[0] != "backup" || sequence[1] != "apply" {
		t.Fatalf("expected backup before apply, got %v", sequence)
	}
}

func TestRekordboxPlaylistSyncUseCaseBackupFailurePreventsWrite(t *testing.T) {
	plan := testRekordboxApplyPlan(t)
	rb := &fakeRekordboxBridge{inspect: bridge.InspectResponse{
		Playlists: []bridge.Playlist{{ID: "3150438241", Name: "fav_imports", Attribute: 0}},
		Contents:  []bridge.Content{{ID: "content-1", Title: "Track", FolderPath: "/Music/Track.mp3"}},
	}}
	useCase := RekordboxPlaylistSyncUseCase{
		Bridge: rb,
		CheckClosed: func(context.Context, string) error {
			return nil
		},
		CreateBackup: func(context.Context, string, string, time.Time) (string, error) {
			return "", errors.New("backup volume unavailable")
		},
	}
	_, err := useCase.Apply(context.Background(), RekordboxPlaylistSyncApplyRequest{
		Config: config.DefaultConfig(),
		Plan:   plan,
	})
	if err == nil || !strings.Contains(err.Error(), "backup volume unavailable") {
		t.Fatalf("expected backup failure, got %v", err)
	}
	if rb.applyCalls != 0 {
		t.Fatalf("database apply ran after backup failure: %d calls", rb.applyCalls)
	}
}

func testRekordboxApplyPlan(t *testing.T) playlistsync.Plan {
	t.Helper()
	plan := playlistsync.Plan{
		Version:        playlistsync.PlanVersion,
		GeneratedAt:    time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		Mode:           playlistsync.DefaultMode,
		RekordboxDBDir: "/tmp/rb",
		BackupDir:      "/tmp/backups",
		MusicPlaylist:  playlistsync.PlanMusicPlaylist{Name: "Favourites", TrackCount: 1},
		RekordboxPlaylist: playlistsync.PlanRekordboxPlaylist{
			ID:           "3150438241",
			Name:         "fav_imports",
			Attribute:    0,
			CurrentCount: 0,
		},
		Summary: playlistsync.PlanSummary{
			MusicTotal:       1,
			MatchedByPath:    1,
			FinalTargetCount: 1,
			WillAdd:          1,
		},
		Rows:            []playlistsync.PlanRow{{MusicIndex: 1, Title: "Track", Path: "/Music/Track.mp3", RekordboxContentID: "content-1", MatchStatus: "matched_path", Action: "add"}},
		FinalContentIDs: []string{"content-1"},
		Preconditions: playlistsync.PlanPreconditions{
			TargetPlaylistID:          "3150438241",
			TargetPlaylistName:        "fav_imports",
			ExpectedCurrentContentIDs: []string{},
			MatchedContent:            []playlistsync.MatchedContentPrecondition{{ContentID: "content-1", Title: "Track", FolderPath: "/Music/Track.mp3"}},
		},
	}
	if err := playlistsync.SignPlan(&plan); err != nil {
		t.Fatalf("SignPlan: %v", err)
	}
	return plan
}
