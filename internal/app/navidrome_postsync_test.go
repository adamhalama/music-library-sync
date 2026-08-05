package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/navidrome"
	"github.com/jaa/update-downloads/internal/output"
)

type recordingEmitter struct {
	events []output.Event
}

func (e *recordingEmitter) Emit(event output.Event) error {
	e.events = append(e.events, event)
	return nil
}

func TestShouldRequestNavidromeScan(t *testing.T) {
	success := engine.SyncResult{Total: 2, Attempted: 2, Succeeded: 2}
	cases := []struct {
		name   string
		req    SyncRequest
		result engine.SyncResult
		err    error
		want   bool
		reason string
	}{
		{name: "successful run", result: success, want: true},
		{name: "dry run", req: SyncRequest{DryRun: true}, result: success, reason: "dry run"},
		{name: "plan only", req: SyncRequest{Plan: true}, result: success, reason: "planning"},
		{name: "failed run", result: success, err: errors.New("boom"), reason: "did not finish"},
		{name: "canceled run", result: engine.SyncResult{Succeeded: 1, Interrupted: true}, reason: "canceled"},
		{name: "no changes", result: engine.SyncResult{Total: 2, Attempted: 2, Skipped: 2}, reason: "no source produced changes"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, reason := ShouldRequestNavidromeScan(testCase.req, testCase.result, testCase.err)
			if got != testCase.want {
				t.Fatalf("got %v, want %v (reason %q)", got, testCase.want, reason)
			}
			if !testCase.want && !strings.Contains(reason, testCase.reason) {
				t.Fatalf("reason = %q, want it to mention %q", reason, testCase.reason)
			}
		})
	}
}

func TestRequestNavidromeScanCoalescesIntoOneRequest(t *testing.T) {
	calls := 0
	scanner := func(context.Context, navidrome.ManagerOptions) (bool, string) {
		calls++
		return true, ""
	}
	decision := RequestNavidromeScan(context.Background(), SyncRequest{},
		engine.SyncResult{Total: 5, Succeeded: 5}, nil, navidrome.ManagerOptions{}, scanner, nil)
	if !decision.Requested {
		t.Fatalf("decision = %+v", decision)
	}
	if calls != 1 {
		t.Fatalf("a five-source run must produce one scan request, got %d", calls)
	}
}

func TestRequestNavidromeScanSkipsWithoutCallingTheServer(t *testing.T) {
	calls := 0
	scanner := func(context.Context, navidrome.ManagerOptions) (bool, string) {
		calls++
		return true, ""
	}
	decision := RequestNavidromeScan(context.Background(), SyncRequest{DryRun: true},
		engine.SyncResult{Succeeded: 3}, nil, navidrome.ManagerOptions{}, scanner, nil)
	if !decision.Skipped || calls != 0 {
		t.Fatalf("a dry run must not reach the server: %+v calls=%d", decision, calls)
	}
}

func TestRequestNavidromeScanReportsFailureAsAWarning(t *testing.T) {
	emitter := &recordingEmitter{}
	scanner := func(context.Context, navidrome.ManagerOptions) (bool, string) {
		return false, "Navidrome scan request failed: connection refused"
	}
	decision := RequestNavidromeScan(context.Background(), SyncRequest{},
		engine.SyncResult{Succeeded: 1}, nil, navidrome.ManagerOptions{}, scanner, emitter)
	if decision.Requested {
		t.Fatalf("decision = %+v", decision)
	}
	if decision.Warning == "" {
		t.Fatalf("a failed scan must produce a visible warning")
	}
	if len(emitter.events) != 1 || emitter.events[0].Level != output.LevelWarn {
		t.Fatalf("events = %+v", emitter.events)
	}
}

func TestPostSyncScanRunsEvenWhenTheSyncContextIsCanceled(t *testing.T) {
	// A run can finish successfully while the caller's context is already done
	// (for example a TUI teardown). The scan request gets its own deadline so
	// it still reaches the server instead of failing instantly.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sawLiveContext := false
	scanner := func(scanCtx context.Context, _ navidrome.ManagerOptions) (bool, string) {
		sawLiveContext = scanCtx.Err() == nil
		return true, ""
	}
	RequestNavidromeScan(ctx, SyncRequest{}, engine.SyncResult{Succeeded: 1}, nil,
		navidrome.ManagerOptions{}, scanner, nil)
	if !sawLiveContext {
		t.Fatalf("the scan hook must not inherit an already-canceled context")
	}
}

func TestSyncRunResultIsUnchangedByScanFailure(t *testing.T) {
	useCase := SyncUseCase{
		Registry: map[string]engine.Adapter{},
		NavidromeScanner: func(context.Context, navidrome.ManagerOptions) (bool, string) {
			return false, "Navidrome scan request failed: server is down"
		},
	}
	// An empty registry with no sources yields a trivially successful run; the
	// point is that the failing hook does not turn it into an error.
	result, err := useCase.Run(context.Background(), emptyConfig(), SyncRequest{}, nil)
	if err != nil {
		t.Fatalf("a failing Navidrome scan must not fail the sync: %v", err)
	}
	if result.Failed != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func emptyConfig() config.Config {
	return config.Config{}
}
