package navidrome

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeSQLite is enough of a database for the client's checks: the client
// verifies magic, size and sha256; the server already ran quick_check.
func fakeSQLite(size int) []byte {
	payload := bytes.Repeat([]byte{0x5a}, size)
	copy(payload, sqliteMagic)
	return payload
}

func exportReply(payload []byte, mutate func(*serverDBExportHeader)) []byte {
	sum := sha256.Sum256(payload)
	header := serverDBExportHeader{
		OK: true, Format: "sqlite", SizeBytes: int64(len(payload)), SHA256: hex.EncodeToString(sum[:]),
		CreatedAt: "2026-10-25T10:00:00.123456Z", Path: "/srv/appdata/navidrome/udl-backups/udl_backup_x_export.db",
	}
	if mutate != nil {
		mutate(&header)
	}
	line, _ := json.Marshal(header)
	return append(append(line, '\n'), payload...)
}

func exportManager(t *testing.T, stdout []byte, exit int) (*Manager, *scriptedRunner) {
	t.Helper()
	cfg, resolved := remoteTestConfig(t)
	runner := &scriptedRunner{reply: func(string, []string) (RemoteOutput, error) {
		return RemoteOutput{Stdout: stdout, ExitCode: exit}, nil
	}}
	clock := time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
	manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run, Now: func() time.Time { return clock }}
	return manager, runner
}

func TestExportServerDBWritesAVerifiedCopy(t *testing.T) {
	payload := fakeSQLite(4096)
	manager, runner := exportManager(t, exportReply(payload, nil), 0)
	result, err := manager.ExportServerDB(context.Background(), ServerDBExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(manager.Resolved.DataDir, ServerDBExportDirName, "navidrome-server-20261025T100000Z.db")
	if result.Path != want || result.SizeBytes != 4096 || result.Kept != 1 || len(result.Warnings) != 0 {
		t.Fatalf("result = %+v", result)
	}
	got, err := os.ReadFile(want)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("written copy differs: %v", err)
	}
	info, _ := os.Stat(want)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode())
	}
	if dir, _ := os.Stat(filepath.Dir(want)); dir.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v", dir.Mode())
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(want), ".*"))
	if len(leftovers) != 0 {
		t.Fatalf("temporary files left: %v", leftovers)
	}
	call := runner.calls[0]
	if call[0] != DefaultRemoteSSHPath || call[len(call)-2] != "udl-mirror" || call[len(call)-1] != "udl-mirror export-db" {
		t.Fatalf("ssh call = %v", call)
	}
	state, err := LoadMirrorState(manager.Resolved)
	if err != nil || state.LastExportAt == nil || !state.LastExportAt.Equal(result.ExportedAt) || state.LastExportError != "" {
		t.Fatalf("state = %+v, %v", state, err)
	}
}

func TestExportServerDBRejectsBadReplies(t *testing.T) {
	payload := fakeSQLite(1024)
	notSQLite := bytes.Repeat([]byte{'x'}, 1024)
	cases := map[string]struct {
		stdout []byte
		exit   int
		want   string
	}{
		"short stream": {exportReply(payload, nil)[:len(exportReply(payload, nil))-500], 1, "sent 524 bytes but declared 1024"},
		"extra bytes":  {append(exportReply(payload, nil), 'x'), 0, "sent 1025 bytes"},
		"size header":  {exportReply(payload, func(h *serverDBExportHeader) { h.SizeBytes = 1000 }), 0, "declared 1000"},
		"sha mismatch": {exportReply(payload, func(h *serverDBExportHeader) { h.SHA256 = strings.Repeat("0", 64) }), 0, "sha256 mismatch"},
		"not sqlite":   {exportReply(notSQLite, nil), 0, "not an SQLite database"},
		"wrong format": {exportReply(payload, func(h *serverDBExportHeader) { h.Format = "tar" }), 0, `format "tar"`},
		"oversized": {exportReply(payload, func(h *serverDBExportHeader) { h.SizeBytes = maxServerDBExportBytes + 1 }), 0,
			"larger than 512 MiB"},
		"gate error":   {[]byte(`{"error": "RuntimeError: the Navidrome database at /x is missing or empty", "ok": false}` + "\n"), 1, "missing or empty"},
		"no reply":     {nil, 255, "not reachable over SSH"},
		"garbage":      {[]byte("Welcome to Ubuntu\n"), 0, "no usable reply"},
		"exit nonzero": {exportReply(payload, nil), 1, "exited 1"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			manager, _ := exportManager(t, tc.stdout, tc.exit)
			_, err := manager.ExportServerDB(context.Background(), ServerDBExportOptions{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if entries, _ := os.ReadDir(ServerDBExportDir(manager.Resolved)); len(entries) != 0 {
				t.Fatalf("a rejected export must write nothing: %v", entries)
			}
			state, _ := LoadMirrorState(manager.Resolved)
			if state.LastExportError == "" || state.LastExportErrorAt == nil || state.LastExportAt != nil {
				t.Fatalf("the failure must be recorded: %+v", state)
			}
		})
	}
}

func TestCappedRunnerStopsAnOversizedStream(t *testing.T) {
	if _, err := exec.LookPath("head"); err != nil {
		t.Skip("needs head")
	}
	run := cappedRemoteRunner(1 << 20)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// An endless stream: the cap must stop it long before the timeout.
	_, err := run(ctx, nil, "/bin/sh", "-c", "head -c 100000000 /dev/zero")
	if !errors.Is(err, errServerDBExportTooLarge) {
		t.Fatalf("err = %v", err)
	}
	out, err := run(ctx, nil, "/bin/sh", "-c", "printf ok; exit 3")
	if err != nil || out.ExitCode != 3 || string(out.Stdout) != "ok" {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
}

func TestCappedRunnerKillsAQuietChildOnOverflow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	// The child overflows the cap and then goes quiet: nothing else would
	// end it before the transfer timeout.
	_, err := cappedRemoteRunner(8)(ctx, nil, "/bin/sh", "-c", "printf 1234567890; sleep 5")
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("the cap must stop the subprocess at once; it ran for %s", elapsed)
	}
	if !errors.Is(err, errServerDBExportTooLarge) {
		t.Fatalf("err = %v", err)
	}
}

func TestExportRotationKeepsTheNewestAndIgnoresForeignFiles(t *testing.T) {
	manager, _ := exportManager(t, nil, 0)
	dir := ServerDBExportDir(manager.Resolved)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := []string{
		"navidrome-server-20260101T110000Z.db",
		"navidrome-server-20260201T110000Z.db",
		"navidrome-server-20260301T110000Z.db",
	}
	foreign := []string{"navidrome-from-server.db", "navidrome-server-20250101T110000Z.db.bak", "notes.txt", "navidrome-server-latest.db"}
	for _, name := range append(append([]string{}, old...), foreign...) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A matching symlink is not ours to delete either.
	if err := os.Symlink("/nonexistent", filepath.Join(dir, "navidrome-server-20200101T000000Z.db")); err != nil {
		t.Fatal(err)
	}
	payload := fakeSQLite(512)
	manager.Remote = (&scriptedRunner{reply: func(string, []string) (RemoteOutput, error) {
		return RemoteOutput{Stdout: exportReply(payload, nil)}, nil
	}}).run
	result, err := manager.ExportServerDB(context.Background(), ServerDBExportOptions{Keep: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kept != 2 || len(result.Removed) != 2 {
		t.Fatalf("result = %+v", result)
	}
	entries, _ := os.ReadDir(dir)
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	want := map[string]bool{
		"navidrome-server-20261025T100000Z.db": true, "navidrome-server-20260301T110000Z.db": true,
		"navidrome-server-20200101T000000Z.db": true,
	}
	for _, name := range foreign {
		want[name] = true
	}
	if len(names) != len(want) {
		t.Fatalf("left = %v", names)
	}
	for _, name := range names {
		if !want[name] {
			t.Fatalf("unexpected survivor %s in %v", name, names)
		}
	}
}

func TestExportServerDBHonoursOutDirAndRefusesToClobber(t *testing.T) {
	payload := fakeSQLite(512)
	manager, _ := exportManager(t, exportReply(payload, nil), 0)
	out := filepath.Join(t.TempDir(), "copies")
	result, err := manager.ExportServerDB(context.Background(), ServerDBExportOptions{OutDir: out})
	if err != nil || filepath.Dir(result.Path) != out {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	// Same clock, same name: never replace an existing copy.
	if _, err := manager.ExportServerDB(context.Background(), ServerDBExportOptions{OutDir: out}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v", err)
	}
	state, _ := LoadMirrorState(manager.Resolved)
	if state.LastExportAt == nil || state.LastExportError == "" {
		t.Fatalf("a later failure keeps the last good time and records the error: %+v", state)
	}
}

func TestExportServerDBNeedsRemoteMode(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	cfg.Mode = ModeLocal
	manager := &Manager{Config: cfg, Resolved: resolved}
	if _, err := manager.ExportServerDB(context.Background(), ServerDBExportOptions{}); err == nil {
		t.Fatal("local mode must refuse")
	}
	if _, err := os.Stat(MirrorStatePath(resolved)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused export must not write state: %v", err)
	}
}

func TestExportStateWaitsForARunningPushAndKeepsItsState(t *testing.T) {
	restoreWait, restorePoll := exportStateLockWait, exportStateLockPoll
	exportStateLockWait, exportStateLockPoll = 10*time.Second, 10*time.Millisecond
	defer func() { exportStateLockWait, exportStateLockPoll = restoreWait, restorePoll }()

	payload := fakeSQLite(512)
	downloaded := make(chan struct{})
	manager, _ := exportManager(t, nil, 0)
	manager.Remote = (&scriptedRunner{reply: func(string, []string) (RemoteOutput, error) {
		close(downloaded)
		return RemoteOutput{Stdout: exportReply(payload, nil)}, nil
	}}).run

	// A push holds the lock for the whole download and writes its own state.
	unlock, err := lockMirror(manager.Resolved)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := manager.ExportServerDB(context.Background(), ServerDBExportOptions{})
		done <- err
	}()
	<-downloaded
	pushed := time.Date(2026, 10, 25, 9, 59, 0, 0, time.UTC)
	if err := saveMirrorState(manager.Resolved, MirrorState{LastPushAt: &pushed, ScanPending: true}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if state, _ := LoadMirrorState(manager.Resolved); state.LastExportAt != nil {
		t.Fatal("the export wrote state while a push held the lock")
	}
	unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	state, err := LoadMirrorState(manager.Resolved)
	if err != nil || state.LastExportAt == nil || state.LastPushAt == nil || !state.ScanPending {
		t.Fatalf("the export must merge into the push's state: %+v, %v", state, err)
	}
}

func TestExportStillSucceedsWhenTheStateLockNeverFrees(t *testing.T) {
	restoreWait, restorePoll := exportStateLockWait, exportStateLockPoll
	exportStateLockWait, exportStateLockPoll = 50*time.Millisecond, 10*time.Millisecond
	defer func() { exportStateLockWait, exportStateLockPoll = restoreWait, restorePoll }()

	manager, _ := exportManager(t, exportReply(fakeSQLite(512), nil), 0)
	unlock, err := lockMirror(manager.Resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	result, err := manager.ExportServerDB(context.Background(), ServerDBExportOptions{})
	if err != nil || result.Path == "" || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "could not record") {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestExportProblems(t *testing.T) {
	now := time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
	recent := now.Add(-7 * 24 * time.Hour)
	stale := now.Add(-15 * 24 * time.Hour)
	failed := now.Add(-time.Hour)
	cases := map[string]struct {
		state MirrorState
		want  string
	}{
		"never":  {MirrorState{}, ""},
		"recent": {MirrorState{LastExportAt: &recent}, ""},
		"stale":  {MirrorState{LastExportAt: &stale}, "15 days ago"},
		"failed": {MirrorState{LastExportAt: &recent, LastExportError: "boom", LastExportErrorAt: &failed}, "failed"},
	}
	for name, tc := range cases {
		problems := exportProblems(tc.state, now)
		if tc.want == "" {
			if len(problems) != 0 {
				t.Errorf("%s: problems = %v", name, problems)
			}
			continue
		}
		if len(problems) != 1 || !strings.Contains(problems[0], tc.want) {
			t.Errorf("%s: problems = %v", name, problems)
		}
	}

	// Status surfaces them through the mirror problems.
	manager, _ := exportManager(t, nil, 0)
	manager.Now = func() time.Time { return now }
	if err := saveMirrorState(manager.Resolved, MirrorState{LastPushAt: &recent, LastExportAt: &stale}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(manager.mirrorProblems(), "\n")
	if !strings.Contains(joined, "off-server copy") {
		t.Fatalf("mirror problems = %s", joined)
	}
}

// TestExportParsesARealGateReply runs the real gate against a throwaway
// SQLite database and feeds its stdout to the client. Opt-in:
//
//	UDL_MIRROR_GATE=/path/to/udl-mirror-gate go test ./internal/navidrome -run RealGate
func TestExportParsesARealGateReply(t *testing.T) {
	gate := os.Getenv("UDL_MIRROR_GATE")
	if gate == "" {
		t.Skip("set UDL_MIRROR_GATE to the gate script to run")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("needs python3")
	}
	tmp := t.TempDir()
	db := filepath.Join(tmp, "navidrome.db")
	create := exec.Command(python, "-I", "-c",
		"import sqlite3, sys\n"+
			"c = sqlite3.connect(sys.argv[1])\n"+
			"c.execute('PRAGMA journal_mode = wal')\n"+
			"c.execute('CREATE TABLE annotation (item_id TEXT, starred INTEGER, play_count INTEGER)')\n"+
			"c.executemany('INSERT INTO annotation VALUES (?, ?, ?)', [(str(i), i % 2, i) for i in range(5000)])\n"+
			"c.commit()\n", db)
	if out, err := create.CombinedOutput(); err != nil {
		t.Fatalf("create db: %v: %s", err, out)
	}
	library := filepath.Join(tmp, "library")
	if err := os.MkdirAll(library, 0o755); err != nil {
		t.Fatal(err)
	}
	// A stand-in for ssh: the forced command sees the remote command in
	// SSH_ORIGINAL_COMMAND, exactly as sshd would set it.
	fakeSSH := filepath.Join(tmp, "fake-ssh")
	script := "#!/bin/sh\n[ \"$1 $2 $3\" = \"-o BatchMode=yes udl-mirror\" ] || exit 255\n" +
		"SSH_ORIGINAL_COMMAND=\"$4\" exec " + python + " -I " + gate +
		" --library " + library + " --db " + db + " --backups " + filepath.Join(tmp, "udl-backups") + "\n"
	if err := os.WriteFile(fakeSSH, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	manager, _ := exportManager(t, nil, 0)
	manager.Remote = nil // the real, capped subprocess runner
	manager.Config.Remote.SSHPath = fakeSSH
	result, err := manager.ExportServerDB(context.Background(), ServerDBExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(result.Path)
	check := exec.Command(python, "-I", "-c",
		"import sqlite3, sys\n"+
			"c = sqlite3.connect(sys.argv[1])\n"+
			"assert c.execute('PRAGMA quick_check').fetchone()[0] == 'ok'\n"+
			"print(c.execute('SELECT count(*), sum(starred) FROM annotation').fetchone())\n", result.Path)
	out, err := check.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "(5000, 2500)" {
		t.Fatalf("received copy: %v: %s (%d bytes)", err, out, len(got))
	}
}
