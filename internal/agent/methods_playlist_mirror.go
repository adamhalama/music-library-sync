package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/jaa/update-downloads/internal/navidrome"
	"github.com/jaa/update-downloads/internal/playlistmirror"
	"github.com/jaa/update-downloads/internal/playlists"
	"github.com/jaa/update-downloads/internal/rekordbox/bridge"
	legacy "github.com/jaa/update-downloads/internal/rekordbox/playlistsync"
	"github.com/jaa/update-downloads/internal/rekordbox/pyruntime"
)

type playlistMirrorInspectRow struct {
	Job        playlists.SyncJob    `json:"job"`
	State      *playlists.PairState `json:"state,omitempty"`
	StateError string               `json:"state_error,omitempty"`
}

type playlistMirrorPlanParams struct {
	JobID     string                  `json:"job_id"`
	Direction playlists.SyncDirection `json:"direction"`
	OutPath   string                  `json:"out_path,omitempty"`
}

type playlistMirrorApplyParams struct {
	Plan   playlistmirror.Plan `json:"plan"`
	DryRun bool                `json:"dry_run"`
}

func (s *Server) inspectPlaylistMirror() (any, *RPCError) {
	main, cfg, rpcErr := s.loadPlaylistConfigs()
	if rpcErr != nil {
		return nil, rpcErr
	}
	rows := make([]playlistMirrorInspectRow, 0, len(cfg.SyncJobs))
	for _, job := range cfg.SyncJobs {
		row := playlistMirrorInspectRow{Job: job}
		state, err := playlists.LoadPairState(main.Defaults.StateDir, job.ID)
		if err == nil {
			if trusted, ok, trustErr := playlists.PairStateForJob(state, job); trustErr != nil {
				row.StateError = trustErr.Error()
			} else if ok {
				row.State = &trusted
			} else {
				row.StateError = "saved binding belongs to an older job configuration"
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			row.StateError = err.Error()
		}
		rows = append(rows, row)
	}
	return map[string]any{"jobs": rows}, nil
}

func (s *Server) planPlaylistMirror(params json.RawMessage) (any, *RPCError) {
	var request playlistMirrorPlanParams
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "invalid playlistSync.plan params", nil)
	}
	if strings.TrimSpace(request.JobID) == "" || !request.Direction.Valid() {
		return nil, NewRPCError(CodeInvalidParams, "playlistSync.plan requires job_id and an explicit valid direction", nil)
	}
	main, playlistConfig, rpcErr := s.loadPlaylistConfigs()
	if rpcErr != nil {
		return nil, rpcErr
	}
	runID, err := s.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		useCase, cfg, dbDir, username, buildErr := s.buildPlaylistMirrorUseCase(ctx, main)
		if buildErr != nil {
			return nil, buildErr, playlistExitCode(buildErr)
		}
		result, planErr := useCase.Plan(ctx, app.PlaylistMirrorPlanRequest{
			Config: cfg, PlaylistConfig: playlistConfig, JobID: request.JobID, Direction: request.Direction,
			RekordboxDBDir: dbDir, NavidromeUser: username, OutPath: request.OutPath,
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

func (s *Server) applyPlaylistMirror(params json.RawMessage) (any, *RPCError) {
	var request playlistMirrorApplyParams
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "invalid playlistSync.apply params", nil)
	}
	if err := playlistmirror.VerifyPlanApplicable(request.Plan); err != nil {
		return nil, NewRPCError(CodeInvalidParams, err.Error(), map[string]any{"blockers": request.Plan.Blockers})
	}
	main, playlistConfig, rpcErr := s.loadPlaylistConfigs()
	if rpcErr != nil {
		return nil, rpcErr
	}
	runID, err := s.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		useCase, cfg, dbDir, username, buildErr := s.buildPlaylistMirrorUseCase(ctx, main)
		if buildErr != nil {
			return nil, buildErr, playlistExitCode(buildErr)
		}
		result, applyErr := useCase.Apply(ctx, app.PlaylistMirrorApplyRequest{
			Config: cfg, PlaylistConfig: playlistConfig, Plan: request.Plan,
			RekordboxDBDir: dbDir, NavidromeUser: username, DryRun: request.DryRun,
		})
		if applyErr != nil {
			code := playlistExitCode(applyErr)
			if errors.Is(applyErr, navidrome.ErrMutationUncertain) || result.BackupPath != "" {
				code = exitcode.PartialSuccess
			}
			return result, applyErr, code
		}
		return result, nil, exitcode.Success
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func (s *Server) buildPlaylistMirrorUseCase(ctx context.Context, main config.Config) (app.PlaylistMirrorUseCase, config.Config, string, string, error) {
	if s.PlaylistMirrorUseCase != nil {
		dbDir := ""
		if main.Rekordbox != nil {
			dbDir, _ = config.ExpandPath(main.Rekordbox.DBDir)
		}
		return *s.PlaylistMirrorUseCase, main, dbDir, "", nil
	}
	cfg, rbCfg, rpcErr := s.loadRekordboxConfigs()
	if rpcErr != nil {
		return app.PlaylistMirrorUseCase{}, main, "", "", errors.New(rpcErr.Message)
	}
	runtime, err := (pyruntime.Resolver{}).Ensure(ctx, pyruntime.Request{Config: cfg})
	if err != nil {
		return app.PlaylistMirrorUseCase{}, cfg, "", "", err
	}
	dbDir, err := config.ExpandPath(rbCfg.Defaults.DBDir)
	if err != nil {
		return app.PlaylistMirrorUseCase{}, cfg, "", "", err
	}
	backupDir, err := config.ExpandPath(rbCfg.Defaults.BackupDir)
	if err != nil {
		return app.PlaylistMirrorUseCase{}, cfg, "", "", err
	}
	manager, err := navidrome.NewManager(navidrome.ManagerOptions{
		ConfigPath: s.NavidromeConfigPath, WorkingDir: s.WorkingDir,
	})
	if err != nil {
		return app.PlaylistMirrorUseCase{}, cfg, "", "", err
	}
	client, err := manager.Client()
	if err != nil {
		return app.PlaylistMirrorUseCase{}, cfg, "", "", err
	}
	useCase := app.PlaylistMirrorUseCase{
		Bridge: bridge.Client{PythonBin: runtime.PythonBin, PythonPath: runtime.PythonPath}, Navidrome: client,
		CheckClosed: legacy.CheckRekordboxClosed,
		BackupRekordbox: func(ctx context.Context, dbDir string) (string, error) {
			return legacy.CreateBackup(ctx, dbDir, backupDir, time.Now())
		},
		BackupNavidrome: func(ctx context.Context) (string, error) {
			result, err := manager.CreateBackup(ctx)
			return result.Backup.Path, err
		},
	}
	return useCase, cfg, dbDir, manager.Config.Server.Username, nil
}

func playlistMirrorRPCError(err error) *RPCError {
	return NewRPCError(CodeInvalidParams, fmt.Sprintf("playlist sync: %v", err), nil)
}
