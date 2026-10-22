package navidrome

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
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
