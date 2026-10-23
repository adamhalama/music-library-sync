//go:build darwin || linux

package navidrome

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// TestRealGateRefusesTheRsyncHandOff pushes with a real rsync 3.x client
// through a stand-in for ssh into the real gate, against a throwaway
// Navidrome-like SQLite database and throwaway lock inodes. No network, no
// sshd and no Navidrome are involved. Opt-in:
//
//	UDL_MIRROR_GATE=/path/to/udl-mirror-gate \
//	UDL_MIRROR_RSYNC=/path/to/rsync-3.x \
//	go test ./internal/navidrome -run RealGateRefuses -v
//
// UDL_MIRROR_RSYNC is optional; it defaults to `rsync` on PATH, and the test
// skips when that is not rsync 3.x (macOS's /usr/bin/rsync is openrsync). The
// same binary plays the server-side receiver, behind a stand-in for rrsync
// that records each hand-off and confines the receiver to the temporary
// mirror.
//
// It verifies the hand-off contract seen by the client: the gate writes one
// JSON refusal line to stderr and exits 2, but the client sees rsync's own
// exit (12, protocol stream closed) and the JSON line mixed with rsync's
// diagnostics. A busy:scan/mirror/recovery refusal must map to its sentinel
// whether it hits the preflight or, after a state change behind the
// preflight's back, the real transfer, and the receiver must never start.
func TestRealGateRefusesTheRsyncHandOff(t *testing.T) {
	h := newRealGateHarness(t)

	t.Run("idle control", func(t *testing.T) {
		f := h.fixture(t)
		if _, err := f.manager.push(context.Background(), 0, nil); err != nil {
			t.Fatalf("idle push: %v", err)
		}
		if got := f.handOffs(); got != 2 {
			t.Fatalf("hand-offs = %d, want preflight + transfer", got)
		}
		raw, err := os.ReadFile(f.sentinel)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Count(string(raw), "no-lock=true\n"); got != 2 {
			t.Fatalf("-no-lock hand-offs = %d, want preflight + transfer with the gate's --lock", got)
		}
		if _, err := os.Stat(filepath.Join(f.library, "a.mp3")); err != nil {
			t.Fatalf("idle push did not reach the mirror: %v", err)
		}
	})

	cases := []struct {
		busy string
		want error
	}{
		{"scan", ErrServerScanning},
		{"mirror", ErrMirrorBusy},
		{"recovery", ErrServerScanning},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.busy+"/raw rsync surface", func(t *testing.T) {
			f := h.fixture(t)
			f.makeBusy(t, tc.busy)
			for _, dry := range []bool{true, false} {
				out, err := DefaultRemoteRunner(context.Background(), nil, h.rsync,
					RsyncPushArgs(f.manager.Config, f.manager.Resolved, 25, dry)...)
				if err != nil {
					t.Fatalf("dry=%v: rsync did not run: %v", dry, err)
				}
				t.Logf("dry=%v exit=%d stderr=%q", dry, out.ExitCode, out.Stderr)
				if out.ExitCode != 12 {
					t.Errorf("dry=%v: local exit = %d, want rsync's 12 (the gate's own exit 2 is not visible)", dry, out.ExitCode)
				}
				if len(bytes.TrimSpace(out.Stdout)) != 0 {
					t.Errorf("dry=%v: stdout = %q; it belongs to the rsync protocol", dry, out.Stdout)
				}
				if json.Valid(bytes.TrimSpace(out.Stderr)) {
					t.Errorf("dry=%v: stderr is only the JSON line; the line-wise parse is untested", dry)
				}
				f.assertRefusal(t, rsyncGateRefusal(out), tc.busy, tc.want)
			}
			if got := f.handOffs(); got != 0 {
				t.Fatalf("the receiver started %d time(s) despite the refusal", got)
			}
		})

		t.Run(tc.busy+"/preflight", func(t *testing.T) {
			f := h.fixture(t)
			f.makeBusy(t, tc.busy)
			transferred := false
			_, err := f.manager.push(context.Background(), 0, func(context.Context) error {
				transferred = true
				return nil
			})
			f.assertRefusal(t, err, tc.busy, tc.want)
			if transferred || f.handOffs() != 0 {
				t.Fatalf("refused preflight went on (transfer hook %v, hand-offs %d)", transferred, f.handOffs())
			}
			f.assertMirrorUntouched(t)
		})

		t.Run(tc.busy+"/transfer after an idle preflight", func(t *testing.T) {
			f := h.fixture(t)
			// The state changes after udl's own checks, which is the race the
			// gate's re-check under its exclusive lock exists to close.
			_, err := f.manager.push(context.Background(), 0, func(context.Context) error {
				f.makeBusy(t, tc.busy)
				return nil
			})
			f.assertRefusal(t, err, tc.busy, tc.want)
			if got := f.handOffs(); got != 1 {
				t.Fatalf("hand-offs = %d, want only the dry-run preflight", got)
			}
			f.assertMirrorUntouched(t)
		})

		t.Run(tc.busy+"/apply-dates", func(t *testing.T) {
			f := h.fixture(t)
			f.makeBusy(t, tc.busy)
			manifest := []byte(`{"version":1,"music_dir":"/synthetic-music","entries":[]}`)
			err := f.manager.callGate(context.Background(), manifest, nil, "apply-dates")
			var refusal *gateCommandError
			if !errors.As(err, &refusal) || refusal.Busy != tc.busy || !errors.Is(err, tc.want) {
				t.Fatalf("apply-dates err = %v (%T), want busy %q mapping to %v", err, err, tc.busy, tc.want)
			}
		})
	}
}

type realGateHarness struct {
	python, gate, rsync string
}

func newRealGateHarness(t *testing.T) realGateHarness {
	t.Helper()
	gate := os.Getenv("UDL_MIRROR_GATE")
	if gate == "" {
		t.Skip("set UDL_MIRROR_GATE to the gate script to run")
	}
	if !filepath.IsAbs(gate) {
		t.Fatalf("UDL_MIRROR_GATE must be absolute, not %q", gate)
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("needs python3")
	}
	rsync := os.Getenv("UDL_MIRROR_RSYNC")
	if rsync == "" {
		if rsync, err = exec.LookPath("rsync"); err != nil {
			t.Skip("needs rsync 3.x; set UDL_MIRROR_RSYNC")
		}
	}
	if rsync, err = filepath.Abs(rsync); err != nil {
		t.Fatal(err)
	}
	version, err := exec.Command(rsync, "--version").Output()
	if err != nil || !rsync3Version.Match(version) {
		t.Skipf("%s is not rsync 3.x; set UDL_MIRROR_RSYNC", rsync)
	}
	return realGateHarness{python: python, gate: gate, rsync: rsync}
}

type realGateFixture struct {
	h                      realGateHarness
	manager                *Manager
	tmp, library, db, lock string
	sentinel               string
	held                   []*os.File
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (h realGateHarness) fixture(t *testing.T) *realGateFixture {
	t.Helper()
	tmp := t.TempDir()
	f := &realGateFixture{
		h:        h,
		tmp:      tmp,
		library:  filepath.Join(tmp, "library"),
		db:       filepath.Join(tmp, "navidrome.db"),
		lock:     filepath.Join(tmp, "scan-mirror.lock"),
		sentinel: filepath.Join(tmp, "hand-offs"),
	}
	t.Cleanup(func() {
		for _, file := range f.held {
			file.Close()
		}
	})
	if err := os.MkdirAll(f.library, 0o755); err != nil {
		t.Fatal(err)
	}
	jobLock := filepath.Join(tmp, "scan-job.lock")
	for _, path := range []string{f.lock, jobLock} {
		// The gate requires admin-made 0600 inodes; chmod defeats the umask.
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Navidrome keeps the database in WAL mode; the zero marker is the
	// go-sqlite3 encoding of time.Time{} that ScanEnd writes.
	f.sqlite(t, "c.execute('PRAGMA journal_mode = wal')\n"+
		"c.executescript('''\n"+
		"CREATE TABLE library (id INTEGER PRIMARY KEY, name TEXT, path TEXT, last_scan_at DATETIME,\n"+
		"  last_scan_started_at DATETIME, full_scan_in_progress BOOLEAN);\n"+
		"CREATE TABLE property (id TEXT PRIMARY KEY, value TEXT);\n"+
		"CREATE TABLE media_file (id TEXT PRIMARY KEY, path TEXT, library_id INTEGER, album_id TEXT,\n"+
		"  missing BOOL DEFAULT false, created_at DATETIME, birth_time DATETIME);\n"+
		"CREATE TABLE album (id TEXT PRIMARY KEY, created_at DATETIME);\n"+
		"''')\n"+
		"c.execute(\"INSERT INTO library VALUES (1, 'Synthetic', '/synthetic-music', \"\n"+
		"  \"'2026-10-22 11:00:00+00:00', '0001-01-01 00:00:00+00:00', 0)\")\n")

	// A stand-in for rrsync -wo: record the hand-off and -no-lock flag, then
	// run the receiver confined to the mirror (the last argument).
	rrsync := filepath.Join(tmp, "fake-rrsync")
	writeScript(t, rrsync, "#!/bin/sh\nset -f\necho hand-off >> "+shellQuote(f.sentinel)+"\n"+
		"[ \"$1 $2\" = \"-wo -munge\" ] || exit 99\n"+
		"no_lock=false\nfor arg in \"$@\"; do\n"+
		"  library=$arg\n  [ \"$arg\" != \"-no-lock\" ] || no_lock=true\ndone\n"+
		"printf 'no-lock=%s\\n' \"$no_lock\" >> "+shellQuote(f.sentinel)+"\n"+
		"cd \"$library\" || exit 98\n"+
		"set -- $SSH_ORIGINAL_COMMAND\nshift\nexec "+shellQuote(h.rsync)+" \"$@\"\n")
	// A stand-in for ssh + sshd's forced command: everything after the
	// target becomes SSH_ORIGINAL_COMMAND, in a scrubbed environment.
	// --nd-url/--scan-credentials are deliberately absent: nothing here may
	// reach an HTTP endpoint.
	gateArgs := []string{
		h.gate, "--library", f.library, "--db", f.db, "--backups", filepath.Join(tmp, "udl-backups"),
		"--min-free-gib", "0", "--rrsync", rrsync, "--lock", f.lock, "--scan-job-lock", jobLock,
	}
	quoted := make([]string, len(gateArgs))
	for i, arg := range gateArgs {
		quoted[i] = shellQuote(arg)
	}
	fakeSSH := filepath.Join(tmp, "fake-ssh")
	writeScript(t, fakeSSH, "#!/bin/sh\n[ \"$1 $2 $3\" = \"-o BatchMode=yes udl-mirror\" ] || exit 255\nshift 3\n"+
		"exec /usr/bin/env -i PATH=/usr/bin:/bin LC_ALL=C SSH_ORIGINAL_COMMAND=\"$*\" "+
		shellQuote(h.python)+" -I "+strings.Join(quoted, " ")+"\n")

	cfg, resolved := remoteTestConfig(t)
	cfg.Remote.SSHPath = fakeSSH
	cfg.Remote.RsyncPath = h.rsync
	writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
	f.manager = &Manager{Config: cfg, Resolved: resolved} // the real subprocess runner
	return f
}

func writeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
}

func (f *realGateFixture) sqlite(t *testing.T, body string) {
	t.Helper()
	script := "import sqlite3, sys\nc = sqlite3.connect(sys.argv[1])\n" + body + "c.commit()\nc.close()\n"
	if out, err := exec.Command(f.h.python, "-I", "-c", script, f.db).CombinedOutput(); err != nil {
		t.Fatalf("fixture database: %v: %s", err, out)
	}
}

// makeBusy puts the server into the state behind each refusal kind: a scan
// holds the common lock shared, another mutation holds it exclusively, and an
// interrupted scan leaves its start marker set until recovery finishes.
func (f *realGateFixture) makeBusy(t *testing.T, busy string) {
	t.Helper()
	if busy == "recovery" {
		f.sqlite(t, "c.execute(\"UPDATE library SET last_scan_started_at = '2026-10-22 12:00:00+00:00'\")\n")
		return
	}
	file, err := os.OpenFile(f.lock, os.O_RDWR, 0) // close-on-exec: not inherited
	if err != nil {
		t.Fatal(err)
	}
	how := syscall.LOCK_EX
	if busy == "scan" {
		how = syscall.LOCK_SH
	}
	if err := syscall.Flock(int(file.Fd()), how|syscall.LOCK_NB); err != nil {
		file.Close()
		t.Fatalf("hold the %s lock: %v", busy, err)
	}
	f.held = append(f.held, file)
}

func (f *realGateFixture) handOffs() int {
	raw, err := os.ReadFile(f.sentinel)
	if err != nil {
		return 0
	}
	return strings.Count(string(raw), "hand-off\n")
}

func (f *realGateFixture) assertRefusal(t *testing.T, err error, busy string, want error) {
	t.Helper()
	var transfer *mirrorTransferRefusal
	var refusal *gateCommandError
	if !errors.As(err, &transfer) || !errors.As(err, &refusal) || refusal.Busy != busy {
		t.Fatalf("err = %v (%T), want the gate's busy %q hand-off refusal", err, err, busy)
	}
	if !errors.Is(err, want) {
		t.Fatalf("busy %q maps to %v, want %v", busy, err, want)
	}
}

func (f *realGateFixture) assertMirrorUntouched(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(f.library)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := []string{}
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("a refused push changed the mirror: %v", names)
	}
}
