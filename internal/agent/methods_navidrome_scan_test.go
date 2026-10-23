package agent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/jaa/update-downloads/internal/navidrome"
)

func writeMirrorTestState(t *testing.T, manager *navidrome.Manager, state navidrome.MirrorState) {
	t.Helper()
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(navidrome.MirrorStatePath(manager.Resolved), payload, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMirrorPushGateStatusRefusalsReachRunFinished(t *testing.T) {
	for _, tt := range []struct {
		name, reply, kind string
		exit              int
		err               error
	}{
		{name: "scanning", reply: `{"ok":true,"scanning":true}`, kind: "server_scanning"},
		{name: "recovery marker", reply: `{"ok":true,"scanning":false,"recovery_pending":true}`, kind: "server_scanning"},
		{name: "migration marker", reply: `{"ok":true,"scanning":false,"migration_scan_pending":true}`, kind: "server_scanning"},
		{name: "scan job lock", reply: `{"ok":true,"scanning":false,"scan_job_busy":true}`, kind: "server_scanning"},
		// A mirror lock belongs to another push, not a Navidrome scan.
		{name: "mirror lock", reply: `{"ok":true,"scanning":false,"mirror_busy":true}`, kind: "busy"},
		{name: "invalid control locks", reply: `{"ok":true,"scanning":false,"lock_error":"scan control locks are unreadable or invalid","mirror_busy":true,"scan_job_busy":true}`, kind: "failed"},
		{name: "invalid locks with API outage", reply: `{"ok":true,"scanning":null,"api_error":"loopback API unavailable","lock_error":"scan control locks are unreadable or invalid","mirror_busy":true,"scan_job_busy":true}`, kind: "failed"},
		{name: "unknown scanning", reply: `{"ok":true,"scanning":null}`, kind: "server_scanning"},
		{name: "missing scanning", reply: `{"ok":true}`, kind: "server_scanning"},
		{name: "API error despite idle", reply: `{"ok":true,"scanning":false,"api_error":"loopback API unavailable"}`, kind: "server_scanning"},
		{name: "API unavailable", reply: `{"ok":true,"scanning":null,"api_error":"loopback API unavailable"}`, kind: "server_scanning"},
		{name: "scan refusal", reply: `{"ok":false,"busy":"scan","error":"scan lock held"}`, exit: 2, kind: "server_scanning"},
		{name: "recovery refusal", reply: `{"ok":false,"busy":"recovery","error":"unfinished scan"}`, exit: 2, kind: "server_scanning"},
		{name: "mirror refusal", reply: `{"ok":false,"busy":"mirror","error":"mirror lock held"}`, exit: 2, kind: "busy"},
		{name: "invalid reply", reply: `not JSON`, kind: "server_scanning"},
		{name: "SSH unavailable", exit: 255, kind: "server_scanning"},
		{name: "command cannot start", err: os.ErrNotExist, kind: "server_scanning"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, pending := range []bool{false, true} {
				name := "fresh"
				if pending {
					name = "existing scan debt"
				}
				t.Run(name, func(t *testing.T) {
					runner := &mirrorTestRunner{reply: func(_ context.Context, name string, args []string) (navidrome.RemoteOutput, error) {
						switch {
						case args[0] == "--version":
							return navidrome.RemoteOutput{Stdout: []byte(mirrorTestRsyncVersion)}, nil
						case name == navidrome.DefaultRemoteRsyncPath && containsArg(args, "--dry-run"):
							return navidrome.RemoteOutput{}, nil
						case mirrorTestGateCommand(name, args) == "udl-mirror scan-status":
							return navidrome.RemoteOutput{Stdout: []byte(tt.reply), ExitCode: tt.exit}, tt.err
						}
						t.Errorf("a refused scan check must not transfer or scan: %s %v", name, args)
						return navidrome.RemoteOutput{ExitCode: 1}, nil
					}}
					server, output, manager := newMirrorPushServer(t, runner, mirrorTestUnusedAPI(t), false)
					writeMirrorTestState(t, manager, navidrome.MirrorState{ScanPending: pending, DatesPending: true})
					terminal := awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{}))
					if terminal.ExitCode != exitcode.RuntimeFailure || terminal.Error == "" ||
						terminal.Result.Failure == nil || terminal.Result.Failure.Kind != tt.kind || terminal.Result.Failure.Detail == "" {
						t.Fatalf("gate status must reach the wire as %s: %+v", tt.kind, terminal)
					}
					if tt.kind == "server_scanning" && !strings.Contains(terminal.Result.Failure.Detail, "deferred") {
						t.Fatalf("scan refusal must explain the deferral: %+v", terminal.Result.Failure)
					}
					if strings.Contains(tt.reply, "lock_error") && (!strings.Contains(terminal.Result.Failure.Detail, "scan control is misconfigured") ||
						!strings.Contains(terminal.Result.Failure.Detail, "scan control locks are unreadable or invalid")) {
						t.Fatalf("invalid locks must reach run.finished as a clear configuration failure: %+v", terminal.Result.Failure)
					}
					mirror := terminal.Result.Mirror
					if mirror == nil || mirror.ScanPending != pending || !mirror.DatesPending || mirror.LastPushError == "" ||
						mirror.LastErrorAt == nil || mirror.LastPushAt != nil || mirror.LastScanAt != nil {
						t.Fatalf("a pre-transfer refusal must preserve debt without recording a push: %+v", mirror)
					}
					if len(runner.realPushes()) != 0 || strings.Join(runner.gateCommands(), "\n") != "udl-mirror scan-status" ||
						terminal.Result.Scanned || terminal.Result.Dates != nil {
						t.Fatalf("the refusal must happen before transfer/scan/dates: %+v calls=%v", terminal.Result, runner.gateCommands())
					}
				})
			}
		})
	}
}

func TestMirrorPushHandoffRefusalAndUnknownTransferFailuresReachRunFinished(t *testing.T) {
	for _, tt := range []struct {
		name, stderr, stdout, kind string
		exit                       int
		refused                    bool
	}{
		{name: "scan handoff exit 2", stderr: `{"ok":false,"busy":"scan","error":"scan active"}`, exit: 2, kind: "server_scanning", refused: true},
		{name: "scan handoff rsync exit 12", stderr: "rsync: connection closed\n" + `{"ok":false,"busy":"scan","error":"scan active"}` + "\nrsync error: protocol data stream (code 12)\n", exit: 12, kind: "server_scanning", refused: true},
		{name: "recovery handoff", stderr: `{"ok":false,"busy":"recovery","error":"scan marker is set"}`, exit: 12, kind: "server_scanning", refused: true},
		{name: "mirror handoff", stderr: `{"ok":false,"busy":"mirror","error":"mirror lock held"}`, exit: 12, kind: "busy", refused: true},
		{name: "invalid locks handoff", stderr: "rsync: remote command refused\n" + `{"ok":false,"lock_error":"scan control locks are unreadable or invalid","error":"mirror admission failed"}` + "\nrsync: connection closed\n", exit: 12, kind: "failed", refused: true},
		{name: "generic exit 12", stderr: "rsync: connection unexpectedly closed", exit: 12, kind: "failed"},
		{name: "unknown busy value", stderr: `{"ok":false,"busy":"other","error":"unrecognized"}`, exit: 12, kind: "failed"},
		{name: "missing refusal flag", stderr: `{"busy":"scan"}`, exit: 12, kind: "failed"},
		{name: "not a refusal", stderr: `{"ok":true,"busy":"scan"}`, exit: 12, kind: "failed"},
		{name: "refusal on wrong stream", stdout: `{"ok":false,"busy":"scan"}`, stderr: "rsync: connection closed", exit: 12, kind: "failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, pending := range []bool{false, true} {
				name := "fresh"
				if pending {
					name = "existing scan debt"
				}
				t.Run(name, func(t *testing.T) {
					transfers := 0
					runner := &mirrorTestRunner{reply: func(_ context.Context, name string, args []string) (navidrome.RemoteOutput, error) {
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
									return navidrome.RemoteOutput{Stderr: []byte(tt.stderr), Stdout: []byte(tt.stdout), ExitCode: tt.exit}, nil
								}
							}
							return navidrome.RemoteOutput{}, nil
						}
						t.Errorf("unexpected call %s %v", name, args)
						return navidrome.RemoteOutput{ExitCode: 1}, nil
					}}
					server, output, manager := newMirrorPushServer(t, runner, nil, false)
					writeMirrorTestState(t, manager, navidrome.MirrorState{ScanPending: pending})
					terminal := awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{}))
					if terminal.ExitCode != exitcode.RuntimeFailure || terminal.Error == "" ||
						terminal.Result.Failure == nil || terminal.Result.Failure.Kind != tt.kind {
						t.Fatalf("handoff/transfer failure must reach the wire as %s: %+v", tt.kind, terminal)
					}
					if strings.Contains(tt.stderr, "lock_error") && (!strings.Contains(terminal.Result.Failure.Detail, "scan control is misconfigured") ||
						!strings.Contains(terminal.Result.Failure.Detail, "scan control locks are unreadable or invalid")) {
						t.Fatalf("lock admission failure must retain a clear diagnostic: %+v", terminal.Result.Failure)
					}
					// Only the gate's explicit refusal proves the receiver never ran.
					wantPending := pending || !tt.refused
					mirror := terminal.Result.Mirror
					if mirror == nil || mirror.ScanPending != wantPending || mirror.LastPushError == "" ||
						mirror.LastPushAt != nil || mirror.LastScanAt != nil {
						t.Fatalf("refused=%v previous pending=%v must leave pending=%v: %+v", tt.refused, pending, wantPending, mirror)
					}
					if len(runner.realPushes()) != 1 || runner.scanCount() != 0 || terminal.Result.Scanned || terminal.Result.Dates != nil {
						t.Fatalf("this regression must reach the real transfer, never its scan: %+v calls=%v", terminal.Result, runner.gateCommands())
					}
					state, err := navidrome.LoadMirrorState(manager.Resolved)
					if err != nil || state.ScanPending != wantPending {
						t.Fatalf("durable scan debt = %+v, err = %v", state, err)
					}
					// The retry has no itemized changes. A generic exit 12 (or debt
					// owed before a known refusal) must nevertheless force a scan.
					second := awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{}))
					if second.ExitCode != exitcode.Success || second.Result.Failure != nil || second.Result.Scanned != wantPending ||
						second.Result.Mirror == nil || second.Result.Mirror.ScanPending || second.Result.Mirror.DatesPending || second.Result.Mirror.LastPushError != "" {
						t.Fatalf("the retry must finish exactly the scan debt it owes: %+v", second)
					}
					wantScans := 0
					if wantPending {
						wantScans = 1
					}
					if runner.scanCount() != wantScans || len(runner.realPushes()) != 2 {
						t.Fatalf("retry calls = %v, real pushes = %d", runner.gateCommands(), len(runner.realPushes()))
					}
				})
			}
		})
	}
}

func TestMirrorPushIncompleteGateScanIsAWarningWithDebt(t *testing.T) {
	for _, tt := range []struct {
		name, reply, detail string
		exit                int
	}{
		{name: "timeout", reply: `{"ok":false,"pending":true,"error":"scan still running"}`, exit: 1, detail: "scan still running"},
		{name: "pending despite ok", reply: `{"ok":true,"pending":true,"scanned":true}`, detail: "pending"},
		{name: "authentication", reply: `{"ok":false,"error":"scan authentication failed"}`, exit: 1, detail: "authentication failed"},
		{name: "invalid control locks", reply: `{"ok":false,"lock_error":"scan control locks are unreadable or invalid","error":"mirror admission failed"}`, exit: 2, detail: "scan control is misconfigured"},
		{name: "scan busy", reply: `{"ok":false,"busy":"scan","error":"another scan job"}`, exit: 2, detail: "another scan job"},
		{name: "mirror busy", reply: `{"ok":false,"busy":"mirror","error":"mirror mutation active"}`, exit: 2, detail: "mirror mutation active"},
		{name: "no completion confirmation", reply: `{"ok":true,"scanned":false}`, detail: "did not confirm"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			transfers, scans := 0, 0
			runner := &mirrorTestRunner{reply: func(_ context.Context, name string, args []string) (navidrome.RemoteOutput, error) {
				switch {
				case args[0] == "--version":
					return navidrome.RemoteOutput{Stdout: []byte(mirrorTestRsyncVersion)}, nil
				case mirrorTestGateCommand(name, args) == "udl-mirror scan-status":
					return navidrome.RemoteOutput{Stdout: []byte(mirrorTestIdle)}, nil
				case mirrorTestGateCommand(name, args) == mirrorTestScanCommand:
					scans++
					if scans == 1 {
						return navidrome.RemoteOutput{Stdout: []byte(tt.reply), ExitCode: tt.exit}, nil
					}
					return navidrome.RemoteOutput{Stdout: []byte(mirrorTestScanned)}, nil
				case mirrorTestGateCommand(name, args) == "udl-mirror apply-dates":
					return navidrome.RemoteOutput{Stdout: []byte(`{"ok":true,"entries":1,"matched":1}`)}, nil
				case name == navidrome.DefaultRemoteRsyncPath:
					if !containsArg(args, "--dry-run") {
						transfers++
						if transfers == 1 {
							return navidrome.RemoteOutput{Stdout: []byte(">f+++++++++ 01 Song.mp3\n")}, nil
						}
					}
					return navidrome.RemoteOutput{}, nil
				}
				t.Errorf("unexpected call %s %v", name, args)
				return navidrome.RemoteOutput{ExitCode: 1}, nil
			}}
			server, output, _ := newMirrorPushServer(t, runner, mirrorTestUnusedAPI(t), false)
			terminal := awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{}))
			if terminal.ExitCode != exitcode.Success || terminal.Error != "" || terminal.Result.Failure != nil || terminal.Result.Push.Changed != 1 ||
				terminal.Result.Scanned || terminal.Result.Dates != nil || !strings.Contains(strings.Join(terminal.Result.Warnings, "\n"), tt.detail) {
				t.Fatalf("a gate scan failure after a completed push is a warning, not a failed push: %+v", terminal)
			}
			mirror := terminal.Result.Mirror
			if mirror == nil || !mirror.ScanPending || !mirror.DatesPending || mirror.LastPushAt == nil || mirror.LastScanAt != nil || mirror.LastPushError != "" {
				t.Fatalf("an incomplete scan must retain both scan debt and opted-in date intent: %+v", mirror)
			}
			if commands := strings.Join(runner.gateCommands(), "\n"); commands != "udl-mirror scan-status\n"+mirrorTestScanCommand {
				t.Fatalf("dates must not run before a completed scan: %q", commands)
			}
			second := awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{}))
			if second.ExitCode != exitcode.Success || !second.Result.Scanned || second.Result.Dates == nil ||
				second.Result.Mirror == nil || second.Result.Mirror.ScanPending || second.Result.Mirror.DatesPending || runner.scanCount() != 2 {
				t.Fatalf("an unchanged retry must finish its scan and Date Added debt: %+v", second)
			}
		})
	}
}

func TestMirrorPushRefusedDateReconcileIsAWarningWithDatesPending(t *testing.T) {
	for _, tc := range []struct {
		name, reply, detail string
	}{
		{name: "scan", reply: `{"ok":false,"busy":"scan","error":"Date Added lock held"}`, detail: "Date Added lock held"},
		{name: "recovery", reply: `{"ok":false,"busy":"recovery","error":"Date Added lock held"}`, detail: "Date Added lock held"},
		{name: "mirror", reply: `{"ok":false,"busy":"mirror","error":"Date Added lock held"}`, detail: "Date Added lock held"},
		{name: "invalid locks", reply: `{"ok":false,"lock_error":"scan control locks are unreadable or invalid","error":"mirror admission failed"}`, detail: "scan control is misconfigured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transfers, dates := 0, 0
			runner := &mirrorTestRunner{reply: func(_ context.Context, name string, args []string) (navidrome.RemoteOutput, error) {
				switch {
				case args[0] == "--version":
					return navidrome.RemoteOutput{Stdout: []byte(mirrorTestRsyncVersion)}, nil
				case mirrorTestGateCommand(name, args) == "udl-mirror scan-status":
					return navidrome.RemoteOutput{Stdout: []byte(mirrorTestIdle)}, nil
				case mirrorTestGateCommand(name, args) == mirrorTestScanCommand:
					return navidrome.RemoteOutput{Stdout: []byte(mirrorTestScanned)}, nil
				case mirrorTestGateCommand(name, args) == "udl-mirror apply-dates":
					dates++
					if dates == 1 {
						return navidrome.RemoteOutput{Stdout: []byte(tc.reply), ExitCode: 2}, nil
					}
					return navidrome.RemoteOutput{Stdout: []byte(`{"ok":true,"entries":1,"matched":1,"candidates":1,"updated":1}`)}, nil
				case name == navidrome.DefaultRemoteRsyncPath:
					if !containsArg(args, "--dry-run") {
						transfers++
						if transfers == 1 {
							return navidrome.RemoteOutput{Stdout: []byte(">f+++++++++ 01 Song.mp3\n")}, nil
						}
					}
					return navidrome.RemoteOutput{}, nil
				}
				t.Errorf("unexpected call %s %v", name, args)
				return navidrome.RemoteOutput{ExitCode: 1}, nil
			}}
			server, output, _ := newMirrorPushServer(t, runner, nil, false)
			terminal := awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{}))
			if terminal.ExitCode != exitcode.Success || terminal.Error != "" || terminal.Result.Failure != nil || !terminal.Result.Scanned || terminal.Result.Dates != nil ||
				!strings.Contains(strings.Join(terminal.Result.Warnings, "\n"), tc.detail) {
				t.Fatalf("a busy reconcile after a completed push/scan must only warn: %+v", terminal)
			}
			mirror := terminal.Result.Mirror
			if mirror == nil || mirror.ScanPending || !mirror.DatesPending || mirror.LastPushAt == nil || mirror.LastScanAt == nil {
				t.Fatalf("only Date Added remains pending: %+v", mirror)
			}
			second := awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{}))
			if second.ExitCode != exitcode.Success || second.Result.Scanned || second.Result.Dates == nil || runner.scanCount() != 1 || dates != 2 ||
				second.Result.Mirror == nil || second.Result.Mirror.ScanPending || second.Result.Mirror.DatesPending {
				t.Fatalf("an unchanged retry must finish Date Added without another scan: %+v", second)
			}
		})
	}
}

func TestNavidromeStatusExposesOptionalRemoteScanFieldsAndProblems(t *testing.T) {
	for _, tt := range []struct {
		name, scan string
		problems   []string
	}{
		{name: "older gate omits scan"},
		{name: "minimal idle", scan: `{"scanning":false}`},
		{name: "scan markers and locks", scan: `{"scanning":true,"mirror_busy":true,"scan_job_busy":true,"recovery_pending":true,"migration_scan_pending":true,"enforcement":1,"libraries":[{"id":7,"last_scan_at":"2026-10-22 10:00:00","last_scan_started_at":"0001-01-01 00:00:00","full_scan_in_progress":true}]}`, problems: []string{"unfinished Navidrome scan", "migration scan"}},
		{name: "unknown API status", scan: `{"scanning":null,"api_error":"loopback API unavailable","enforcement":1}`, problems: []string{"cannot read Navidrome's scan status"}},
		{name: "invalid control locks", scan: `{"scanning":false,"lock_error":"scan control locks are unreadable or invalid","mirror_busy":true,"scan_job_busy":true,"enforcement":1}`, problems: []string{"scan control is misconfigured", "scan control locks are unreadable or invalid"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runner := &mirrorTestRunner{reply: func(_ context.Context, name string, args []string) (navidrome.RemoteOutput, error) {
				if mirrorTestGateCommand(name, args) != "udl-mirror status" {
					t.Errorf("status must remain read-only: %s %v", name, args)
					return navidrome.RemoteOutput{ExitCode: 1}, nil
				}
				reply := `{"ok":true,"database":{"present":true},"library":{"files":1}`
				if tt.scan != "" {
					reply += `,"scan":` + tt.scan
				}
				return navidrome.RemoteOutput{Stdout: []byte(reply + "}")}, nil
			}}
			server, _, _ := newMirrorPushServer(t, runner, nil, false)
			value, rpcErr := server.navidromeStatus()
			if rpcErr != nil {
				t.Fatalf("status must render with optional gate fields and no Mac API credentials: %+v", rpcErr)
			}
			payload, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				Mode   string `json:"mode"`
				Remote struct {
					SSHTarget string                     `json:"ssh_target"`
					Scan      map[string]json.RawMessage `json:"scan"`
				} `json:"remote"`
				Problems []string `json:"problems"`
			}
			if err := json.Unmarshal(payload, &wire); err != nil {
				t.Fatal(err)
			}
			if wire.Mode != "remote" || wire.Remote.SSHTarget != "udl-mirror" || wire.Problems == nil {
				t.Fatalf("remote status wire = %s", payload)
			}
			if tt.scan == "" {
				if wire.Remote.Scan != nil {
					t.Fatalf("an older gate must not invent scan state: %s", payload)
				}
			} else {
				var want map[string]json.RawMessage
				if err := json.Unmarshal([]byte(tt.scan), &want); err != nil {
					t.Fatal(err)
				}
				for key, expected := range want {
					// null scanning may be omitted by Go's optional pointer field;
					// it must never turn into false (confirmed idle) on the wire.
					if key == "scanning" && string(expected) == "null" {
						if actual := wire.Remote.Scan[key]; len(actual) > 0 && string(actual) != "null" {
							t.Fatalf("unknown scanning became %s: %s", actual, payload)
						}
						continue
					}
					if actual := wire.Remote.Scan[key]; string(actual) != string(expected) {
						t.Fatalf("remote.scan.%s = %s, want %s: %s", key, actual, expected, payload)
					}
				}
			}
			for _, problem := range tt.problems {
				if !strings.Contains(strings.Join(wire.Problems, "\n"), problem) {
					t.Fatalf("status must surface %q: %s", problem, payload)
				}
			}
			if calls := runner.gateCommands(); len(calls) != 1 || calls[0] != "udl-mirror status" {
				t.Fatalf("status must only read the gate: %v", calls)
			}
		})
	}
}
