package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/jaa/update-downloads/internal/navidrome"
	"github.com/jaa/update-downloads/internal/playlists"
)

type navidromeConfigResult struct {
	Path    string           `json:"path"`
	Config  navidrome.Config `json:"config"`
	Content string           `json:"content"`
}

type navidromeSetupApplyParams struct {
	Plan navidrome.SetupPlan `json:"plan"`
}

type navidromeFavoriteApplyParams struct {
	Plan navidrome.FavoritePlan `json:"plan"`
}

type navidromeEnsureParams struct {
	Confirm bool `json:"confirm"`
}

type navidromeSaveGenresParams struct {
	Genres []string `json:"genres"`
}

func (s *Server) navidromeManager(skipCredentials bool) (*navidrome.Manager, *RPCError) {
	manager, err := navidrome.NewManager(navidrome.ManagerOptions{
		ConfigPath:      s.NavidromeConfigPath,
		WorkingDir:      s.WorkingDir,
		SkipCredentials: skipCredentials,
	})
	if err != nil {
		return nil, NewRPCError(CodeInvalidParams, "navidrome configuration is invalid", map[string]string{
			"detail": err.Error(),
		})
	}
	return manager, nil
}

func (s *Server) readNavidromeConfig() (any, *RPCError) {
	manager, rpcErr := s.navidromeManager(true)
	if rpcErr != nil {
		return nil, rpcErr
	}
	path, err := navidrome.ResolveWritePath(s.NavidromeConfigPath, s.WorkingDir)
	if err != nil {
		return nil, navidromeRPCError(err)
	}
	content := ""
	if payload, readErr := os.ReadFile(path); readErr == nil {
		content = string(payload)
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, navidromeRPCError(readErr)
	}
	if content == "" {
		payload, marshalErr := navidrome.Marshal(manager.Config)
		if marshalErr != nil {
			return nil, navidromeRPCError(marshalErr)
		}
		content = string(payload)
	}
	return navidromeConfigResult{Path: path, Config: manager.Config, Content: content}, nil
}

func (s *Server) writeNavidromeConfig(params json.RawMessage) (any, *RPCError) {
	var request struct {
		Config *navidrome.Config `json:"config"`
	}
	if err := json.Unmarshal(params, &request); err != nil || request.Config == nil {
		return nil, NewRPCError(CodeInvalidParams, "invalid navidrome.config.write params", nil)
	}
	path, err := navidrome.ResolveWritePath(s.NavidromeConfigPath, s.WorkingDir)
	if err != nil {
		return nil, navidromeRPCError(err)
	}
	if err := navidrome.Save(path, *request.Config); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "navidrome configuration is invalid", map[string]string{
			"detail": err.Error(),
		})
	}
	return s.readNavidromeConfig()
}

func (s *Server) navidromeDepsStatus() (any, *RPCError) {
	return navidrome.DependencyChecker{}.Status(s.RunContext()), nil
}

func (s *Server) ensureNavidromeDeps(params json.RawMessage) (any, *RPCError) {
	var request navidromeEnsureParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, NewRPCError(CodeInvalidParams, "invalid navidrome.deps.ensure params", nil)
		}
	}
	if !request.Confirm {
		return nil, NewRPCError(CodeInvalidParams, "installing Navidrome requires explicit confirmation", nil)
	}
	runID, err := s.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		result, ensureErr := navidrome.DependencyChecker{}.Ensure(ctx, true)
		if ensureErr != nil {
			return result, ensureErr, exitcode.RuntimeFailure
		}
		return result, nil, exitcode.Success
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func (s *Server) navidromeStatus() (any, *RPCError) {
	manager, rpcErr := s.navidromeManager(false)
	if rpcErr != nil {
		return nil, rpcErr
	}
	return manager.Status(s.RunContext()), nil
}

func (s *Server) navidromeSetupPlan() (any, *RPCError) {
	manager, rpcErr := s.navidromeManager(true)
	if rpcErr != nil {
		return nil, rpcErr
	}
	runID, err := s.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		plan, planErr := manager.SetupPlan(ctx)
		if planErr != nil {
			return nil, planErr, exitcode.RuntimeFailure
		}
		return plan, nil, exitcode.Success
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func (s *Server) navidromeSetupApply(params json.RawMessage) (any, *RPCError) {
	var request navidromeSetupApplyParams
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "invalid navidrome.setup.apply params", nil)
	}
	if err := navidrome.VerifySetupPlanChecksum(request.Plan); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "navidrome setup plan is stale or modified", map[string]string{
			"detail": err.Error(),
		})
	}
	manager, rpcErr := s.navidromeManager(true)
	if rpcErr != nil {
		return nil, rpcErr
	}
	runID, err := s.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		result, applyErr := manager.ApplySetup(ctx, request.Plan)
		if applyErr != nil {
			return result, applyErr, exitcode.RuntimeFailure
		}
		// A managed definition nobody wrote down cannot be refreshed, so setup
		// registers them. Failing to register is not a reason to fail an
		// otherwise successful setup, so it is reported as a warning.
		if _, defErr := playlists.WriteNavidromeDefinitions(s.PlaylistsConfigPath, s.WorkingDir); defErr != nil {
			result.Message = strings.TrimSpace(result.Message +
				" Managed playlist definitions were not registered: " + defErr.Error())
		}
		return result, nil, exitcode.Success
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func (s *Server) navidromePlaylistsRefresh() (any, *RPCError) {
	manager, rpcErr := s.navidromeManager(false)
	if rpcErr != nil {
		return nil, rpcErr
	}
	runID, err := s.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		result, refreshErr := manager.RefreshPlaylists(ctx)
		if refreshErr != nil {
			return result, refreshErr, exitcode.RuntimeFailure
		}
		return result, nil, exitcode.Success
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func (s *Server) navidromePlaylistsRegister() (any, *RPCError) {
	added, err := playlists.WriteNavidromeDefinitions(s.PlaylistsConfigPath, s.WorkingDir)
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]any{"added": added}, nil
}

func (s *Server) navidromeDeriveGenres() (any, *RPCError) {
	manager, rpcErr := s.navidromeManager(false)
	if rpcErr != nil {
		return nil, rpcErr
	}
	runID, err := s.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		derivation, deriveErr := manager.DeriveGenres(ctx)
		if deriveErr != nil {
			return nil, deriveErr, exitcode.RuntimeFailure
		}
		return derivation, nil, exitcode.Success
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func (s *Server) navidromeSaveGenres(params json.RawMessage) (any, *RPCError) {
	var request navidromeSaveGenresParams
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "invalid navidrome.playlists.saveGenres params", nil)
	}
	if len(navidrome.NormalizeGenres(request.Genres)) == 0 {
		return nil, NewRPCError(CodeInvalidParams, "the approved genre allowlist must not be empty", nil)
	}
	manager, rpcErr := s.navidromeManager(false)
	if rpcErr != nil {
		return nil, rpcErr
	}
	runID, err := s.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		result, saveErr := manager.SaveGenres(ctx, request.Genres)
		if saveErr != nil {
			return result, saveErr, exitcode.RuntimeFailure
		}
		return result, nil, exitcode.Success
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func (s *Server) navidromeFavoritesPlan() (any, *RPCError) {
	manager, rpcErr := s.navidromeManager(false)
	if rpcErr != nil {
		return nil, rpcErr
	}
	runID, err := s.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		plan, planErr := manager.FavoritePlan(ctx)
		if planErr != nil {
			return nil, planErr, exitcode.RuntimeFailure
		}
		return plan, nil, exitcode.Success
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

// navidromeStarredListResult is what the server has starred right now. The
// count is carried explicitly so a caller can show it without walking the list,
// and `tracks` is always an array — never null — so an empty library decodes.
type navidromeStarredListResult struct {
	Count  int              `json:"count"`
	Tracks []navidrome.Song `json:"tracks"`
}

func (s *Server) navidromeFavoritesList() (any, *RPCError) {
	manager, rpcErr := s.navidromeManager(false)
	if rpcErr != nil {
		return nil, rpcErr
	}
	runID, err := s.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		songs, listErr := manager.StarredFavorites(ctx)
		if listErr != nil {
			return nil, listErr, exitcode.RuntimeFailure
		}
		if songs == nil {
			songs = []navidrome.Song{}
		}
		return navidromeStarredListResult{Count: len(songs), Tracks: songs}, nil, exitcode.Success
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func (s *Server) navidromeFavoritesApply(params json.RawMessage) (any, *RPCError) {
	var request navidromeFavoriteApplyParams
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "invalid navidrome.favorites.apply params", nil)
	}
	if err := navidrome.VerifyFavoritePlanChecksum(request.Plan); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "navidrome favorite plan is stale or modified", map[string]string{
			"detail": err.Error(),
		})
	}
	manager, rpcErr := s.navidromeManager(false)
	if rpcErr != nil {
		return nil, rpcErr
	}
	runID, err := s.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		result, applyErr := manager.ApplyFavorites(ctx, request.Plan)
		if applyErr != nil {
			return result, applyErr, exitcode.RuntimeFailure
		}
		return result, nil, exitcode.Success
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func (s *Server) navidromeBackupCreate() (any, *RPCError) {
	manager, rpcErr := s.navidromeManager(true)
	if rpcErr != nil {
		return nil, rpcErr
	}
	runID, err := s.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		result, backupErr := manager.CreateBackup(ctx)
		if backupErr != nil {
			return result, backupErr, exitcode.RuntimeFailure
		}
		return result, nil, exitcode.Success
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func (s *Server) navidromeServiceControl(params json.RawMessage) (any, *RPCError) {
	var request struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "invalid navidrome.service.control params", nil)
	}
	action := strings.ToLower(strings.TrimSpace(request.Action))
	switch action {
	case "start", "stop", "restart":
	default:
		return nil, NewRPCError(CodeInvalidParams, "action must be start, stop, or restart", nil)
	}
	manager, rpcErr := s.navidromeManager(true)
	if rpcErr != nil {
		return nil, rpcErr
	}
	runID, err := s.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		var controlErr error
		switch action {
		case "start":
			controlErr = manager.Svc.Start(ctx, manager.Resolved)
		case "stop":
			controlErr = manager.Svc.Stop(ctx, manager.Resolved)
		case "restart":
			controlErr = manager.Svc.Restart(ctx, manager.Resolved)
		}
		if controlErr != nil {
			return nil, controlErr, exitcode.RuntimeFailure
		}
		return manager.Svc.Status(ctx, manager.Config, manager.Resolved), nil, exitcode.Success
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func navidromeRPCError(err error) *RPCError {
	return NewRPCError(CodeInternalError, "navidrome operation failed", map[string]string{"detail": err.Error()})
}
