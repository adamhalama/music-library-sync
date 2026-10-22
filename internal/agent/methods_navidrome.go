package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// navidromeOptions selects the same phone-library config every navidrome.*
// method uses, so the post-sync hook pushes and scans the server the app shows.
func (s *Server) navidromeOptions() navidrome.ManagerOptions {
	return navidrome.ManagerOptions{ConfigPath: s.NavidromeConfigPath, WorkingDir: s.WorkingDir}
}

func (s *Server) navidromeManager(skipCredentials bool) (*navidrome.Manager, *RPCError) {
	build := navidrome.NewManager
	if s.newNavidromeManager != nil {
		build = s.newNavidromeManager
	}
	manager, err := build(navidrome.ManagerOptions{
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
	if current, rpcErr := s.navidromeManager(true); rpcErr == nil {
		if err := keepOmittedRemoteSettings(params, request.Config, current.Config); err != nil {
			return nil, NewRPCError(CodeInvalidParams, "invalid navidrome.config.write params", nil)
		}
	}
	if err := navidrome.Save(path, *request.Config); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "navidrome configuration is invalid", map[string]string{
			"detail": err.Error(),
		})
	}
	return s.readNavidromeConfig()
}

// keepOmittedRemoteSettings carries the stored mode and remote server over
// when a write omits them. A native build that predates remote mode decodes
// the config, edits it, and sends back only the fields it knows; without this
// a remote install would silently turn back into a local one on its next
// save. A client that does know the fields sends them and is taken as is.
func keepOmittedRemoteSettings(params json.RawMessage, incoming *navidrome.Config, current navidrome.Config) error {
	var raw struct {
		Config map[string]json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(params, &raw); err != nil {
		return err
	}
	if _, sent := raw.Config["mode"]; !sent {
		incoming.Mode = current.Mode
	}
	if _, sent := raw.Config["remote"]; !sent && current.Remote != nil {
		remote := *current.Remote
		incoming.Remote = &remote
	}
	return nil
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
	if manager, rpcErr := s.navidromeManager(true); rpcErr == nil && manager.Config.IsRemote() {
		return nil, NewRPCError(CodeInvalidParams, "navidrome runs on the remote server", map[string]string{
			"detail": "navidrome.mode is remote, so there is nothing to install on this Mac",
		})
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
		if _, defErr := s.writeNavidromeDefinitions(); defErr != nil {
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
	added, err := s.writeNavidromeDefinitions()
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
	if err := manager.ServiceControlAllowed(action); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "navidrome runs on the remote server", map[string]string{"detail": err.Error()})
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

type navidromeMirrorPushParams struct {
	// AllowDeletes raises the delete limit for one intended reorganisation,
	// exactly like `udl navidrome push --allow-deletes N`. Zero keeps the
	// configured limit.
	AllowDeletes int  `json:"allow_deletes"`
	ForceScan    bool `json:"force_scan"`
}

// navidromeMirrorPushResult is what a `navidrome.mirror.push` run finishes
// with: the push and what followed it (MirrorResult's fields, flattened), the
// mirror state as recorded afterwards, and — when the push did not go through
// — why, in a form the app can show without parsing the error text.
type navidromeMirrorPushResult struct {
	navidrome.MirrorResult
	Mirror  *navidrome.MirrorState      `json:"mirror,omitempty"`
	Failure *navidromeMirrorPushFailure `json:"failure,omitempty"`
}

// Failure kinds of a mirror push.
const (
	mirrorPushBusy           = "busy"
	mirrorPushDeleteLimit    = "delete_limit"
	mirrorPushServerScanning = "server_scanning"
	mirrorPushCanceled       = "canceled"
	mirrorPushFailed         = "failed"
)

type navidromeMirrorPushFailure struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
	// PlannedDeletes and AllowDeletesCommand are set for a delete-limit
	// refusal. The override stays a deliberate CLI step; the app offers no
	// one-click equivalent.
	PlannedDeletes      int    `json:"planned_deletes,omitempty"`
	AllowDeletesCommand string `json:"allow_deletes_command,omitempty"`
}

// mirrorPushBusyDetail is shown when another push holds the per-data-directory mirror
// lock: the post-sync hook, the launchd fallback, or a CLI push.
const mirrorPushBusyDetail = "A push is already running (a sync's post-sync push, the scheduled push, or `udl navidrome push`); " +
	"this one was skipped and nothing changed. Try again when it finishes."

// navidromeMirrorPush runs the same push, scan and Date Added sequence as
// `udl navidrome push`. Only remote mode has a mirror; a local install is
// refused before any run starts.
func (s *Server) navidromeMirrorPush(params json.RawMessage) (any, *RPCError) {
	var request navidromeMirrorPushParams
	if len(params) > 0 && string(params) != "null" {
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, NewRPCError(CodeInvalidParams, "invalid navidrome.mirror.push params", nil)
		}
	}
	if request.AllowDeletes < 0 {
		return nil, NewRPCError(CodeInvalidParams, "allow_deletes must not be negative", nil)
	}
	manager, rpcErr := s.navidromeManager(false)
	if rpcErr != nil {
		return nil, rpcErr
	}
	if !manager.Config.IsRemote() {
		return nil, NewRPCError(CodeInvalidParams, "the mirror push needs remote mode", map[string]string{
			"detail": "navidrome.mode is local: Navidrome on this Mac reads the music folder directly, so there is no mirror to push",
		})
	}
	runID, err := s.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		return pushNavidromeMirror(ctx, manager, request)
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func pushNavidromeMirror(ctx context.Context, manager *navidrome.Manager, request navidromeMirrorPushParams) (any, error, int) {
	mirrored, pushErr := manager.SyncMirror(ctx, navidrome.ManualPushOptions(request.AllowDeletes, request.ForceScan))
	result := navidromeMirrorPushResult{MirrorResult: mirrored}
	if result.Warnings == nil {
		result.Warnings = []string{}
	}
	if state, err := navidrome.LoadMirrorState(manager.Resolved); err == nil {
		result.Mirror = &state
	} else {
		result.Warnings = append(result.Warnings, err.Error())
	}
	if pushErr == nil && ctx.Err() != nil {
		// SyncMirror reports a scan or Date Added step cut short as a
		// warning, but the run itself was canceled. The partial result and
		// the recorded pending flags stay; the next push finishes the rest.
		pushErr = fmt.Errorf("%s: %w", navidrome.MirrorCanceledAfterPush, ctx.Err())
	}
	if pushErr == nil {
		return result, nil, exitcode.Success
	}
	failure := classifyMirrorPushError(ctx, pushErr, mirrored.Push)
	result.Failure = &failure
	switch failure.Kind {
	case mirrorPushCanceled:
		return result, pushErr, exitcode.Interrupted
	case mirrorPushBusy:
		return result, errors.New(failure.Detail), exitcode.RuntimeFailure
	}
	return result, pushErr, exitcode.RuntimeFailure
}

func classifyMirrorPushError(ctx context.Context, err error, push navidrome.PushResult) navidromeMirrorPushFailure {
	switch {
	case errors.Is(err, navidrome.ErrMirrorBusy):
		return navidromeMirrorPushFailure{Kind: mirrorPushBusy, Detail: mirrorPushBusyDetail}
	case errors.Is(err, navidrome.ErrDeleteLimit):
		// The error itself names the planned count and the override.
		return navidromeMirrorPushFailure{
			Kind:                mirrorPushDeleteLimit,
			Detail:              err.Error(),
			PlannedDeletes:      push.PlannedDeletes,
			AllowDeletesCommand: fmt.Sprintf("udl navidrome push --allow-deletes %d", push.PlannedDeletes),
		}
	case errors.Is(err, context.Canceled), ctx.Err() != nil:
		return navidromeMirrorPushFailure{Kind: mirrorPushCanceled, Detail: err.Error()}
	case errors.Is(err, navidrome.ErrServerScanning):
		// Deferred before the transfer: nothing changed, and the next push
		// (the scheduled one included) tries again.
		return navidromeMirrorPushFailure{Kind: mirrorPushServerScanning, Detail: err.Error()}
	}
	return navidromeMirrorPushFailure{Kind: mirrorPushFailed, Detail: err.Error()}
}

func navidromeRPCError(err error) *RPCError {
	return NewRPCError(CodeInternalError, "navidrome operation failed", map[string]string{"detail": err.Error()})
}

// Setup and explicit registration mutate the same destination as the editor,
// so they must share its read/merge/write transaction lock.
func (s *Server) writeNavidromeDefinitions() ([]string, error) {
	path, rpcErr := s.resolvePlaylistsWritePath()
	if rpcErr != nil {
		return nil, rpcErr
	}
	unlock := s.lockConfigDestination(path)
	defer unlock()
	return playlists.WriteNavidromeDefinitions(path, s.WorkingDir)
}
