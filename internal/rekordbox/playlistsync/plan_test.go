package playlistsync

import (
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/rekordbox/bridge"
	"github.com/jaa/update-downloads/internal/rekordbox/music"
)

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
	if resolved.CreatePlaylist {
		t.Fatalf("expected job create_playlist=false")
	}
}
