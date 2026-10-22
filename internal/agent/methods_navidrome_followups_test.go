package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/jaa/update-downloads/internal/navidrome"
)

func TestMirrorPushPreflightLockErrorReachesRunFinishedWithoutNewDebt(t *testing.T) {
	for _, prior := range []navidrome.MirrorState{{}, {ScanPending: true}, {DatesPending: true}, {ScanPending: true, DatesPending: true}} {
		t.Run(string(mustJSON(t, prior)), func(t *testing.T) {
			runner := &mirrorTestRunner{reply: func(_ context.Context, name string, args []string) (navidrome.RemoteOutput, error) {
				switch {
				case name == navidrome.DefaultRemoteRsyncPath && args[0] == "--version":
					return navidrome.RemoteOutput{Stdout: []byte(mirrorTestRsyncVersion)}, nil
				case name == navidrome.DefaultRemoteRsyncPath && containsArg(args, "--dry-run"):
					return navidrome.RemoteOutput{ExitCode: 12, Stderr: []byte("rsync: remote command refused\n" +
						`{"ok":false,"lock_error":"scan control locks are unreadable or invalid","error":"mirror admission failed"}` + "\nrsync: connection closed\n")}, nil
				}
				t.Errorf("a refused dry-run must never check scan status, transfer, or mutate: %s %v", name, args)
				return navidrome.RemoteOutput{ExitCode: 1}, nil
			}}
			server, output, manager := newMirrorPushServer(t, runner, nil, false)
			writeMirrorTestState(t, manager, prior)
			terminal := awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{}))
			if terminal.ExitCode != exitcode.RuntimeFailure || terminal.Error == "" || terminal.Result.Failure == nil || terminal.Result.Failure.Kind != "failed" ||
				!strings.Contains(terminal.Result.Failure.Detail, "scan control is misconfigured") ||
				!strings.Contains(terminal.Result.Failure.Detail, "scan control locks are unreadable or invalid") {
				t.Fatalf("dry-run admission must expose a clear configuration failure: %+v", terminal)
			}
			mirror := terminal.Result.Mirror
			if mirror == nil || mirror.ScanPending != prior.ScanPending || mirror.DatesPending != prior.DatesPending ||
				mirror.LastPushAt != nil || mirror.LastScanAt != nil || mirror.LastPushError == "" || mirror.LastErrorAt == nil {
				t.Fatalf("dry-run refusal must preserve earlier debt and timestamps: %+v", mirror)
			}
			if len(runner.realPushes()) != 0 || len(runner.gateCommands()) != 0 || terminal.Result.Scanned || terminal.Result.Dates != nil {
				t.Fatalf("dry-run failure must stop before all subsequent work: %+v, %v", terminal.Result, runner.gateCommands())
			}
			state, err := navidrome.LoadMirrorState(manager.Resolved)
			if err != nil || state.ScanPending != prior.ScanPending || state.DatesPending != prior.DatesPending {
				t.Fatalf("durable debt changed after dry-run refusal: %+v, %v", state, err)
			}
		})
	}
}

func TestMirrorPushUnmatchedDatesReachRunFinishedAndStatus(t *testing.T) {
	transfers, dates := 0, 0
	runner := &mirrorTestRunner{reply: func(_ context.Context, name string, args []string) (navidrome.RemoteOutput, error) {
		switch {
		case args[0] == "--version":
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestRsyncVersion)}, nil
		case name == navidrome.DefaultRemoteRsyncPath:
			if !containsArg(args, "--dry-run") {
				transfers++
				if transfers == 1 {
					return navidrome.RemoteOutput{Stdout: []byte(">f+++++++++ 01 Song.mp3\n")}, nil
				}
			}
			return navidrome.RemoteOutput{}, nil
		case mirrorTestGateCommand(name, args) == "udl-mirror scan-status":
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestIdle)}, nil
		case mirrorTestGateCommand(name, args) == mirrorTestScanCommand:
			return navidrome.RemoteOutput{Stdout: []byte(mirrorTestScanned)}, nil
		case mirrorTestGateCommand(name, args) == "udl-mirror apply-dates":
			dates++
			if dates == 1 {
				return navidrome.RemoteOutput{Stdout: []byte(`{"ok":true,"unmatched":1,"unmatched_sample":["01 Song.mp3"]}`)}, nil
			}
			return navidrome.RemoteOutput{Stdout: []byte(`{"ok":true,"matched":1,"unmatched":0,"updated":1}`)}, nil
		case mirrorTestGateCommand(name, args) == "udl-mirror status":
			return navidrome.RemoteOutput{Stdout: []byte(`{"ok":true,"database":{"present":true},"library":{"partial":true}}`)}, nil
		}
		t.Errorf("unexpected call %s %v", name, args)
		return navidrome.RemoteOutput{ExitCode: 1}, nil
	}}
	server, output, _ := newMirrorPushServer(t, runner, nil, false)
	terminal := awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{}))
	problem := "Date Added could not be applied to 1 tracks the server has not indexed; they are retried after the next scan"
	var reconciled navidrome.ReconcileResult
	if terminal.Result.Dates == nil {
		t.Fatalf("partial reconciliation must return its date result: %+v", terminal)
	}
	if err := json.Unmarshal(*terminal.Result.Dates, &reconciled); err != nil {
		t.Fatal(err)
	}
	if terminal.ExitCode != exitcode.Success || terminal.Result.Failure != nil || reconciled.Unmatched != 1 ||
		terminal.Result.Mirror == nil || terminal.Result.Mirror.DatesPending || terminal.Result.Mirror.DatesUnmatched != 1 ||
		strings.Join(terminal.Result.Mirror.DatesUnmatchedSample, ",") != "01 Song.mp3" || strings.Join(terminal.Result.Warnings, "\n") != problem {
		t.Fatalf("partial reconciliation must reach run.finished as a warning with diagnostics: %+v", terminal)
	}
	value, rpcErr := server.navidromeStatus()
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	status := string(mustJSON(t, value))
	if !strings.Contains(status, problem) || !strings.Contains(status, "the server's file count is incomplete (status timed out)") || strings.Contains(status, "files but this library") {
		t.Fatalf("status must expose unmatched dates and partial count without a false mismatch: %s", status)
	}
	noop := awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{}))
	if noop.ExitCode != exitcode.Success || noop.Result.Scanned || noop.Result.Dates != nil || dates != 1 || noop.Result.Mirror == nil || noop.Result.Mirror.DatesUnmatched != 1 {
		t.Fatalf("unchanged push must not repeatedly reconcile unindexed files: %+v", noop)
	}
	retry := awaitMirrorPush(t, output, startMirrorPush(t, server, map[string]any{"force_scan": true}))
	if retry.ExitCode != exitcode.Success || !retry.Result.Scanned || retry.Result.Dates == nil || dates != 2 || retry.Result.Mirror == nil ||
		retry.Result.Mirror.DatesPending || retry.Result.Mirror.DatesUnmatched != 0 || len(retry.Result.Mirror.DatesUnmatchedSample) != 0 {
		t.Fatalf("a completed forced scan must retry and clear unmatched dates: %+v", retry)
	}
}
