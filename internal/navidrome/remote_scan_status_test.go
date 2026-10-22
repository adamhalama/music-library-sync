package navidrome

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRemoteStatusCarriesGateScanState(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	runner := &scriptedRunner{reply: func(name string, args []string) (RemoteOutput, error) {
		if name != cfg.Remote.SSHPath || args[len(args)-1] != "udl-mirror status" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		return RemoteOutput{Stdout: []byte(`{"ok":true,"database":{"present":true},"scan":{"ok":true,"scanning":false,"api_error":null,"mirror_busy":false,"scan_job_busy":true,"libraries":[{"id":1,"last_scan_at":null,"last_scan_started_at":"0001-01-01T00:00:00Z","full_scan_in_progress":false}],"recovery_pending":true,"migration_scan_pending":true,"enforcement":1}}`)}, nil
	}}
	manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run}
	remote, err := manager.RemoteStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if remote.Scan == nil || !remote.Scan.OK || remote.Scan.Scanning == nil || *remote.Scan.Scanning || !remote.Scan.ScanJobBusy || !remote.Scan.RecoveryPending || !remote.Scan.MigrationScanPending || remote.Scan.Enforcement != 1 {
		t.Fatalf("remote scan = %+v", remote.Scan)
	}
	if len(remote.Scan.Libraries) != 1 || remote.Scan.Libraries[0].ID != 1 || remote.Scan.Libraries[0].LastScanAt != "" || remote.Scan.Libraries[0].FullScanInProgress {
		t.Fatalf("library scan markers = %+v", remote.Scan.Libraries)
	}
	payload, err := json.Marshal(Status{Mode: ModeRemote, Remote: &remote})
	if err != nil {
		t.Fatal(err)
	}
	var status struct {
		Remote struct {
			Scan map[string]any `json:"scan"`
		} `json:"remote"`
	}
	if err := json.Unmarshal(payload, &status); err != nil {
		t.Fatal(err)
	}
	if scanning, ok := status.Remote.Scan["scanning"]; !ok || scanning != false {
		t.Fatalf("known idle must retain scanning:false: %s", payload)
	}
	for _, key := range []string{"api_error", "mirror_busy"} {
		if _, ok := status.Remote.Scan[key]; ok {
			t.Fatalf("empty field %q must be omitted: %s", key, payload)
		}
	}
}

func TestRemoteScanOptionalWireFields(t *testing.T) {
	for _, tc := range []struct {
		name   string
		value  any
		absent string
	}{
		{"local", Status{}, `"remote"`},
		{"legacy gate", RemoteStatus{}, `"scan"`},
		{"unknown scanning", RemoteScanStatus{}, `"scanning"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(payload), tc.absent) {
				t.Fatalf("unexpected field %s: %s", tc.absent, payload)
			}
		})
	}
}

func TestRemoteScanProblemsExplainRequiredRecovery(t *testing.T) {
	idle := false
	for _, tc := range []struct {
		name string
		scan *RemoteScanStatus
		want string
	}{
		{"legacy gate", nil, ""},
		{"idle", &RemoteScanStatus{Scanning: &idle}, ""},
		{"recovery", &RemoteScanStatus{Scanning: &idle, RecoveryPending: true}, "unfinished Navidrome scan"},
		{"migration", &RemoteScanStatus{Scanning: &idle, MigrationScanPending: true}, "migration scan"},
		{"unreachable", &RemoteScanStatus{APIError: "connection refused"}, "API is reachable again"},
		{"unknown", &RemoteScanStatus{}, "cannot read Navidrome's scan status"},
		{"API error with stale idle", &RemoteScanStatus{Scanning: &idle, APIError: "authentication failed"}, "cannot read Navidrome's scan status"},
		{"invalid control locks", &RemoteScanStatus{Scanning: &idle, LockError: "scan control locks are unreadable or invalid", MirrorBusy: true, ScanJobBusy: true}, "scan control is misconfigured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := &Manager{}
			remote := RemoteStatus{Scan: tc.scan}
			remote.Database.Present = true
			remote.Database.Libraries = append(remote.Database.Libraries, struct {
				ID   int    `json:"id"`
				Path string `json:"path"`
			}{ID: 1})
			problems := manager.remoteProblems(remote)
			if tc.want == "" {
				if len(problems) != 0 {
					t.Fatalf("unexpected problems: %v", problems)
				}
			} else if !strings.Contains(strings.Join(problems, "; "), tc.want) {
				t.Fatalf("problems = %v, want %q", problems, tc.want)
			}
		})
	}
}

func TestRemoteStatusPreservesPartialStatusWhenScanAPIIsUnavailable(t *testing.T) {
	cfg, resolved := remoteTestConfig(t)
	const reply = `{"ok":true,"library":{"files":3,"bytes":4096},"database":{"path":"/data/navidrome.db","present":true,"size_bytes":8192,"tracks":3},"disk":{"free_bytes":42949672960,"total_bytes":85899345920},"udl_backups":[{"path":"/backups/udl.db","created_at":"2026-10-22T11:00:00Z","size_bytes":8192}],"navidrome_backups":[{"path":"/backups/scheduled.db","created_at":"2026-10-22T12:00:00Z","size_bytes":8192}],"scan":{"ok":true,"scanning":null,"api_error":"loopback API probe timed out","mirror_busy":false,"scan_job_busy":false,"enforcement":1}}`
	calls := 0
	manager := &Manager{Config: cfg, Resolved: resolved}
	manager.Remote = func(ctx context.Context, _ []byte, name string, args ...string) (RemoteOutput, error) {
		if requireGateCommand(t, name, args) != "udl-mirror status" {
			t.Fatalf("unexpected gate command: %v", args)
		}
		calls++
		deadline, bounded := ctx.Deadline()
		if remaining := time.Until(deadline); !bounded || remaining <= 0 || remaining > 10*time.Second || remoteStatusTimeout != 10*time.Second {
			t.Fatalf("whole-SSH status budget must remain 10s: deadline=%v present=%t timeout=%v", deadline, bounded, remoteStatusTimeout)
		}
		return RemoteOutput{Stdout: []byte(reply)}, nil
	}
	var status Status
	manager.remoteStatusInto(context.Background(), &status)
	remote := status.Remote
	if calls != 1 || remote == nil || remote.SSHTarget != cfg.Remote.SSHTarget || remote.Scan == nil ||
		remote.Scan.Scanning != nil || remote.Scan.APIError != "loopback API probe timed out" {
		t.Fatalf("an API outage must not discard the reachable gate's status: %+v calls=%d", status, calls)
	}
	if !remote.Database.Present || remote.Database.Path != "/data/navidrome.db" || remote.Database.SizeBytes != 8192 || remote.Database.Tracks != 3 ||
		remote.Disk.FreeBytes != 42949672960 || remote.Disk.TotalBytes != 85899345920 || remote.Library.Files != 3 || remote.Library.Bytes != 4096 {
		t.Fatalf("database, disk and library status must survive an API outage: %+v", remote)
	}
	if len(remote.UDLBackups) != 1 || len(remote.NavidromeBackups) != 1 || status.BackupCount != 2 ||
		status.LatestBackup == nil || status.LatestBackup.Path != "/backups/scheduled.db" {
		t.Fatalf("backups must survive an API outage: %+v", status)
	}
	if !strings.Contains(strings.Join(status.Problems, "\n"), "cannot read Navidrome's scan status") {
		t.Fatalf("the API outage must still be explained: %v", status.Problems)
	}
}
