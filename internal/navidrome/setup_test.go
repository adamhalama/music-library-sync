package navidrome

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupHarness builds a Planner whose whole world is a temporary HOME and a
// scripted command runner, so planning and applying never touch the real
// machine.
type setupHarness struct {
	Home    string
	Config  Config
	Planner Planner
	Calls   *fakeCommands
}

func newSetupHarness(t *testing.T) *setupHarness {
	t.Helper()
	requireDarwinConfig(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	music := filepath.Join(home, "Music", "downloaded")
	if err := os.MkdirAll(music, 0o755); err != nil {
		t.Fatalf("create music dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, "Library", "LaunchAgents"), 0o755); err != nil {
		t.Fatalf("create LaunchAgents: %v", err)
	}

	binary := writeFakeBinary(t, "navidrome")
	calls := &fakeCommands{response: func(name string, args ...string) ([]byte, error) {
		switch {
		case name == binary:
			return []byte("navidrome version 0.63.2 (abc)"), nil
		case name == "lsof":
			return []byte(""), nil
		case name == "launchctl" && len(args) > 0 && args[0] == "print":
			return []byte(""), errNotLoaded
		default:
			return []byte(""), nil
		}
	}}

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Server.Username = "jaa"
	normalize(&cfg)

	planner := Planner{
		Deps: DependencyChecker{
			Run:      calls.run,
			LookPath: func(string) (string, error) { return binary, nil },
		},
		Service: Service{Run: calls.run, UID: "501", LookupHost: func() (string, string) { return "test.local", "127.0.0.1" }},
		Now:     func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}
	return &setupHarness{Home: home, Config: cfg, Planner: planner, Calls: calls}
}

var errNotLoaded = &notLoadedError{}

type notLoadedError struct{}

func (e *notLoadedError) Error() string { return "could not find service" }

func TestPlanIsApplicableAndWritesNothing(t *testing.T) {
	h := newSetupHarness(t)
	before := snapshotTree(t, h.Home)

	plan, err := h.Planner.Plan(context.Background(), h.Config, "")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !plan.Applicable() {
		t.Fatalf("plan blockers = %v", plan.Blockers)
	}
	if !plan.Changes() {
		t.Fatalf("a fresh install must plan changes")
	}
	if err := VerifySetupPlanChecksum(plan); err != nil {
		t.Fatalf("VerifySetupPlanChecksum: %v", err)
	}
	if after := snapshotTree(t, h.Home); after != before {
		t.Fatalf("planning mutated the filesystem:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	for _, call := range h.Calls.calls {
		if call.name == "launchctl" && len(call.args) > 0 && call.args[0] != "print" {
			t.Fatalf("planning changed the service: %+v", call)
		}
	}
}

func TestPlanOmitsCredentialFields(t *testing.T) {
	h := newSetupHarness(t)
	plan, err := h.Planner.Plan(context.Background(), h.Config, "")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	rendered := strings.ToLower(mustJSON(t, plan))
	for _, forbidden := range []string{"password", "secret"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("plan must not contain %q", forbidden)
		}
	}
}

func TestPlanChecksumRejectsTampering(t *testing.T) {
	h := newSetupHarness(t)
	plan, err := h.Planner.Plan(context.Background(), h.Config, "")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	plan.Port = 9999
	if err := VerifySetupPlanChecksum(plan); err == nil {
		t.Fatalf("a modified plan must fail verification")
	}
	plan.ChecksumSHA256 = ""
	if err := VerifySetupPlanChecksum(plan); err == nil {
		t.Fatalf("an unsigned plan must fail verification")
	}
}

func TestPlanBlocksUnownedConfig(t *testing.T) {
	h := newSetupHarness(t)
	resolved, err := Resolve(h.Config)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(resolved.ConfigFile), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(resolved.ConfigFile, []byte("Port = 4533\n"), 0o600); err != nil {
		t.Fatalf("write foreign config: %v", err)
	}
	plan, err := h.Planner.Plan(context.Background(), h.Config, "")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Applicable() {
		t.Fatalf("an unowned config must block apply")
	}
	if !containsFragment(plan.Blockers, "not UDL-managed") {
		t.Fatalf("blockers = %v", plan.Blockers)
	}
	if _, err := h.Planner.Apply(context.Background(), h.Config, plan); err == nil {
		t.Fatalf("apply must refuse a blocked plan")
	}
	body, err := os.ReadFile(resolved.ConfigFile)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(body) != "Port = 4533\n" {
		t.Fatalf("the foreign config was modified: %q", body)
	}
}

func TestPlanBlocksPortConflict(t *testing.T) {
	h := newSetupHarness(t)
	h.Calls.response = func(name string, args ...string) ([]byte, error) {
		switch {
		case name == "lsof":
			return []byte("4242\n"), nil
		case name == "launchctl":
			return []byte(""), errNotLoaded
		default:
			return []byte("navidrome version 0.63.2 (abc)"), nil
		}
	}
	plan, err := h.Planner.Plan(context.Background(), h.Config, "")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !containsFragment(plan.Blockers, "already in use") {
		t.Fatalf("blockers = %v", plan.Blockers)
	}
}

func TestApplyWritesOwnedFilesAndStartsService(t *testing.T) {
	h := newSetupHarness(t)
	plan, err := h.Planner.Plan(context.Background(), h.Config, "")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	result, err := h.Planner.Apply(context.Background(), h.Config, plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(result.WrittenFiles) != 2 {
		t.Fatalf("written files = %v", result.WrittenFiles)
	}
	resolved, err := Resolve(h.Config)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for _, path := range []string{resolved.ConfigFile, resolved.LaunchAgent} {
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		if !IsUDLOwned(string(body)) {
			t.Fatalf("%s is missing the ownership marker", path)
		}
	}
	info, err := os.Stat(resolved.ConfigFile)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v, want 0600", info.Mode().Perm())
	}
	if !sawCommand(h.Calls, "launchctl", "bootstrap") {
		t.Fatalf("apply must bootstrap the agent: %+v", h.Calls.calls)
	}
}

func TestApplyRejectsStalePlan(t *testing.T) {
	h := newSetupHarness(t)
	plan, err := h.Planner.Plan(context.Background(), h.Config, "")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if _, err := h.Planner.Apply(context.Background(), h.Config, plan); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	// The plan was built when nothing existed; replaying it must be refused
	// because the on-disk state no longer matches its recorded precondition.
	_, err = h.Planner.Apply(context.Background(), h.Config, plan)
	if err == nil || !strings.Contains(err.Error(), "regenerate the plan") {
		t.Fatalf("stale apply error = %v, want a regenerate hint", err)
	}
}

// Both fresh installs and replacements must reject a plan for a different
// desired configuration without creating directories, rewriting files, or
// issuing mutating launchctl commands.
func TestApplyRejectsConfigurationDriftBeforeMutation(t *testing.T) {
	changes := []struct {
		name   string
		change func(*Config, string)
	}{
		{"port", func(c *Config, _ string) { c.Server.Port++ }},
		{"address", func(c *Config, _ string) { c.Server.Address = "127.0.0.1" }},
		{"music", func(c *Config, home string) { c.Paths.MusicDir = filepath.Join(home, "other-music") }},
		{"data", func(c *Config, home string) { c.Paths.DataDir = filepath.Join(home, "other-data") }},
		{"cache", func(c *Config, home string) { c.Paths.CacheDir = filepath.Join(home, "other-cache") }},
		{"log", func(c *Config, home string) { c.Paths.LogFile = filepath.Join(home, "other-logs", "server.log") }},
		{"config", func(c *Config, home string) { c.Paths.ConfigFile = filepath.Join(home, "other-config", "server.toml") }},
		{"backups", func(c *Config, home string) { c.Paths.BackupDir = filepath.Join(home, "other-backups") }},
		{"playlists", func(c *Config, _ string) { c.Paths.PlaylistsPath = "other-playlists" }},
		{"scan schedule", func(c *Config, _ string) { c.Scan.Schedule = "@every 2h" }},
		{"backup schedule", func(c *Config, _ string) { c.Backup.Schedule = "0 4 * * *" }},
		{"backup count", func(c *Config, _ string) { c.Backup.Count++ }},
		{"invalid configuration", func(c *Config, _ string) { c.Server.Port = -1 }},
	}
	for _, installed := range []bool{false, true} {
		for _, change := range changes {
			name := "fresh/"
			if installed {
				name = "installed/"
			}
			t.Run(name+change.name, func(t *testing.T) {
				h := newSetupHarness(t)
				plan, err := h.Planner.Plan(context.Background(), h.Config, "")
				if err != nil {
					t.Fatal(err)
				}
				if installed {
					if _, err := h.Planner.Apply(context.Background(), h.Config, plan); err != nil {
						t.Fatal(err)
					}
					// Plan a replacement, not merely an unchanged file.
					h.Config.Scan.Schedule = "@every 3h"
					plan, err = h.Planner.Plan(context.Background(), h.Config, "")
					if err != nil {
						t.Fatal(err)
					}
				}
				change.change(&h.Config, h.Home)
				if change.name == "music" {
					if err := os.MkdirAll(h.Config.Paths.MusicDir, 0o755); err != nil {
						t.Fatal(err)
					}
				}
				assertSetupApplyRefusesWithoutMutation(t, h, plan)
			})
		}
	}
}

func TestApplyRejectsDependencyDriftBeforeMutation(t *testing.T) {
	for _, change := range []string{"binary", "ffmpeg", "unknown version"} {
		t.Run(change, func(t *testing.T) {
			h := newSetupHarness(t)
			plan, err := h.Planner.Plan(context.Background(), h.Config, "")
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "binary", "ffmpeg":
				original := h.Planner.Deps.LookPath
				h.Planner.Deps.LookPath = func(name string) (string, error) {
					if name == "navidrome" && change == "binary" || name == "ffmpeg" && change == "ffmpeg" {
						return filepath.Join(h.Home, "other", name), nil
					}
					return original(name)
				}
			case "unknown version":
				original := h.Calls.response
				h.Calls.response = func(name string, args ...string) ([]byte, error) {
					if len(args) == 1 && args[0] == "--version" {
						return nil, errNotLoaded
					}
					return original(name, args...)
				}
			}
			assertSetupApplyRefusesWithoutMutation(t, h, plan)
		})
	}
}

func assertSetupApplyRefusesWithoutMutation(t *testing.T, h *setupHarness, plan SetupPlan) {
	t.Helper()
	before := snapshotTree(t, h.Home)
	h.Calls.calls = nil
	result, err := h.Planner.Apply(context.Background(), h.Config, plan)
	if err == nil || !strings.Contains(err.Error(), "regenerate the plan") {
		t.Errorf("Apply error = %v, want refusal with regenerate hint", err)
	}
	if len(result.WrittenFiles) != 0 || len(result.CreatedDirectories) != 0 || result.ServiceAction != "" {
		t.Errorf("refused apply reported mutations: %+v", result)
	}
	if after := snapshotTree(t, h.Home); after != before {
		t.Errorf("refused apply mutated filesystem:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	for _, call := range h.Calls.calls {
		if call.name == "launchctl" && len(call.args) > 0 && call.args[0] != "print" {
			t.Errorf("refused apply changed service: %+v", call)
		}
	}
}

func TestApplyIsIdempotentThroughReplanning(t *testing.T) {
	h := newSetupHarness(t)
	first, err := h.Planner.Plan(context.Background(), h.Config, "")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if _, err := h.Planner.Apply(context.Background(), h.Config, first); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	second, err := h.Planner.Plan(context.Background(), h.Config, "")
	if err != nil {
		t.Fatalf("re-Plan: %v", err)
	}
	for _, file := range second.Files {
		if file.Action != FileActionUnchanged {
			t.Fatalf("file %s = %s, want unchanged", file.Path, file.Action)
		}
	}
	result, err := h.Planner.Apply(context.Background(), h.Config, second)
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if len(result.WrittenFiles) != 0 {
		t.Fatalf("second apply rewrote files: %v", result.WrittenFiles)
	}
}

func TestSetupPlanFileRoundTrip(t *testing.T) {
	h := newSetupHarness(t)
	plan, err := h.Planner.Plan(context.Background(), h.Config, "")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := WriteSetupPlan(path, plan); err != nil {
		t.Fatalf("WriteSetupPlan: %v", err)
	}
	reread, err := ReadSetupPlan(path)
	if err != nil {
		t.Fatalf("ReadSetupPlan: %v", err)
	}
	if reread.ChecksumSHA256 != plan.ChecksumSHA256 {
		t.Fatalf("checksum changed across the round trip")
	}
}

func sawCommand(calls *fakeCommands, name string, firstArg string) bool {
	for _, call := range calls.calls {
		if call.name == name && len(call.args) > 0 && call.args[0] == firstArg {
			return true
		}
	}
	return false
}

func snapshotTree(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		b.WriteString(path)
		b.WriteString("\n")
		b.WriteString(info.Mode().String())
		b.WriteString("\n")
		if !info.IsDir() {
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			b.Write(body)
			b.WriteString("\n")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return b.String()
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(payload)
}
