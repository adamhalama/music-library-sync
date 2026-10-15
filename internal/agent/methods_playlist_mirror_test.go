package agent

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jaa/update-downloads/internal/playlistmirror"
	"github.com/jaa/update-downloads/internal/playlists"
)

func TestPlaylistMirrorMethodsAreAdvertised(t *testing.T) {
	advertised := map[string]bool{}
	for _, method := range protocolMethods {
		advertised[method] = true
	}
	for _, method := range []string{"playlistSync.inspect", "playlistSync.plan", "playlistSync.apply"} {
		if !advertised[method] {
			t.Fatalf("method %q is not advertised", method)
		}
	}
}

func TestPlaylistMirrorInspectReturnsConfiguredJobsWithoutProviders(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeAgentTestConfig(t, dir)
	playlistPath := filepath.Join(dir, "udl.playlists.yaml")
	if err := playlists.Save(playlistPath, playlists.Config{Version: playlists.ConfigVersion, SyncJobs: []playlists.SyncJob{{
		ID: "favs", Rekordbox: playlists.PlaylistSelector{Playlist: "favs"}, Navidrome: playlists.PlaylistSelector{Playlist: "favs"},
	}}}); err != nil {
		t.Fatal(err)
	}
	server := &Server{WorkingDir: dir, ConfigPath: mainPath, PlaylistsConfigPath: playlistPath}
	value, rpcErr := server.inspectPlaylistMirror()
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	rows := value.(map[string]any)["jobs"].([]playlistMirrorInspectRow)
	if len(rows) != 1 || rows[0].Job.ID != "favs" || rows[0].State != nil || rows[0].StateError != "" {
		t.Fatalf("unexpected inspect rows: %#v", rows)
	}
}

func TestPlaylistMirrorPlanAndApplyRejectInvalidContractsBeforeStartingRun(t *testing.T) {
	server := &Server{Runs: NewRunRegistry(), runContext: context.Background()}
	if _, rpcErr := server.planPlaylistMirror(mustJSON(t, playlistMirrorPlanParams{JobID: "favs", Direction: "to-phone"})); rpcErr == nil || rpcErr.Code != CodeInvalidParams {
		t.Fatalf("invalid direction RPC error = %#v", rpcErr)
	}
	blocked := playlistmirror.Plan{
		Version: playlistmirror.PlanVersion, JobID: "favs", Direction: playlists.DirectionRekordboxToNavidrome,
		Blockers: []string{"unmatched path"},
	}
	if _, rpcErr := server.applyPlaylistMirror(mustJSON(t, playlistMirrorApplyParams{Plan: blocked})); rpcErr == nil || rpcErr.Code != CodeInvalidParams {
		t.Fatalf("blocked apply RPC error = %#v", rpcErr)
	}
}
