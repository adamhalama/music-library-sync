package navidrome

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func remoteTestConfig(t *testing.T) (Config, Resolved) {
	t.Helper()
	music := t.TempDir()
	cfg := DefaultConfig()
	cfg.Mode = ModeRemote
	cfg.Remote = &RemoteConfig{SSHTarget: "udl-mirror"}
	cfg.Paths.MusicDir = music
	cfg.Server.URL = "https://music-server.example.ts.net:4533"
	normalize(&cfg)
	resolved, err := Resolve(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, resolved
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// scriptedRunner answers rsync and ssh invocations from a table.
type scriptedRunner struct {
	mu    sync.Mutex
	calls [][]string
	stdin [][]byte
	reply func(name string, args []string) (RemoteOutput, error)
}

func (r *scriptedRunner) run(_ context.Context, stdin []byte, name string, args ...string) (RemoteOutput, error) {
	r.mu.Lock()
	r.calls = append(r.calls, append([]string{name}, args...))
	r.stdin = append(r.stdin, stdin)
	r.mu.Unlock()
	return r.reply(name, args)
}

const rsync3VersionOutput = "rsync  version 3.4.1  protocol version 32\nCopyright (C) 1996-2025\n"

func TestDefaultConfigMarshalsWithoutRemoteKeys(t *testing.T) {
	payload, err := Marshal(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "mode:") || strings.Contains(string(payload), "remote:") {
		t.Fatalf("a local config must stay byte-identical to older builds:\n%s", payload)
	}
	wire, err := json.Marshal(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), `"mode"`) || strings.Contains(string(wire), `"remote"`) {
		t.Fatalf("a local config's wire form must not grow new keys: %s", wire)
	}
}

func TestRemoteConfigLoadsAndFillsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "navidrome.yaml")
	body := "mode: remote\nserver:\n  url: https://music-server.example.ts.net:4533\nremote:\n  ssh_target: udl-mirror\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(LoadOptions{ExplicitPath: path, Env: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.IsRemote() || cfg.Remote.SSHTarget != "udl-mirror" {
		t.Fatalf("cfg = %+v", cfg.Remote)
	}
	if cfg.Remote.RsyncPath != DefaultRemoteRsyncPath || cfg.Remote.SSHPath != DefaultRemoteSSHPath || cfg.Remote.MaxDelete != DefaultRemoteMaxDelete {
		t.Fatalf("defaults not applied: %+v", cfg.Remote)
	}
	if problems := validateRemote(cfg); len(problems) != 0 {
		t.Fatalf("problems = %v", problems)
	}
}

func TestRemoteConfigValidation(t *testing.T) {
	cases := map[string]func(*Config){
		"unknown mode":     func(c *Config) { c.Mode = "cloud" },
		"missing target":   func(c *Config) { c.Remote.SSHTarget = "" },
		"option as target": func(c *Config) { c.Remote.SSHTarget = "-oProxyCommand=evil" },
		"spaced target":    func(c *Config) { c.Remote.SSHTarget = "a b" },
		"relative rsync":   func(c *Config) { c.Remote.RsyncPath = "rsync" },
		"zero max delete":  func(c *Config) { c.Remote.MaxDelete = -1 },
		"loopback url":     func(c *Config) { c.Server.URL = "http://localhost:4533" },
		"loopback ip url":  func(c *Config) { c.Server.URL = "http://127.0.0.1:4533" },
		"remote w/o block": func(c *Config) { c.Remote = nil },
	}
	for name, mutate := range cases {
		cfg, _ := remoteTestConfig(t)
		mutate(&cfg)
		if problems := validateRemote(cfg); len(problems) == 0 {
			t.Errorf("%s: expected a validation problem", name)
		}
	}
}

func TestRsyncPushArgsPinTheSafetyFlags(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	args := RsyncPushArgs(cfg, resolved)
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--times", "--delete-delay", "--delay-updates", "--partial", "--max-delete=250",
		"--filter=- .*", "--itemize-changes", "--timeout=120",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s in %v", want, args)
		}
	}
	for _, forbidden := range []string{"--checksum", "--iconv", "--inplace", "--append", "-z"} {
		for _, arg := range args {
			if arg == forbidden || strings.HasPrefix(arg, forbidden+"=") {
				t.Errorf("forbidden flag %s in %v", forbidden, args)
			}
		}
	}
	if got := args[len(args)-2]; got != resolved.MusicDir+"/" {
		t.Errorf("source = %q; the trailing slash mirrors the folder's contents", got)
	}
	if got := args[len(args)-1]; got != "udl-mirror:" {
		t.Errorf("destination = %q", got)
	}
}

func TestParseItemizedChanges(t *testing.T) {
	out := strings.Join([]string{
		"cd+++++++++ New Artist/",
		">f+++++++++ New Artist/01 Song.mp3",
		">f.st...... Old/02 Retag.mp3",
		".f..t...... Old/03 Touched.mp3",
		".d..t...... Old/",
		"*deleting   Gone/04 Removed.mp3",
		"*deleting   Gone/",
		"",
	}, "\n")
	changed, deleted, items := ParseItemizedChanges(out)
	if changed != 3 || deleted != 1 || len(items) != 4 {
		t.Fatalf("changed=%d deleted=%d items=%v", changed, deleted, items)
	}
}

func TestPushRefusesAnEmptyLibrary(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	writeFile(t, filepath.Join(resolved.MusicDir, ".DS_Store"))
	runner := &scriptedRunner{reply: func(string, []string) (RemoteOutput, error) {
		t.Fatal("no command may run for an empty library")
		return RemoteOutput{}, nil
	}}
	manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run}
	if _, err := manager.Push(context.Background()); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("err = %v", err)
	}
}

func TestPushRefusesOpenrsync(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
	runner := &scriptedRunner{reply: func(_ string, args []string) (RemoteOutput, error) {
		if args[0] != "--version" {
			t.Fatal("must not push with openrsync")
		}
		return RemoteOutput{Stdout: []byte("openrsync: protocol version 29\nrsync version 2.6.9 compatible\n")}, nil
	}}
	manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run}
	if _, err := manager.Push(context.Background()); err == nil || !strings.Contains(err.Error(), "openrsync") {
		t.Fatalf("err = %v", err)
	}
}

func TestPushClassifiesRsyncExits(t *testing.T) {
	cases := []struct {
		code    int
		stderr  string
		wantErr string
		warn    bool
	}{
		{code: 0},
		{code: rsyncExitVanished, warn: true},
		{code: rsyncExitMaxDelete, wantErr: "max_delete"},
		{code: 1, stderr: "Another instance of rrsync is already accessing this directory", wantErr: "still running"},
		{code: 255, stderr: "ssh: connect to host music-server: Operation timed out", wantErr: "rsync exit 255"},
	}
	for _, tc := range cases {
		cfg, resolved := remoteTestConfig(t)
		writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
		runner := &scriptedRunner{reply: func(_ string, args []string) (RemoteOutput, error) {
			if args[0] == "--version" {
				return RemoteOutput{Stdout: []byte(rsync3VersionOutput)}, nil
			}
			return RemoteOutput{Stdout: []byte(">f+++++++++ a.mp3\n"), Stderr: []byte(tc.stderr), ExitCode: tc.code}, nil
		}}
		manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run}
		result, err := manager.Push(context.Background())
		switch {
		case tc.wantErr != "":
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("exit %d: err = %v", tc.code, err)
			}
		case err != nil:
			t.Errorf("exit %d: unexpected err %v", tc.code, err)
		default:
			if result.Changed != 1 || (result.Warning != "") != tc.warn {
				t.Errorf("exit %d: result = %+v", tc.code, result)
			}
		}
	}
}

func TestGateCallsUseTheForcedCommandVocabulary(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	runner := &scriptedRunner{reply: func(name string, args []string) (RemoteOutput, error) {
		if name != DefaultRemoteSSHPath || args[len(args)-2] != "udl-mirror" || args[len(args)-1] != "udl-mirror backup" {
			t.Fatalf("unexpected call %s %v", name, args)
		}
		return RemoteOutput{Stdout: []byte(`{"ok":true,"backup":{"path":"/srv/b/udl_backup_x.db","size_bytes":42,"created_at":"2026-10-21T18:00:00Z"}}`)}, nil
	}}
	manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run}
	result, err := manager.CreateBackup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Backup.Path != "/srv/b/udl_backup_x.db" || result.Backup.SizeBytes != 42 {
		t.Fatalf("result = %+v", result)
	}
}

func TestGateErrorsAreReported(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	for _, tc := range []struct {
		out  RemoteOutput
		want string
	}{
		{RemoteOutput{Stdout: []byte(`{"ok":false,"error":"command not allowed"}`), ExitCode: 2}, "command not allowed"},
		{RemoteOutput{Stderr: []byte("ssh: Could not resolve hostname"), ExitCode: 255}, "not reachable"},
		{RemoteOutput{Stdout: []byte(`{"ok":true,"backup":{"path":"","size_bytes":0}}`)}, "without a file"},
	} {
		runner := &scriptedRunner{reply: func(string, []string) (RemoteOutput, error) { return tc.out, nil }}
		manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run}
		if _, err := manager.CreateBackup(context.Background()); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("err = %v, want %q", err, tc.want)
		}
	}
}

func TestBirthTimeManifestCoversVisibleAudioOnly(t *testing.T) {
	_, resolved := remoteTestConfig(t)
	for _, rel := range []string{
		"Artist/Album/01 Song.mp3",
		"Artist/Album/cover.jpg",
		"Artist/Album/02 Song.M4A",
		".hidden/x.mp3",
		"Artist/.DS_Store",
		"udl-playlists/All Music.nsp",
	} {
		writeFile(t, filepath.Join(resolved.MusicDir, rel))
	}
	stamp := time.Date(2024, 5, 6, 7, 8, 9, 123456789, time.UTC)
	manifest, err := BuildBirthTimeManifest(resolved, func(string) (time.Time, error) { return stamp, nil })
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 1 || manifest.MusicDir != resolved.MusicDir {
		t.Fatalf("manifest = %+v", manifest)
	}
	got := []string{}
	for _, entry := range manifest.Entries {
		got = append(got, entry.Path)
		if entry.BirthTimeNS != stamp.UnixNano() {
			t.Errorf("%s birth = %d", entry.Path, entry.BirthTimeNS)
		}
	}
	want := []string{"Artist/Album/01 Song.mp3", "Artist/Album/02 Song.M4A"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("entries = %v, want %v", got, want)
	}
}

func TestBirthTimeManifestFailsWithoutCreationTimes(t *testing.T) {
	_, resolved := remoteTestConfig(t)
	writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
	_, err := BuildBirthTimeManifest(resolved, func(string) (time.Time, error) {
		return time.Time{}, errors.New("no creation time here")
	})
	if err == nil {
		t.Fatal("a manifest without creation times must not be sent")
	}
}

// fakeScanClient scripts scanner state for scanAfterPush.
type fakeScanClient struct {
	states  []ScanState
	started int
}

func (f *fakeScanClient) StartScan(context.Context, bool) error {
	f.started++
	return nil
}

func (f *fakeScanClient) ScanState(context.Context) (ScanState, error) {
	if len(f.states) == 0 {
		return ScanState{}, errors.New("script exhausted")
	}
	state := f.states[0]
	if len(f.states) > 1 {
		f.states = f.states[1:]
	}
	return state, nil
}

func TestScanAfterPushWaitsForIdleThenForItsOwnScan(t *testing.T) {
	scanPollInterval = time.Millisecond
	t.Cleanup(func() { scanPollInterval = time.Second })
	before := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	after := before.Add(time.Hour)
	client := &fakeScanClient{states: []ScanState{
		{Scanning: true, LastScan: before.Add(-time.Hour)}, // someone else's scan
		{Scanning: false, LastScan: before},                // idle: request ours
		{Scanning: false, LastScan: before},                // not started yet
		{Scanning: false, LastScan: after},                 // quick scan finished between polls
	}}
	if err := scanAfterPush(context.Background(), client); err != nil {
		t.Fatal(err)
	}
	if client.started != 1 {
		t.Fatalf("started = %d", client.started)
	}
	if len(client.states) != 1 {
		t.Fatalf("returned before the scan finished; remaining script %v", client.states)
	}
}

func TestScanAfterPushWithoutLastScanUsesTheRunningSignal(t *testing.T) {
	scanPollInterval = time.Millisecond
	t.Cleanup(func() { scanPollInterval = time.Second })
	client := &fakeScanClient{states: []ScanState{{}, {Scanning: true}, {Scanning: true}, {}}}
	if err := scanAfterPush(context.Background(), client); err != nil {
		t.Fatal(err)
	}
}

// mirrorServer is a minimal Subsonic server whose scans finish.
func mirrorServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	var mu sync.Mutex
	scans := 0
	polls := 0
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
			last := time.Date(2026, 10, 21, 10, scans, 0, 0, time.UTC)
			body["scanStatus"] = map[string]any{"scanning": scans > 0 && polls == 1, "count": 3, "lastScan": last.Format(time.RFC3339Nano)}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"subsonic-response": body})
	}))
	t.Cleanup(server.Close)
	return server, &scans
}

func TestSyncMirrorPushesScansAndAppliesDates(t *testing.T) {
	scanPollInterval = time.Millisecond
	t.Cleanup(func() { scanPollInterval = time.Second })
	cfg, resolved := remoteTestConfig(t)
	writeFile(t, filepath.Join(resolved.MusicDir, "Artist/01 Song.mp3"))
	server, scans := mirrorServer(t)
	runner := &scriptedRunner{reply: func(name string, args []string) (RemoteOutput, error) {
		switch {
		case args[0] == "--version":
			return RemoteOutput{Stdout: []byte(rsync3VersionOutput)}, nil
		case name == DefaultRemoteRsyncPath:
			return RemoteOutput{Stdout: []byte(">f+++++++++ Artist/01 Song.mp3\n")}, nil
		case strings.HasSuffix(args[len(args)-1], "apply-dates"):
			return RemoteOutput{Stdout: []byte(`{"ok":true,"entries":1,"matched":1,"candidates":1,"updated":1,"albums_updated":1,"backup":{"path":"/b/x.db","size_bytes":9}}`)}, nil
		}
		t.Fatalf("unexpected %s %v", name, args)
		return RemoteOutput{}, nil
	}}
	stamp := time.Date(2023, 1, 2, 3, 4, 5, 0, time.UTC)
	manager := &Manager{
		Config: cfg, Resolved: resolved, Remote: runner.run,
		BirthTime: func(string) (time.Time, error) { return stamp, nil },
		API:       NewClient(server.URL, Credentials{Username: "u", Password: "p"}),
	}
	result, err := manager.SyncMirror(context.Background(), MirrorOptions{Scan: true, Dates: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Scanned || *scans != 1 || result.Dates == nil || result.Dates.Updated != 1 || len(result.Warnings) != 0 {
		t.Fatalf("result = %+v scans=%d", result, *scans)
	}
	var manifest BirthTimeManifest
	if err := json.Unmarshal(runner.stdin[len(runner.stdin)-1], &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) != 1 || manifest.Entries[0].Path != "Artist/01 Song.mp3" || manifest.Entries[0].BirthTimeNS != stamp.UnixNano() {
		t.Fatalf("manifest = %+v", manifest)
	}
}

func TestSyncMirrorSkipsTheScanWhenNothingChanged(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
	server, scans := mirrorServer(t)
	runner := &scriptedRunner{reply: func(_ string, args []string) (RemoteOutput, error) {
		if args[0] == "--version" {
			return RemoteOutput{Stdout: []byte(rsync3VersionOutput)}, nil
		}
		return RemoteOutput{}, nil
	}}
	manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run,
		API: NewClient(server.URL, Credentials{Username: "u", Password: "p"})}
	result, err := manager.SyncMirror(context.Background(), MirrorOptions{Scan: true, Dates: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Scanned || *scans != 0 || len(runner.calls) != 2 {
		t.Fatalf("result = %+v scans=%d calls=%v", result, *scans, runner.calls)
	}
}

func TestSyncMirrorReportsAFailedPushAsAnError(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
	runner := &scriptedRunner{reply: func(_ string, args []string) (RemoteOutput, error) {
		if args[0] == "--version" {
			return RemoteOutput{Stdout: []byte(rsync3VersionOutput)}, nil
		}
		return RemoteOutput{ExitCode: 12, Stderr: []byte("rsync error: error in rsync protocol data stream")}, nil
	}}
	manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run}
	if _, err := manager.SyncMirror(context.Background(), MirrorOptions{Scan: true}); err == nil {
		t.Fatal("a failed push must be an error")
	}
}

func TestRemoteModeRefusesToBringUpALocalServer(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	manager := &Manager{Config: cfg, Resolved: resolved}
	for _, action := range []string{"start", "restart", "setup"} {
		if err := manager.ServiceControlAllowed(action); err == nil {
			t.Errorf("%s must be refused in remote mode", action)
		}
	}
	if err := manager.ServiceControlAllowed("stop"); err != nil {
		t.Errorf("stopping the local service is part of the move: %v", err)
	}
	if _, err := manager.SetupPlan(context.Background()); err == nil {
		t.Error("a setup plan must be refused in remote mode")
	}
	if _, err := manager.ApplySetup(context.Background(), SetupPlan{}); err == nil {
		t.Error("a setup apply must be refused in remote mode")
	}
	local := &Manager{Config: DefaultConfig()}
	if err := local.ServiceControlAllowed("start"); err != nil {
		t.Errorf("local mode must keep its service controls: %v", err)
	}
}
