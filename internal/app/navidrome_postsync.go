package app

import (
	"context"
	"time"

	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/navidrome"
	"github.com/jaa/update-downloads/internal/output"
)

// postSyncScanTimeout bounds the optional scan request. The phone server is a
// convenience; a slow or wedged one must not hold up the end of a sync.
const postSyncScanTimeout = 15 * time.Second

// NavidromeScanner requests a library rescan. It exists so tests can drive the
// post-sync hook without a server.
type NavidromeScanner func(ctx context.Context, opts navidrome.ManagerOptions) (bool, string)

// PostSyncScanDecision explains what the hook did, and why.
type PostSyncScanDecision struct {
	Requested bool
	Skipped   bool
	Reason    string
	Warning   string
}

// ShouldRequestNavidromeScan decides whether a completed run earned a scan.
// Dry runs write nothing, interrupted runs are not a finished state, and a run
// that changed nothing gives the server nothing to index.
func ShouldRequestNavidromeScan(req SyncRequest, result engine.SyncResult, runErr error) (bool, string) {
	switch {
	case req.DryRun:
		return false, "dry run made no changes"
	case req.Plan:
		return false, "planning made no changes"
	case runErr != nil:
		return false, "the sync did not finish"
	case result.Interrupted:
		return false, "the sync was canceled"
	case result.Succeeded == 0:
		return false, "no source produced changes"
	default:
		return true, ""
	}
}

// RequestNavidromeScan runs the post-sync hook. It never returns an error: an
// unavailable, unauthenticated, or failing phone server must not change the
// download result. One request covers the whole run, so multi-source
// completions coalesce into a single scan.
func RequestNavidromeScan(ctx context.Context, req SyncRequest, result engine.SyncResult, runErr error, opts navidrome.ManagerOptions, scanner NavidromeScanner, emitter output.EventEmitter) PostSyncScanDecision {
	should, reason := ShouldRequestNavidromeScan(req, result, runErr)
	if !should {
		return PostSyncScanDecision{Skipped: true, Reason: reason}
	}
	if scanner == nil {
		scanner = navidrome.RequestScanBestEffort
	}
	scanCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), postSyncScanTimeout)
	defer cancel()

	requested, warning := scanner(scanCtx, opts)
	decision := PostSyncScanDecision{Requested: requested, Warning: warning}
	if warning != "" && emitter != nil {
		_ = emitter.Emit(output.Event{
			Timestamp: time.Now().UTC(),
			Level:     output.LevelWarn,
			Event:     output.EventSyncFinished,
			Message:   warning,
		})
	}
	return decision
}
