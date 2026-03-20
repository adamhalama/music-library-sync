package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDryRunConfig(t *testing.T, dir string) string {
	t.Helper()
	stateDir := filepath.Join(dir, "state")
	targetDir := filepath.Join(dir, "target")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}

	configPath := filepath.Join(dir, "config.yaml")
	payload := `version: 1
defaults:
  state_dir: "` + stateDir + `"
  archive_file: "archive.txt"
  threads: 1
  continue_on_error: true
  command_timeout_seconds: 900
sources:
  - id: "spotify-test"
    type: "spotify"
    enabled: true
    target_dir: "` + targetDir + `"
    url: "https://open.spotify.com/playlist/test"
    state_file: "spotify-test.sync.spotdl"
    adapter:
      kind: "spotdl"
`
	if err := os.WriteFile(configPath, []byte(payload), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}

func TestSyncDryRunHumanOutput(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeDryRunConfig(t, tmp)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: stdout, ErrOut: stderr},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{"sync", "--config", configPath, "--dry-run"})

	if err := root.Execute(); err != nil {
		t.Fatalf("sync --dry-run failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "sync finished") {
		t.Fatalf("expected summary in output, got: %s", stdout.String())
	}
}

func TestSyncDryRunJSONOutput(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeDryRunConfig(t, tmp)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: stdout, ErrOut: stderr},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{"sync", "--config", configPath, "--dry-run", "--json"})

	if err := root.Execute(); err != nil {
		t.Fatalf("sync --dry-run --json failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected multiple json events, got: %s", stdout.String())
	}

	last := map[string]any{}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatalf("unmarshal last event: %v", err)
	}
	if last["event"] != "sync_finished" {
		t.Fatalf("expected final event sync_finished, got %v", last["event"])
	}
}

func TestSyncRejectsInvalidProgressMode(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeDryRunConfig(t, tmp)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: stdout, ErrOut: stderr},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{"sync", "--config", configPath, "--dry-run", "--progress", "bad"})

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected invalid progress mode error")
	}
	if !strings.Contains(err.Error(), "invalid --progress mode") {
		t.Fatalf("expected invalid progress mode guidance, got: %v", err)
	}
}

func TestSyncDryRunAcceptsOutputModeFlags(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeDryRunConfig(t, tmp)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: stdout, ErrOut: stderr},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{
		"sync",
		"--config", configPath,
		"--dry-run",
		"--progress", "never",
		"--preflight-summary", "always",
		"--track-status", "count",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("sync output mode flags failed: %v", err)
	}
}

func TestSyncPlanLimitDefaultIsTen(t *testing.T) {
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
	}
	cmd := newSyncCommand(app)
	flag := cmd.Flags().Lookup("plan-limit")
	if flag == nil {
		t.Fatalf("expected --plan-limit flag to be registered")
	}
	if flag.DefValue != "10" {
		t.Fatalf("expected --plan-limit default 10, got %q", flag.DefValue)
	}
}

func TestSyncPlanLimitRequiresPlan(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeDryRunConfig(t, tmp)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: stdout, ErrOut: stderr},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{"sync", "--config", configPath, "--dry-run", "--plan-limit", "5"})

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected usage error for --plan-limit without --plan")
	}
	if !strings.Contains(err.Error(), "--plan-limit requires --plan") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSyncRejectsNegativePlanLimit(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeDryRunConfig(t, tmp)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: stdout, ErrOut: stderr},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{"sync", "--config", configPath, "--dry-run", "--plan-limit", "-1"})

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected usage error for negative --plan-limit")
	}
	if !strings.Contains(err.Error(), "invalid --plan-limit -1") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSyncPlanRejectsJSON(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeDryRunConfig(t, tmp)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: stdout, ErrOut: stderr},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{"sync", "--config", configPath, "--dry-run", "--plan", "--json"})

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected usage error for --plan --json")
	}
	if !strings.Contains(err.Error(), "--plan cannot be used with --json") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSyncPlanRejectsNoInput(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeDryRunConfig(t, tmp)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: stdout, ErrOut: stderr},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{"sync", "--config", configPath, "--dry-run", "--plan", "--no-input"})

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected usage error for --plan --no-input")
	}
	if !strings.Contains(err.Error(), "--plan cannot be used with --no-input") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSyncPlanRejectsIncompatibleFlowFlags(t *testing.T) {
	testCases := []struct {
		name       string
		flag       string
		errSnippet string
	}{
		{name: "scan-gaps", flag: "--scan-gaps", errSnippet: "--plan cannot be combined with --scan-gaps"},
		{name: "ask-on-existing", flag: "--ask-on-existing", errSnippet: "--plan cannot be combined with --ask-on-existing"},
		{name: "no-preflight", flag: "--no-preflight", errSnippet: "--plan cannot be combined with --no-preflight"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			configPath := writeDryRunConfig(t, tmp)

			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			app := &AppContext{
				Build: BuildInfo{Version: "test"},
				IO:    IOStreams{In: strings.NewReader(""), Out: stdout, ErrOut: stderr},
			}
			root := newRootCommand(app)
			root.SetArgs([]string{"sync", "--config", configPath, "--dry-run", "--plan", tc.flag})

			err := root.Execute()
			if err == nil {
				t.Fatalf("expected usage error for --plan with %s", tc.flag)
			}
			if !strings.Contains(err.Error(), tc.errSnippet) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestSyncPlanRequiresTTY(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeDryRunConfig(t, tmp)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: stdout, ErrOut: stderr},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{"sync", "--config", configPath, "--dry-run", "--plan"})

	err := root.Execute()
	if err == nil {
		t.Fatalf("expected usage error for --plan without tty")
	}
	if !strings.Contains(err.Error(), "--plan requires an interactive TTY on stdin and stdout") {
		t.Fatalf("unexpected error: %v", err)
	}
}
