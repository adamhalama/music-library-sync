package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/jaa/update-downloads/internal/navidrome"
)

const (
	mirrorTestRsyncVersion = "rsync  version 3.4.1  protocol version 32\n"
	mirrorTestIdle         = `{"ok":true,"scanning":false,"enforcement":1}`
	mirrorTestScanned      = `{"ok":true,"scanned":true,"full":false,"count":1}`
	mirrorTestScanCommand  = "udl-mirror scan --timeout-seconds 900"
)

// mirrorTestRunner answers rsync and ssh invocations from a function, like
// the navidrome package's scriptedRunner, and records every call.
type mirrorTestRunner struct {
	mu    sync.Mutex
	calls [][]string
	reply func(ctx context.Context, name string, args []string) (navidrome.RemoteOutput, error)
}

func (r *mirrorTestRunner) run(ctx context.Context, _ []byte, name string, args ...string) (navidrome.RemoteOutput, error) {
	r.mu.Lock()
	r.calls = append(r.calls, append([]string{name}, args...))
	r.mu.Unlock()
	return r.reply(ctx, name, args)
}

func (r *mirrorTestRunner) realPushes() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var pushes [][]string
	for _, call := range r.calls {
		if call[0] == navidrome.DefaultRemoteRsyncPath && call[1] != "--version" && !containsArg(call, "--dry-run") {
			pushes = append(pushes, call)
		}
	}
	return pushes
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

// gateCommands preserves command order, including flags, without exposing the
// runner's mutable call log while a run is active.
func (r *mirrorTestRunner) gateCommands() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	commands := []string{}
	for _, call := range r.calls {
		if call[0] == navidrome.DefaultRemoteSSHPath {
			commands = append(commands, call[len(call)-1])
		}
	}
	return commands
}

func (r *mirrorTestRunner) scanCount() int {
	count := 0
	for _, command := range r.gateCommands() {
		if strings.HasPrefix(command, "udl-mirror scan ") {
			count++
		}
	}
	return count
}

func mirrorTestGateCommand(name string, args []string) string {
	if name == navidrome.DefaultRemoteSSHPath && len(args) > 0 {
		return args[len(args)-1]
	}
	return ""
}

// Public API credentials can exist, but a remote push must neither query scan
// state nor start a scan there: only the SSH gate owns scans and their locks.
func mirrorTestUnusedAPI(t *testing.T) *navidrome.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("remote mirror push must not call the public API: %s", r.URL.Path)
		http.Error(w, "scans belong to the SSH gate", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)
	return navidrome.NewClient(server.URL, navidrome.Credentials{Username: "u", Password: "p"})
}

func awaitMirrorPhase(t *testing.T, entered <-chan struct{}, output *synchronizedBuffer) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatalf("the push did not reach the intended phase: %s", output.String())
	}
}

// newMirrorPushServer builds an agent server whose navidrome.* methods see a
// remote-mode manager (or a local one when local is set) that talks to the
// scripted runner instead of rsync and ssh. It runs on any OS: the manager is
// built directly rather than from a validated, macOS-only config file.
func newMirrorPushServer(t *testing.T, runner *mirrorTestRunner, api *navidrome.Client, local bool) (*Server, *synchronizedBuffer, *navidrome.Manager) {
	t.Helper()
	cfg := navidrome.DefaultConfig()
	cfg.Enabled = true
	cfg.Server.URL = "https://music-server.example.ts.net:4533"
	cfg.Paths.MusicDir = t.TempDir()
	cfg.Paths.DataDir = t.TempDir()
	if !local {
		cfg.Mode = navidrome.ModeRemote
		cfg.Remote = &navidrome.RemoteConfig{
			SSHTarget: "udl-mirror", RsyncPath: navidrome.DefaultRemoteRsyncPath,
			SSHPath: navidrome.DefaultRemoteSSHPath, MaxDelete: navidrome.DefaultRemoteMaxDelete,
		}
	}
	resolved, err := navidrome.Resolve(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resolved.MusicDir, "01 Song.mp3"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := &navidrome.Manager{
		Config: cfg, Resolved: resolved, Remote: runner.run, API: api,
		BirthTime: func(string) (time.Time, error) { return time.Date(2023, 1, 2, 3, 4, 5, 0, time.UTC), nil },
	}
	if api == nil {
		manager.APIErr = os.ErrNotExist
	}
	output := &synchronizedBuffer{}
	server := &Server{
		Conn: NewConn(strings.NewReader(""), output), Runs: NewRunRegistry(), runContext: context.Background(),
		newNavidromeManager: func(navidrome.ManagerOptions) (*navidrome.Manager, error) { return manager, nil },
	}
	return server, output, manager
}

// mirrorPushTerminal is run.finished as the app decodes it.
type mirrorPushTerminal struct {
	RunID    string `json:"run_id"`
	Error    string `json:"error"`
	ExitCode int    `json:"exit_code"`
	Result   struct {
		Push struct {
			Changed        int `json:"changed"`
			PlannedDeletes int `json:"planned_deletes"`
		} `json:"push"`
		Scanned  bool                   `json:"scanned"`
		Dates    *json.RawMessage       `json:"dates"`
		Warnings []string               `json:"warnings"`
		Mirror   *navidrome.MirrorState `json:"mirror"`
		Failure  *struct {
			Kind                string `json:"kind"`
			Detail              string `json:"detail"`
			PlannedDeletes      int    `json:"planned_deletes"`
			AllowDeletesCommand string `json:"allow_deletes_command"`
		} `json:"failure"`
	} `json:"result"`
}

func startMirrorPush(t *testing.T, server *Server, params any) string {
	t.Helper()
	value, rpcErr := server.navidromeMirrorPush(mustJSON(t, params))
	if rpcErr != nil {
		t.Fatalf("navidrome.mirror.push: %+v", rpcErr)
	}
	return value.(map[string]string)["run_id"]
}

func awaitMirrorPush(t *testing.T, output *synchronizedBuffer, runID string) mirrorPushTerminal {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
			var frame envelope
			if json.Unmarshal([]byte(line), &frame) != nil || frame.Method != "run.finished" {
				continue
			}
			var terminal mirrorPushTerminal
			if err := json.Unmarshal(frame.Params, &terminal); err != nil {
				t.Fatalf("decode run.finished: %v\n%s", err, line)
			}
			if terminal.RunID == runID {
				return terminal
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("no run.finished for %s: %s", runID, output.String())
	return mirrorPushTerminal{}
}

func TestMirrorPushRefusesLocalModeBeforeStartingARun(t *testing.T) {
	runner := &mirrorTestRunner{reply: func(context.Context, string, []string) (navidrome.RemoteOutput, error) {
		t.Fatal("a local install must not run rsync")
		return navidrome.RemoteOutput{}, nil
	}}
	server, _, _ := newMirrorPushServer(t, runner, nil, true)
	_, rpcErr := server.navidromeMirrorPush(mustJSON(t, map[string]any{}))
	if rpcErr == nil || rpcErr.Code != CodeInvalidParams {
		t.Fatalf("rpcErr = %+v, want invalid params", rpcErr)
	}
	var data map[string]string
	if err := json.Unmarshal(rpcErr.Data, &data); err != nil || !strings.Contains(data["detail"], "navidrome.mode is local") {
		t.Fatalf("detail must say why: %s (%v)", rpcErr.Data, err)
	}
}

func TestMirrorPushRejectsInvalidParams(t *testing.T) {
	runner := &mirrorTestRunner{reply: func(context.Context, string, []string) (navidrome.RemoteOutput, error) {
		t.Fatal("invalid params must not run rsync")
		return navidrome.RemoteOutput{}, nil
	}}
	server, _, _ := newMirrorPushServer(t, runner, nil, false)
	for _, params := range []string{`{"allow_deletes":-1}`, `{"allow_deletes":"all"}`, `[1]`} {
		if _, rpcErr := server.navidromeMirrorPush(json.RawMessage(params)); rpcErr == nil || rpcErr.Code != CodeInvalidParams {
			t.Fatalf("%s: rpcErr = %+v", params, rpcErr)
		}
	}
}

func TestMirrorPushRunsTheCLISequenceAndReturnsTheRecordedState(t *testing.T) {
	runner := &mirrorTestRunner{reply: func(_ context.Context, name string, args []string) (navidrome.RemoteOutput, error) {
		switch {
		case args[0] == "--version":
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestRsyncVersion)}, nil
		case name == navidrome.DefaultRemoteRsyncPath && containsArg(args, "--dry-run"):
			return navidrome.RemoteOutput{Stdout: []byte(">f+++++++++ 01 Song.mp3\n")}, nil
		case name == navidrome.DefaultRemoteRsyncPath:
			return navidrome.RemoteOutput{Stdout: []byte(">f+++++++++ 01 Song.mp3\n")}, nil
		case mirrorTestGateCommand(name, args) == "udl-mirror scan-status":
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestIdle)}, nil
		case mirrorTestGateCommand(name, args) == mirrorTestScanCommand:
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestScanned)}, nil
		case mirrorTestGateCommand(name, args) == "udl-mirror apply-dates":
			return navidrome.RemoteOutput{Stdout: []byte(`{"ok":true,"entries":1,"matched":1,"candidates":1,"updated":1}`)}, nil
		}
		t.Errorf("unexpected call %s %v", name, args)
		return navidrome.RemoteOutput{ExitCode: 1}, nil
	}}
	server, output, _ := newMirrorPushServer(t, runner, mirrorTestUnusedAPI(t), false)

	// Empty params are the app's plain "Push now".
	terminal := awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{}))
	if terminal.ExitCode != exitcode.Success || terminal.Error != "" || terminal.Result.Failure != nil {
		t.Fatalf("terminal = %+v", terminal)
	}
	result := terminal.Result
	if result.Push.Changed != 1 || !result.Scanned || runner.scanCount() != 1 || result.Dates == nil {
		t.Fatalf("the push must scan and re-apply Date Added like `udl navidrome push`: %+v scans=%d", result, runner.scanCount())
	}
	if commands := strings.Join(runner.gateCommands(), "\n"); commands != "udl-mirror scan-status\n"+mirrorTestScanCommand+"\nudl-mirror apply-dates" {
		t.Fatalf("gate sequence = %q", commands)
	}
	if result.Warnings == nil {
		t.Fatalf("warnings must be an array, never null")
	}
	mirror := result.Mirror
	if mirror == nil || mirror.LastPushAt == nil || mirror.LastScanAt == nil || mirror.ScanPending || mirror.LastPushError != "" {
		t.Fatalf("the refreshed mirror state must be returned: %+v", mirror)
	}
	if pushes := runner.realPushes(); len(pushes) != 1 {
		t.Fatalf("real pushes = %v", pushes)
	}
}

func TestMirrorPushForceScanScansAnUnchangedMirror(t *testing.T) {
	runner := &mirrorTestRunner{reply: func(_ context.Context, name string, args []string) (navidrome.RemoteOutput, error) {
		switch {
		case args[0] == "--version":
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestRsyncVersion)}, nil
		case name == navidrome.DefaultRemoteRsyncPath:
			return navidrome.RemoteOutput{}, nil
		case mirrorTestGateCommand(name, args) == "udl-mirror scan-status":
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestIdle)}, nil
		case mirrorTestGateCommand(name, args) == mirrorTestScanCommand:
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestScanned)}, nil
		case mirrorTestGateCommand(name, args) == "udl-mirror apply-dates":
			return navidrome.RemoteOutput{Stdout: []byte(`{"ok":true,"entries":1,"matched":1}`)}, nil
		}
		t.Errorf("unexpected call %s %v", name, args)
		return navidrome.RemoteOutput{ExitCode: 1}, nil
	}}
	// Gate-owned scans do not need the Mac's public API credentials.
	server, output, _ := newMirrorPushServer(t, runner, nil, false)

	terminal := awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{}))
	if terminal.ExitCode != exitcode.Success || terminal.Result.Scanned || runner.scanCount() != 0 {
		t.Fatalf("an unchanged mirror is not rescanned: %+v scans=%d", terminal, runner.scanCount())
	}
	terminal = awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{"force_scan": true}))
	if terminal.ExitCode != exitcode.Success || !terminal.Result.Scanned || runner.scanCount() != 1 || terminal.Result.Dates == nil {
		t.Fatalf("force_scan must request a normal gate scan and reconcile dates: %+v scans=%d", terminal, runner.scanCount())
	}
}

func TestMirrorPushReportsTheDeleteLimitWithItsCountAndTheCLIOverride(t *testing.T) {
	var dry strings.Builder
	for i := 0; i < 300; i++ {
		dry.WriteString("*deleting   Old Folder/track-" + time.Duration(i).String() + ".mp3\n")
	}
	runner := &mirrorTestRunner{reply: func(_ context.Context, name string, args []string) (navidrome.RemoteOutput, error) {
		switch {
		case args[0] == "--version":
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestRsyncVersion)}, nil
		case containsArg(args, "--dry-run"):
			return navidrome.RemoteOutput{Stdout: []byte(dry.String())}, nil
		case name == navidrome.DefaultRemoteRsyncPath:
			return navidrome.RemoteOutput{}, nil
		case mirrorTestGateCommand(name, args) == "udl-mirror scan-status":
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestIdle)}, nil
		}
		t.Errorf("unexpected call %s %v", name, args)
		return navidrome.RemoteOutput{ExitCode: 1}, nil
	}}
	server, output, _ := newMirrorPushServer(t, runner, nil, false)

	terminal := awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{}))
	if terminal.ExitCode != exitcode.RuntimeFailure {
		t.Fatalf("exit = %d", terminal.ExitCode)
	}
	failure := terminal.Result.Failure
	if failure == nil || failure.Kind != "delete_limit" || failure.PlannedDeletes != 300 ||
		failure.AllowDeletesCommand != "udl navidrome push --allow-deletes 300" {
		t.Fatalf("failure = %+v", failure)
	}
	for _, text := range []string{terminal.Error, failure.Detail} {
		if !strings.Contains(text, "300 files") || !strings.Contains(text, "--allow-deletes 300") {
			t.Fatalf("the error must name the planned count and the override: %q", text)
		}
	}
	if pushes := runner.realPushes(); len(pushes) != 0 {
		t.Fatalf("a refused push must change nothing: %v", pushes)
	}
	if mirror := terminal.Result.Mirror; mirror == nil || mirror.LastPushError == "" {
		t.Fatalf("the refusal is recorded like any failed push: %+v", mirror)
	}

	// The explicit allowance is the same one the CLI flag passes.
	terminal = awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{"allow_deletes": 300}))
	if terminal.ExitCode != exitcode.Success || terminal.Result.Failure != nil || len(runner.realPushes()) != 1 {
		t.Fatalf("allow_deletes must let the reorganisation through: %+v", terminal)
	}
}

func TestMirrorPushSaysAPushIsAlreadyRunning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the mirror lock is a no-op where flock is unavailable")
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	runner := &mirrorTestRunner{reply: func(_ context.Context, name string, args []string) (navidrome.RemoteOutput, error) {
		if args[0] == "--version" {
			once.Do(func() { close(entered) })
			<-release
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestRsyncVersion)}, nil
		}
		if mirrorTestGateCommand(name, args) == "udl-mirror scan-status" {
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestIdle)}, nil
		}
		if name != navidrome.DefaultRemoteRsyncPath {
			t.Errorf("unexpected call %s %v", name, args)
		}
		return navidrome.RemoteOutput{}, nil
	}}
	server, output, _ := newMirrorPushServer(t, runner, nil, false)

	// The first push holds the per-data-directory mirror lock, as the post-sync hook or
	// the launchd push would.
	first := startMirrorPush(t, server, map[string]any{})
	awaitMirrorPhase(t, entered, output)
	second := awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{}))
	close(release)

	if second.ExitCode != exitcode.RuntimeFailure || second.Result.Failure == nil || second.Result.Failure.Kind != "busy" {
		t.Fatalf("second = %+v", second)
	}
	if !strings.Contains(second.Error, "already running") || strings.Contains(second.Error, "udl push to the mirror") {
		t.Fatalf("the busy error must be the user-readable one: %q", second.Error)
	}
	if terminal := awaitMirrorPush(t, output, first); terminal.ExitCode != exitcode.Success {
		t.Fatalf("the first push must be unaffected: %+v", terminal)
	}
}

func TestMirrorPushHonoursRunCancellation(t *testing.T) {
	entered := make(chan struct{})
	runner := &mirrorTestRunner{reply: func(ctx context.Context, _ string, args []string) (navidrome.RemoteOutput, error) {
		switch {
		case args[0] == "--version":
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestRsyncVersion)}, nil
		case containsArg(args, "--dry-run"):
			close(entered)
			<-ctx.Done()
			return navidrome.RemoteOutput{}, ctx.Err()
		}
		t.Error("a canceled push must not reach the real transfer")
		return navidrome.RemoteOutput{}, nil
	}}
	server, output, _ := newMirrorPushServer(t, runner, nil, false)
	runID := startMirrorPush(t, server, map[string]any{})
	awaitMirrorPhase(t, entered, output)
	if _, rpcErr := server.cancelRun(mustJSON(t, map[string]string{"run_id": runID})); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	terminal := awaitMirrorPush(t, output, runID)
	if terminal.ExitCode != exitcode.Interrupted || terminal.Result.Failure == nil || terminal.Result.Failure.Kind != "canceled" {
		t.Fatalf("terminal = %+v", terminal)
	}
}

// A canceled real transfer may have changed the mirror without producing any
// itemized output. Its retry must scan even when rsync reports no new changes.
func TestMirrorPushCanceledDuringTheTransferStillOwesAScan(t *testing.T) {
	entered := make(chan struct{})
	transfers := 0
	runner := &mirrorTestRunner{reply: func(ctx context.Context, name string, args []string) (navidrome.RemoteOutput, error) {
		switch {
		case args[0] == "--version":
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestRsyncVersion)}, nil
		case mirrorTestGateCommand(name, args) == "udl-mirror scan-status":
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestIdle)}, nil
		case mirrorTestGateCommand(name, args) == mirrorTestScanCommand:
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestScanned)}, nil
		case mirrorTestGateCommand(name, args) == "udl-mirror apply-dates":
			return navidrome.RemoteOutput{Stdout: []byte(`{"ok":true,"entries":1,"matched":1}`)}, nil
		case name == navidrome.DefaultRemoteRsyncPath:
			if !containsArg(args, "--dry-run") {
				transfers++
				if transfers == 1 {
					// No stdout does not prove that the receiver changed nothing.
					close(entered)
					<-ctx.Done()
					return navidrome.RemoteOutput{}, ctx.Err()
				}
			}
			return navidrome.RemoteOutput{}, nil
		}
		t.Errorf("unexpected call %s %v", name, args)
		return navidrome.RemoteOutput{ExitCode: 1}, nil
	}}
	server, output, manager := newMirrorPushServer(t, runner, nil, false)
	first := startMirrorPush(t, server, map[string]any{})
	awaitMirrorPhase(t, entered, output)
	if _, rpcErr := server.cancelRun(mustJSON(t, map[string]string{"run_id": first})); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	terminal := awaitMirrorPush(t, output, first)
	if terminal.ExitCode != exitcode.Interrupted || terminal.Result.Failure == nil || terminal.Result.Failure.Kind != "canceled" ||
		terminal.Result.Mirror == nil || !terminal.Result.Mirror.ScanPending || terminal.Result.Mirror.LastPushAt != nil {
		t.Fatalf("a canceled transfer must leave a scan owed, not a completed push: %+v", terminal)
	}
	if state, err := navidrome.LoadMirrorState(manager.Resolved); err != nil || !state.ScanPending {
		t.Fatalf("state = %+v, err = %v", state, err)
	}
	if len(runner.realPushes()) != 1 || runner.scanCount() != 0 {
		t.Fatalf("the canceled run must reach the transfer, not its scan: %v", runner.gateCommands())
	}
	second := awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{}))
	if second.ExitCode != exitcode.Success || !second.Result.Scanned || runner.scanCount() != 1 ||
		second.Result.Mirror == nil || second.Result.Mirror.ScanPending || second.Result.Mirror.DatesPending {
		t.Fatalf("the owed scan must run on the next push: %+v scans=%d", second, runner.scanCount())
	}
}

func TestMirrorPushCanceledDuringItsScanFinishesCanceled(t *testing.T) {
	entered := make(chan struct{})
	var mu sync.Mutex
	scanning, scanCalls, transfers := false, 0, 0
	runner := &mirrorTestRunner{reply: func(ctx context.Context, name string, args []string) (navidrome.RemoteOutput, error) {
		switch {
		case args[0] == "--version":
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestRsyncVersion)}, nil
		case mirrorTestGateCommand(name, args) == "udl-mirror scan-status":
			mu.Lock()
			active := scanning
			mu.Unlock()
			if active {
				return navidrome.RemoteOutput{Stdout: []byte(`{"ok":true,"scanning":true}`)}, nil
			}
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestIdle)}, nil
		case mirrorTestGateCommand(name, args) == mirrorTestScanCommand:
			mu.Lock()
			scanCalls++
			first := scanCalls == 1
			scanning = first
			mu.Unlock()
			if first {
				close(entered)
				<-ctx.Done()
				return navidrome.RemoteOutput{}, ctx.Err()
			}
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestScanned)}, nil
		case mirrorTestGateCommand(name, args) == "udl-mirror apply-dates":
			return navidrome.RemoteOutput{Stdout: []byte(`{"ok":true,"entries":1,"matched":1}`)}, nil
		case name == navidrome.DefaultRemoteRsyncPath:
			if containsArg(args, "--dry-run") {
				return navidrome.RemoteOutput{}, nil
			}
			mu.Lock()
			active := scanning
			transfers++
			first := transfers == 1
			mu.Unlock()
			if active {
				t.Error("a real transfer overlapped a running gate scan")
			}
			if first {
				return navidrome.RemoteOutput{Stdout: []byte(">f+++++++++ 01 Song.mp3\n")}, nil
			}
			return navidrome.RemoteOutput{}, nil
		}
		t.Errorf("unexpected call %s %v", name, args)
		return navidrome.RemoteOutput{ExitCode: 1}, nil
	}}
	server, output, _ := newMirrorPushServer(t, runner, mirrorTestUnusedAPI(t), false)
	runID := startMirrorPush(t, server, map[string]any{})
	awaitMirrorPhase(t, entered, output)
	if _, rpcErr := server.cancelRun(mustJSON(t, map[string]string{"run_id": runID})); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	terminal := awaitMirrorPush(t, output, runID)
	if terminal.ExitCode != exitcode.Interrupted || terminal.Error == "" ||
		terminal.Result.Failure == nil || terminal.Result.Failure.Kind != "canceled" {
		t.Fatalf("a cancel after the transfer is still a canceled run: %+v", terminal)
	}
	if terminal.Result.Push.Changed != 1 || terminal.Result.Scanned || terminal.Result.Dates != nil || len(terminal.Result.Warnings) == 0 ||
		terminal.Result.Mirror == nil || !terminal.Result.Mirror.ScanPending || terminal.Result.Mirror.LastPushAt == nil {
		t.Fatalf("the partial transfer result and scan debt must survive cancellation: %+v", terminal.Result)
	}

	// Canceling SSH does not stop the server scan. The next push is refused,
	// not held waiting on the public API, and keeps the previous scan debt.
	second := awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{}))
	if second.ExitCode != exitcode.RuntimeFailure || second.Result.Failure == nil || second.Result.Failure.Kind != "server_scanning" ||
		second.Result.Mirror == nil || !second.Result.Mirror.ScanPending || len(runner.realPushes()) != 1 {
		t.Fatalf("an active server scan must defer the next transfer: %+v", second)
	}
	mu.Lock()
	scanning = false
	mu.Unlock()
	third := awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{}))
	if third.ExitCode != exitcode.Success || third.Result.Failure != nil || !third.Result.Scanned || third.Result.Dates == nil ||
		third.Result.Mirror == nil || third.Result.Mirror.ScanPending || third.Result.Mirror.DatesPending ||
		len(runner.realPushes()) != 2 || runner.scanCount() != 2 {
		t.Fatalf("an idle retry must transfer and finish its scan debt: %+v", third)
	}
}
