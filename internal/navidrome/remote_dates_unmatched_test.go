package navidrome

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPartialDateReconcileWaitsForTheNextSuccessfulScan(t *testing.T) {
	for _, applyAfterScan := range []bool{false, true} {
		t.Run(fmt.Sprintf("applyAfterScan=%t", applyAfterScan), func(t *testing.T) {
			cfg, resolved := remoteTestConfig(t)
			writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
			first := pushRunner(t, RemoteOutput{Stdout: []byte(">f+++++++++ a.mp3\n")}, "",
				`{"ok":true,"entries":5,"matched":1,"unmatched":4,"unmatched_sample":["b.mp3","c.mp3","d.mp3","e.mp3"],"candidates":1,"updated":1}`)
			manager := &Manager{Config: cfg, Resolved: resolved, Remote: first.run,
				BirthTime: func(string) (time.Time, error) { return time.Unix(1_700_000_000, 0), nil }}
			result, err := manager.SyncMirror(context.Background(), MirrorOptions{Scan: true, Dates: true})
			problem := "Date Added could not be applied to 4 tracks the server has not indexed; they are retried after the next scan"
			if err != nil || !result.Scanned || result.Dates == nil || result.Dates.Unmatched != 4 ||
				strings.Join(result.Dates.UnmatchedSample, ",") != "b.mp3,c.mp3,d.mp3" || strings.Join(result.Warnings, "\n") != problem {
				t.Fatalf("partial reconcile must report unmatched files without failing: %+v, %v", result, err)
			}
			state, err := LoadMirrorState(resolved)
			if err != nil || state.ScanPending || state.DatesPending || state.DatesUnmatched != 4 ||
				strings.Join(state.DatesUnmatchedSample, ",") != "b.mp3,c.mp3,d.mp3" {
				t.Fatalf("partial success must settle immediate debt and record a bounded sample: %+v, %v", state, err)
			}
			if problems := strings.Join(manager.mirrorProblems(), "\n"); problems != problem {
				t.Fatalf("status problem = %q, want %q", problems, problem)
			}

			// An ordinary unchanged push must not spin on unindexed files.
			noop := pushRunner(t, RemoteOutput{}, "", "")
			manager.Remote = noop.run
			result, err = manager.SyncMirror(context.Background(), MirrorOptions{Scan: true, Dates: true})
			if err != nil || result.Scanned || result.Dates != nil || len(result.Warnings) != 0 || gateScanCalls(noop) != 0 {
				t.Fatalf("unchanged push must wait for indexing, not reconcile again: %+v, %v", result, err)
			}
			state, err = LoadMirrorState(resolved)
			if err != nil || state.DatesPending || state.DatesUnmatched != 4 || len(state.DatesUnmatchedSample) != 3 {
				t.Fatalf("unchanged push must retain unmatched diagnostics: %+v, %v", state, err)
			}

			// A failed forced scan must not re-arm dates or erase diagnostics.
			failed := pushRunner(t, RemoteOutput{}, "", "")
			original := failed.reply
			failed.reply = func(name string, args []string) (RemoteOutput, error) {
				if name == DefaultRemoteSSHPath && strings.HasPrefix(requireGateCommand(t, name, args), "udl-mirror scan ") {
					return RemoteOutput{Stdout: []byte(`{"ok":false,"pending":true,"error":"scan still running"}`), ExitCode: 1}, nil
				}
				return original(name, args)
			}
			manager.Remote = failed.run
			result, err = manager.SyncMirror(context.Background(), MirrorOptions{Scan: true, ForceScan: true, Dates: false})
			state, loadErr := LoadMirrorState(resolved)
			if err != nil || loadErr != nil || result.Scanned || result.Dates != nil || len(result.Warnings) != 1 ||
				!state.ScanPending || state.DatesPending || state.DatesUnmatched != 4 {
				t.Fatalf("an incomplete scan must retain unmatched diagnostics without date intent: %+v, %+v, %v, %v", result, state, err, loadErr)
			}

			// The next completed scan re-arms dates, including when --no-dates
			// postpones their application to a later unchanged push.
			scanned := pushRunner(t, RemoteOutput{}, "", `{"ok":true,"entries":5,"matched":5,"unmatched":0,"candidates":4,"updated":4}`)
			manager.Remote = scanned.run
			result, err = manager.SyncMirror(context.Background(), MirrorOptions{Scan: true, Dates: applyAfterScan})
			state, loadErr = LoadMirrorState(resolved)
			if err != nil || loadErr != nil || !result.Scanned || (result.Dates != nil) != applyAfterScan || len(result.Warnings) != 0 ||
				state.ScanPending || state.DatesPending == applyAfterScan || gateScanCalls(scanned) != 1 {
				t.Fatalf("completed scan must re-arm unmatched date intent: %+v, %+v, %v, %v", result, state, err, loadErr)
			}
			if !applyAfterScan {
				if state.DatesUnmatched != 4 || len(state.DatesUnmatchedSample) != 3 {
					t.Fatalf("postponing dates must preserve the last reconcile's diagnostics: %+v", state)
				}
				retry := pushRunner(t, RemoteOutput{}, "", `{"ok":true,"entries":5,"matched":5,"unmatched":0,"candidates":4,"updated":4}`)
				manager.Remote = retry.run
				result, err = manager.SyncMirror(context.Background(), MirrorOptions{Scan: true, Dates: true})
				if err != nil || result.Scanned || result.Dates == nil || len(result.Warnings) != 0 || gateScanCalls(retry) != 0 {
					t.Fatalf("post-scan retry must apply dates without another scan: %+v, %v", result, err)
				}
			}
			state, err = LoadMirrorState(resolved)
			if err != nil || state.DatesPending || state.DatesUnmatched != 0 || len(state.DatesUnmatchedSample) != 0 {
				t.Fatalf("zero unmatched must clear the count and stale sample: %+v, %v", state, err)
			}
			wire, err := json.Marshal(state)
			if err != nil || strings.Contains(string(wire), "dates_unmatched") || len(manager.mirrorProblems()) != 0 {
				t.Fatalf("resolved unmatched diagnostics must disappear from state and status: %s, %v", wire, err)
			}
		})
	}
}

func TestExplicitRemoteReconcileRecordsAndClearsUnmatched(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
	if err := saveMirrorState(resolved, MirrorState{ScanPending: true, DatesPending: true}); err != nil {
		t.Fatal(err)
	}
	manager := &Manager{Config: cfg, Resolved: resolved,
		BirthTime: func(string) (time.Time, error) { return time.Unix(1_700_000_000, 0), nil }}
	for _, unmatched := range []int{1, 0} {
		runner := pushRunner(t, RemoteOutput{}, "", fmt.Sprintf(`{"ok":true,"unmatched":%d,"unmatched_sample":["a.mp3"]}`, unmatched))
		manager.Remote = runner.run
		if _, err := manager.ReconcileDateAdded(context.Background()); err != nil {
			t.Fatal(err)
		}
		state, err := LoadMirrorState(resolved)
		if err != nil || !state.ScanPending || state.DatesPending || state.DatesUnmatched != unmatched || len(state.DatesUnmatchedSample) != unmatched {
			t.Fatalf("explicit reconcile must update date diagnostics without clearing scan debt: %+v, %v", state, err)
		}
	}
}
