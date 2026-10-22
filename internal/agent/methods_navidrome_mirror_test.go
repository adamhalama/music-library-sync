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

const mirrorTestRsyncVersion = "rsync  version 3.4.1  protocol version 32\n"

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

// mirrorTestSubsonic is a Navidrome stand-in for the scan the push requests:
// each startScan is reported as running on the next poll and finished after.
func mirrorTestSubsonic(t *testing.T) (*navidrome.Client, *int) {
	t.Helper()
	var mu sync.Mutex
	scans, polls := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		body := map[string]any{"status": "ok", "version": "1.16.1"}
		switch strings.TrimPrefix(r.URL.Path, "/rest/") {
		case "startScan.view":
			scans++
			polls = 0
		case "getScanStatus.view":
			polls++
			last := time.Date(2026, 10, 22, 10, scans, 0, 0, time.UTC)
			body["scanStatus"] = map[string]any{"scanning": scans > 0 && polls == 1, "count": 1, "lastScan": last.Format(time.RFC3339Nano)}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"subsonic-response": body})
	}))
	t.Cleanup(server.Close)
	return navidrome.NewClient(server.URL, navidrome.Credentials{Username: "u", Password: "p"}), &scans
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
		case strings.HasSuffix(args[len(args)-1], "apply-dates"):
			return navidrome.RemoteOutput{Stdout: []byte(`{"ok":true,"entries":1,"matched":1,"candidates":1,"updated":1}`)}, nil
		}
		t.Errorf("unexpected call %s %v", name, args)
		return navidrome.RemoteOutput{ExitCode: 1}, nil
	}}
	api, scans := mirrorTestSubsonic(t)
	server, output, _ := newMirrorPushServer(t, runner, api, false)

	// Empty params are the app's plain "Push now".
	terminal := awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{}))
	if terminal.ExitCode != exitcode.Success || terminal.Error != "" || terminal.Result.Failure != nil {
		t.Fatalf("terminal = %+v", terminal)
	}
	result := terminal.Result
	if result.Push.Changed != 1 || !result.Scanned || *scans != 1 || result.Dates == nil {
		t.Fatalf("the push must scan and re-apply Date Added like `udl navidrome push`: %+v scans=%d", result, *scans)
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
		case strings.HasSuffix(args[len(args)-1], "apply-dates"):
			return navidrome.RemoteOutput{Stdout: []byte(`{"ok":true,"entries":1,"matched":1}`)}, nil
		}
		return navidrome.RemoteOutput{}, nil
	}}
	api, scans := mirrorTestSubsonic(t)
	server, output, _ := newMirrorPushServer(t, runner, api, false)

	terminal := awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{}))
	if terminal.ExitCode != exitcode.Success || terminal.Result.Scanned || *scans != 0 {
		t.Fatalf("an unchanged mirror is not rescanned: %+v scans=%d", terminal, *scans)
	}
	terminal = awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{"force_scan": true}))
	if terminal.ExitCode != exitcode.Success || !terminal.Result.Scanned || *scans != 1 {
		t.Fatalf("force_scan must scan: %+v scans=%d", terminal, *scans)
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
		}
		return navidrome.RemoteOutput{}, nil
	}}
	api, _ := mirrorTestSubsonic(t)
	server, output, _ := newMirrorPushServer(t, runner, api, false)

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
	runner := &mirrorTestRunner{reply: func(_ context.Context, _ string, args []string) (navidrome.RemoteOutput, error) {
		if args[0] == "--version" {
			once.Do(func() { close(entered) })
			<-release
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestRsyncVersion)}, nil
		}
		return navidrome.RemoteOutput{}, nil
	}}
	api, _ := mirrorTestSubsonic(t)
	server, output, _ := newMirrorPushServer(t, runner, api, false)

	// The first push holds the per-Mac mirror lock, as the post-sync hook or
	// the launchd push would.
	first := startMirrorPush(t, server, map[string]any{})
	<-entered
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
	<-entered
	if _, rpcErr := server.cancelRun(mustJSON(t, map[string]string{"run_id": runID})); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	terminal := awaitMirrorPush(t, output, runID)
	if terminal.ExitCode != exitcode.Interrupted || terminal.Result.Failure == nil || terminal.Result.Failure.Kind != "canceled" {
		t.Fatalf("terminal = %+v", terminal)
	}
}

// heldScanSubsonic is a Navidrome stand-in whose scans keep running while
// hold is set, like a scan the user started from the phone app or one a
// canceled push left behind. Released, each startScan runs for one poll.
type heldScanSubsonic struct {
	mu       sync.Mutex
	hold     bool
	scanning bool
	polls    int
	scans    int
	seen     chan struct{}
	once     sync.Once
}

func newHeldScanSubsonic(t *testing.T) (*heldScanSubsonic, *navidrome.Client) {
	t.Helper()
	fake := &heldScanSubsonic{hold: true, seen: make(chan struct{})}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		body := map[string]any{"status": "ok", "version": "1.16.1"}
		switch strings.TrimPrefix(r.URL.Path, "/rest/") {
		case "startScan.view":
			fake.scans++
			fake.scanning = true
		case "getScanStatus.view":
			fake.polls++
			body["scanStatus"] = map[string]any{"scanning": fake.scanning, "count": 1,
				"lastScan": time.Date(2026, 10, 22, 10, fake.scans, 0, 0, time.UTC).Format(time.RFC3339Nano)}
			if fake.scanning {
				fake.once.Do(func() { close(fake.seen) })
				if !fake.hold {
					fake.scanning = false
				}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"subsonic-response": body})
	}))
	t.Cleanup(server.Close)
	return fake, navidrome.NewClient(server.URL, navidrome.Credentials{Username: "u", Password: "p"})
}

func (f *heldScanSubsonic) state() (scanning bool, polls int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.scanning, f.polls
}

func (f *heldScanSubsonic) release() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hold = false
}

func TestMirrorPushWaitsForARunningScanBeforeItsTransfer(t *testing.T) {
	fake, api := newHeldScanSubsonic(t)
	var mu sync.Mutex
	var overlapped []bool
	runner := &mirrorTestRunner{reply: func(_ context.Context, name string, args []string) (navidrome.RemoteOutput, error) {
		switch {
		case args[0] == "--version":
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestRsyncVersion)}, nil
		case name == navidrome.DefaultRemoteRsyncPath && !containsArg(args, "--dry-run"):
			scanning, _ := fake.state()
			mu.Lock()
			overlapped = append(overlapped, scanning)
			mu.Unlock()
			return navidrome.RemoteOutput{Stdout: []byte(">f+++++++++ 01 Song.mp3\n")}, nil
		case strings.HasSuffix(args[len(args)-1], "apply-dates"):
			return navidrome.RemoteOutput{Stdout: []byte(`{"ok":true,"entries":1,"matched":1}`)}, nil
		}
		return navidrome.RemoteOutput{}, nil
	}}
	server, output, _ := newMirrorPushServer(t, runner, api, false)

	// The first push transfers and requests its scan, then is canceled while
	// that scan runs on: the server keeps scanning after the run ends.
	first := startMirrorPush(t, server, map[string]any{})
	select {
	case <-fake.seen:
	case <-time.After(5 * time.Second):
		t.Fatal("the first push never started its scan")
	}
	if _, rpcErr := server.cancelRun(mustJSON(t, map[string]string{"run_id": first})); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	_ = awaitMirrorPush(t, output, first)

	// The next push must not start rsync while that scan is still running.
	_, pollsBefore := fake.state()
	second := startMirrorPush(t, server, map[string]any{})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, polls := fake.state(); polls > pollsBefore {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the second push never checked the server's scan state")
		}
		time.Sleep(time.Millisecond)
	}
	if pushes := runner.realPushes(); len(pushes) != 1 {
		t.Fatalf("the second push transferred while the server was scanning: %d real pushes", len(pushes))
	}
	fake.release()
	terminal := awaitMirrorPush(t, output, second)
	if terminal.ExitCode != exitcode.Success || terminal.Result.Failure != nil {
		t.Fatalf("once the scan finished the push must go through: %+v", terminal)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(overlapped) != 2 || overlapped[1] {
		t.Fatalf("a real transfer overlapped a running scan: %v", overlapped)
	}
}

func TestMirrorPushIsDeferredWhenTheScanStateIsUnknown(t *testing.T) {
	unreachable := httptest.NewServer(http.NotFoundHandler())
	unreachable.Close()
	for name, api := range map[string]*navidrome.Client{
		"no credentials":     nil,
		"server unreachable": navidrome.NewClient(unreachable.URL, navidrome.Credentials{Username: "u", Password: "p"}),
	} {
		t.Run(name, func(t *testing.T) {
			runner := &mirrorTestRunner{reply: func(_ context.Context, name string, args []string) (navidrome.RemoteOutput, error) {
				switch {
				case args[0] == "--version":
					return navidrome.RemoteOutput{Stdout: []byte(mirrorTestRsyncVersion)}, nil
				case containsArg(args, "--dry-run"):
					return navidrome.RemoteOutput{Stdout: []byte(">f+++++++++ 01 Song.mp3\n")}, nil
				}
				t.Errorf("a push whose scan state is unknown must not transfer: %s %v", name, args)
				return navidrome.RemoteOutput{}, nil
			}}
			server, output, _ := newMirrorPushServer(t, runner, api, false)
			terminal := awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{}))
			if terminal.ExitCode != exitcode.RuntimeFailure || terminal.Result.Failure == nil || terminal.Result.Failure.Kind != "server_scanning" {
				t.Fatalf("terminal = %+v", terminal)
			}
			if !strings.Contains(terminal.Error, "deferred") || !strings.Contains(terminal.Result.Failure.Detail, "deferred") {
				t.Fatalf("the error must say the push was deferred: %q / %q", terminal.Error, terminal.Result.Failure.Detail)
			}
			mirror := terminal.Result.Mirror
			if mirror == nil || mirror.LastPushError == "" || mirror.ScanPending {
				t.Fatalf("a deferred push records its error and nothing else: %+v", mirror)
			}
		})
	}
}

func TestMirrorPushCanceledDuringTheTransferStillOwesAScan(t *testing.T) {
	entered := make(chan struct{})
	transfers := 0
	runner := &mirrorTestRunner{reply: func(ctx context.Context, name string, args []string) (navidrome.RemoteOutput, error) {
		switch {
		case args[0] == "--version":
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestRsyncVersion)}, nil
		case strings.HasSuffix(args[len(args)-1], "apply-dates"):
			return navidrome.RemoteOutput{Stdout: []byte(`{"ok":true,"entries":1,"matched":1}`)}, nil
		case name == navidrome.DefaultRemoteRsyncPath && !containsArg(args, "--dry-run"):
			transfers++
			if transfers == 1 {
				// The server can apply updates before any itemized line
				// reaches this Mac, so no stdout proves nothing changed.
				close(entered)
				<-ctx.Done()
				return navidrome.RemoteOutput{}, ctx.Err()
			}
		}
		return navidrome.RemoteOutput{}, nil
	}}
	api, scans := mirrorTestSubsonic(t)
	server, output, manager := newMirrorPushServer(t, runner, api, false)
	first := startMirrorPush(t, server, map[string]any{})
	<-entered
	if _, rpcErr := server.cancelRun(mustJSON(t, map[string]string{"run_id": first})); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if terminal := awaitMirrorPush(t, output, first); terminal.Result.Mirror == nil || !terminal.Result.Mirror.ScanPending {
		t.Fatalf("a canceled transfer must leave a scan owed: %+v", terminal.Result.Mirror)
	}
	if state, _ := navidrome.LoadMirrorState(manager.Resolved); !state.ScanPending {
		t.Fatalf("state = %+v", state)
	}
	// The retry transfers nothing but still scans, because one is owed.
	second := awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{}))
	if second.ExitCode != exitcode.Success || !second.Result.Scanned || *scans != 1 || second.Result.Mirror.ScanPending {
		t.Fatalf("the owed scan must run on the next push: %+v scans=%d", second, *scans)
	}
}
