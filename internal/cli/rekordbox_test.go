package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/rekordbox/playlistsync"
)

func TestRekordboxPlaylistSyncCommandRegistered(t *testing.T) {
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{"rekordbox", "playlist-sync", "plan", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("help failed: %v", err)
	}
}

func TestRekordboxPlaylistSyncApplyNoInputRequiresForce(t *testing.T) {
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{"rekordbox", "playlist-sync", "apply", "--plan-file", "/tmp/plan.json", "--no-input"})

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected --force error")
	}
	if !strings.Contains(err.Error(), "--force is required with --no-input") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrintPlaylistSyncPlanNamesBlockingTracks(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	app := &AppContext{IO: IOStreams{Out: &out, ErrOut: &errOut}}
	plan := playlistsync.Plan{
		Version:       playlistsync.PlanVersion,
		MusicPlaylist: playlistsync.PlanMusicPlaylist{Name: "Favourites", TrackCount: 1},
		RekordboxPlaylist: playlistsync.PlanRekordboxPlaylist{
			Name: "fav_imports",
		},
		Summary: playlistsync.PlanSummary{MusicTotal: 2, MissingInRekordbox: 2},
		Rows: []playlistsync.PlanRow{
			{
				MusicIndex:  1,
				Artist:      "Netherworld",
				Title:       "Atalantis",
				Path:        "/Music/Netherworld/Atalantis.m4a",
				MatchStatus: "missing",
			},
			{
				MusicIndex:  2,
				Artist:      "Second Artist",
				Title:       "Second Track",
				Path:        "/Music/Second Track.m4a",
				MatchStatus: "missing",
			},
		},
	}

	printPlaylistSyncPlan(app, plan, "/tmp/plan.json")

	blockers := errOut.String()
	for _, want := range []string{"Apply blockers:", "Netherworld — Atalantis", "/Music/Netherworld/Atalantis.m4a", "Second Artist — Second Track", "/Music/Second Track.m4a"} {
		if !strings.Contains(blockers, want) {
			t.Fatalf("expected blocker output to contain %q:\n%s", want, blockers)
		}
	}
}

func TestPrintPlaylistSyncPlanNamesDuplicateTrack(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	app := &AppContext{IO: IOStreams{Out: &out, ErrOut: &errOut}}
	plan := playlistsync.Plan{
		Version:           playlistsync.PlanVersion,
		MusicPlaylist:     playlistsync.PlanMusicPlaylist{Name: "Favourites", TrackCount: 2},
		RekordboxPlaylist: playlistsync.PlanRekordboxPlaylist{ID: "p1", Name: "fav_imports"},
		Summary:           playlistsync.PlanSummary{MusicTotal: 2, MatchedByPath: 1, DuplicateInPlaylist: 1},
		Rows: []playlistsync.PlanRow{
			{
				MusicIndex:         1,
				Artist:             "Netherworld",
				Title:              "Atalantis",
				Path:               "/Music/Netherworld/Atalantis.aiff",
				RekordboxContentID: "c1",
				MatchStatus:        "matched_path",
			},
			{
				MusicIndex:         2,
				Artist:             "Netherworld",
				Title:              "Atalantis",
				Path:               "/Music/Netherworld/Atalantis.aiff",
				RekordboxContentID: "c1",
				MatchStatus:        "duplicate_path",
			},
		},
	}

	printPlaylistSyncPlan(app, plan, "/tmp/plan.json")

	blockers := errOut.String()
	for _, want := range []string{"Apply blockers:", "[duplicate_path]", "Netherworld — Atalantis", "/Music/Netherworld/Atalantis.aiff"} {
		if !strings.Contains(blockers, want) {
			t.Fatalf("expected blocker output to contain %q:\n%s", want, blockers)
		}
	}
	if strings.Count(blockers, "[duplicate_path]") != 1 {
		t.Fatalf("expected only the duplicate row to be listed as a blocker:\n%s", blockers)
	}
}

func TestRekordboxConfigPathCommandUsesExplicitPath(t *testing.T) {
	var out bytes.Buffer
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: &out, ErrOut: &bytes.Buffer{}},
	}
	path := filepath.Join(t.TempDir(), "rb.yaml")
	root := newRootCommand(app)
	root.SetArgs([]string{"--rekordbox-config", path, "rekordbox", "config", "path"})

	if err := root.Execute(); err != nil {
		t.Fatalf("config path failed: %v", err)
	}
	if strings.TrimSpace(out.String()) != path {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestRekordboxConfigShowCommandPrintsFeatureConfig(t *testing.T) {
	tmp := t.TempDir()
	rbPath := filepath.Join(tmp, "rb.yaml")
	if err := os.WriteFile(rbPath, []byte(`
version: 1
sync:
  folders:
    - id: phone
      music_folder: Phone
      rekordbox_folder: Phone RB
`), 0o644); err != nil {
		t.Fatalf("write rb config: %v", err)
	}
	var out bytes.Buffer
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: &out, ErrOut: &bytes.Buffer{}},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{"--rekordbox-config", rbPath, "rekordbox", "config", "show"})

	if err := root.Execute(); err != nil {
		t.Fatalf("config show failed: %v", err)
	}
	text := out.String()
	for _, want := range []string{"Path: " + rbPath, "Folder mappings: 1", "id: phone"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in output:\n%s", want, text)
		}
	}
}

// The snapshot source is opt-in: without --playlist-id the plan still reads
// Music.app, and an unknown id fails before anything touches Rekordbox.
func TestPlaylistSyncPlanRejectsAnUnknownPlaylistID(t *testing.T) {
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{"rekordbox", "playlist-sync", "plan", "--playlist-id", "no-such-playlist"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected an error for an unconfigured playlist id")
	}
	if !strings.Contains(err.Error(), "no-such-playlist") {
		t.Fatalf("the error must name the id: %v", err)
	}
}
