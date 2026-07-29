package playlistsync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/rekordbox/bridge"
	"github.com/jaa/update-downloads/internal/rekordbox/music"
	"github.com/jaa/update-downloads/internal/rekordbox/syncconfig"
)

func TestResolveOptionsExpandsPortableDefaultBackupDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	resolved, err := ResolveOptions(config.DefaultConfig(), Options{})
	if err != nil {
		t.Fatalf("ResolveOptions: %v", err)
	}
	if want := filepath.Join(home, "Music", "rb-library-export"); resolved.BackupDir != want {
		t.Fatalf("expected portable backup dir %q, got %q", want, resolved.BackupDir)
	}
}

func TestBuildPlanMatchesByExactNormalizedPath(t *testing.T) {
	opts := ResolvedOptions{
		MusicPlaylist:     "Favourites",
		RekordboxPlaylist: "fav_imports",
		RekordboxDBDir:    "/tmp/rb",
		BackupDir:         "/tmp/backups",
		Mode:              DefaultMode,
		CreatePlaylist:    true,
	}
	playlist := music.Playlist{Name: "Favourites", PersistentID: "pid", Smart: true, TrackCount: 2}
	tracks := []music.Track{
		{Index: 1, PersistentID: "m1", DatabaseID: "1", Artist: "A", Title: "One", Path: "/Music/One.mp3"},
		{Index: 2, PersistentID: "m2", DatabaseID: "2", Artist: "B", Title: "Two", Path: "/Music/Two.mp3"},
	}
	inspect := bridge.InspectResponse{
		Playlists: []bridge.Playlist{{ID: "3150438241", Name: "fav_imports", Attribute: 0, ParentID: "root", ContentIDs: []string{"old"}}},
		Contents: []bridge.Content{
			{ID: "c1", Title: "One", FolderPath: "/Music/One.mp3"},
			{ID: "c2", Title: "Two", FolderPath: "/Music/Two.mp3"},
		},
	}

	plan, err := BuildPlan(BuildRequest{Options: opts, MusicPlaylist: playlist, MusicTracks: tracks, Inspect: inspect}, time.Unix(0, 0))
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.Summary.MatchedByPath != 2 || plan.Summary.MissingInRekordbox != 0 {
		t.Fatalf("unexpected match summary: %+v", plan.Summary)
	}
	if plan.Summary.WillAdd != 2 || plan.Summary.WillRemove != 1 || plan.Summary.FinalTargetCount != 2 {
		t.Fatalf("unexpected action summary: %+v", plan.Summary)
	}
	if got := plan.FinalContentIDs; len(got) != 2 || got[0] != "c1" || got[1] != "c2" {
		t.Fatalf("unexpected final IDs: %#v", got)
	}
	if err := VerifyPlanChecksum(plan); err != nil {
		t.Fatalf("VerifyPlanChecksum: %v", err)
	}
}

func TestBuildPlanFlagsAmbiguousPath(t *testing.T) {
	opts := ResolvedOptions{
		MusicPlaylist:     "Favourites",
		RekordboxPlaylist: "fav_imports",
		RekordboxDBDir:    "/tmp/rb",
		BackupDir:         "/tmp/backups",
		Mode:              DefaultMode,
		CreatePlaylist:    true,
	}
	inspect := bridge.InspectResponse{
		Playlists: []bridge.Playlist{{ID: "p1", Name: "fav_imports", Attribute: 0}},
		Contents: []bridge.Content{
			{ID: "c1", Title: "One", FolderPath: "/Music/One.mp3"},
			{ID: "c2", Title: "One copy", FolderPath: "/Music/One.mp3"},
		},
	}
	plan, err := BuildPlan(BuildRequest{
		Options:       opts,
		MusicPlaylist: music.Playlist{Name: "Favourites", TrackCount: 1},
		MusicTracks:   []music.Track{{Index: 1, Title: "One", Path: "/Music/One.mp3"}},
		Inspect:       inspect,
	}, time.Unix(0, 0))
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.Summary.AmbiguousInRB != 1 {
		t.Fatalf("expected ambiguous match, got %+v", plan.Summary)
	}
	if err := ValidatePlanForApply(plan); err == nil {
		t.Fatalf("expected apply validation to reject ambiguous plan")
	}
}

func TestValidatePlanForApplyRejectsMissingTrackWithoutPartialMirror(t *testing.T) {
	opts := ResolvedOptions{
		MusicPlaylist:     "Favourites",
		RekordboxPlaylist: "fav_imports",
		RekordboxDBDir:    "/tmp/rb",
		BackupDir:         "/tmp/backups",
		Mode:              DefaultMode,
		CreatePlaylist:    true,
	}
	plan, err := BuildPlan(BuildRequest{
		Options:       opts,
		MusicPlaylist: music.Playlist{Name: "Favourites", TrackCount: 2},
		MusicTracks: []music.Track{
			{Index: 1, Artist: "Artist", Title: "Matched", Path: "/Music/Matched.mp3"},
			{Index: 2, Artist: "Netherworld", Title: "Atalantis", Path: "/Music/Atalantis.mp3"},
		},
		Inspect: bridge.InspectResponse{
			Playlists: []bridge.Playlist{{ID: "p1", Name: "fav_imports", Attribute: 0}},
			Contents:  []bridge.Content{{ID: "c1", Title: "Matched", FolderPath: "/Music/Matched.mp3"}},
		},
	}, time.Unix(0, 0))
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.Summary.MissingInRekordbox != 1 || len(plan.Rows) != 2 {
		t.Fatalf("expected full two-row blocked plan, got summary=%+v rows=%+v", plan.Summary, plan.Rows)
	}
	if len(plan.FinalContentIDs) != 1 {
		t.Fatalf("expected only matched content ID in unapplied final membership, got %v", plan.FinalContentIDs)
	}
	if err := ValidatePlanForApply(plan); err == nil || !strings.Contains(err.Error(), "refuses partial mirror apply") {
		t.Fatalf("expected partial mirror refusal, got %v", err)
	}
}

func TestResolveOptionsUsesConfigJobAndEnvStyleOverrides(t *testing.T) {
	create := false
	cfg := config.DefaultConfig()
	cfg.Rekordbox = &config.RekordboxConfig{
		DBDir:     "/rb",
		PythonBin: "/python",
		BackupDir: "/backups",
		PlaylistSync: config.RekordboxPlaylistSyncConfig{Jobs: []config.RekordboxPlaylistSyncJob{{
			ID:                "apple-favourites",
			MusicPlaylist:     "Favourites",
			MusicPlaylistID:   "music-id",
			RekordboxPlaylist: "fav_imports",
			Mode:              "mirror",
			CreatePlaylist:    &create,
		}}},
	}
	resolved, err := ResolveOptions(cfg, Options{JobID: "apple-favourites", RekordboxPlaylistID: "rb-id"})
	if err != nil {
		t.Fatalf("ResolveOptions: %v", err)
	}
	if resolved.MusicPlaylistID != "music-id" || resolved.RekordboxPlaylistID != "rb-id" {
		t.Fatalf("unexpected resolved IDs: %+v", resolved)
	}
	if resolved.BackupDir != "/backups" {
		t.Fatalf("expected explicit backup dir to be preserved, got %q", resolved.BackupDir)
	}
	if resolved.CreatePlaylist {
		t.Fatalf("expected job create_playlist=false")
	}
}

func TestBuildFolderPlanMirrorsChildPlaylists(t *testing.T) {
	opts := ResolvedOptions{
		MappingID:         "phone",
		RekordboxDBDir:    "/tmp/rb",
		BackupDir:         "/tmp/backups",
		Mode:              DefaultMode,
		CreatePlaylist:    true,
		RekordboxPlaylist: DefaultRekordboxPlaylist,
		MusicPlaylist:     DefaultMusicPlaylist,
	}
	mapping := syncconfig.FolderMapping{
		ID:              "phone",
		MusicFolder:     "Phone",
		RekordboxFolder: "Phone RB",
		PlaylistNameMap: map[string]string{"Favourites": "fav_imports"},
	}
	inspect := bridge.InspectResponse{
		Playlists: []bridge.Playlist{
			{ID: "folder-1", Name: "Phone RB", Attribute: 1, ParentID: "root"},
			{ID: "playlist-1", Name: "fav_imports", Attribute: 0, ParentID: "folder-1", ContentIDs: []string{"old"}},
		},
		Contents: []bridge.Content{{ID: "c1", Title: "One", FolderPath: "/Music/One.mp3"}},
	}

	plan, err := BuildFolderPlan(FolderBuildRequest{
		Options:     opts,
		Mapping:     mapping,
		MusicFolder: music.Playlist{Name: "Phone", PersistentID: "folder-pid", Folder: true},
		MusicChildren: []FolderMusicPlaylistTracks{{
			Playlist: music.Playlist{Name: "Favourites", PersistentID: "fav-pid", Smart: true, TrackCount: 1, ParentID: "folder-pid"},
			Tracks:   []music.Track{{Index: 1, Title: "One", Path: "/Music/One.mp3"}},
		}},
		Inspect: inspect,
	}, time.Unix(0, 0))
	if err != nil {
		t.Fatalf("BuildFolderPlan: %v", err)
	}
	if plan.Version != PlanVersionFolder || len(plan.Operations) != 1 {
		t.Fatalf("expected folder plan operation, got version=%s ops=%d", plan.Version, len(plan.Operations))
	}
	op := plan.Operations[0]
	if op.RekordboxPlaylist.Name != "fav_imports" || op.Preconditions.TargetParentID != "folder-1" {
		t.Fatalf("unexpected operation target: %+v preconditions=%+v", op.RekordboxPlaylist, op.Preconditions)
	}
	if plan.Summary.MatchedByPath != 1 || plan.Summary.WillAdd != 1 || plan.Summary.WillRemove != 1 {
		t.Fatalf("unexpected folder summary: %+v", plan.Summary)
	}
	if err := ValidatePlanForApply(plan); err != nil {
		t.Fatalf("ValidatePlanForApply: %v", err)
	}
}
