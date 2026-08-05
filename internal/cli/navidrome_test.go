package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/navidrome"
)

func newNavidromeTestApp() (*AppContext, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	return &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: out, ErrOut: errOut},
	}, out, errOut
}

func TestNavidromeCommandTreeIsRegistered(t *testing.T) {
	for _, args := range [][]string{
		{"navidrome", "--help"},
		{"navidrome", "config", "path", "--help"},
		{"navidrome", "config", "show", "--help"},
		{"navidrome", "deps", "status", "--help"},
		{"navidrome", "deps", "ensure", "--help"},
		{"navidrome", "status", "--help"},
		{"navidrome", "setup", "plan", "--help"},
		{"navidrome", "setup", "apply", "--help"},
		{"navidrome", "playlists", "refresh", "--help"},
		{"navidrome", "favorites", "import", "plan", "--help"},
		{"navidrome", "favorites", "import", "apply", "--help"},
		{"navidrome", "favorites", "list", "--help"},
		{"navidrome", "backup", "create", "--help"},
	} {
		app, _, _ := newNavidromeTestApp()
		root := newRootCommand(app)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
}

func TestNavidromeConfigPathHonoursTheExplicitFlag(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "custom.navidrome.yaml")
	app, out, _ := newNavidromeTestApp()
	root := newRootCommand(app)
	root.SetArgs([]string{"--navidrome-config", target, "navidrome", "config", "path"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.TrimSpace(out.String()) != target {
		t.Fatalf("path = %q, want %q", strings.TrimSpace(out.String()), target)
	}
}

func TestNavidromeConfigShowOmitsCredentialFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "navidrome.yaml")
	body := "version: 1\nenabled: true\nserver:\n  username: jaa\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	app, out, _ := newNavidromeTestApp()
	root := newRootCommand(app)
	root.SetArgs([]string{"--navidrome-config", path, "navidrome", "config", "show"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	rendered := out.String()
	if !strings.Contains(rendered, "username: jaa") {
		t.Fatalf("rendered config = %s", rendered)
	}
	for _, forbidden := range []string{"password", "keychain"} {
		if strings.Contains(strings.ToLower(rendered), forbidden) {
			t.Fatalf("config show must not mention %q:\n%s", forbidden, rendered)
		}
	}
}

func TestNavidromeDepsEnsureRequiresConfirm(t *testing.T) {
	app, _, _ := newNavidromeTestApp()
	root := newRootCommand(app)
	root.SetArgs([]string{"navidrome", "deps", "ensure"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "confirmation") {
		t.Fatalf("error = %v, want an explicit-confirmation refusal", err)
	}
}

func TestNavidromeSetupApplyRequiresAPlanFile(t *testing.T) {
	app, _, _ := newNavidromeTestApp()
	root := newRootCommand(app)
	root.SetArgs([]string{"navidrome", "setup", "apply"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--plan-file is required") {
		t.Fatalf("error = %v", err)
	}
}

func TestNavidromeFavoritesApplyRequiresAPlanFile(t *testing.T) {
	app, _, _ := newNavidromeTestApp()
	root := newRootCommand(app)
	root.SetArgs([]string{"navidrome", "favorites", "import", "apply"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--plan-file is required") {
		t.Fatalf("error = %v", err)
	}
}

func TestNavidromeFavoritesApplyRejectsATamperedPlanFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(path, []byte(`{"version":"1","checksum_sha256":"deadbeef"}`), 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	app, _, _ := newNavidromeTestApp()
	root := newRootCommand(app)
	root.SetArgs([]string{"navidrome", "favorites", "import", "apply", "--plan-file", path})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("error = %v, want a checksum refusal", err)
	}
}

func TestPrintFavoritePlanShowsEveryExcludedCategory(t *testing.T) {
	app, out, errOut := newNavidromeTestApp()
	plan := navidrome.FavoritePlan{
		Counts: navidrome.FavoriteCounts{
			SourceTotal: 10, Matched: 4, AlreadyStarred: 1,
			OutsideLibrary: 2, Missing: 2, Ambiguous: 1,
		},
		Warnings: []string{"2 favorites have no matching Navidrome track and will be skipped"},
		Blockers: []string{"the Navidrome library is empty; scan it before importing favorites"},
	}
	printFavoritePlan(app, plan, "")
	rendered := out.String()
	for _, fragment := range []string{"Apple favorites: 10", "Will star: 4", "Outside library: 2", "Missing: 2", "Ambiguous: 1"} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("plan output is missing %q:\n%s", fragment, rendered)
		}
	}
	if !strings.Contains(errOut.String(), "Blocked:") {
		t.Fatalf("blockers must be explicit:\n%s", errOut.String())
	}
}

func TestPrintSetupPlanNamesEveryMutation(t *testing.T) {
	app, out, errOut := newNavidromeTestApp()
	plan := navidrome.SetupPlan{
		ServerVersion: "0.63.2",
		BinaryPath:    "/opt/homebrew/bin/navidrome",
		MusicDir:      "/Users/jaa/Music/downloaded",
		Directories: []navidrome.PlannedDirectory{
			{Label: "data", Path: "/data", Exists: false},
			{Label: "cache", Path: "/cache", Exists: true},
		},
		Files: []navidrome.PlannedFile{
			{Label: "navidrome.toml", Path: "/data/navidrome.toml", Action: navidrome.FileActionCreate},
		},
		Service:  navidrome.PlannedService{Action: navidrome.ServiceActionLoad},
		Blockers: []string{"port 4533 is already in use by PID 42"},
	}
	printSetupPlan(app, plan, "")
	rendered := out.String()
	if !strings.Contains(rendered, "create directory\t/data") {
		t.Fatalf("missing directory line:\n%s", rendered)
	}
	if strings.Contains(rendered, "create directory\t/cache") {
		t.Fatalf("an existing directory must not be listed as a change:\n%s", rendered)
	}
	if !strings.Contains(rendered, "create\t/data/navidrome.toml") {
		t.Fatalf("missing file line:\n%s", rendered)
	}
	if !strings.Contains(errOut.String(), "already in use") {
		t.Fatalf("blockers must be explicit:\n%s", errOut.String())
	}
}

// The starred listing renders without a server, so an empty result is not
// mistaken for a failure and a path-less track does not print a bare dash.
func TestPrintStarredFavoritesRendersEveryShape(t *testing.T) {
	app, out, _ := newNavidromeTestApp()
	printStarredFavorites(app, nil)
	if !strings.Contains(out.String(), "Starred on Navidrome: 0") {
		t.Fatalf("empty listing = %q", out.String())
	}

	app, out, _ = newNavidromeTestApp()
	printStarredFavorites(app, []navidrome.Song{
		{ID: "a", Title: "First", Artist: "Someone", Path: "/music/a.mp3"},
		{ID: "b", Title: "Second"},
	})
	rendered := out.String()
	for _, want := range []string{
		"Starred on Navidrome: 2",
		"Someone — First",
		"/music/a.mp3",
		"Unknown artist — Second",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("listing missing %q: %q", want, rendered)
		}
	}
}
