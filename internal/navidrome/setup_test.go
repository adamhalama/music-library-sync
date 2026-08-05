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
		Service: Service{Run: calls.run, UID: "501"},
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
