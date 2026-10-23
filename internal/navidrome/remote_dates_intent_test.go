package navidrome

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Regression for the three-run review probe: opted-in dates must outlive a
// failed scan, a no-dates retry that settles only the scan, and a no-op push.
func TestDateIntentSurvivesScanFailureAndNoDatesRetry(t *testing.T) {
	for _, tc := range []struct {
		name, scanReply string
		force, noScan   bool
	}{
		{name: "scan timeout", scanReply: `{"ok":false,"pending":true,"error":"scan still running"}`},
		{name: "scan error", scanReply: `{"ok":false,"error":"loopback API unavailable"}`},
		{name: "forced scan timeout", force: true, scanReply: `{"ok":false,"pending":true,"error":"scan still running"}`},
		{name: "scan explicitly skipped", noScan: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, resolved := remoteTestConfig(t)
			writeFile(t, filepath.Join(resolved.MusicDir, "a.mp3"))
			changed := RemoteOutput{Stdout: []byte(">f+++++++++ a.mp3\n")}
			if tc.force {
				changed = RemoteOutput{}
			}
			first := pushRunner(t, changed, "", "")
			original := first.reply
			first.reply = func(name string, args []string) (RemoteOutput, error) {
				if name == DefaultRemoteSSHPath {
					switch command := requireGateCommand(t, name, args); {
					case strings.HasPrefix(command, "udl-mirror scan "):
						state, err := LoadMirrorState(resolved)
						if err != nil || !state.ScanPending || !state.DatesPending {
							t.Fatalf("date intent must already be durable before the scan: %+v, %v", state, err)
						}
						return RemoteOutput{Stdout: []byte(tc.scanReply), ExitCode: 1}, nil
					case command == "udl-mirror apply-dates":
						t.Fatal("dates must wait until the owed scan completes")
					}
				}
				return original(name, args)
			}
			manager := &Manager{Config: cfg, Resolved: resolved, Remote: first.run,
				BirthTime: func(string) (time.Time, error) { return time.Unix(1_700_000_000, 0), nil }}
			result, err := manager.SyncMirror(context.Background(), MirrorOptions{Scan: !tc.noScan, ForceScan: tc.force, Dates: true})
			if err != nil || result.Scanned || result.Dates != nil || (len(result.Warnings) > 0) == tc.noScan {
				t.Fatalf("first push = %+v, %v", result, err)
			}
			state, err := LoadMirrorState(resolved)
			if err != nil || !state.ScanPending || !state.DatesPending {
				t.Fatalf("the first push must owe both scan and dates: %+v, %v", state, err)
			}

			// The next push transfers nothing and opts out of dates for this run.
			// Completing the owed scan must not forgive the original date intent.
			second := pushRunner(t, RemoteOutput{}, "", "")
			manager.Remote = second.run
			result, err = manager.SyncMirror(context.Background(), MirrorOptions{Scan: true, Dates: false})
			if err != nil || !result.Scanned || result.Dates != nil || len(result.Warnings) != 0 || gateScanCalls(second) != 1 {
				t.Fatalf("no-dates retry = %+v, %v", result, err)
			}
			state, err = LoadMirrorState(resolved)
			if err != nil || state.ScanPending || !state.DatesPending {
				t.Fatalf("no-dates retry must settle only the scan: %+v, %v", state, err)
			}

			// A later ordinary no-op push must reconcile, without another scan.
			third := pushRunner(t, RemoteOutput{}, "", `{"ok":true,"entries":1,"matched":1,"candidates":1,"updated":1}`)
			manager.Remote = third.run
			result, err = manager.SyncMirror(context.Background(), MirrorOptions{Scan: true, Dates: true})
			if err != nil || result.Scanned || result.Dates == nil || result.Dates.Updated != 1 || len(result.Warnings) != 0 || gateScanCalls(third) != 0 {
				t.Fatalf("ordinary no-op retry must reconcile the original date intent: %+v, %v", result, err)
			}
			state, err = LoadMirrorState(resolved)
			if err != nil || state.ScanPending || state.DatesPending {
				t.Fatalf("successful reconciliation must settle date debt: %+v, %v", state, err)
			}
		})
	}
}
