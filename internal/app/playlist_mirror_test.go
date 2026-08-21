package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/navidrome"
	"github.com/jaa/update-downloads/internal/playlists"
	"github.com/jaa/update-downloads/internal/rekordbox/bridge"
)

type mirrorBridgeStub struct {
	inspect    bridge.InspectResponse
	applyCalls int
}

func (stub *mirrorBridgeStub) Inspect(context.Context, string) (bridge.InspectResponse, error) {
	return stub.inspect, nil
}

func (stub *mirrorBridgeStub) Apply(_ context.Context, request bridge.ApplyRequest) (bridge.ApplyResponse, error) {
	stub.applyCalls++
	for index := range stub.inspect.Playlists {
		if stub.inspect.Playlists[index].ID == request.TargetPlaylistID || (request.TargetPlaylistID == "" && stub.inspect.Playlists[index].Name == request.TargetPlaylistName) {
			stub.inspect.Playlists[index].ContentIDs = append([]string(nil), request.FinalContentIDs...)
			return bridge.ApplyResponse{
				PlaylistID: stub.inspect.Playlists[index].ID, PlaylistName: stub.inspect.Playlists[index].Name,
				FinalContentIDs: append([]string(nil), request.FinalContentIDs...), FinalTrackCount: len(request.FinalContentIDs),
			}, nil
		}
	}
	return bridge.ApplyResponse{}, errors.New("playlist not found")
}

func (stub *mirrorBridgeStub) ApplyBatch(context.Context, bridge.ApplyBatchRequest) (bridge.ApplyBatchResponse, error) {
	return bridge.ApplyBatchResponse{}, errors.New("not used")
}

type mirrorNavStub struct {
	playlists    []navidrome.Playlist
	memberships  map[string][]string
	catalog      []navidrome.Song
	replaceCalls int
}

func (stub *mirrorNavStub) Playlists(context.Context) ([]navidrome.Playlist, error) {
	return append([]navidrome.Playlist(nil), stub.playlists...), nil
}

func (stub *mirrorNavStub) Playlist(_ context.Context, id string) (navidrome.Playlist, []navidrome.Song, error) {
	var selected navidrome.Playlist
	for _, item := range stub.playlists {
		if item.ID == id {
			selected = item
			break
		}
	}
	songs := []navidrome.Song{}
	for _, memberID := range stub.memberships[id] {
		for _, song := range stub.catalog {
			if song.ID == memberID {
				songs = append(songs, song)
			}
		}
	}
	return selected, songs, nil
}

func (stub *mirrorNavStub) Songs(context.Context) ([]navidrome.Song, error) {
	return append([]navidrome.Song(nil), stub.catalog...), nil
}

func (stub *mirrorNavStub) ReplacePlaylist(_ context.Context, id, name string, songIDs []string) (navidrome.PlaylistMutationResult, error) {
	stub.replaceCalls++
	created := id == ""
	if created {
		id = "nd-created"
		stub.playlists = append(stub.playlists, navidrome.Playlist{ID: id, Name: name, Owner: "dj", TrackCount: len(songIDs)})
	}
	stub.memberships[id] = append([]string(nil), songIDs...)
	var selected navidrome.Playlist
	for _, item := range stub.playlists {
		if item.ID == id {
			selected = item
		}
	}
	return navidrome.PlaylistMutationResult{Playlist: selected, SongIDs: append([]string(nil), songIDs...), Created: created}, nil
}

func mirrorFixture(t *testing.T) (config.Config, playlists.Config, *mirrorBridgeStub, *mirrorNavStub) {
	t.Helper()
	main := config.DefaultConfig()
	main.Defaults.StateDir = filepath.Join(t.TempDir(), "state")
	playlistConfig := playlists.Config{Version: playlists.ConfigVersion, SyncJobs: []playlists.SyncJob{{
		ID: "favs", Rekordbox: playlists.PlaylistSelector{Playlist: "favs"}, Navidrome: playlists.PlaylistSelector{Playlist: "favs"},
	}}}
	rb := &mirrorBridgeStub{inspect: bridge.InspectResponse{
		Playlists: []bridge.Playlist{{ID: "rb", Name: "favs", Attribute: 0, ContentIDs: []string{"c1"}}},
		Contents:  []bridge.Content{{ID: "c1", Title: "One", FolderPath: "/Music/one.mp3"}, {ID: "c2", Title: "Two", FolderPath: "/Music/two.mp3"}},
	}}
	nd := &mirrorNavStub{
		playlists:   []navidrome.Playlist{{ID: "nd", Name: "favs", Owner: "dj", TrackCount: 1}},
		memberships: map[string][]string{"nd": {"s2"}},
		catalog: []navidrome.Song{
			{ID: "s1", Title: "One", Path: "/Music/one.mp3"},
			{ID: "s2", Title: "Two", Path: "/Music/two.mp3"},
		},
	}
	return main, playlistConfig, rb, nd
}

func TestPlaylistMirrorPlanChecksRekordboxClosedAndWritesPlan(t *testing.T) {
	main, cfg, rb, nd := mirrorFixture(t)
	closedCalls := 0
	useCase := PlaylistMirrorUseCase{
		Bridge: rb, Navidrome: nd, Now: func() time.Time { return time.Unix(10, 0) },
		CheckClosed: func(context.Context, string) error { closedCalls++; return nil },
	}
	result, err := useCase.Plan(context.Background(), PlaylistMirrorPlanRequest{
		Config: main, PlaylistConfig: cfg, JobID: "favs", Direction: playlists.DirectionRekordboxToNavidrome,
		RekordboxDBDir: "/rb", NavidromeUser: "dj",
	})
	if err != nil {
		t.Fatal(err)
	}
	if closedCalls != 1 || result.Plan.Summary.WillAdd != 1 || result.Plan.Summary.WillRemove != 1 {
		t.Fatalf("unexpected planning result: calls=%d plan=%#v", closedCalls, result.Plan)
	}
	if _, err := os.Stat(result.PlanPath); err != nil {
		t.Fatalf("plan was not written: %v", err)
	}
}

func TestPlaylistMirrorApplyBacksUpWritesVerifiesAndAdvancesState(t *testing.T) {
	main, cfg, rb, nd := mirrorFixture(t)
	sequence := []string{}
	useCase := PlaylistMirrorUseCase{
		Bridge: rb, Navidrome: nd, Now: func() time.Time { return time.Unix(10, 0) },
		CheckClosed: func(context.Context, string) error { return nil },
		BackupNavidrome: func(context.Context) (string, error) {
			sequence = append(sequence, "backup")
			return "/backup/navidrome", nil
		},
	}
	planned, err := useCase.Plan(context.Background(), PlaylistMirrorPlanRequest{
		Config: main, PlaylistConfig: cfg, JobID: "favs", Direction: playlists.DirectionRekordboxToNavidrome,
		RekordboxDBDir: "/rb", NavidromeUser: "dj",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := useCase.Apply(context.Background(), PlaylistMirrorApplyRequest{
		Config: main, PlaylistConfig: cfg, Plan: planned.Plan, RekordboxDBDir: "/rb", NavidromeUser: "dj",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.BackupPath != "/backup/navidrome" || nd.replaceCalls != 1 || result.PairStatePath == "" {
		t.Fatalf("unexpected apply result: %#v calls=%d", result, nd.replaceCalls)
	}
	if len(sequence) != 1 || sequence[0] != "backup" {
		t.Fatalf("backup sequence = %v", sequence)
	}
	state, err := playlists.LoadPairState(main.Defaults.StateDir, "favs")
	if err != nil || state.Navidrome.PlaylistID != "nd" || state.LastDirection != playlists.DirectionRekordboxToNavidrome {
		t.Fatalf("pair state = %#v, %v", state, err)
	}
}

func TestPlaylistMirrorApplyRejectsStaleStateBeforeBackup(t *testing.T) {
	main, cfg, rb, nd := mirrorFixture(t)
	backupCalls := 0
	useCase := PlaylistMirrorUseCase{
		Bridge: rb, Navidrome: nd, Now: func() time.Time { return time.Unix(10, 0) },
		CheckClosed:     func(context.Context, string) error { return nil },
		BackupNavidrome: func(context.Context) (string, error) { backupCalls++; return "/backup", nil },
	}
	planned, err := useCase.Plan(context.Background(), PlaylistMirrorPlanRequest{
		Config: main, PlaylistConfig: cfg, JobID: "favs", Direction: playlists.DirectionRekordboxToNavidrome,
		RekordboxDBDir: "/rb", NavidromeUser: "dj",
	})
	if err != nil {
		t.Fatal(err)
	}
	nd.memberships["nd"] = []string{"s1", "s2"}
	if _, err := useCase.Apply(context.Background(), PlaylistMirrorApplyRequest{
		Config: main, PlaylistConfig: cfg, Plan: planned.Plan, RekordboxDBDir: "/rb", NavidromeUser: "dj",
	}); err == nil || !strings.Contains(err.Error(), "changed since planning") {
		t.Fatalf("stale apply error = %v", err)
	}
	if backupCalls != 0 || nd.replaceCalls != 0 {
		t.Fatalf("stale plan reached mutation: backup=%d replace=%d", backupCalls, nd.replaceCalls)
	}
}

func TestPlaylistMirrorDryRunAndNoOpSkipBackups(t *testing.T) {
	main, cfg, rb, nd := mirrorFixture(t)
	backupCalls := 0
	useCase := PlaylistMirrorUseCase{
		Bridge: rb, Navidrome: nd, Now: func() time.Time { return time.Unix(10, 0) },
		CheckClosed:     func(context.Context, string) error { return nil },
		BackupNavidrome: func(context.Context) (string, error) { backupCalls++; return "/backup", nil },
	}
	planned, err := useCase.Plan(context.Background(), PlaylistMirrorPlanRequest{
		Config: main, PlaylistConfig: cfg, JobID: "favs", Direction: playlists.DirectionRekordboxToNavidrome,
		RekordboxDBDir: "/rb", NavidromeUser: "dj",
	})
	if err != nil {
		t.Fatal(err)
	}
	dry, err := useCase.Apply(context.Background(), PlaylistMirrorApplyRequest{
		Config: main, PlaylistConfig: cfg, Plan: planned.Plan, RekordboxDBDir: "/rb", NavidromeUser: "dj", DryRun: true,
	})
	if err != nil || !dry.DryRun || backupCalls != 0 || nd.replaceCalls != 0 {
		t.Fatalf("dry run = %#v err=%v backup=%d replace=%d", dry, err, backupCalls, nd.replaceCalls)
	}

	nd.memberships["nd"] = []string{"s1"}
	noOpPlan, err := useCase.Plan(context.Background(), PlaylistMirrorPlanRequest{
		Config: main, PlaylistConfig: cfg, JobID: "favs", Direction: playlists.DirectionRekordboxToNavidrome,
		RekordboxDBDir: "/rb", NavidromeUser: "dj", OutPath: filepath.Join(t.TempDir(), "noop.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	noOp, err := useCase.Apply(context.Background(), PlaylistMirrorApplyRequest{
		Config: main, PlaylistConfig: cfg, Plan: noOpPlan.Plan, RekordboxDBDir: "/rb", NavidromeUser: "dj",
	})
	if err != nil || !noOp.NoOp || noOp.PairStatePath == "" || backupCalls != 0 {
		t.Fatalf("no-op = %#v err=%v backup=%d", noOp, err, backupCalls)
	}
}

func TestPlaylistMirrorBackupFailurePreventsMutation(t *testing.T) {
	main, cfg, rb, nd := mirrorFixture(t)
	useCase := PlaylistMirrorUseCase{
		Bridge: rb, Navidrome: nd, Now: func() time.Time { return time.Unix(10, 0) },
		CheckClosed:     func(context.Context, string) error { return nil },
		BackupNavidrome: func(context.Context) (string, error) { return "", errors.New("disk full") },
	}
	planned, err := useCase.Plan(context.Background(), PlaylistMirrorPlanRequest{
		Config: main, PlaylistConfig: cfg, JobID: "favs", Direction: playlists.DirectionRekordboxToNavidrome,
		RekordboxDBDir: "/rb", NavidromeUser: "dj",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := useCase.Apply(context.Background(), PlaylistMirrorApplyRequest{
		Config: main, PlaylistConfig: cfg, Plan: planned.Plan, RekordboxDBDir: "/rb", NavidromeUser: "dj",
	}); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("backup failure = %v", err)
	}
	if nd.replaceCalls != 0 {
		t.Fatalf("mutation ran after backup failure: %d", nd.replaceCalls)
	}
}

func TestPlaylistMirrorCancellationAfterBackupPreventsMutation(t *testing.T) {
	main, cfg, rb, nd := mirrorFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	useCase := PlaylistMirrorUseCase{
		Bridge: rb, Navidrome: nd, Now: func() time.Time { return time.Unix(10, 0) },
		CheckClosed: func(context.Context, string) error { return nil },
		BackupNavidrome: func(context.Context) (string, error) {
			cancel()
			return "/backup/navidrome", nil
		},
	}
	planned, err := useCase.Plan(context.Background(), PlaylistMirrorPlanRequest{
		Config: main, PlaylistConfig: cfg, JobID: "favs", Direction: playlists.DirectionRekordboxToNavidrome,
		RekordboxDBDir: "/rb", NavidromeUser: "dj",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := useCase.Apply(ctx, PlaylistMirrorApplyRequest{
		Config: main, PlaylistConfig: cfg, Plan: planned.Plan, RekordboxDBDir: "/rb", NavidromeUser: "dj",
	})
	if !errors.Is(err, context.Canceled) || result.BackupPath != "/backup/navidrome" {
		t.Fatalf("canceled result = %#v, %v", result, err)
	}
	if nd.replaceCalls != 0 {
		t.Fatalf("mutation ran after cancellation: %d", nd.replaceCalls)
	}
}

func TestPlaylistMirrorStateFailureFollowsVerifiedWriteAndKeepsBackup(t *testing.T) {
	main, cfg, rb, nd := mirrorFixture(t)
	useCase := PlaylistMirrorUseCase{
		Bridge: rb, Navidrome: nd, Now: func() time.Time { return time.Unix(10, 0) },
		CheckClosed: func(context.Context, string) error { return nil },
		BackupNavidrome: func(context.Context) (string, error) {
			if err := os.RemoveAll(main.Defaults.StateDir); err != nil {
				return "", err
			}
			if err := os.WriteFile(main.Defaults.StateDir, []byte("blocks state directory"), 0o600); err != nil {
				return "", err
			}
			return "/backup/navidrome", nil
		},
	}
	planned, err := useCase.Plan(context.Background(), PlaylistMirrorPlanRequest{
		Config: main, PlaylistConfig: cfg, JobID: "favs", Direction: playlists.DirectionRekordboxToNavidrome,
		RekordboxDBDir: "/rb", NavidromeUser: "dj",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := useCase.Apply(context.Background(), PlaylistMirrorApplyRequest{
		Config: main, PlaylistConfig: cfg, Plan: planned.Plan, RekordboxDBDir: "/rb", NavidromeUser: "dj",
	})
	if err == nil || !strings.Contains(err.Error(), "reached verified parity") {
		t.Fatalf("state-write error = %v", err)
	}
	if result.BackupPath != "/backup/navidrome" || nd.replaceCalls != 1 {
		t.Fatalf("partial result = %#v replace=%d", result, nd.replaceCalls)
	}
}
