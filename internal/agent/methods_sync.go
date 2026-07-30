package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/jaa/update-downloads/internal/adapters/deemix"
	"github.com/jaa/update-downloads/internal/adapters/scdl"
	"github.com/jaa/update-downloads/internal/adapters/scdlfreedl"
	"github.com/jaa/update-downloads/internal/adapters/spotdl"
	"github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/jaa/update-downloads/internal/output"
	"github.com/jaa/update-downloads/internal/runstate"
)

type syncStartParams struct {
	SourceIDs             []string                        `json:"source_ids"`
	DryRun                bool                            `json:"dry_run"`
	TimeoutSeconds        int                             `json:"timeout_seconds"`
	Plan                  bool                            `json:"plan"`
	PlanLimit             int                             `json:"plan_limit"`
	PlanWindow            engine.PlanWindow               `json:"plan_window"`
	PlanWindowBySource    map[string]engine.PlanWindow    `json:"plan_window_by_source"`
	DownloadOrderBySource map[string]engine.DownloadOrder `json:"download_order_by_source"`
	AskOnExisting         bool                            `json:"ask_on_existing"`
	AskOnExistingSet      bool                            `json:"ask_on_existing_set"`
	ScanGaps              bool                            `json:"scan_gaps"`
	NoPreflight           bool                            `json:"no_preflight"`
	TrackStatus           engine.TrackStatusMode          `json:"track_status"`
}

type syncEventParams struct {
	RunID    string                            `json:"run_id"`
	Event    output.Event                      `json:"event"`
	Source   runstate.SourceSnapshot           `json:"source"`
	Progress output.StructuredProgressSnapshot `json:"progress"`
}

type agentSyncEmitter struct {
	mu       sync.Mutex
	conn     *Conn
	runID    string
	tracker  *runstate.Tracker
	progress *output.StructuredProgressTracker
}

func (e *agentSyncEmitter) Emit(event output.Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.progress.ObserveEvent(event)
	outcomes := e.progress.DrainTrackOutcomes()
	e.tracker.ObserveEvent(event, outcomes, "", false)
	return e.conn.Notify("sync.event", syncEventParams{
		RunID: e.runID, Event: event,
		Source:   e.tracker.SourceSnapshot(event.SourceID),
		Progress: e.progress.Snapshot(),
	})
}

func (s *Server) startSync(params json.RawMessage) (any, *RPCError) {
	var request syncStartParams
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "invalid sync.start params", nil)
	}
	if request.PlanLimit < 0 || request.TimeoutSeconds < 0 {
		return nil, NewRPCError(CodeInvalidParams, "plan_limit and timeout_seconds must be non-negative", nil)
	}
	cfg, err := config.Load(config.LoadOptions{ExplicitPath: s.ConfigPath, WorkingDir: s.WorkingDir})
	if err != nil {
		return nil, NewRPCError(CodeInvalidParams, err.Error(), nil)
	}
	if err := config.Validate(cfg); err != nil {
		return nil, NewRPCError(CodeInvalidParams, err.Error(), nil)
	}
	sourceByID := make(map[string]config.Source, len(cfg.Sources))
	for _, source := range cfg.Sources {
		sourceByID[source.ID] = source
	}
	windows := make(map[string]engine.PlanWindow, len(request.PlanWindowBySource))
	for sourceID, window := range request.PlanWindowBySource {
		windows[sourceID] = window
	}
	registry := s.SyncRegistry
	if registry == nil {
		registry = map[string]engine.Adapter{
			"deemix": deemix.New(), "spotdl": spotdl.New(),
			"scdl": scdl.New(), "scdl-freedl": scdlfreedl.New(),
		}
	}
	errOut := s.ErrOut
	if errOut == nil {
		errOut = io.Discard
	}
	runner := s.SyncRunner
	if runner == nil {
		// Protocol stdin is never exposed to adapters, and all child output is
		// routed to stderr so stdout remains valid NDJSON.
		runner = engine.NewSubprocessRunner(nil, errOut, errOut)
	}
	runID, startErr := s.StartRun(func(ctx context.Context, runID string) (any, error, int) {
		tracker := runstate.NewTracker()
		tracker.Reset(cfg.Sources)
		progress := output.NewStructuredProgressTracker(nil)
		emitter := &agentSyncEmitter{conn: s.Conn, runID: runID, tracker: tracker, progress: progress}
		useCase := app.SyncUseCase{Registry: registry, Runner: runner, Emitter: emitter}
		interaction := &Interaction{
			Conn: s.Conn, Runs: s.Runs, RunID: runID, Tracker: tracker,
			SourceDetails: func(sourceID string) app.PlanSourceDetails {
				source := sourceByID[sourceID]
				window := windows[sourceID]
				if window == "" {
					window = request.PlanWindow
				}
				if window == "" {
					window = engine.DefaultPlanWindowForSource(source)
				}
				return app.BuildPlanSourceDetails(source, cfg.Defaults, request.PlanLimit, window, request.DryRun)
			},
			DownloadOrder: func(sourceID string) engine.DownloadOrder {
				return request.DownloadOrderBySource[sourceID]
			},
			PlanWindow: func(sourceID string) engine.PlanWindow {
				window := windows[sourceID]
				if window == "" {
					window = request.PlanWindow
				}
				if window == "" {
					window = engine.DefaultPlanWindowForSource(sourceByID[sourceID])
				}
				return window
			},
			SetPlanWindow: func(sourceID string, window engine.PlanWindow) {
				windows[sourceID] = window
			},
		}
		result, runErr := useCase.Run(ctx, cfg, app.SyncRequest{
			SourceIDs: request.SourceIDs, DryRun: request.DryRun,
			TimeoutOverride: time.Duration(request.TimeoutSeconds) * time.Second,
			Plan:            request.Plan, PlanLimit: request.PlanLimit,
			PlanWindow: request.PlanWindow, PlanWindowBySource: windows,
			AskOnExisting: request.AskOnExisting, AskOnExistingSet: request.AskOnExistingSet,
			ScanGaps: request.ScanGaps, NoPreflight: request.NoPreflight,
			AllowPrompt: true, TrackStatus: request.TrackStatus,
		}, interaction)
		return result, runErr, syncExitCode(result, runErr)
	})
	if startErr != nil {
		return nil, NewRPCError(CodeInternalError, startErr.Error(), nil)
	}
	return map[string]string{"run_id": runID}, nil
}

func syncExitCode(result engine.SyncResult, err error) int {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, engine.ErrInterrupted), result.Interrupted:
		return exitcode.Interrupted
	case err != nil:
		return exitcode.RuntimeFailure
	case result.DependencyFailures > 0:
		return exitcode.MissingDependency
	case result.Failed > 0:
		return exitcode.PartialSuccess
	default:
		return exitcode.Success
	}
}
