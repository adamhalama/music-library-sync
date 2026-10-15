package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/jaa/update-downloads/internal/rekordbox/bridge"
	"github.com/jaa/update-downloads/internal/rekordbox/music"
	"github.com/jaa/update-downloads/internal/rekordbox/playlistsync"
	"github.com/jaa/update-downloads/internal/rekordbox/pyruntime"
	"github.com/jaa/update-downloads/internal/rekordbox/syncconfig"
)

func writeAgentRekordboxConfig(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, syncconfig.ProjectConfigName)
	cfg := syncconfig.Config{
		Version: syncconfig.Version,
		Defaults: syncconfig.Defaults{
			DBDir: filepath.Join(dir, "rekordbox-db"), BackupDir: filepath.Join(dir, "backups"),
			Mode: "mirror", CreateFolders: true, CreatePlaylists: true,
		},
		Sync: syncconfig.Sync{Jobs: []syncconfig.PlaylistJob{{
			ID: "favorites", MusicPlaylist: "Favorites", RekordboxPlaylist: "fav_imports", Mode: "mirror",
		}}},
	}
	if err := syncconfig.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	return path
}

func testAgentRekordboxPlan(t *testing.T, dir string) playlistsync.Plan {
	t.Helper()
	musicPath := filepath.Join(dir, "music", "track.m4a")
	plan, err := playlistsync.BuildPlan(playlistsync.BuildRequest{
		Options: playlistsync.ResolvedOptions{
			MusicPlaylist: "Favorites", RekordboxPlaylist: "fav_imports",
			RekordboxDBDir: filepath.Join(dir, "rekordbox-db"),
			BackupDir:      filepath.Join(dir, "backups"), Mode: "mirror", CreatePlaylist: true,
		},
		MusicPlaylist: music.Playlist{Name: "Favorites", PersistentID: "music-id", TrackCount: 1},
		MusicTracks: []music.Track{{
			Index: 1, PersistentID: "music-track", Artist: "Artist", Title: "Track", Path: musicPath,
		}},
		Inspect: bridge.InspectResponse{
			Playlists: []bridge.Playlist{{ID: "target-id", Name: "fav_imports", ContentIDs: []string{"content-id"}}},
			Contents:  []bridge.Content{{ID: "content-id", Title: "Track", FolderPath: musicPath}},
		},
	}, time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := playlistsync.ValidatePlanForApply(plan); err != nil {
		t.Fatalf("fixture plan is not applicable: %v", err)
	}
	return plan
}

func TestRekordboxConfigAndDependencyMethods(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeAgentTestConfig(t, dir)
	rbPath := writeAgentRekordboxConfig(t, dir)
	statusCalls := 0
	ensureCalls := make(chan struct{}, 1)
	resetCalls := make(chan struct{}, 1)
	inspectCalls := make(chan struct{}, 1)
	ops := &RekordboxOperations{
		Status: func(context.Context, pyruntime.Request) pyruntime.Status {
			statusCalls++
			return pyruntime.Status{Runtime: pyruntime.Runtime{PythonBin: "/managed/python"}, Installed: true, Healthy: true}
		},
		Ensure: func(context.Context, pyruntime.Request) (pyruntime.Runtime, error) {
			ensureCalls <- struct{}{}
			return pyruntime.Runtime{PythonBin: "/managed/python"}, nil
		},
		Reset: func(pyruntime.Request) error {
			resetCalls <- struct{}{}
			return nil
		},
		Inspect: func(context.Context, config.Config) (bridge.InspectResponse, error) {
			inspectCalls <- struct{}{}
			return bridge.InspectResponse{}, nil
		},
	}
	output := &synchronizedBuffer{}
	server := &Server{
		Conn: NewConn(strings.NewReader(""), output), Runs: NewRunRegistry(),
		WorkingDir: dir, ConfigPath: mainPath, RekordboxConfigPath: rbPath,
		RekordboxOps: ops, runContext: context.Background(),
	}

	value, rpcErr := server.readRekordboxConfig()
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	read := value.(rekordboxConfigResult)
	if read.Path.Path != rbPath || len(read.Config.Sync.Jobs) != 1 || !strings.Contains(read.Content, "favorites") {
		t.Fatalf("unexpected Rekordbox config read: %+v", read)
	}
	read.Config.Defaults.CreatePlaylists = false
	if _, rpcErr := server.writeRekordboxConfig(mustJSON(t, map[string]any{"config": read.Config})); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	statusValue, rpcErr := server.rekordboxDepsStatus()
	if rpcErr != nil || statusCalls != 1 || statusValue.(map[string]any)["exit_code"].(int) != 0 {
		t.Fatalf("unexpected dependency status: value=%+v error=%v calls=%d", statusValue, rpcErr, statusCalls)
	}

	if _, rpcErr := server.ensureRekordboxDeps(); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if _, rpcErr := server.resetRekordboxDeps(); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if _, rpcErr := server.inspectRekordbox(); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	for name, calls := range map[string]<-chan struct{}{
		"ensure": ensureCalls, "reset": resetCalls, "inspect": inspectCalls,
	} {
		select {
		case <-calls:
		case <-time.After(time.Second):
			t.Fatalf("%s operation did not run", name)
		}
	}
}

func TestRekordboxDependencyMethodsReportUnhealthyEnsureAndResetFailures(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeAgentTestConfig(t, dir)
	rbPath := writeAgentRekordboxConfig(t, dir)
	output := &synchronizedBuffer{}
	server := &Server{
		Conn: NewConn(strings.NewReader(""), output), Runs: NewRunRegistry(),
		WorkingDir: dir, ConfigPath: mainPath, RekordboxConfigPath: rbPath,
		RekordboxOps: &RekordboxOperations{
			Status: func(context.Context, pyruntime.Request) pyruntime.Status {
				return pyruntime.Status{
					Runtime:   pyruntime.Runtime{PythonBin: "/managed/python", Managed: true},
					Installed: true, Healthy: false,
					Message: "pyrekordbox import failed",
				}
			},
			Ensure: func(context.Context, pyruntime.Request) (pyruntime.Runtime, error) {
				return pyruntime.Runtime{}, errors.New("install failed")
			},
			Reset: func(pyruntime.Request) error {
				return errors.New("reset failed")
			},
		},
		runContext: context.Background(),
	}

	statusValue, rpcErr := server.rekordboxDepsStatus()
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	status := statusValue.(map[string]any)
	if status["exit_code"].(int) != exitcode.MissingDependency ||
		status["status"].(pyruntime.Status).Healthy {
		t.Fatalf("unhealthy runtime was not reported as a dependency failure: %+v", status)
	}
	if _, rpcErr := server.ensureRekordboxDeps(); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if _, rpcErr := server.resetRekordboxDeps(); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	waitForNotifications(t, output, 2)

	seen := map[int]string{}
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var frame envelope
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatal(err)
		}
		var finished runFinishedParams
		if err := json.Unmarshal(frame.Params, &finished); err != nil {
			t.Fatal(err)
		}
		seen[finished.ExitCode] = finished.Error
	}
	if !strings.Contains(seen[exitcode.MissingDependency], "install failed") {
		t.Fatalf("missing ensure failure terminal state: %+v", seen)
	}
	if !strings.Contains(seen[exitcode.RuntimeFailure], "reset failed") {
		t.Fatalf("missing reset failure terminal state: %+v", seen)
	}
}

func TestRekordboxApplyValidatesChecksumAndReturnsBlockerRows(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeAgentTestConfig(t, dir)
	rbPath := writeAgentRekordboxConfig(t, dir)
	validPlan := testAgentRekordboxPlan(t, dir)
	applyCalls := make(chan app.RekordboxPlaylistSyncApplyRequest, 1)
	ops := &RekordboxOperations{
		Apply: func(_ context.Context, request app.RekordboxPlaylistSyncApplyRequest) (app.RekordboxPlaylistSyncApplyResult, error) {
			applyCalls <- request
			return app.RekordboxPlaylistSyncApplyResult{DryRun: request.DryRun}, nil
		},
	}
	output := &synchronizedBuffer{}
	server := &Server{
		Conn: NewConn(strings.NewReader(""), output), Runs: NewRunRegistry(),
		WorkingDir: dir, ConfigPath: mainPath, RekordboxConfigPath: rbPath,
		RekordboxOps: ops, runContext: context.Background(),
	}

	tampered := validPlan
	tampered.BackupDir = filepath.Join(dir, "client-override")
	if _, rpcErr := server.applyRekordbox(mustJSON(t, rekordboxApplyParams{Plan: tampered})); rpcErr == nil ||
		!strings.Contains(rpcErr.Message, "checksum") {
		t.Fatalf("tampered plan was not rejected before overrides: %+v", rpcErr)
	}
	select {
	case <-applyCalls:
		t.Fatal("tampered plan reached the apply use case")
	default:
	}

	blocked := validPlan
	blocked.Rows = append([]playlistsync.PlanRow(nil), validPlan.Rows...)
	blocked.Rows[0].MatchStatus = "missing"
	blocked.Rows[0].Action = "skip"
	blocked.Summary.MissingInRekordbox = 1
	if err := playlistsync.SignPlan(&blocked); err != nil {
		t.Fatal(err)
	}
	if _, rpcErr := server.applyRekordbox(mustJSON(t, rekordboxApplyParams{Plan: blocked})); rpcErr == nil ||
		!strings.Contains(string(rpcErr.Data), `"match_status":"missing"`) {
		t.Fatalf("blocked plan did not return structured rows: %+v", rpcErr)
	}

	value, rpcErr := server.applyRekordbox(mustJSON(t, rekordboxApplyParams{Plan: validPlan, DryRun: true}))
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if value.(map[string]string)["run_id"] == "" {
		t.Fatal("valid apply did not start a run")
	}
	select {
	case request := <-applyCalls:
		if !request.DryRun || request.Plan.ChecksumSHA256 != validPlan.ChecksumSHA256 {
			t.Fatalf("valid signed plan changed before use case: %+v", request)
		}
	case <-time.After(time.Second):
		t.Fatal("valid apply did not reach the use case")
	}
}

func TestRekordboxPlanBlockersIncludeAmbiguousAndDuplicateRows(t *testing.T) {
	rows := []playlistsync.PlanRow{
		{MusicIndex: 1, Title: "Missing", MatchStatus: "missing", Action: "skip"},
		{MusicIndex: 2, Title: "Ambiguous", MatchStatus: "ambiguous_path", Action: "skip"},
		{MusicIndex: 3, Title: "Duplicate", MatchStatus: "duplicate_path", Action: "skip"},
		{MusicIndex: 4, Title: "Matched", MatchStatus: "matched_path", Action: "keep"},
	}
	blockers := rekordboxPlanBlockers(playlistsync.Plan{
		MusicPlaylist: playlistsync.PlanMusicPlaylist{Name: "Music"},
		Rows:          rows,
	})
	if len(blockers) != 3 {
		t.Fatalf("expected all three blocker kinds, got %+v", blockers)
	}
}
