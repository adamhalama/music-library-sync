package navidrome

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// RemoteScanStatus is the gate's view of scans and the server-side locks.
// Scanning is nil when the loopback API is unavailable; it must not be
// mistaken for an idle server. Optional fields keep older status replies and
// clients compatible without weakening the pre-transfer check.
type RemoteScanStatus struct {
	OK                   bool                `json:"ok,omitempty"`
	Scanning             *bool               `json:"scanning,omitempty"`
	APIError             string              `json:"api_error,omitempty"`
	MirrorBusy           bool                `json:"mirror_busy,omitempty"`
	ScanJobBusy          bool                `json:"scan_job_busy,omitempty"`
	Libraries            []RemoteScanLibrary `json:"libraries,omitempty"`
	RecoveryPending      bool                `json:"recovery_pending,omitempty"`
	MigrationScanPending bool                `json:"migration_scan_pending,omitempty"`
	Enforcement          int                 `json:"enforcement,omitempty"`
}

// RemoteScanLibrary preserves the database scan markers as the gate reports
// them (including its zero-date sentinel), rather than parsing them as Go dates.
type RemoteScanLibrary struct {
	ID                 int    `json:"id,omitempty"`
	LastScanAt         string `json:"last_scan_at,omitempty"`
	LastScanStartedAt  string `json:"last_scan_started_at,omitempty"`
	FullScanInProgress bool   `json:"full_scan_in_progress,omitempty"`
}

// gateCommandError retains the gate's structured refusal instead of making
// callers interpret its human-readable error. Pending means a scan may still
// be running; neither it nor an unknown/auth error proves a scan completed.
type gateCommandError struct {
	Verb    string
	Message string
	Busy    string
	Pending bool
}

func (e *gateCommandError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("udl-mirror %s: %s", e.Verb, e.Message)
	}
	if e.Pending {
		return fmt.Sprintf("udl-mirror %s: the scan is still pending", e.Verb)
	}
	if e.Busy != "" {
		return fmt.Sprintf("udl-mirror %s: command refused (busy: %s)", e.Verb, e.Busy)
	}
	return fmt.Sprintf("udl-mirror %s: command refused", e.Verb)
}

func (e *gateCommandError) Unwrap() error { return gateBusyCause(e.Busy) }

func gateBusyCause(busy string) error {
	switch busy {
	case "scan", "recovery":
		return ErrServerScanning
	case "mirror":
		return ErrMirrorBusy
	default:
		return nil
	}
}

// mirrorTransferRefusal is stronger than a generic rsync failure: the gate
// declined its hand-off before exec'ing the receiver, so nothing transferred.
// Only this explicit refusal can undo the durable pre-transfer pending write.
type mirrorTransferRefusal struct{ cause error }

func (e *mirrorTransferRefusal) Error() string { return e.cause.Error() }
func (e *mirrorTransferRefusal) Unwrap() error { return e.cause }

func rsyncGateRefusal(out RemoteOutput) error {
	if out.ExitCode == 0 {
		return nil
	}
	// ssh forwards the gate's one-line JSON verbatim to rsync's stderr,
	// alongside rsync's own protocol/connection-closed diagnostics. The local
	// exit code is rsync's (often 12), not necessarily the gate's exit 2.
	for _, line := range bytes.Split(out.Stderr, []byte{'\n'}) {
		var reply struct {
			OK    *bool  `json:"ok"`
			Busy  string `json:"busy"`
			Error string `json:"error"`
		}
		if json.Unmarshal(bytes.TrimSpace(line), &reply) != nil || reply.OK == nil || *reply.OK || gateBusyCause(reply.Busy) == nil {
			continue
		}
		return &mirrorTransferRefusal{cause: &gateCommandError{Verb: "rsync", Message: reply.Error, Busy: reply.Busy}}
	}
	return nil
}

// confirmScanIdle fails closed on the gate's current scan/marker/lock state.
// This is only a preflight: the gate repeats the check under its mutation lock
// when rsync takes over, closing the check-to-transfer race.
func (m *Manager) confirmScanIdle(ctx context.Context, idleWait time.Duration) error {
	// This is a single query, not a wait for an active scan. Keep an
	// unreachable SSH server from consuming the full scan-idle budget.
	if idleWait <= 0 || idleWait > remoteStatusTimeout {
		idleWait = remoteStatusTimeout
	}
	checkCtx, cancel := context.WithTimeout(ctx, idleWait)
	defer cancel()
	var state RemoteScanStatus
	if err := m.callGate(checkCtx, nil, &state, "scan-status"); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, ErrMirrorBusy) {
			return err
		}
		return fmt.Errorf("%w: could not read the server's scan state: %v", ErrServerScanning, err)
	}
	switch {
	case state.MirrorBusy:
		return ErrMirrorBusy
	case state.Scanning == nil || state.APIError != "":
		return fmt.Errorf("%w: the server could not confirm that Navidrome is idle", ErrServerScanning)
	case *state.Scanning || state.RecoveryPending || state.MigrationScanPending || state.ScanJobBusy:
		return ErrServerScanning
	default:
		return nil
	}
}

// remoteScanTimeoutSeconds gives the gate one budget for its idle and finish
// waits. Round up sub-second tests/callers and honor the gate's 3600s ceiling.
func remoteScanTimeoutSeconds(idleWait, finishWait time.Duration) int {
	if idleWait <= 0 {
		idleWait = scanIdleTimeout
	}
	if finishWait <= 0 {
		finishWait = scanFinishTimeout
	}
	const maxWait = time.Hour
	if idleWait >= maxWait || finishWait >= maxWait || idleWait >= maxWait-finishWait {
		return 3600
	}
	return max(1, int((idleWait+finishWait+time.Second-1)/time.Second))
}

// scanRemote is the only remote scan trigger. Scans use the forced-command
// gate's loopback API and lock, never the public API used for playlist reads.
func (m *Manager) scanRemote(ctx context.Context, full bool, idleWait, finishWait time.Duration) error {
	seconds := remoteScanTimeoutSeconds(idleWait, finishWait)
	// Leave time for SSH setup and the gate's timeout reply. The caller's
	// deadline/cancellation still wins, and any interruption stays pending.
	scanCtx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second+15*time.Second)
	defer cancel()
	flags := []string{}
	if full {
		flags = append(flags, "--full")
	}
	flags = append(flags, "--timeout-seconds", fmt.Sprint(seconds))
	var reply struct {
		Scanned bool `json:"scanned"`
	}
	if err := m.callGate(scanCtx, nil, &reply, "scan", flags...); err != nil {
		return err
	}
	if !reply.Scanned {
		return errors.New("udl-mirror scan did not confirm a completed scan")
	}
	return nil
}

func remoteScanProblems(state *RemoteScanStatus) []string {
	if state == nil {
		return nil
	}
	problems := []string{}
	if state.RecoveryPending {
		problems = append(problems, "the server has an unfinished Navidrome scan; pushes are paused to protect track IDs. Finish a recovery scan on the server before pushing again")
	}
	if state.MigrationScanPending {
		problems = append(problems, "the server needs its migration scan before this Mac can push; finish that scan on the server first")
	}
	if state.Scanning == nil || state.APIError != "" {
		problems = append(problems, "the mirror server cannot read Navidrome's scan status; pushes are paused until its Navidrome API is reachable again")
	}
	return problems
}
