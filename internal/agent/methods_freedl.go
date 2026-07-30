package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaa/update-downloads/internal/adapters/deemix"
	"github.com/jaa/update-downloads/internal/adapters/scdl"
	"github.com/jaa/update-downloads/internal/adapters/scdlfreedl"
	"github.com/jaa/update-downloads/internal/adapters/spotdl"
	"github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/jaa/update-downloads/internal/freedl"
	"github.com/jaa/update-downloads/internal/output"
	"github.com/jaa/update-downloads/internal/playlists"
	"github.com/jaa/update-downloads/internal/runstate"
)

type freeDLConfigResult struct {
	Path    string        `json:"path"`
	Config  freedl.Config `json:"config"`
	Content string        `json:"content"`
}

type freeDLPlanParams struct {
	JobID              string          `json:"job_id"`
	PlanLimit          *int            `json:"plan_limit,omitempty"`
	PlaylistID         string          `json:"playlist_id,omitempty"`
	SelectionOverrides map[string]bool `json:"selection_overrides,omitempty"`
}

type freeDLPlanEvent struct {
	Kind    freedl.CapturePlanEventKind `json:"kind"`
	Stage   string                      `json:"stage,omitempty"`
	Status  string                      `json:"status,omitempty"`
	Detail  string                      `json:"detail,omitempty"`
	Row     *freedl.PlanRow             `json:"row,omitempty"`
	Plan    *freedl.CapturePlan         `json:"plan,omitempty"`
	Error   string                      `json:"error,omitempty"`
	Current int                         `json:"current,omitempty"`
	Total   int                         `json:"total,omitempty"`
}

type freeDLPlanEventParams struct {
	RunID string          `json:"run_id"`
	Event freeDLPlanEvent `json:"event"`
}

type freeDLCaptureParams struct {
	Plan              freedl.CapturePlan `json:"plan"`
	SelectedRemoteIDs []string           `json:"selected_remote_ids,omitempty"`
}

type freeDLPromotionBuildParams struct {
	JobID        string `json:"job_id"`
	CaptureRunID string `json:"capture_run_id"`
	TargetFormat string `json:"target_format,omitempty"`
}

type FreeDLOperations struct {
	BuildCapturePlanProgress    func(context.Context, config.Config, freedl.Job) <-chan freedl.CapturePlanEvent
	BuildCapturePlanForPlaylist func(context.Context, config.Config, freedl.Job, playlists.Snapshot) <-chan freedl.CapturePlanEvent
	BuildPromotionPlan          func(context.Context, freedl.Job, string, string) (freedl.PromotionPlan, error)
	ApplyPromotionPlan          func(context.Context, freedl.PromotionPlan) freedl.PromotionResult
}

func (s *Server) readFreeDLConfig() (any, *RPCError) {
	main, cfg, rpcErr := s.loadFreeDLConfigs()
	if rpcErr != nil {
		return nil, rpcErr
	}
	path, rpcErr := s.resolveFreeDLWritePath()
	if rpcErr != nil {
		return nil, rpcErr
	}
	content := ""
	if payload, err := os.ReadFile(path); err == nil {
		content = string(payload)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, freeDLRPCError(err)
	}
	canonical, err := freedl.MarshalCanonical(cfg, main)
	if err != nil {
		return nil, freeDLRPCError(err)
	}
	if content == "" {
		content = string(canonical)
	}
	return freeDLConfigResult{Path: path, Config: cfg, Content: content}, nil
}

func (s *Server) writeFreeDLConfig(params json.RawMessage) (any, *RPCError) {
	var request struct {
		Config freedl.Config `json:"config"`
	}
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "invalid freedl.config.write params", nil)
	}
	main, _, rpcErr := s.loadFreeDLConfigs()
	if rpcErr != nil {
		return nil, rpcErr
	}
	if err := freedl.Validate(request.Config); err != nil {
		return nil, freeDLRPCError(err)
	}
	path, rpcErr := s.resolveFreeDLWritePath()
	if rpcErr != nil {
		return nil, rpcErr
	}
	if err := freedl.SaveSingleFile(path, request.Config, main); err != nil {
		return nil, freeDLRPCError(err)
	}
	content, err := freedl.MarshalCanonical(request.Config, main)
	if err != nil {
		return nil, freeDLRPCError(err)
	}
	return freeDLConfigResult{Path: path, Config: request.Config, Content: string(content)}, nil
}

func (s *Server) startFreeDLPlan(params json.RawMessage) (any, *RPCError) {
	var request freeDLPlanParams
	if err := json.Unmarshal(params, &request); err != nil || strings.TrimSpace(request.JobID) == "" {
		return nil, NewRPCError(CodeInvalidParams, "job_id must be set", nil)
	}
	if request.PlanLimit != nil && *request.PlanLimit < 0 {
		return nil, NewRPCError(CodeInvalidParams, "plan_limit must be non-negative", nil)
	}
	main, cfg, rpcErr := s.loadFreeDLConfigs()
	if rpcErr != nil {
		return nil, rpcErr
	}
	job, ok := freedl.JobByID(cfg, strings.TrimSpace(request.JobID))
	if !ok || !job.Enabled {
		return nil, NewRPCError(CodeInvalidParams, "enabled Free DL job not found", map[string]string{"job_id": request.JobID})
	}
	if request.PlanLimit != nil {
		job.PlanLimit = *request.PlanLimit
	}
	var snapshot *playlists.Snapshot
	if strings.TrimSpace(request.PlaylistID) != "" {
		value, err := playlists.LoadSnapshot(main.Defaults.StateDir, strings.TrimSpace(request.PlaylistID))
		if err != nil {
			return nil, NewRPCError(CodeInvalidParams, "playlist snapshot is unavailable", map[string]string{"playlist_id": request.PlaylistID})
		}
		snapshot = &value
	}
	runID, err := s.StartRun(func(ctx context.Context, runID string) (any, error, int) {
		ops := s.freeDLOperations()
		events := ops.BuildCapturePlanProgress(ctx, main, job)
		if snapshot != nil {
			events = ops.BuildCapturePlanForPlaylist(ctx, main, job, *snapshot)
		}
		for event := range events {
			transport := capturePlanTransportEvent(event, request.SelectionOverrides)
			if notifyErr := s.Conn.Notify("freedl.planEvent", freeDLPlanEventParams{RunID: runID, Event: transport}); notifyErr != nil {
				return nil, notifyErr, exitcode.RuntimeFailure
			}
			switch event.Kind {
			case freedl.CapturePlanEventDone:
				plan := event.Plan
				applyFreeDLSelectionOverrides(&plan, request.SelectionOverrides)
				return plan, nil, exitcode.Success
			case freedl.CapturePlanEventFailed:
				if event.Err == nil {
					event.Err = errors.New("Free DL planning failed")
				}
				return nil, event.Err, playlistExitCode(event.Err)
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err, exitcode.Interrupted
		}
		return nil, errors.New("Free DL planning stopped before completion"), exitcode.RuntimeFailure
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func (s *Server) startFreeDLCapture(params json.RawMessage) (any, *RPCError) {
	var request freeDLCaptureParams
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "invalid freedl.capture.start params", nil)
	}
	main, cfg, rpcErr := s.loadFreeDLConfigs()
	if rpcErr != nil {
		return nil, rpcErr
	}
	job, ok := freedl.JobByID(cfg, request.Plan.Job.ID)
	if !ok || !job.Enabled {
		return nil, NewRPCError(CodeInvalidParams, "enabled Free DL job not found", map[string]string{"job_id": request.Plan.Job.ID})
	}
	plan, rpcErr := authorizeCapturePlan(request.Plan, job)
	if rpcErr != nil {
		return nil, rpcErr
	}
	selected := selectedFreeDLRemoteIDs(plan, request.SelectedRemoteIDs)
	if len(selected) == 0 {
		return nil, NewRPCError(CodeInvalidParams, "select at least one Free DL row before capture", nil)
	}
	runCfg, err := buildFreeDLCaptureConfig(main, plan)
	if err != nil {
		return nil, freeDLRPCError(err)
	}
	runID, err := s.StartRun(func(ctx context.Context, runID string) (any, error, int) {
		if err := os.MkdirAll(runCfg.Sources[0].TargetDir, 0o755); err != nil {
			return nil, err, exitcode.RuntimeFailure
		}
		tracker := runstate.NewTracker()
		tracker.Reset(runCfg.Sources)
		progress := output.NewStructuredProgressTracker(nil)
		emitter := &agentSyncEmitter{conn: s.Conn, runID: runID, tracker: tracker, progress: progress}
		useCase := app.SyncUseCase{
			Registry: s.syncAdapterRegistry(),
			Runner:   s.syncExecRunner(),
			Emitter:  emitter,
		}
		result, runErr := useCase.Run(ctx, runCfg, app.SyncRequest{
			SourceIDs: []string{job.ID}, Plan: true, PlanLimit: job.PlanLimit,
			AllowPrompt: false, TrackStatus: engine.TrackStatusNone,
		}, freeDLCaptureInteraction{selected: selected, order: freeDLDownloadOrder(job.DownloadOrder)})
		if runErr == nil {
			_ = freedl.WriteJSON(filepath.Join(plan.LogDir, "capture-result.json"), result)
		}
		return result, runErr, syncExitCode(result, runErr)
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func (s *Server) buildFreeDLPromotionPlan(params json.RawMessage) (any, *RPCError) {
	var request freeDLPromotionBuildParams
	if err := json.Unmarshal(params, &request); err != nil ||
		strings.TrimSpace(request.JobID) == "" || strings.TrimSpace(request.CaptureRunID) == "" {
		return nil, NewRPCError(CodeInvalidParams, "job_id and capture_run_id must be set", nil)
	}
	if rpcErr := validateFreeDLRunID(request.CaptureRunID); rpcErr != nil {
		return nil, rpcErr
	}
	_, cfg, rpcErr := s.loadFreeDLConfigs()
	if rpcErr != nil {
		return nil, rpcErr
	}
	job, ok := freedl.JobByID(cfg, request.JobID)
	if !ok || !job.Enabled {
		return nil, NewRPCError(CodeInvalidParams, "enabled Free DL job not found", map[string]string{"job_id": request.JobID})
	}
	runID, err := s.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		plan, buildErr := s.freeDLOperations().BuildPromotionPlan(ctx, job, request.CaptureRunID, request.TargetFormat)
		if buildErr != nil {
			return nil, buildErr, playlistExitCode(buildErr)
		}
		return plan, nil, exitcode.Success
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func (s *Server) applyFreeDLPromotion(params json.RawMessage) (any, *RPCError) {
	var request struct {
		Plan freedl.PromotionPlan `json:"plan"`
	}
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "invalid freedl.promote.apply params", nil)
	}
	if rpcErr := validateFreeDLRunID(request.Plan.RunID); rpcErr != nil {
		return nil, rpcErr
	}
	_, cfg, rpcErr := s.loadFreeDLConfigs()
	if rpcErr != nil {
		return nil, rpcErr
	}
	job, ok := freedl.JobByID(cfg, request.Plan.Job.ID)
	if !ok || !job.Enabled {
		return nil, NewRPCError(CodeInvalidParams, "enabled Free DL job not found", map[string]string{"job_id": request.Plan.Job.ID})
	}
	runID, err := s.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		ops := s.freeDLOperations()
		authoritative, buildErr := ops.BuildPromotionPlan(ctx, job, request.Plan.RunID, request.Plan.TargetFormat)
		if buildErr != nil {
			return nil, buildErr, playlistExitCode(buildErr)
		}
		selection := map[string]bool{}
		for _, row := range request.Plan.Rows {
			selection[row.LibraryPath] = row.Selected
		}
		for index := range authoritative.Rows {
			if selected, exists := selection[authoritative.Rows[index].LibraryPath]; exists {
				authoritative.Rows[index].Selected = selected && authoritative.Rows[index].Action != freedl.PromotionSkip
			}
		}
		result := ops.ApplyPromotionPlan(ctx, authoritative)
		switch {
		case errors.Is(ctx.Err(), context.Canceled):
			return result, ctx.Err(), exitcode.Interrupted
		case result.Failed > 0:
			return result, nil, exitcode.PartialSuccess
		default:
			return result, nil, exitcode.Success
		}
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func (s *Server) loadFreeDLConfigs() (config.Config, freedl.Config, *RPCError) {
	main, err := config.Load(config.LoadOptions{ExplicitPath: s.ConfigPath, WorkingDir: s.WorkingDir})
	if err != nil {
		return config.Config{}, freedl.Config{}, configRPCError(err)
	}
	cfg, err := freedl.Load(freedl.LoadOptions{
		ExplicitPath: s.FreeDLConfigPath, WorkingDir: s.WorkingDir, MainConfig: main,
	})
	if err != nil {
		return config.Config{}, freedl.Config{}, freeDLRPCError(err)
	}
	if err := freedl.Validate(cfg); err != nil {
		return config.Config{}, freedl.Config{}, freeDLRPCError(err)
	}
	return main, cfg, nil
}

func (s *Server) resolveFreeDLWritePath() (string, *RPCError) {
	path := strings.TrimSpace(s.FreeDLConfigPath)
	if path == "" {
		var err error
		path, err = freedl.UserConfigPath()
		if err != nil {
			return "", freeDLRPCError(err)
		}
	}
	expanded, err := config.ExpandPath(path)
	if err != nil {
		return "", NewRPCError(CodeInvalidParams, "invalid Free DL config path", nil)
	}
	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return "", NewRPCError(CodeInvalidParams, "invalid Free DL config path", nil)
	}
	absolute = filepath.Clean(absolute)
	if info, statErr := os.Lstat(absolute); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", NewRPCError(CodeInvalidParams, "symbolic-link Free DL config paths are not allowed", map[string]string{"path": absolute})
	}
	return absolute, nil
}

func (s *Server) freeDLService() freedl.Service {
	if s.FreeDLService != nil {
		return *s.FreeDLService
	}
	return freedl.Service{}
}

func (s *Server) freeDLOperations() FreeDLOperations {
	service := s.freeDLService()
	defaults := FreeDLOperations{
		BuildCapturePlanProgress:    service.BuildCapturePlanProgress,
		BuildCapturePlanForPlaylist: service.BuildCapturePlanProgressForPlaylist,
		BuildPromotionPlan:          service.BuildPromotionPlan,
		ApplyPromotionPlan:          service.ApplyPromotionPlan,
	}
	if s.FreeDLOps == nil {
		return defaults
	}
	ops := *s.FreeDLOps
	if ops.BuildCapturePlanProgress == nil {
		ops.BuildCapturePlanProgress = defaults.BuildCapturePlanProgress
	}
	if ops.BuildCapturePlanForPlaylist == nil {
		ops.BuildCapturePlanForPlaylist = defaults.BuildCapturePlanForPlaylist
	}
	if ops.BuildPromotionPlan == nil {
		ops.BuildPromotionPlan = defaults.BuildPromotionPlan
	}
	if ops.ApplyPromotionPlan == nil {
		ops.ApplyPromotionPlan = defaults.ApplyPromotionPlan
	}
	return ops
}

func (s *Server) syncAdapterRegistry() map[string]engine.Adapter {
	if s.SyncRegistry != nil {
		return s.SyncRegistry
	}
	return map[string]engine.Adapter{
		"deemix": deemix.New(), "spotdl": spotdl.New(),
		"scdl": scdl.New(), "scdl-freedl": scdlfreedl.New(),
	}
}

func (s *Server) syncExecRunner() engine.ExecRunner {
	if s.SyncRunner != nil {
		return s.SyncRunner
	}
	errOut := s.ErrOut
	if errOut == nil {
		errOut = io.Discard
	}
	return engine.NewSubprocessRunner(nil, errOut, errOut)
}

func capturePlanTransportEvent(event freedl.CapturePlanEvent, overrides map[string]bool) freeDLPlanEvent {
	result := freeDLPlanEvent{
		Kind: event.Kind, Stage: event.Stage, Status: event.Status, Detail: event.Detail,
		Current: event.Current, Total: event.Total,
	}
	if event.Err != nil {
		result.Error = event.Err.Error()
	}
	if event.Row.RemoteID != "" {
		row := event.Row
		if selected, ok := overrides[row.RemoteID]; ok {
			row.Selected = selected && row.Selectable
		}
		result.Row = &row
	}
	if event.Plan.RunID != "" {
		plan := event.Plan
		applyFreeDLSelectionOverrides(&plan, overrides)
		result.Plan = &plan
	}
	return result
}

func applyFreeDLSelectionOverrides(plan *freedl.CapturePlan, overrides map[string]bool) {
	if plan == nil {
		return
	}
	for index := range plan.Rows {
		if selected, ok := overrides[plan.Rows[index].RemoteID]; ok {
			plan.Rows[index].Selected = selected && plan.Rows[index].Selectable
		}
	}
}

func authorizeCapturePlan(plan freedl.CapturePlan, job freedl.Job) (freedl.CapturePlan, *RPCError) {
	if rpcErr := validateFreeDLRunID(plan.RunID); rpcErr != nil {
		return freedl.CapturePlan{}, rpcErr
	}
	bufferDir, err := config.ExpandPath(job.BufferDir)
	if err != nil {
		return freedl.CapturePlan{}, freeDLRPCError(err)
	}
	logDir, err := config.ExpandPath(job.LogDir)
	if err != nil {
		return freedl.CapturePlan{}, freeDLRPCError(err)
	}
	plan.Job = job
	plan.BufferRoot = filepath.Join(bufferDir, plan.RunID)
	plan.LogDir = filepath.Join(logDir, plan.RunID)
	return plan, nil
}

func validateFreeDLRunID(runID string) *RPCError {
	trimmed := strings.TrimSpace(runID)
	if trimmed == "" || trimmed == "." || trimmed == ".." ||
		filepath.Base(trimmed) != trimmed || strings.ContainsAny(trimmed, `/\`) {
		return NewRPCError(CodeInvalidParams, "invalid Free DL run_id", nil)
	}
	return nil
}

func selectedFreeDLRemoteIDs(plan freedl.CapturePlan, explicit []string) map[string]struct{} {
	requested := map[string]bool{}
	for _, remoteID := range explicit {
		requested[strings.TrimSpace(remoteID)] = true
	}
	selected := map[string]struct{}{}
	for _, row := range plan.Rows {
		use := row.Selected
		if len(explicit) > 0 {
			use = requested[row.RemoteID]
		}
		if use && row.Selectable {
			selected[row.RemoteID] = struct{}{}
		}
	}
	return selected
}

func buildFreeDLCaptureConfig(main config.Config, plan freedl.CapturePlan) (config.Config, error) {
	downloadsDir := filepath.Join(plan.BufferRoot, "downloads")
	runCfg := main
	runCfg.Defaults.StateDir = plan.LogDir
	runCfg.Defaults.ArchiveFile = "archive.txt"
	runCfg.Sources = []config.Source{{
		ID: plan.Job.ID, Type: config.SourceTypeSoundCloud, Enabled: true,
		TargetDir: downloadsDir, URL: plan.Job.SourceURL, StateFile: "capture.sync.scdl",
		Adapter: config.AdapterSpec{Kind: "scdl-freedl"},
		Sync: config.SyncPolicy{
			BreakOnExisting: agentBoolPtr(true), AskOnExisting: agentBoolPtr(false),
			LocalIndexCache: agentBoolPtr(false),
		},
	}}
	if err := config.Validate(runCfg); err != nil {
		return config.Config{}, err
	}
	return runCfg, nil
}

type freeDLCaptureInteraction struct {
	selected map[string]struct{}
	order    engine.DownloadOrder
}

func (i freeDLCaptureInteraction) Confirm(string, bool) (bool, error) { return true, nil }
func (i freeDLCaptureInteraction) Input(string) (string, error)       { return "", nil }
func (i freeDLCaptureInteraction) SelectRows(sourceID string, rows []engine.PlanRow) (engine.PlanSelectionResult, error) {
	indices := []int{}
	for _, row := range rows {
		if _, ok := i.selected[row.RemoteID]; ok && row.Toggleable {
			indices = append(indices, row.Index)
		}
	}
	manifest, err := engine.BuildExecutionManifest(sourceID, rows, indices, i.order)
	if err != nil {
		return engine.PlanSelectionResult{}, err
	}
	return engine.PlanSelectionResult{Manifest: manifest}, nil
}

func freeDLDownloadOrder(value string) engine.DownloadOrder {
	if strings.TrimSpace(value) == string(engine.DownloadOrderNewestFirst) {
		return engine.DownloadOrderNewestFirst
	}
	return engine.DownloadOrderOldestFirst
}

func agentBoolPtr(value bool) *bool { return &value }

func freeDLRPCError(err error) *RPCError {
	return NewRPCError(CodeInvalidParams, err.Error(), nil)
}
