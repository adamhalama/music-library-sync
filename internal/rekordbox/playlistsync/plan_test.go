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

func TestRekordboxProcessNamesMatchProductButNotUDLHelpers(t *testing.T) {
	got := rekordboxProcessNames(strings.Join([]string{
		"/Applications/rekordbox 7/rekordbox.app/Contents/MacOS/rekordbox",
		"/Applications/UDL.app/Contents/MacOS/UDL",
		"/Applications/UDL.app/Contents/Resources/udl",
	}, "\n"))
	if len(got) != 1 || got[0] != "rekordbox" {
		t.Fatalf("unexpected process matches: %v", got)
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

func TestBuildPlanBlocksDuplicateTrackInsteadOfRepeatingContentID(t *testing.T) {
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
		MusicPlaylist: music.Playlist{Name: "Favourites", TrackCount: 3},
		MusicTracks: []music.Track{
			{Index: 1, Artist: "Netherworld", Title: "Atalantis", Path: "/Music/Atalantis.aiff"},
			{Index: 2, Artist: "Other", Title: "Two", Path: "/Music/Two.mp3"},
			{Index: 3, Artist: "Netherworld", Title: "Atalantis", Path: "/Music/Atalantis.aiff"},
		},
		Inspect: bridge.InspectResponse{
			Playlists: []bridge.Playlist{{ID: "p1", Name: "fav_imports", Attribute: 0}},
			Contents: []bridge.Content{
				{ID: "c1", Title: "Atalantis", FolderPath: "/Music/Atalantis.aiff"},
				{ID: "c2", Title: "Two", FolderPath: "/Music/Two.mp3"},
			},
		},
	}, time.Unix(0, 0))
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.Summary.DuplicateInPlaylist != 1 || plan.Summary.MatchedByPath != 2 {
		t.Fatalf("expected one duplicate and two matches, got %+v", plan.Summary)
	}
	if total := plan.Summary.MatchedByPath + plan.Summary.MissingInRekordbox + plan.Summary.AmbiguousInRB + plan.Summary.DuplicateInPlaylist; total != plan.Summary.MusicTotal {
		t.Fatalf("row counters %d do not add up to music total %d: %+v", total, plan.Summary.MusicTotal, plan.Summary)
	}
	if got := plan.FinalContentIDs; len(got) != 2 || got[0] != "c1" || got[1] != "c2" {
		t.Fatalf("expected each content ID once, got %#v", got)
	}
	if len(plan.Preconditions.MatchedContent) != 2 {
		t.Fatalf("expected duplicate to be excluded from matched preconditions, got %+v", plan.Preconditions.MatchedContent)
	}
	third := plan.Rows[2]
	if third.MatchStatus != "duplicate_path" || third.Action != "skip" {
		t.Fatalf("expected third row reported as duplicate, got %+v", third)
	}
	if third.Title != "Atalantis" || third.Path != "/Music/Atalantis.aiff" {
		t.Fatalf("expected duplicate row to keep its identity for blocker output, got %+v", third)
	}
	if err := ValidatePlanForApply(plan); err == nil || !strings.Contains(err.Error(), "refuses to mirror duplicates") {
		t.Fatalf("expected duplicate refusal, got %v", err)
	}
}

func TestValidatePlanForApplyRejectsRepeatedContentIDFromEditedPlan(t *testing.T) {
	plan := Plan{
		Version:         PlanVersion,
		Mode:            DefaultMode,
		FinalContentIDs: []string{"c1", "c2", "c1"},
		Summary:         PlanSummary{FinalTargetCount: 3},
	}
	if err := SignPlan(&plan); err != nil {
		t.Fatalf("SignPlan: %v", err)
	}
	if err := ValidatePlanForApply(plan); err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("expected repeated content ID refusal even with zeroed counters, got %v", err)
	}
}

func TestValidatePlanForApplyRejectsDuplicateInFolderOperation(t *testing.T) {
	plan := Plan{
		Version: PlanVersionFolder,
		Mode:    DefaultMode,
		Operations: []PlanOperation{{
			MusicPlaylist:   PlanMusicPlaylist{Name: "Favourites"},
			FinalContentIDs: []string{"c1"},
			Summary:         PlanSummary{FinalTargetCount: 1, DuplicateInPlaylist: 1},
		}},
	}
	if err := SignPlan(&plan); err != nil {
		t.Fatalf("SignPlan: %v", err)
	}
	err := ValidatePlanForApply(plan)
	if err == nil || !strings.Contains(err.Error(), "refuses to mirror duplicates") {
		t.Fatalf("expected folder duplicate refusal, got %v", err)
	}
	if !strings.Contains(err.Error(), "Favourites") {
		t.Fatalf("expected refusal to name the playlist, got %v", err)
	}
}

// A plan with no duplicates must serialize exactly as it did before
// DuplicateInPlaylist existed, so plan files written by older builds still
// verify. VerifyPlanChecksum re-marshals the parsed struct, so an
// always-emitted new field would invalidate every stored plan.
func TestPlanChecksumStaysStableForPlansWithoutDuplicates(t *testing.T) {
	plan, err := BuildPlan(BuildRequest{
		Options: ResolvedOptions{
			MusicPlaylist:     "Favourites",
			RekordboxPlaylist: "fav_imports",
			RekordboxDBDir:    "/tmp/rb",
			BackupDir:         "/tmp/backups",
			Mode:              DefaultMode,
			CreatePlaylist:    true,
		},
		MusicPlaylist: music.Playlist{Name: "Favourites", TrackCount: 1},
		MusicTracks:   []music.Track{{Index: 1, Title: "One", Path: "/Music/One.mp3"}},
		Inspect: bridge.InspectResponse{
			Playlists: []bridge.Playlist{{ID: "p1", Name: "fav_imports", Attribute: 0}},
			Contents:  []bridge.Content{{ID: "c1", Title: "One", FolderPath: "/Music/One.mp3"}},
		},
	}, time.Unix(0, 0))
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	path := filepath.Join(t.TempDir(), "plan.json")
	if err := WritePlan(path, plan); err != nil {
		t.Fatalf("WritePlan: %v", err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(payload), "duplicate_in_playlist") {
		t.Fatalf("expected zero duplicate count to stay out of the plan payload:\n%s", payload)
	}
	reread, err := ReadPlan(path)
	if err != nil {
		t.Fatalf("ReadPlan: %v", err)
	}
	if err := VerifyPlanChecksum(reread); err != nil {
		t.Fatalf("VerifyPlanChecksum after round trip: %v", err)
	}
}

func TestNormalizePathKeepsLiteralPercentInFileURL(t *testing.T) {
	// url.Parse already decodes %2520 to %20; unescaping a second time would
	// turn the literal percent sign into a space and break path matching.
	if got, want := NormalizePath("file:///Music/100%2520mix.mp3"), "/Music/100%20mix.mp3"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if got, want := NormalizePath("file:///Music/One%20Two.mp3"), "/Music/One Two.mp3"; got != want {
		t.Fatalf("expected percent-encoded space to decode once, got %q (want %q)", got, want)
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
