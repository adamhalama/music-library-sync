package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/jaa/update-downloads/internal/playlists"
	"github.com/jaa/update-downloads/internal/rekordbox/bridge"
	"github.com/jaa/update-downloads/internal/rekordbox/playlistsync"
	"github.com/jaa/update-downloads/internal/rekordbox/pyruntime"
	"github.com/jaa/update-downloads/internal/rekordbox/syncconfig"
)

type RekordboxOperations struct {
	Status  func(context.Context, pyruntime.Request) pyruntime.Status
	Ensure  func(context.Context, pyruntime.Request) (pyruntime.Runtime, error)
	Reset   func(pyruntime.Request) error
	Inspect func(context.Context, config.Config) (bridge.InspectResponse, error)
	Plan    func(context.Context, app.RekordboxPlaylistSyncPlanRequest) (app.RekordboxPlaylistSyncPlanResult, error)
	Apply   func(context.Context, app.RekordboxPlaylistSyncApplyRequest) (app.RekordboxPlaylistSyncApplyResult, error)
}

type rekordboxConfigResult struct {
	Path    syncconfig.WritePathResolution `json:"path"`
	Config  syncconfig.Config              `json:"config"`
	Content string                         `json:"content"`
}

type rekordboxPlanParams struct {
	JobID               string `json:"job_id,omitempty"`
	MappingID           string `json:"mapping_id,omitempty"`
	MusicPlaylist       string `json:"music_playlist,omitempty"`
	MusicPlaylistID     string `json:"music_playlist_id,omitempty"`
	RekordboxPlaylist   string `json:"rekordbox_playlist,omitempty"`
	RekordboxPlaylistID string `json:"rekordbox_playlist_id,omitempty"`
	RekordboxDBDir      string `json:"rekordbox_db_dir,omitempty"`
	PythonBin           string `json:"python_bin,omitempty"`
	PythonPath          string `json:"python_path,omitempty"`
	BackupDir           string `json:"backup_dir,omitempty"`
	Mode                string `json:"mode,omitempty"`
	CreatePlaylist      bool   `json:"create_playlist"`
	CreatePlaylistSet   bool   `json:"create_playlist_set"`
	PlaylistID          string `json:"playlist_id,omitempty"`
}

type rekordboxApplyParams struct {
	Plan       playlistsync.Plan `json:"plan"`
	PythonBin  string            `json:"python_bin,omitempty"`
	PythonPath string            `json:"python_path,omitempty"`
	BackupDir  string            `json:"backup_dir,omitempty"`
	DryRun     bool              `json:"dry_run"`
}

type rekordboxBlocker struct {
	OperationID  string `json:"operation_id,omitempty"`
	PlaylistName string `json:"playlist_name"`
	MusicIndex   int    `json:"music_index"`
	Artist       string `json:"artist,omitempty"`
	Title        string `json:"title"`
	Path         string `json:"path,omitempty"`
	MatchStatus  string `json:"match_status"`
	Action       string `json:"action"`
}

func (s *Server) readRekordboxConfig() (any, *RPCError) {
	_, rbCfg, rpcErr := s.loadRekordboxConfigs()
	if rpcErr != nil {
		return nil, rpcErr
	}
	resolution, rpcErr := s.resolveRekordboxWritePath()
	if rpcErr != nil {
		return nil, rpcErr
	}
	content := ""
	if payload, err := os.ReadFile(resolution.Path); err == nil {
		content = string(payload)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, rekordboxRPCError(err)
	}
	if content == "" {
		payload, err := syncconfig.Marshal(rbCfg)
		if err != nil {
			return nil, rekordboxRPCError(err)
		}
		content = string(payload)
	}
	return rekordboxConfigResult{Path: resolution, Config: rbCfg, Content: content}, nil
}

func (s *Server) writeRekordboxConfig(params json.RawMessage) (any, *RPCError) {
	var request struct {
		Config syncconfig.Config `json:"config"`
	}
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "invalid rekordbox.config.write params", nil)
	}
	if err := syncconfig.Validate(request.Config); err != nil {
		return nil, rekordboxRPCError(err)
	}
	resolution, rpcErr := s.resolveRekordboxWritePath()
	if rpcErr != nil {
		return nil, rpcErr
	}
	if err := syncconfig.Save(resolution.Path, request.Config); err != nil {
		return nil, rekordboxRPCError(err)
	}
	content, err := syncconfig.Marshal(request.Config)
	if err != nil {
		return nil, rekordboxRPCError(err)
	}
	resolution.Exists = true
	return rekordboxConfigResult{Path: resolution, Config: request.Config, Content: string(content)}, nil
}

func (s *Server) rekordboxDepsStatus() (any, *RPCError) {
	cfg, _, rpcErr := s.loadRekordboxConfigs()
	if rpcErr != nil {
		return nil, rpcErr
	}
	status := s.rekordboxOperations().Status(context.Background(), pyruntime.Request{Config: cfg})
	code := exitcode.Success
	if !status.Healthy {
		code = exitcode.MissingDependency
	}
	return map[string]any{"status": status, "exit_code": code}, nil
}

func (s *Server) ensureRekordboxDeps() (any, *RPCError) {
	cfg, _, rpcErr := s.loadRekordboxConfigs()
	if rpcErr != nil {
		return nil, rpcErr
	}
	runID, err := s.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		runtime, ensureErr := s.rekordboxOperations().Ensure(ctx, pyruntime.Request{Config: cfg})
		if ensureErr != nil {
			return nil, ensureErr, exitcode.MissingDependency
		}
		return runtime, nil, exitcode.Success
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func (s *Server) resetRekordboxDeps() (any, *RPCError) {
	cfg, _, rpcErr := s.loadRekordboxConfigs()
	if rpcErr != nil {
		return nil, rpcErr
	}
	runID, err := s.StartRun(func(context.Context, string) (any, error, int) {
		if resetErr := s.rekordboxOperations().Reset(pyruntime.Request{Config: cfg}); resetErr != nil {
			return nil, resetErr, exitcode.RuntimeFailure
		}
		return map[string]bool{"reset": true}, nil, exitcode.Success
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func (s *Server) inspectRekordbox() (any, *RPCError) {
	cfg, _, rpcErr := s.loadRekordboxConfigs()
	if rpcErr != nil {
		return nil, rpcErr
	}
	runID, err := s.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		inspect, inspectErr := s.rekordboxOperations().Inspect(ctx, cfg)
		if inspectErr != nil {
			return nil, inspectErr, playlistExitCode(inspectErr)
		}
		return inspect, nil, exitcode.Success
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func (s *Server) planRekordbox(params json.RawMessage) (any, *RPCError) {
	var request rekordboxPlanParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, NewRPCError(CodeInvalidParams, "invalid rekordbox.plan params", nil)
		}
	}
	cfg, rbCfg, rpcErr := s.loadRekordboxConfigs()
	if rpcErr != nil {
		return nil, rpcErr
	}
	var snapshot *playlists.Snapshot
	if strings.TrimSpace(request.PlaylistID) != "" {
		value, err := playlists.LoadSnapshot(cfg.Defaults.StateDir, request.PlaylistID)
		if err != nil {
			return nil, NewRPCError(CodeInvalidParams, "playlist snapshot is unavailable", map[string]string{"playlist_id": request.PlaylistID})
		}
		snapshot = &value
	}
	runID, err := s.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		result, planErr := s.rekordboxOperations().Plan(ctx, app.RekordboxPlaylistSyncPlanRequest{
			Config: cfg, SyncConfig: &rbCfg, MappingID: request.MappingID, Snapshot: snapshot,
			Options: playlistsync.Options{
				JobID: request.JobID, MusicPlaylist: request.MusicPlaylist,
				MusicPlaylistID: request.MusicPlaylistID, RekordboxPlaylist: request.RekordboxPlaylist,
				RekordboxPlaylistID: request.RekordboxPlaylistID, RekordboxDBDir: request.RekordboxDBDir,
				PythonBin: request.PythonBin, PythonPath: request.PythonPath, BackupDir: request.BackupDir,
				Mode: request.Mode, CreatePlaylist: request.CreatePlaylist,
				CreatePlaylistSet: request.CreatePlaylistSet, MappingID: request.MappingID,
			},
		})
		if planErr != nil {
			return nil, planErr, playlistExitCode(planErr)
		}
		return result, nil, exitcode.Success
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func (s *Server) applyRekordbox(params json.RawMessage) (any, *RPCError) {
	var request rekordboxApplyParams
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "invalid rekordbox.apply params", nil)
	}
	if err := playlistsync.ValidatePlanForApply(request.Plan); err != nil {
		return nil, NewRPCError(CodeInvalidParams, err.Error(), map[string]any{
			"blockers": rekordboxPlanBlockers(request.Plan),
		})
	}
	cfg, rbCfg, rpcErr := s.loadRekordboxConfigs()
	if rpcErr != nil {
		return nil, rpcErr
	}
	runID, err := s.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		result, applyErr := s.rekordboxOperations().Apply(ctx, app.RekordboxPlaylistSyncApplyRequest{
			Config: cfg, SyncConfig: &rbCfg, Plan: request.Plan,
			PythonBin: request.PythonBin, PythonPath: request.PythonPath,
			BackupDir: request.BackupDir, DryRun: request.DryRun,
		})
		if applyErr != nil {
			return nil, applyErr, playlistExitCode(applyErr)
		}
		return result, nil, exitcode.Success
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func (s *Server) loadRekordboxConfigs() (config.Config, syncconfig.Config, *RPCError) {
	base, err := config.Load(config.LoadOptions{ExplicitPath: s.ConfigPath, WorkingDir: s.WorkingDir})
	if err != nil {
		return config.Config{}, syncconfig.Config{}, configRPCError(err)
	}
	rbCfg, err := syncconfig.Load(syncconfig.LoadOptions{
		ExplicitPath: s.RekordboxConfigPath, WorkingDir: s.WorkingDir, BaseConfig: base,
	})
	if err != nil {
		return config.Config{}, syncconfig.Config{}, rekordboxRPCError(err)
	}
	cfg := configWithRekordboxDefaults(base, rbCfg)
	if err := config.ValidateRekordbox(cfg); err != nil {
		return config.Config{}, syncconfig.Config{}, configRPCError(err)
	}
	return cfg, rbCfg, nil
}

func (s *Server) resolveRekordboxWritePath() (syncconfig.WritePathResolution, *RPCError) {
	resolution, err := syncconfig.ResolveWritePath(syncconfig.WritePathOptions{
		ExplicitPath: s.RekordboxConfigPath, WorkingDir: s.WorkingDir,
	})
	if err != nil {
		return syncconfig.WritePathResolution{}, rekordboxRPCError(err)
	}
	absolute, err := filepath.Abs(resolution.Path)
	if err != nil {
		return syncconfig.WritePathResolution{}, NewRPCError(CodeInvalidParams, "invalid Rekordbox config path", nil)
	}
	absolute = filepath.Clean(absolute)
	allowed := map[string]bool{filepath.Clean(syncconfig.ProjectConfigPath(s.WorkingDir)): true}
	if user, userErr := syncconfig.UserConfigPath(); userErr == nil {
		if userAbsolute, absErr := filepath.Abs(user); absErr == nil {
			allowed[filepath.Clean(userAbsolute)] = true
		}
	}
	if strings.TrimSpace(s.RekordboxConfigPath) != "" {
		expanded, expandErr := config.ExpandPath(s.RekordboxConfigPath)
		if expandErr != nil {
			return syncconfig.WritePathResolution{}, NewRPCError(CodeInvalidParams, "invalid Rekordbox config path", nil)
		}
		explicitAbsolute, absErr := filepath.Abs(expanded)
		if absErr != nil {
			return syncconfig.WritePathResolution{}, NewRPCError(CodeInvalidParams, "invalid Rekordbox config path", nil)
		}
		allowed = map[string]bool{filepath.Clean(explicitAbsolute): true}
	}
	if !allowed[absolute] {
		return syncconfig.WritePathResolution{}, NewRPCError(CodeInvalidParams, "Rekordbox config path is outside the initialized session scope", map[string]string{"path": absolute})
	}
	if info, statErr := os.Lstat(absolute); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return syncconfig.WritePathResolution{}, NewRPCError(CodeInvalidParams, "symbolic-link Rekordbox config paths are not allowed", map[string]string{"path": absolute})
	}
	resolution.Path = absolute
	return resolution, nil
}

func (s *Server) rekordboxOperations() RekordboxOperations {
	defaults := defaultRekordboxOperations()
	if s.RekordboxOps == nil {
		return defaults
	}
	ops := *s.RekordboxOps
	if ops.Status == nil {
		ops.Status = defaults.Status
	}
	if ops.Ensure == nil {
		ops.Ensure = defaults.Ensure
	}
	if ops.Reset == nil {
		ops.Reset = defaults.Reset
	}
	if ops.Inspect == nil {
		ops.Inspect = defaults.Inspect
	}
	if ops.Plan == nil {
		ops.Plan = defaults.Plan
	}
	if ops.Apply == nil {
		ops.Apply = defaults.Apply
	}
	return ops
}

func defaultRekordboxOperations() RekordboxOperations {
	resolver := pyruntime.Resolver{}
	useCase := app.RekordboxPlaylistSyncUseCase{}
	return RekordboxOperations{
		Status: resolver.Status,
		Ensure: resolver.Ensure,
		Reset:  resolver.Reset,
		Inspect: func(ctx context.Context, cfg config.Config) (bridge.InspectResponse, error) {
			resolved, err := playlistsync.ResolveOptions(cfg, playlistsync.Options{})
			if err != nil {
				return bridge.InspectResponse{}, err
			}
			runtime, err := resolver.Ensure(ctx, pyruntime.Request{Config: cfg})
			if err != nil {
				return bridge.InspectResponse{}, err
			}
			if err := playlistsync.CheckRekordboxClosed(ctx, resolved.RekordboxDBDir); err != nil {
				return bridge.InspectResponse{}, err
			}
			return (bridge.Client{PythonBin: runtime.PythonBin, PythonPath: runtime.PythonPath}).Inspect(ctx, resolved.RekordboxDBDir)
		},
		Plan:  useCase.Plan,
		Apply: useCase.Apply,
	}
}

func configWithRekordboxDefaults(base config.Config, rb syncconfig.Config) config.Config {
	base.Rekordbox = &config.RekordboxConfig{
		DBDir: rb.Defaults.DBDir, PythonBin: rb.Defaults.PythonBin,
		PythonPath: rb.Defaults.PythonPath, BackupDir: rb.Defaults.BackupDir,
	}
	return base
}

func rekordboxPlanBlockers(plan playlistsync.Plan) []rekordboxBlocker {
	blockers := []rekordboxBlocker{}
	addRows := func(operationID, playlistName string, rows []playlistsync.PlanRow) {
		for _, row := range rows {
			if !rekordboxPlanRowBlocked(row) {
				continue
			}
			blockers = append(blockers, rekordboxBlocker{
				OperationID: operationID, PlaylistName: playlistName,
				MusicIndex: row.MusicIndex, Artist: row.Artist, Title: row.Title,
				Path: row.Path, MatchStatus: row.MatchStatus, Action: row.Action,
			})
		}
	}
	if plan.Version == playlistsync.PlanVersionFolder {
		for _, operation := range plan.Operations {
			addRows(operation.ID, operation.MusicPlaylist.Name, operation.Rows)
		}
	} else {
		addRows("", plan.MusicPlaylist.Name, plan.Rows)
	}
	return blockers
}

func rekordboxPlanRowBlocked(row playlistsync.PlanRow) bool {
	if row.Action != "skip" {
		return false
	}
	switch row.MatchStatus {
	case "missing", "ambiguous", "ambiguous_path", "duplicate_path":
		return true
	default:
		return false
	}
}

func rekordboxRPCError(err error) *RPCError {
	return NewRPCError(CodeInvalidParams, err.Error(), nil)
}
