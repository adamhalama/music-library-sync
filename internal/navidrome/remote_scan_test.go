package navidrome

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	idleRemoteScanStatus = `{"ok":true,"scanning":false,"api_error":null,"mirror_busy":false,"scan_job_busy":false,"libraries":[],"recovery_pending":false,"migration_scan_pending":false,"enforcement":1}`
	completedRemoteScan  = `{"ok":true,"scanned":true,"full":false,"count":1}`
)

// requireGateCommand checks the complete SSH vocabulary, not just a suffix;
// an unexpected command must never accidentally receive a successful reply.
func requireGateCommand(t *testing.T, name string, args []string) string {
	t.Helper()
	if name != DefaultRemoteSSHPath || len(args) != 4 || args[0] != "-o" || args[1] != "BatchMode=yes" || args[2] != "udl-mirror" {
		t.Fatalf("unexpected SSH invocation: %s %v", name, args)
	}
	return args[3]
}

func gateScanCalls(runner *scriptedRunner) int {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	count := 0
	for _, call := range runner.calls {
		if call[0] == DefaultRemoteSSHPath && strings.HasPrefix(call[len(call)-1], "udl-mirror scan ") {
			count++
		}
	}
	return count
}

func TestRemoteStatusDecodesScanEnforcement(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	runner := &scriptedRunner{reply: func(name string, args []string) (RemoteOutput, error) {
		if requireGateCommand(t, name, args) != "udl-mirror status" {
			t.Fatalf("unexpected gate command: %v", args)
		}
		return RemoteOutput{Stdout: []byte(`{"ok":true,"scan":{"ok":true,"scanning":false,"api_error":"loopback unavailable","recovery_pending":true,"migration_scan_pending":true,"mirror_busy":true,"scan_job_busy":true,"enforcement":1,"libraries":[{"id":7,"last_scan_at":"2026-10-22 12:00:00.123456789+00:00","last_scan_started_at":"2026-10-22 12:01:00+00:00","full_scan_in_progress":true}]}}`)}, nil
	}}
	manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run}
	status, err := manager.RemoteStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Pin the exported wire types as well as the JSON tags. Scan dates are
	// raw database strings, not necessarily RFC3339 timestamps.
	var scan *RemoteScanStatus = status.Scan
	if scan == nil || scan.Scanning == nil || *scan.Scanning || scan.APIError != "loopback unavailable" ||
		!scan.RecoveryPending || !scan.MigrationScanPending || !scan.MirrorBusy || !scan.ScanJobBusy || scan.Enforcement != 1 {
		t.Fatalf("scan = %+v", scan)
	}
	if len(scan.Libraries) != 1 {
		t.Fatalf("libraries = %+v", scan.Libraries)
	}
	var library RemoteScanLibrary = scan.Libraries[0]
	if library.ID != 7 || library.LastScanAt != "2026-10-22 12:00:00.123456789+00:00" ||
		library.LastScanStartedAt != "2026-10-22 12:01:00+00:00" || !library.FullScanInProgress {
		t.Fatalf("library = %+v", library)
	}
	if status.SSHTarget != cfg.Remote.SSHTarget {
		t.Fatalf("ssh_target = %q", status.SSHTarget)
	}
	for _, payload := range []string{`{}`, `{"scan":null}`, `{"scan":{"scanning":null,"api_error":null}}`, `{"scan":{}}`} {
		var decoded RemoteStatus
		if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
			t.Fatalf("decode %s: %v", payload, err)
		}
		if decoded.Scan != nil && decoded.Scan.Scanning != nil {
			t.Fatalf("unknown scan state became known idle: %s -> %+v", payload, decoded.Scan)
		}
	}
}

func TestSyncMirrorDefersItsTransferWhileTheServerScans(t *testing.T) {
	cases := []struct {
		name string
		out  RemoteOutput
		err  error
		want error
	}{
		{name: "scanning", out: RemoteOutput{Stdout: []byte(`{"ok":true,"scanning":true}`)}, want: ErrServerScanning},
		{name: "recovery", out: RemoteOutput{Stdout: []byte(`{"ok":true,"scanning":false,"recovery_pending":true}`)}, want: ErrServerScanning},
		{name: "migration", out: RemoteOutput{Stdout: []byte(`{"ok":true,"scanning":false,"migration_scan_pending":true}`)}, want: ErrServerScanning},
		{name: "scan job", out: RemoteOutput{Stdout: []byte(`{"ok":true,"scanning":false,"scan_job_busy":true}`)}, want: ErrServerScanning},
		{name: "mirror", out: RemoteOutput{Stdout: []byte(`{"ok":true,"scanning":false,"mirror_busy":true}`)}, want: ErrMirrorBusy},
		{name: "null scanning", out: RemoteOutput{Stdout: []byte(`{"ok":true,"scanning":null}`)}, want: ErrServerScanning},
		{name: "omitted scanning", out: RemoteOutput{Stdout: []byte(`{"ok":true}`)}, want: ErrServerScanning},
		{name: "API error despite idle", out: RemoteOutput{Stdout: []byte(`{"ok":true,"scanning":false,"api_error":"authentication failed"}`)}, want: ErrServerScanning},
		{name: "gate error", out: RemoteOutput{Stdout: []byte(`{"ok":false,"error":"database unreadable"}`), ExitCode: 1}, want: ErrServerScanning},
		{name: "malformed", out: RemoteOutput{Stdout: []byte(`{"ok":true,"scanning":`)}, want: ErrServerScanning},
		{name: "wrong type", out: RemoteOutput{Stdout: []byte(`{"ok":true,"scanning":"false"}`)}, want: ErrServerScanning},
		{name: "nonzero idle reply", out: RemoteOutput{Stdout: []byte(idleRemoteScanStatus), ExitCode: 1}, want: ErrServerScanning},
		{name: "unreachable", out: RemoteOutput{Stderr: []byte("ssh: connection refused"), ExitCode: 255}, want: ErrServerScanning},
		{name: "transport error", err: errors.New("could not start SSH"), want: ErrServerScanning},
	}
	for _, tc := range cases {
		for _, pending := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/pending=%t", tc.name, pending), func(t *testing.T) {
				cfg, resolved := remoteTestConfig(t)
				writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
				before := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
				if err := saveMirrorState(resolved, MirrorState{ScanPending: pending, DatesPending: true, LastPushAt: &before, LastScanAt: &before}); err != nil {
					t.Fatal(err)
				}
				queries := 0
				runner := &scriptedRunner{reply: func(name string, args []string) (RemoteOutput, error) {
					switch {
					case name == DefaultRemoteRsyncPath && len(args) == 1 && args[0] == "--version":
						return RemoteOutput{Stdout: []byte(rsync3VersionOutput)}, nil
					case name == DefaultRemoteRsyncPath && hasArg(args, "--dry-run"):
						return RemoteOutput{}, nil
					case name == DefaultRemoteRsyncPath:
						t.Fatal("a refused push must not start a real transfer")
					}
					if requireGateCommand(t, name, args) != "udl-mirror scan-status" {
						t.Fatalf("unexpected gate command: %v", args)
					}
					queries++
					return tc.out, tc.err
				}}
				// No client is configured: only the gate is authoritative. Scan
				// being false must not bypass pre-transfer protection either.
				manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run}
				result, err := manager.SyncMirror(context.Background(), MirrorOptions{Scan: false, IdleWait: time.Nanosecond})
				if !errors.Is(err, tc.want) || queries != 1 || result.Scanned || result.Push.HasChanges() {
					t.Fatalf("result=%+v err=%v queries=%d, want %v and one query", result, err, queries, tc.want)
				}
				state, err := LoadMirrorState(resolved)
				if err != nil || state.ScanPending != pending || !state.DatesPending || state.LastPushError == "" || state.LastErrorAt == nil ||
					state.LastPushAt == nil || !state.LastPushAt.Equal(before) || state.LastScanAt == nil || !state.LastScanAt.Equal(before) {
					t.Fatalf("a deferred push must preserve its prior work and timestamps: %+v err=%v", state, err)
				}
			})
		}
	}
}

func TestSyncMirrorQueriesScanStatusOnceWithABoundedContext(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
	base := pushRunner(t, RemoteOutput{}, "", "")
	queries := 0
	manager := &Manager{Config: cfg, Resolved: resolved}
	manager.Remote = func(ctx context.Context, stdin []byte, name string, args ...string) (RemoteOutput, error) {
		if name == DefaultRemoteSSHPath {
			if requireGateCommand(t, name, args) != "udl-mirror scan-status" {
				t.Fatalf("a no-op push must only query status: %v", args)
			}
			queries++
			deadline, ok := ctx.Deadline()
			if remaining := time.Until(deadline); !ok || remaining <= 0 || remaining > time.Minute {
				t.Fatalf("scan-status must have a short bounded context, deadline=%v present=%t", deadline, ok)
			}
		}
		return base.run(ctx, stdin, name, args...)
	}
	if _, err := manager.SyncMirror(context.Background(), MirrorOptions{Scan: true}); err != nil {
		t.Fatal(err)
	}
	if queries != 1 {
		t.Fatalf("scan-status calls = %d, want one bounded query, not polling", queries)
	}
}

func TestRsyncGateRefusalsRestoreThePreviousPendingScan(t *testing.T) {
	for _, step := range []string{"preflight", "transfer"} {
		for _, pending := range []bool{false, true} {
			for _, busy := range []string{"scan", "recovery", "mirror"} {
				// rsync can propagate or translate the gate's exit. Even exits
				// normally treated as warnings must not override a gate refusal.
				for _, code := range []int{2, 12, rsyncExitPartial, rsyncExitVanished, rsyncExitMaxDelete, 255} {
					t.Run(fmt.Sprintf("%s/pending=%t/%s/exit=%d", step, pending, busy, code), func(t *testing.T) {
						cfg, resolved := remoteTestConfig(t)
						writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
						if err := saveMirrorState(resolved, MirrorState{ScanPending: pending, DatesPending: true}); err != nil {
							t.Fatal(err)
						}
						refusal := RemoteOutput{ExitCode: code, Stderr: []byte("rsync: remote command refused\n" +
							fmt.Sprintf(`{"ok":false,"busy":%q,"error":"gate refused before mutation"}`, busy) + "\nrsync: connection unexpectedly closed\n")}
						runner := pushRunner(t, refusal, "", "")
						original := runner.reply
						transfers := 0
						runner.reply = func(name string, args []string) (RemoteOutput, error) {
							if name == DefaultRemoteRsyncPath && hasArg(args, "--dry-run") && step == "preflight" {
								return refusal, nil
							}
							if name == DefaultRemoteRsyncPath && !hasArg(args, "--dry-run") && args[0] != "--version" {
								transfers++
								state, err := LoadMirrorState(resolved)
								if err != nil || !state.ScanPending {
									t.Fatalf("owed scan must be recorded before attempting the hand-off: %+v, %v", state, err)
								}
							}
							return original(name, args)
						}
						manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run}
						result, err := manager.SyncMirror(context.Background(), MirrorOptions{Scan: true, Dates: true})
						want := ErrServerScanning
						if busy == "mirror" {
							want = ErrMirrorBusy
						}
						if !errors.Is(err, want) || result.Scanned || result.Push.HasChanges() || gateScanCalls(runner) != 0 {
							t.Fatalf("result=%+v err=%v, want %v", result, err, want)
						}
						if (step == "transfer" && transfers != 1) || (step == "preflight" && transfers != 0) {
							t.Fatalf("transfer attempts=%d for %s refusal", transfers, step)
						}
						state, err := LoadMirrorState(resolved)
						if err != nil || state.ScanPending != pending || !state.DatesPending || state.LastPushAt != nil || state.LastPushError == "" {
							t.Fatalf("a gate refusal changed the previously owed work: %+v, %v", state, err)
						}
					})
				}
			}
		}
	}
}

func TestUnstructuredTransferErrorsStillOweAScan(t *testing.T) {
	for _, stderr := range []string{
		"connection unexpectedly closed",
		`{"ok":false,"busy":"unknown","error":"not a recognized refusal"}`,
		`{"busy":"scan"}`,
		`{"ok":true,"busy":"scan"}`,
		`prefix {"ok":false,"busy":"scan"} is not a JSON line`,
		`{"ok":false,"busy":`,
	} {
		t.Run(stderr, func(t *testing.T) {
			cfg, resolved := remoteTestConfig(t)
			writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
			runner := pushRunner(t, RemoteOutput{ExitCode: 12, Stderr: []byte(stderr)}, "", "")
			manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run}
			_, err := manager.SyncMirror(context.Background(), MirrorOptions{Scan: true})
			if err == nil || errors.Is(err, ErrServerScanning) || errors.Is(err, ErrMirrorBusy) {
				t.Fatalf("an arbitrary transfer error must not claim a no-mutation refusal: %v", err)
			}
			state, loadErr := LoadMirrorState(resolved)
			if loadErr != nil || !state.ScanPending || state.LastPushError == "" {
				t.Fatalf("a possibly partial transfer must remain pending: %+v, %v", state, loadErr)
			}
		})
	}
}

func TestRemoteRequestScanUsesTheGateIncludingFull(t *testing.T) {
	for _, full := range []bool{false, true} {
		for _, withAPI := range []bool{false, true} {
			t.Run(fmt.Sprintf("full=%t/API=%t", full, withAPI), func(t *testing.T) {
				cfg, resolved := remoteTestConfig(t)
				want := "udl-mirror scan --timeout-seconds 900"
				if full {
					want = "udl-mirror scan --full --timeout-seconds 900"
				}
				runner := &scriptedRunner{reply: func(name string, args []string) (RemoteOutput, error) {
					if command := requireGateCommand(t, name, args); command != want {
						t.Fatalf("command = %q, want %q", command, want)
					}
					return RemoteOutput{Stdout: []byte(fmt.Sprintf(`{"ok":true,"scanned":true,"full":%t}`, full))}, nil
				}}
				manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run, APIErr: errors.New("API credentials intentionally unavailable")}
				if withAPI {
					server, _ := mirrorServer(t)
					manager.API = NewClient(server.URL, Credentials{Username: "u", Password: "p"})
				}
				if err := manager.RequestScan(context.Background(), full); err != nil {
					t.Fatal(err)
				}
				if len(runner.calls) != 1 {
					t.Fatalf("RequestScan must use exactly one gate-owned scan: %v", runner.calls)
				}
			})
		}
	}
}

func TestRemoteScansRequirePositiveCompletionAndZeroExit(t *testing.T) {
	cases := []struct {
		name string
		out  RemoteOutput
		err  error
	}{
		{name: "pending timeout", out: RemoteOutput{Stdout: []byte(`{"ok":false,"pending":true,"error":"scan still running"}`), ExitCode: 1}},
		{name: "pending despite success", out: RemoteOutput{Stdout: []byte(`{"ok":true,"scanned":true,"pending":true}`)}},
		{name: "scan busy", out: RemoteOutput{Stdout: []byte(`{"ok":false,"busy":"scan","error":"scan job busy"}`), ExitCode: 2}},
		{name: "recovery busy", out: RemoteOutput{Stdout: []byte(`{"ok":false,"busy":"recovery","error":"recovery scan busy"}`), ExitCode: 2}},
		{name: "mirror busy", out: RemoteOutput{Stdout: []byte(`{"ok":false,"busy":"mirror","error":"mirror busy"}`), ExitCode: 2}},
		{name: "busy despite success", out: RemoteOutput{Stdout: []byte(`{"ok":true,"scanned":true,"busy":"mirror"}`)}},
		{name: "auth error", out: RemoteOutput{Stdout: []byte(`{"ok":false,"error":"authentication failed"}`), ExitCode: 1}},
		{name: "not scanned", out: RemoteOutput{Stdout: []byte(`{"ok":true,"scanned":false}`)}},
		{name: "missing scanned", out: RemoteOutput{Stdout: []byte(`{"ok":true}`)}},
		{name: "null scanned", out: RemoteOutput{Stdout: []byte(`{"ok":true,"scanned":null}`)}},
		{name: "missing ok", out: RemoteOutput{Stdout: []byte(`{"scanned":true}`)}},
		{name: "failed despite scanned", out: RemoteOutput{Stdout: []byte(`{"ok":false,"scanned":true,"error":"failed"}`)}},
		{name: "nonzero success", out: RemoteOutput{Stdout: []byte(completedRemoteScan), ExitCode: 1}},
		{name: "SSH nonzero success", out: RemoteOutput{Stdout: []byte(completedRemoteScan), ExitCode: 255}},
		{name: "wrong type", out: RemoteOutput{Stdout: []byte(`{"ok":true,"scanned":"true"}`)}},
		{name: "malformed", out: RemoteOutput{Stdout: []byte(`{"ok":true,"scanned":`)}},
		{name: "no reply", out: RemoteOutput{Stderr: []byte("ssh connection failed"), ExitCode: 255}},
		{name: "transport deadline", err: context.DeadlineExceeded},
		{name: "canceled", err: context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, resolved := remoteTestConfig(t)
			writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
			before := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
			if err := saveMirrorState(resolved, MirrorState{LastScanAt: &before}); err != nil {
				t.Fatal(err)
			}
			runner := pushRunner(t, RemoteOutput{Stdout: []byte(">f+++++++++ a.mp3\n")}, "", "")
			original := runner.reply
			runner.reply = func(name string, args []string) (RemoteOutput, error) {
				if name == DefaultRemoteSSHPath {
					switch requireGateCommand(t, name, args) {
					case "udl-mirror scan --timeout-seconds 900":
						return tc.out, tc.err
					case "udl-mirror apply-dates":
						t.Fatal("dates must not be applied before confirmed scan completion")
					}
				}
				return original(name, args)
			}
			manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run}
			result, err := manager.SyncMirror(context.Background(), MirrorOptions{Scan: true, Dates: true})
			if err != nil || result.Scanned || result.Dates != nil || len(result.Warnings) == 0 || gateScanCalls(runner) != 1 {
				t.Fatalf("completed push with an unconfirmed scan must warn: %+v, %v", result, err)
			}
			state, err := LoadMirrorState(resolved)
			if err != nil || !state.ScanPending || state.LastScanAt == nil || !state.LastScanAt.Equal(before) || state.LastPushAt == nil || state.LastPushError != "" {
				t.Fatalf("unconfirmed scan must not clear pending or advance LastScanAt: %+v, %v", state, err)
			}
			if err := manager.RequestScan(context.Background(), false); err == nil {
				t.Fatal("RequestScan must reject the same unconfirmed gate response")
			}
		})
	}
}

func TestRemoteScanBudgetRoundsDefaultsAndClamps(t *testing.T) {
	cases := []struct {
		name         string
		idle, finish time.Duration
		seconds      int
	}{
		{name: "defaults", seconds: 900},
		{name: "negative defaults", idle: -time.Second, finish: -time.Second, seconds: 900},
		{name: "idle defaults independently", finish: 2 * time.Second, seconds: 302},
		{name: "finish defaults independently", idle: 2 * time.Second, finish: -time.Second, seconds: 602},
		{name: "round sum not each wait", idle: 100 * time.Millisecond, finish: 100 * time.Millisecond, seconds: 1},
		{name: "ceil fractional sum", idle: 501 * time.Millisecond, finish: 500 * time.Millisecond, seconds: 2},
		{name: "exact seconds", idle: time.Second, finish: 2 * time.Second, seconds: 3},
		{name: "minimum", idle: time.Nanosecond, finish: time.Nanosecond, seconds: 1},
		{name: "maximum", idle: time.Hour, finish: time.Minute, seconds: 3600},
		{name: "large durations", idle: time.Duration(1<<63 - 1), finish: time.Duration(1<<63 - 1), seconds: 3600},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, resolved := remoteTestConfig(t)
			writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
			runner := pushRunner(t, RemoteOutput{}, "", "")
			manager := &Manager{Config: cfg, Resolved: resolved}
			scans := 0
			manager.Remote = func(ctx context.Context, stdin []byte, name string, args ...string) (RemoteOutput, error) {
				if name == DefaultRemoteSSHPath {
					command := requireGateCommand(t, name, args)
					if strings.HasPrefix(command, "udl-mirror scan ") {
						scans++
						if want := fmt.Sprintf("udl-mirror scan --timeout-seconds %d", tc.seconds); command != want {
							t.Fatalf("command = %q, want %q", command, want)
						}
						deadline, ok := ctx.Deadline()
						remaining := time.Until(deadline)
						budget := time.Duration(tc.seconds) * time.Second
						if !ok || remaining <= budget || remaining > budget+30*time.Second {
							t.Fatalf("scan context must add a small transport grace to %s: remaining=%s present=%t", budget, remaining, ok)
						}
						return RemoteOutput{Stdout: []byte(completedRemoteScan)}, nil
					}
				}
				return runner.run(ctx, stdin, name, args...)
			}
			// ForceScan proves a playlist-only/no-op transfer still uses the
			// same gate scan and budget as an ordinary changed push.
			result, err := manager.SyncMirror(context.Background(), MirrorOptions{Scan: true, ForceScan: true, IdleWait: tc.idle, FinishWait: tc.finish})
			if err != nil || !result.Scanned || scans != 1 || len(result.Warnings) != 0 {
				t.Fatalf("result=%+v err=%v scans=%d", result, err, scans)
			}
		})
	}
}

func TestRemoteScanContextRespectsParentDeadlineAndCancellation(t *testing.T) {
	for _, cancelAtScan := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%t", cancelAtScan), func(t *testing.T) {
			cfg, resolved := remoteTestConfig(t)
			writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
			parent, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			parentDeadline, _ := parent.Deadline()
			runner := pushRunner(t, RemoteOutput{Stdout: []byte(">f+++++++++ a.mp3\n")}, "", "")
			scans := 0
			manager := &Manager{Config: cfg, Resolved: resolved}
			manager.Remote = func(ctx context.Context, stdin []byte, name string, args ...string) (RemoteOutput, error) {
				if name == DefaultRemoteSSHPath {
					if deadline, ok := ctx.Deadline(); !ok || deadline.After(parentDeadline) {
						t.Fatalf("remote context escaped its parent deadline: %v present=%t parent=%v", deadline, ok, parentDeadline)
					}
					if requireGateCommand(t, name, args) == "udl-mirror scan --timeout-seconds 900" {
						scans++
						if cancelAtScan {
							cancel()
							select {
							case <-ctx.Done():
								return RemoteOutput{}, ctx.Err()
							default:
								t.Fatal("scan context must inherit parent cancellation")
							}
						}
					}
				}
				return runner.run(ctx, stdin, name, args...)
			}
			result, err := manager.SyncMirror(parent, MirrorOptions{Scan: true})
			if err != nil || scans != 1 || result.Scanned == cancelAtScan || (len(result.Warnings) > 0) != cancelAtScan {
				t.Fatalf("result=%+v err=%v scans=%d", result, err, scans)
			}
			state, err := LoadMirrorState(resolved)
			if err != nil || state.ScanPending != cancelAtScan {
				t.Fatalf("state=%+v err=%v", state, err)
			}
		})
	}
}

func TestRemoteApplyDatesMapsBusyRefusalsAndKeepsDatesPending(t *testing.T) {
	for _, busy := range []string{"scan", "recovery", "mirror"} {
		t.Run(busy, func(t *testing.T) {
			cfg, resolved := remoteTestConfig(t)
			writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
			refusal := fmt.Sprintf(`{"ok":false,"busy":%q,"error":"gate refused dates"}`, busy)
			runner := pushRunner(t, RemoteOutput{}, "", refusal)
			original := runner.reply
			runner.reply = func(name string, args []string) (RemoteOutput, error) {
				out, err := original(name, args)
				if name == DefaultRemoteSSHPath && requireGateCommand(t, name, args) == "udl-mirror apply-dates" {
					out.ExitCode = 2
				}
				return out, err
			}
			manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run,
				BirthTime: func(string) (time.Time, error) { return time.Unix(1_700_000_000, 0), nil }}
			want := ErrServerScanning
			if busy == "mirror" {
				want = ErrMirrorBusy
			}
			if _, err := manager.ReconcileDateAdded(context.Background()); !errors.Is(err, want) {
				t.Fatalf("ReconcileDateAdded error=%v, want %v", err, want)
			}
			if err := saveMirrorState(resolved, MirrorState{DatesPending: true}); err != nil {
				t.Fatal(err)
			}
			result, err := manager.SyncMirror(context.Background(), MirrorOptions{Scan: true, Dates: true})
			if err != nil || result.Scanned || result.Dates != nil || len(result.Warnings) == 0 || gateScanCalls(runner) != 0 {
				t.Fatalf("dates refusal after a push must warn without inventing another scan: %+v, %v", result, err)
			}
			state, err := LoadMirrorState(resolved)
			if err != nil || !state.DatesPending || state.ScanPending {
				t.Fatalf("dates refusal must preserve dates_pending: %+v, %v", state, err)
			}
		})
	}
}

func TestRemotePlaylistRefreshForcesAGateScanWithoutAPIScans(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	cfg.Server.Username = "u"
	cfg.Playlists.HardBounceGenres = []string{"Hard Bounce"}
	writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
	reads := 0
	scanned := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{"status": "ok", "version": "1.16.1"}
		switch strings.TrimPrefix(r.URL.Path, "/rest/") {
		case "getPlaylists.view":
			if !scanned {
				t.Error("playlist reads must follow the gate-owned scan")
			}
			reads++
			body["playlists"] = map[string]any{"playlist": []Playlist{}}
		default:
			t.Errorf("unexpected API call during remote playlist refresh: %s", r.URL.Path)
			body["status"] = "failed"
			body["error"] = map[string]any{"code": 50, "message": "unexpected API call"}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"subsonic-response": body})
	}))
	t.Cleanup(server.Close)
	runner := pushRunner(t, RemoteOutput{}, "", `{"ok":true,"entries":1,"matched":1}`)
	original := runner.reply
	runner.reply = func(name string, args []string) (RemoteOutput, error) {
		if name == DefaultRemoteRsyncPath && hasArg(args, "--dry-run") {
			for _, playlist := range []string{SmartPlaylistAllName, SmartPlaylistHardBounceName, SmartPlaylistFavoritesName} {
				payload, err := os.ReadFile(SmartPlaylistPath(resolved, playlist))
				if err != nil || !IsUDLOwned(string(payload)) {
					t.Fatalf("playlist %q must be written before the push: %v", playlist, err)
				}
			}
		}
		if name == DefaultRemoteSSHPath && requireGateCommand(t, name, args) == "udl-mirror scan --timeout-seconds 900" {
			scanned = true
		}
		return original(name, args)
	}
	manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run,
		API:       NewClient(server.URL, Credentials{Username: "u", Password: "p"}),
		BirthTime: func(string) (time.Time, error) { return time.Unix(1_700_000_000, 0), nil }}
	result, err := manager.RefreshPlaylists(context.Background())
	if err != nil || !result.Scanned || len(result.Generated) != 3 || gateScanCalls(runner) != 1 || reads != 1 {
		t.Fatalf("refresh=%+v err=%v gate scans=%d API reads=%d", result, err, gateScanCalls(runner), reads)
	}
	state, err := LoadMirrorState(resolved)
	if err != nil || state.LastScanAt == nil || state.ScanPending || state.DatesPending {
		t.Fatalf("forced refresh scan and dates must complete despite a no-op rsync: %+v, %v", state, err)
	}
}
