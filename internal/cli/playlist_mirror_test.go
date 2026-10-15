package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/playlistmirror"
	"github.com/jaa/update-downloads/internal/playlists"
)

func mirrorCLIApp() (*AppContext, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	return &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: out, ErrOut: errOut},
	}, out, errOut
}

func TestPlaylistMirrorCommandsAreRegistered(t *testing.T) {
	app, _, _ := mirrorCLIApp()
	root := newRootCommand(app)
	for _, args := range [][]string{
		{"playlist", "sync", "list", "--help"},
		{"playlist", "sync", "plan", "--help"},
		{"playlist", "sync", "show", "--help"},
		{"playlist", "sync", "apply", "--help"},
	} {
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
}

func TestPlaylistMirrorPlanRequiresJobAndExactDirection(t *testing.T) {
	app, _, _ := mirrorCLIApp()
	root := newRootCommand(app)
	root.SetArgs([]string{"playlist", "sync", "plan", "--direction", string(playlists.DirectionRekordboxToNavidrome)})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "--job is required") {
		t.Fatalf("missing job error = %v", err)
	}

	app, _, _ = mirrorCLIApp()
	root = newRootCommand(app)
	root.SetArgs([]string{"playlist", "sync", "plan", "--job", "favs", "--direction", "to-phone"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "rekordbox-to-navidrome") {
		t.Fatalf("invalid direction error = %v", err)
	}
}

func TestPlaylistMirrorApplyNoInputRequiresForce(t *testing.T) {
	app, _, _ := mirrorCLIApp()
	root := newRootCommand(app)
	root.SetArgs([]string{"playlist", "sync", "apply", "--plan-file", "/tmp/plan.json", "--no-input"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "--force is required with --no-input") {
		t.Fatalf("no-input error = %v", err)
	}
}

func TestPrintPlaylistMirrorPlanNamesDirectionChangesAndBlockers(t *testing.T) {
	app, out, _ := mirrorCLIApp()
	plan := playlistmirror.Plan{
		Direction:         playlists.DirectionRekordboxToNavidrome,
		Source:            playlistmirror.PlaylistDescriptor{Provider: playlistmirror.ProviderRekordbox, ID: "rb", Name: "favs", TrackCount: 2},
		Destination:       playlistmirror.PlaylistDescriptor{Provider: playlistmirror.ProviderNavidrome, Name: "favs"},
		DestinationCreate: true,
		Summary:           playlistmirror.Summary{WillAdd: 1, WillRemove: 1, Blocked: 1, FinalTotal: 2},
		Rows: []playlistmirror.PlanRow{
			{Artist: "DJ", Title: "One", NormalizedPath: "/Music/one.mp3", Action: playlistmirror.ActionAdd},
			{Title: "Missing", Action: playlistmirror.ActionBlocked, Blocker: "no exact match"},
		},
		Blockers: []string{"source row 2: no exact match"}, ChecksumSHA256: "checksum",
	}
	printPlaylistMirrorPlan(app, plan, "/tmp/plan.json")
	for _, want := range []string{"rekordbox → navidrome", "will be created", "+1 -1", "blocked=1", "DJ — One", "BLOCKED: source row 2", "Checksum: checksum"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output does not contain %q:\n%s", want, out.String())
		}
	}
}
