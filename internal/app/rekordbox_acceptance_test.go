package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/rekordbox/bridge"
	"github.com/jaa/update-downloads/internal/rekordbox/music"
	"github.com/jaa/update-downloads/internal/rekordbox/playlistsync"
)

// TestRekordboxIsolatedApplyAndRestore is an opt-in destructive-path
// acceptance test. All mutation is confined to t.TempDir; the repository
// fixture and the user's live Rekordbox database are never opened for write.
//
// Run with:
//
//	UDL_REKORDBOX_ACCEPTANCE_PYTHON=/path/to/python go test ./internal/app \
//	  -run TestRekordboxIsolatedApplyAndRestore -count=1 -v
func TestRekordboxIsolatedApplyAndRestore(t *testing.T) {
	pythonBin := strings.TrimSpace(os.Getenv("UDL_REKORDBOX_ACCEPTANCE_PYTHON"))
	if pythonBin == "" {
		t.Skip("set UDL_REKORDBOX_ACCEPTANCE_PYTHON to a Python runtime with pyrekordbox")
	}
	if !filepath.IsAbs(pythonBin) {
		t.Fatal("UDL_REKORDBOX_ACCEPTANCE_PYTHON must be an absolute path")
	}

	root := t.TempDir()
	fixtureDir := filepath.Clean("../../experiments/rb-date-order/pyrekordbox-lab/sandbox-db")
	dbDir := filepath.Join(root, "rekordbox-db")
	backupRoot := filepath.Join(root, "backups")
	if err := copyAcceptanceTree(fixtureDir, dbDir); err != nil {
		t.Fatalf("copy isolated fixture: %v", err)
	}

	ctx := context.Background()
	client := bridge.Client{PythonBin: pythonBin}
	inspect, err := client.Inspect(ctx, dbDir)
	if err != nil {
		t.Fatalf("inspect isolated fixture: %v", err)
	}
	target, tracks := acceptanceTargetAndTracks(t, inspect)

	now := time.Date(2026, 7, 30, 20, 0, 0, 0, time.UTC)
	plan, err := playlistsync.BuildPlan(playlistsync.BuildRequest{
		Options: playlistsync.ResolvedOptions{
			MusicPlaylist:       "UDL isolated acceptance",
			RekordboxPlaylist:   target.Name,
			RekordboxPlaylistID: target.ID,
			RekordboxDBDir:      dbDir,
			PythonBin:           pythonBin,
			BackupDir:           backupRoot,
			Mode:                playlistsync.DefaultMode,
			CreatePlaylist:      false,
		},
		MusicPlaylist: music.Playlist{
			Name:         "UDL isolated acceptance",
			PersistentID: "udl-isolated-acceptance",
			TrackCount:   len(tracks),
		},
		MusicTracks: tracks,
		Inspect:     inspect,
	}, now)
	if err != nil {
		t.Fatalf("build isolated plan: %v", err)
	}
	if reflect.DeepEqual(plan.FinalContentIDs, target.ContentIDs) {
		t.Fatal("acceptance plan must change the target playlist")
	}

	before, err := hashAcceptanceTree(dbDir)
	if err != nil {
		t.Fatalf("hash pre-apply fixture: %v", err)
	}
	cfg := config.DefaultConfig()
	cfg.Defaults.StateDir = filepath.Join(root, "state")
	cfg.Rekordbox = &config.RekordboxConfig{
		DBDir:     dbDir,
		PythonBin: pythonBin,
		BackupDir: backupRoot,
	}
	useCase := RekordboxPlaylistSyncUseCase{
		Bridge: client,
		Now:    func() time.Time { return now },
	}
	result, err := useCase.Apply(ctx, RekordboxPlaylistSyncApplyRequest{
		Config:    cfg,
		Plan:      plan,
		PythonBin: pythonBin,
		BackupDir: backupRoot,
	})
	if err != nil {
		t.Fatalf("apply isolated plan: %v", err)
	}
	if result.BackupPath == "" || filepath.Dir(result.BackupPath) != backupRoot {
		t.Fatalf("unexpected reported backup path %q", result.BackupPath)
	}
	if !playlistsync.SameStrings(result.Response.FinalContentIDs, plan.FinalContentIDs) {
		t.Fatalf("apply result order=%v, want %v", result.Response.FinalContentIDs, plan.FinalContentIDs)
	}

	afterApply, err := hashAcceptanceTree(dbDir)
	if err != nil {
		t.Fatalf("hash applied fixture: %v", err)
	}
	if reflect.DeepEqual(before, afterApply) {
		t.Fatal("isolated apply did not change the database directory")
	}

	appliedInspect, err := client.Inspect(ctx, dbDir)
	if err != nil {
		t.Fatalf("inspect applied fixture: %v", err)
	}
	appliedTarget := acceptancePlaylistByID(t, appliedInspect.Playlists, target.ID)
	if !playlistsync.SameStrings(appliedTarget.ContentIDs, plan.FinalContentIDs) {
		t.Fatalf("applied playlist order=%v, want %v", appliedTarget.ContentIDs, plan.FinalContentIDs)
	}

	quarantine := filepath.Join(root, "applied-quarantine")
	if err := os.Rename(dbDir, quarantine); err != nil {
		t.Fatalf("quarantine applied database: %v", err)
	}
	if err := copyAcceptanceTree(result.BackupPath, dbDir); err != nil {
		t.Fatalf("restore exact reported backup: %v", err)
	}

	restored, err := hashAcceptanceTree(dbDir)
	if err != nil {
		t.Fatalf("hash restored fixture: %v", err)
	}
	if !reflect.DeepEqual(before, restored) {
		t.Fatalf("restored directory differs from pre-apply fixture:\nbefore=%v\nrestored=%v", before, restored)
	}
	restoredInspect, err := client.Inspect(ctx, dbDir)
	if err != nil {
		t.Fatalf("inspect restored fixture: %v", err)
	}
	restoredTarget := acceptancePlaylistByID(t, restoredInspect.Playlists, target.ID)
	if !playlistsync.SameStrings(restoredTarget.ContentIDs, target.ContentIDs) {
		t.Fatalf("restored playlist order=%v, want %v", restoredTarget.ContentIDs, target.ContentIDs)
	}

	t.Logf(
		"isolated apply changed playlist %q from %d to %d tracks; backup %s restored byte-for-byte",
		target.Name,
		len(target.ContentIDs),
		len(plan.FinalContentIDs),
		result.BackupPath,
	)
}

func acceptanceTargetAndTracks(t *testing.T, inspect bridge.InspectResponse) (bridge.Playlist, []music.Track) {
	t.Helper()
	pathCounts := make(map[string]int, len(inspect.Contents))
	for _, content := range inspect.Contents {
		pathCounts[playlistsync.NormalizePath(content.FolderPath)]++
	}
	unique := make([]bridge.Content, 0, len(inspect.Contents))
	for _, content := range inspect.Contents {
		path := playlistsync.NormalizePath(content.FolderPath)
		if path != "" && pathCounts[path] == 1 {
			unique = append(unique, content)
		}
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i].ID < unique[j].ID })
	if len(unique) < 2 {
		t.Fatal("isolated fixture needs at least two uniquely pathed contents")
	}

	var target bridge.Playlist
	found := false
	for _, playlist := range inspect.Playlists {
		if playlist.Attribute == 0 {
			target = playlist
			found = true
			break
		}
	}
	if !found {
		t.Fatal("isolated fixture has no normal target playlist")
	}

	desired := unique[:2]
	if len(target.ContentIDs) == 2 &&
		target.ContentIDs[0] == desired[0].ID &&
		target.ContentIDs[1] == desired[1].ID {
		desired[0], desired[1] = desired[1], desired[0]
	}
	tracks := make([]music.Track, 0, len(desired))
	for idx, content := range desired {
		tracks = append(tracks, music.Track{
			Index:        idx + 1,
			PersistentID: fmt.Sprintf("acceptance-%d", idx+1),
			DatabaseID:   fmt.Sprintf("acceptance-db-%d", idx+1),
			Title:        content.Title,
			Path:         content.FolderPath,
		})
	}
	return target, tracks
}

func acceptancePlaylistByID(t *testing.T, playlists []bridge.Playlist, id string) bridge.Playlist {
	t.Helper()
	for _, playlist := range playlists {
		if playlist.ID == id {
			return playlist
		}
	}
	t.Fatalf("playlist ID %q not found", id)
	return bridge.Playlist{}
}

func copyAcceptanceTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, payload, info.Mode().Perm())
	})
}

func hashAcceptanceTree(root string) (map[string][sha256.Size]byte, error) {
	result := map[string][sha256.Size]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[relative] = sha256.Sum256(payload)
		return nil
	})
	return result, err
}
