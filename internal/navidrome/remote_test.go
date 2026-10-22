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
	// Each test gets its own data dir: the mirror state file lives there,
	// and the default would be the real ~/Library path.
	cfg.Paths.DataDir = t.TempDir()
	cfg.Paths.CacheDir, cfg.Paths.LogFile, cfg.Paths.ConfigFile, cfg.Paths.BackupDir = "", "", "", ""
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
	args := RsyncPushArgs(cfg, resolved, 31, false)
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--times", "--delete-delay", "--delay-updates", "--partial", "--max-delete=31",
		"--filter=+ ..*", "--filter=-s .*", "--itemize-changes", "--timeout=120",
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
	// ".."-names come before the hidden exclude, which is sender-side only.
	if strings.Index(joined, "--filter=+ ..*") > strings.Index(joined, "--filter=-s .*") {
		t.Errorf("the .. include must precede the hidden exclude: %v", args)
	}
	dry := strings.Join(RsyncPushArgs(cfg, resolved, 0, true), " ")
	if !strings.Contains(dry, "--dry-run") || strings.Contains(dry, "--max-delete") {
		t.Errorf("preflight args = %s", dry)
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
		"*deleting   Old/.03 Touched.mp3.Ab12Cd",
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
	// A playlist refresh recreates this folder on its own; it is not a library.
	writeFile(t, filepath.Join(resolved.MusicDir, "udl-playlists", "All Music.nsp"))
	runner := &scriptedRunner{reply: func(string, []string) (RemoteOutput, error) {
		t.Fatal("no command may run for an empty library")
		return RemoteOutput{}, nil
	}}
	manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run}
	if _, err := manager.push(context.Background(), 0, nil); err == nil || !strings.Contains(err.Error(), "empty") {
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
	if _, err := manager.push(context.Background(), 0, nil); err == nil || !strings.Contains(err.Error(), "openrsync") {
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
		{code: rsyncExitMaxDelete, wantErr: "delete limit"},
		{code: rsyncExitPartial, stderr: "IO error encountered -- skipping file deletion", wantErr: "skipped all deletions"},
		{code: 1, stderr: "Another instance of rrsync is already accessing this directory", wantErr: "still running"},
		{code: 255, stderr: "ssh: connect to host music-server: Operation timed out", wantErr: "rsync exit 255"},
	}
	for _, tc := range cases {
		cfg, resolved := remoteTestConfig(t)
		writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
		runner := &scriptedRunner{reply: func(_ string, args []string) (RemoteOutput, error) {
			switch {
			case args[0] == "--version":
				return RemoteOutput{Stdout: []byte(rsync3VersionOutput)}, nil
			case hasArg(args, "--dry-run"):
				return RemoteOutput{Stdout: []byte(">f+++++++++ a.mp3\n")}, nil
			}
			return RemoteOutput{Stdout: []byte(">f+++++++++ a.mp3\n"), Stderr: []byte(tc.stderr), ExitCode: tc.code}, nil
		}}
		manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run}
		result, err := manager.push(context.Background(), 0, nil)
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

func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func argWithPrefix(args []string, prefix string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return arg
		}
	}
	return ""
}

func deletionRunner(t *testing.T, planned int, real *[]string) *scriptedRunner {
	t.Helper()
	var dry strings.Builder
	for i := 0; i < planned; i++ {
		dry.WriteString("*deleting   Old Folder/" + strings.Repeat("x", i%7+1) + "-" + time.Duration(i).String() + ".mp3\n")
	}
	dry.WriteString("*deleting   Old Folder/\n*deleting   Kept/.orphan.mp3.Ab12Cd\n")
	return &scriptedRunner{reply: func(_ string, args []string) (RemoteOutput, error) {
		switch {
		case args[0] == "--version":
			return RemoteOutput{Stdout: []byte(rsync3VersionOutput)}, nil
		case hasArg(args, "--dry-run"):
			return RemoteOutput{Stdout: []byte(dry.String())}, nil
		}
		*real = args
		return RemoteOutput{}, nil
	}}
}

func TestPushRefusesALargeDeletionBeforeChangingAnything(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
	var real []string
	runner := deletionRunner(t, 300, &real)
	manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run}
	result, err := manager.push(context.Background(), 0, nil)
	if err == nil || !strings.Contains(err.Error(), "nothing was pushed") || !strings.Contains(err.Error(), "--allow-deletes 300") {
		t.Fatalf("err = %v", err)
	}
	if real != nil {
		t.Fatalf("the real push ran despite the refusal: %v", real)
	}
	if result.PlannedDeletes != 300 {
		t.Fatalf("planned = %d (folders and hidden temps must not count)", result.PlannedDeletes)
	}

	// An explicit allowance lets the reorganisation through, and the real
	// push may delete everything the preflight listed, so it cannot stop
	// half-way.
	if _, err := manager.push(context.Background(), 300, nil); err != nil {
		t.Fatal(err)
	}
	if got := argWithPrefix(real, "--max-delete="); got != "--max-delete=327" {
		t.Fatalf("real push %s, want 300 files + 2 other deletions + slack", got)
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
		"Artist/Album/...Ready For It.mp3",
		"..Dots/x.mp3",
		".hidden/x.mp3",
		"Artist/.DS_Store",
		"Artist/.hidden.mp3",
		"udl-playlists/All Music.nsp",
		"udl-playlists/stray.mp3",
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
	// Navidrome indexes ".."-names, so they need Date Added too.
	want := []string{"..Dots/x.mp3", "Artist/Album/...Ready For It.mp3", "Artist/Album/01 Song.mp3", "Artist/Album/02 Song.M4A"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("entries = %v, want %v", got, want)
	}
}

func TestBirthTimeManifestFollowsASymlinkedLibrary(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	real := t.TempDir()
	writeFile(t, filepath.Join(real, "Artist", "01 Song.mp3"))
	link := filepath.Join(t.TempDir(), "downloaded")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	cfg.Paths.MusicDir = link
	resolved, err := Resolve(cfg)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := BuildBirthTimeManifest(resolved, func(string) (time.Time, error) { return time.Unix(1_700_000_000, 0), nil })
	if err != nil {
		t.Fatal(err)
	}
	if manifest.MusicDir != link || len(manifest.Entries) != 1 || manifest.Entries[0].Path != "Artist/01 Song.mp3" {
		t.Fatalf("manifest = %+v; it must name the configured root and relative paths under its target", manifest)
	}
}

func TestDateAddedRefusesAnEmptyManifest(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	writeFile(t, filepath.Join(resolved.MusicDir, "cover.jpg"))
	runner := &scriptedRunner{reply: func(string, []string) (RemoteOutput, error) {
		t.Fatal("an empty manifest must not reach the server")
		return RemoteOutput{}, nil
	}}
	manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run,
		BirthTime: func(string) (time.Time, error) { return time.Now(), nil }}
	if _, err := manager.ReconcileDateAdded(context.Background()); err == nil {
		t.Fatal("expected an error rather than \"already matches\"")
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

// mirrorServer supports API reads but rejects API scan calls: remote scans
// belong to the SSH gate, never to the externally reachable Subsonic API.
func mirrorServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	var mu sync.Mutex
	scans := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		body := map[string]any{"status": "ok", "version": "1.16.1"}
		switch strings.TrimPrefix(r.URL.Path, "/rest/") {
		case "startScan.view", "startScan", "getScanStatus.view", "getScanStatus":
			scans++
			t.Errorf("remote mode must not call the scan API: %s", r.URL.Path)
			body["status"] = "failed"
			body["error"] = map[string]any{"code": 50, "message": "use the SSH gate"}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"subsonic-response": body})
	}))
	t.Cleanup(server.Close)
	return server, &scans
}

func TestSyncMirrorPushesScansAndAppliesDates(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	writeFile(t, filepath.Join(resolved.MusicDir, "Artist/01 Song.mp3"))
	server, apiScans := mirrorServer(t)
	gateScans := 0
	runner := &scriptedRunner{reply: func(name string, args []string) (RemoteOutput, error) {
		switch {
		case args[0] == "--version":
			return RemoteOutput{Stdout: []byte(rsync3VersionOutput)}, nil
		case name == DefaultRemoteRsyncPath:
			return RemoteOutput{Stdout: []byte(">f+++++++++ Artist/01 Song.mp3\n")}, nil
		case name == DefaultRemoteSSHPath && requireGateCommand(t, name, args) == "udl-mirror scan-status":
			return RemoteOutput{Stdout: []byte(idleRemoteScanStatus)}, nil
		case name == DefaultRemoteSSHPath && requireGateCommand(t, name, args) == "udl-mirror scan --timeout-seconds 900":
			gateScans++
			return RemoteOutput{Stdout: []byte(completedRemoteScan)}, nil
		case name == DefaultRemoteSSHPath && requireGateCommand(t, name, args) == "udl-mirror apply-dates":
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
	if !result.Scanned || gateScans != 1 || *apiScans != 0 || result.Dates == nil || result.Dates.Updated != 1 || len(result.Warnings) != 0 {
		t.Fatalf("result = %+v gate scans=%d API scans=%d", result, gateScans, *apiScans)
	}
	state, err := LoadMirrorState(resolved)
	if err != nil || state.LastPushAt == nil || state.LastScanAt == nil || state.ScanPending || state.LastPushError != "" {
		t.Fatalf("state = %+v, %v", state, err)
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
	runner := pushRunner(t, RemoteOutput{}, "", "")
	manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run,
		API: NewClient(server.URL, Credentials{Username: "u", Password: "p"})}
	result, err := manager.SyncMirror(context.Background(), MirrorOptions{Scan: true, Dates: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Scanned || *scans != 0 || len(runner.calls) != 4 || gateScanCalls(runner) != 0 {
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
	server, _ := mirrorServer(t)
	manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run,
		API: NewClient(server.URL, Credentials{Username: "u", Password: "p"})}
	if _, err := manager.SyncMirror(context.Background(), MirrorOptions{Scan: true}); err == nil {
		t.Fatal("a failed push must be an error")
	}
	state, _ := LoadMirrorState(resolved)
	if state.LastPushError == "" || state.LastErrorAt == nil {
		t.Fatalf("a failed push must be recorded: %+v", state)
	}
	problems := manager.mirrorProblems()
	if len(problems) == 0 || !strings.Contains(problems[0], "last mirror push failed") {
		t.Fatalf("problems = %v", problems)
	}
	for _, want := range []string{"push is incomplete", "server may be partly updated", "next `udl navidrome push` completes it"} {
		if !strings.Contains(problems[0], want) {
			t.Errorf("failed-push warning must say %q: %s", want, problems[0])
		}
	}
	if strings.Contains(problems[0], "previous mirror") {
		t.Fatalf("a failed push must not promise the previous mirror: %s", problems[0])
	}
}

func TestAnUnfinishedScanIsRetriedByTheNextPush(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
	changed := true
	gateScans := 0
	runner := &scriptedRunner{reply: func(name string, args []string) (RemoteOutput, error) {
		switch {
		case name == DefaultRemoteRsyncPath && len(args) == 1 && args[0] == "--version":
			return RemoteOutput{Stdout: []byte(rsync3VersionOutput)}, nil
		case name == DefaultRemoteRsyncPath && hasArg(args, "--dry-run"):
			return RemoteOutput{}, nil
		case name == DefaultRemoteRsyncPath:
			if changed {
				return RemoteOutput{Stdout: []byte(">f+++++++++ a.mp3\n")}, nil
			}
			return RemoteOutput{}, nil
		}
		switch requireGateCommand(t, name, args) {
		case "udl-mirror scan-status":
			return RemoteOutput{Stdout: []byte(idleRemoteScanStatus)}, nil
		case "udl-mirror scan --timeout-seconds 1":
			gateScans++
			return RemoteOutput{Stdout: []byte(`{"ok":false,"pending":true,"error":"scan still running"}`), ExitCode: 1}, nil
		case "udl-mirror scan --timeout-seconds 900":
			gateScans++
			return RemoteOutput{Stdout: []byte(completedRemoteScan)}, nil
		case "udl-mirror apply-dates":
			return RemoteOutput{Stdout: []byte(`{"ok":true,"entries":1,"matched":1}`)}, nil
		default:
			t.Fatalf("unexpected gate call: %s %v", name, args)
			return RemoteOutput{}, nil
		}
	}}
	server, apiScans := mirrorServer(t)
	manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run,
		BirthTime: func(string) (time.Time, error) { return time.Unix(1_700_000_000, 0), nil },
		API:       NewClient(server.URL, Credentials{Username: "u", Password: "p"})}
	first, err := manager.SyncMirror(context.Background(), MirrorOptions{
		Scan: true, Dates: true, IdleWait: 20 * time.Millisecond, FinishWait: 20 * time.Millisecond})
	if err != nil || first.Scanned || first.Dates != nil || len(first.Warnings) == 0 || gateScans != 1 {
		t.Fatalf("first = %+v, gate scans=%d err=%v", first, gateScans, err)
	}
	if state, _ := LoadMirrorState(resolved); !state.ScanPending {
		t.Fatalf("an unfinished scan must stay pending: %+v", state)
	}

	// The retry transfers nothing, but still owes the first push's scan.
	changed = false
	second, err := manager.SyncMirror(context.Background(), MirrorOptions{Scan: true, Dates: true})
	if err != nil || !second.Scanned || second.Dates == nil || gateScans != 2 || *apiScans != 0 {
		t.Fatalf("second = %+v gate scans=%d API scans=%d err=%v", second, gateScans, *apiScans, err)
	}
	if state, _ := LoadMirrorState(resolved); state.ScanPending || state.DatesPending {
		t.Fatalf("completed scan and dates must clear the pending flags: %+v", state)
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

// pushRunner answers the rsync steps, the idle gate query and a successful
// gate-owned scan; status/apply-dates get gateReply. Unknown commands fail.
func pushRunner(t *testing.T, real RemoteOutput, preflight string, gateReply string) *scriptedRunner {
	t.Helper()
	return &scriptedRunner{reply: func(name string, args []string) (RemoteOutput, error) {
		switch {
		case name == DefaultRemoteRsyncPath && len(args) == 1 && args[0] == "--version":
			return RemoteOutput{Stdout: []byte(rsync3VersionOutput)}, nil
		case name == DefaultRemoteRsyncPath && hasArg(args, "--dry-run"):
			return RemoteOutput{Stdout: []byte(preflight)}, nil
		case name == DefaultRemoteRsyncPath:
			return real, nil
		}
		switch requireGateCommand(t, name, args) {
		case "udl-mirror scan-status":
			return RemoteOutput{Stdout: []byte(idleRemoteScanStatus)}, nil
		case "udl-mirror scan --timeout-seconds 900":
			return RemoteOutput{Stdout: []byte(completedRemoteScan)}, nil
		case "udl-mirror status", "udl-mirror apply-dates":
			return RemoteOutput{Stdout: []byte(gateReply)}, nil
		default:
			t.Fatalf("unexpected gate call: %s %v", name, args)
			return RemoteOutput{}, nil
		}
	}}
}

func TestAnUnreadableFileIsAWarningButAnUnreadableFolderBlocksTheScan(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
	server, scans := mirrorServer(t)

	fileErr := RemoteOutput{ExitCode: rsyncExitPartial, Stdout: []byte(">f+++++++++ b.mp3\n*deleting   Old/x.mp3\n"),
		Stderr: []byte("rsync: [sender] send_files failed to open \"/m/c.mp3\": Permission denied (13)\n")}
	fileRunner := pushRunner(t, fileErr, "", `{"ok":true,"entries":1}`)
	manager := &Manager{Config: cfg, Resolved: resolved, Remote: fileRunner.run,
		BirthTime: func(string) (time.Time, error) { return time.Unix(1_700_000_000, 0), nil },
		API:       NewClient(server.URL, Credentials{Username: "u", Password: "p"})}
	result, err := manager.SyncMirror(context.Background(), MirrorOptions{Scan: true, Dates: true})
	if err != nil || !result.Scanned || gateScanCalls(fileRunner) != 1 || *scans != 0 || len(result.Warnings) == 0 {
		t.Fatalf("one unreadable file must not block the scan: %+v, %v", result, err)
	}

	folderErr := RemoteOutput{ExitCode: rsyncExitPartial, Stdout: []byte(">f+++++++++ New/b.mp3\n"),
		Stderr: []byte("IO error encountered -- skipping file deletion\n")}
	folderRunner := pushRunner(t, folderErr, "", "")
	manager.Remote = folderRunner.run
	result, err = manager.SyncMirror(context.Background(), MirrorOptions{Scan: true, Dates: true})
	if err == nil || result.Scanned || gateScanCalls(folderRunner) != 0 || *scans != 0 {
		t.Fatalf("skipped deletions must not be scanned: %+v, %v", result, err)
	}
	if state, _ := LoadMirrorState(resolved); !state.ScanPending || state.LastPushError == "" {
		t.Fatalf("files were transferred, so a scan is owed once a clean push lands: %+v", state)
	}
}

func TestASecondConcurrentPushIsSkippedWithoutTouchingState(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
	unlock, err := lockMirror(resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	runner := &scriptedRunner{reply: func(string, []string) (RemoteOutput, error) {
		t.Fatal("a skipped push must not run rsync")
		return RemoteOutput{}, nil
	}}
	manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run}
	if _, err := manager.SyncMirror(context.Background(), MirrorOptions{Scan: true}); !errors.Is(err, ErrMirrorBusy) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(MirrorStatePath(resolved)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a skipped push must not write state: %v", err)
	}
}

func TestDeletionsInsideHiddenFoldersAreNotTracks(t *testing.T) {
	tracks, total := countPlannedDeletions(strings.Join([]string{
		"*deleting   Album/.~tmp~/01 Song.mp3",
		"*deleting   .hd/x.mp3",
		"*deleting   Album/..Still A Track.mp3",
		"*deleting   Album/02 Gone.mp3",
	}, "\n"))
	if tracks != 2 || total != 4 {
		t.Fatalf("tracks=%d total=%d", tracks, total)
	}
}

func TestPushRefusesATransferThatWouldFillTheServer(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
	status := `{"ok":true,"library":{"files":1},"database":{"present":true},"disk":{"free_bytes":42949672960}}` // 40 GiB
	var real []string
	big := "Total transferred file size: 30,000,000,000 bytes\n"
	runner := pushRunner(t, RemoteOutput{}, big, status)
	original := runner.reply
	runner.reply = func(name string, args []string) (RemoteOutput, error) {
		if name == DefaultRemoteRsyncPath && !hasArg(args, "--dry-run") && args[0] != "--version" {
			real = args
		}
		return original(name, args)
	}
	manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run}
	if _, err := manager.push(context.Background(), 0, nil); err == nil || !strings.Contains(err.Error(), "free") || real != nil {
		t.Fatalf("err = %v, real push ran: %v", err, real != nil)
	}
	small := "Total transferred file size: 5,000,000 bytes\n"
	manager.Remote = pushRunner(t, RemoteOutput{}, small, status).run
	if _, err := manager.push(context.Background(), 0, nil); err != nil {
		t.Fatalf("a small push must pass: %v", err)
	}
}

func TestAFailedReconcileIsRetriedWithoutAnotherScan(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
	server, scans := mirrorServer(t)
	changed := RemoteOutput{Stdout: []byte(">f+++++++++ a.mp3\n")}
	firstRunner := pushRunner(t, changed, "", `{"ok":false,"error":"database is locked"}`)
	manager := &Manager{Config: cfg, Resolved: resolved,
		Remote:    firstRunner.run,
		BirthTime: func(string) (time.Time, error) { return time.Unix(1_700_000_000, 0), nil },
		API:       NewClient(server.URL, Credentials{Username: "u", Password: "p"})}
	first, err := manager.SyncMirror(context.Background(), MirrorOptions{Scan: true, Dates: true})
	if err != nil || !first.Scanned || first.Dates != nil || gateScanCalls(firstRunner) != 1 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	if state, _ := LoadMirrorState(resolved); state.ScanPending || !state.DatesPending {
		t.Fatalf("the scan finished but the dates did not: %+v", state)
	}
	secondRunner := pushRunner(t, RemoteOutput{}, "", `{"ok":true,"entries":1,"updated":1,"candidates":1}`)
	manager.Remote = secondRunner.run
	second, err := manager.SyncMirror(context.Background(), MirrorOptions{Scan: true, Dates: true})
	if err != nil || second.Scanned || second.Dates == nil || gateScanCalls(secondRunner) != 0 || *scans != 0 {
		t.Fatalf("second = %+v scans=%d err=%v", second, *scans, err)
	}
	if state, _ := LoadMirrorState(resolved); state.DatesPending {
		t.Fatalf("dates_pending must clear: %+v", state)
	}
}

func TestStatusNoticesAMirrorThatFellBehind(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
	writeFile(t, filepath.Join(resolved.MusicDir, "..b.mp3"))
	writeFile(t, filepath.Join(resolved.MusicDir, ".DS_Store"))
	writeFile(t, filepath.Join(resolved.MusicDir, "c.mp3.part"))
	manager := &Manager{Config: cfg, Resolved: resolved}
	_, newest, err := localLibraryShape(resolved.MusicDir)
	if err != nil {
		t.Fatal(err)
	}
	var status RemoteStatus
	status.Library.Files = 2
	status.Library.NewestMtime = &newest
	if problems := manager.staleMirrorProblems(status); len(problems) != 0 {
		t.Fatalf("an in-sync mirror has no problems: %v", problems)
	}
	status.Library.Files = 1
	if problems := manager.staleMirrorProblems(status); len(problems) != 1 || !strings.Contains(problems[0], "has 1 files") {
		t.Fatalf("a missing file must show: %v", problems)
	}
	status.Library.Files = 2
	older := newest.Add(-time.Hour)
	status.Library.NewestMtime = &older
	if problems := manager.staleMirrorProblems(status); len(problems) != 1 || !strings.Contains(problems[0], "behind") {
		t.Fatalf("an older mirror must show: %v", problems)
	}
}

func TestSyncMirrorRecordsTheOwedScanBeforeTheTransfer(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
	server, _ := mirrorServer(t)
	api := NewClient(server.URL, Credentials{Username: "u", Password: "p"})

	cases := []struct {
		name        string
		preflight   RemoteOutput
		real        RemoteOutput
		realErr     error
		wantPending bool
		wantErr     bool
	}{
		// A real transfer that failed with nothing itemized may still have
		// changed the server: rsync's stdout reaches this Mac late or never.
		{name: "transfer canceled", realErr: context.Canceled, wantPending: true, wantErr: true},
		{name: "transfer failed", real: RemoteOutput{ExitCode: 12, Stderr: []byte("connection unexpectedly closed")}, wantPending: true, wantErr: true},
		// No real transfer ran, so nothing can be owed.
		{name: "preflight failed", preflight: RemoteOutput{ExitCode: 12, Stderr: []byte("connection unexpectedly closed")}, wantErr: true},
		// A clean no-op restores the previous (clear) flag.
		{name: "nothing changed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.Remove(MirrorStatePath(resolved)); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			var pendingAtTransfer *bool
			runner := &scriptedRunner{reply: func(name string, args []string) (RemoteOutput, error) {
				switch {
				case args[0] == "--version":
					return RemoteOutput{Stdout: []byte(rsync3VersionOutput)}, nil
				case name == DefaultRemoteRsyncPath && hasArg(args, "--dry-run"):
					return tc.preflight, nil
				case name == DefaultRemoteRsyncPath:
					state, err := LoadMirrorState(resolved)
					if err != nil {
						t.Fatal(err)
					}
					pendingAtTransfer = &state.ScanPending
					return tc.real, tc.realErr
				}
				if requireGateCommand(t, name, args) != "udl-mirror scan-status" {
					t.Fatalf("unexpected gate call: %s %v", name, args)
				}
				return RemoteOutput{Stdout: []byte(idleRemoteScanStatus)}, nil
			}}
			manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run, API: api,
				BirthTime: func(string) (time.Time, error) { return time.Unix(1_700_000_000, 0), nil }}
			_, err := manager.SyncMirror(context.Background(), MirrorOptions{Scan: true, Dates: true})
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v", err)
			}
			if pendingAtTransfer != nil && !*pendingAtTransfer {
				t.Fatal("scan_pending must be on disk before the real transfer starts")
			}
			if state, _ := LoadMirrorState(resolved); state.ScanPending != tc.wantPending {
				t.Fatalf("scan_pending = %v, want %v", state.ScanPending, tc.wantPending)
			}
		})
	}
}
