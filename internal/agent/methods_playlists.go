package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/jaa/update-downloads/internal/playlists"
)

type playlistListRow struct {
	Definition    playlists.Definition `json:"definition"`
	Snapshot      *playlists.Snapshot  `json:"snapshot,omitempty"`
	SnapshotError string               `json:"snapshot_error,omitempty"`
}

type playlistIDParams struct {
	PlaylistID string `json:"playlist_id"`
}

type playlistConfigResult struct {
	Path    string           `json:"path"`
	Config  playlists.Config `json:"config"`
	Content string           `json:"content"`
}

func (s *Server) listPlaylists() (any, *RPCError) {
	main, playlistConfig, rpcErr := s.loadPlaylistConfigs()
	if rpcErr != nil {
		return nil, rpcErr
	}
	rows := make([]playlistListRow, 0, len(playlistConfig.Playlists))
	for _, definition := range playlistConfig.Playlists {
		row := playlistListRow{Definition: definition}
		snapshot, err := playlists.LoadSnapshot(main.Defaults.StateDir, definition.ID)
		if err == nil {
			row.Snapshot = &snapshot
		} else if !errors.Is(err, os.ErrNotExist) {
			row.SnapshotError = err.Error()
		}
		rows = append(rows, row)
	}
	return map[string]any{"playlists": rows}, nil
}

func (s *Server) showPlaylist(params json.RawMessage) (any, *RPCError) {
	request, rpcErr := parsePlaylistID(params)
	if rpcErr != nil {
		return nil, rpcErr
	}
	main, playlistConfig, rpcErr := s.loadPlaylistConfigs()
	if rpcErr != nil {
		return nil, rpcErr
	}
	definition, ok := playlistConfig.Definition(request.PlaylistID)
	if !ok {
		return nil, NewRPCError(CodeInvalidParams, "playlist is not configured", map[string]string{"playlist_id": request.PlaylistID})
	}
	snapshot, err := playlists.LoadSnapshot(main.Defaults.StateDir, definition.ID)
	if err != nil {
		return nil, NewRPCError(CodeInvalidParams, "playlist snapshot is unavailable", map[string]string{
			"playlist_id": request.PlaylistID,
		})
	}
	return map[string]any{"definition": definition, "snapshot": snapshot}, nil
}

func (s *Server) listProviderPlaylists(params json.RawMessage) (any, *RPCError) {
	var request struct {
		Provider string `json:"provider"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, NewRPCError(CodeInvalidParams, "invalid playlists.providerList params", nil)
		}
	}
	if strings.TrimSpace(request.Provider) == "" {
		request.Provider = playlists.ProviderAppleMusic
	}
	runID, err := s.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		items, listErr := s.playlistService().ListProviderPlaylists(ctx, request.Provider)
		if listErr != nil {
			return nil, listErr, playlistExitCode(listErr)
		}
		return map[string]any{"playlists": items}, nil, exitcode.Success
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func (s *Server) refreshPlaylist(params json.RawMessage) (any, *RPCError) {
	request, rpcErr := parsePlaylistID(params)
	if rpcErr != nil {
		return nil, rpcErr
	}
	main, playlistConfig, rpcErr := s.loadPlaylistConfigs()
	if rpcErr != nil {
		return nil, rpcErr
	}
	definition, ok := playlistConfig.Definition(request.PlaylistID)
	if !ok {
		return nil, NewRPCError(CodeInvalidParams, "playlist is not configured", map[string]string{"playlist_id": request.PlaylistID})
	}
	runID, err := s.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		result, refreshErr := s.playlistService().Refresh(ctx, main, definition)
		if refreshErr != nil {
			return nil, refreshErr, playlistExitCode(refreshErr)
		}
		return result, nil, exitcode.Success
	})
	if err != nil {
		return nil, asRPCError(err)
	}
	return map[string]string{"run_id": runID}, nil
}

func (s *Server) savePlaylistDefinition(params json.RawMessage) (any, *RPCError) {
	var request struct {
		Definition playlists.Definition `json:"definition"`
	}
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "invalid playlists.saveDefinition params", nil)
	}
	cfg, err := playlists.Load(playlists.LoadOptions{
		ExplicitPath: s.PlaylistsConfigPath, WorkingDir: s.WorkingDir,
	})
	if err != nil {
		return nil, playlistRPCError(err)
	}
	replaced := false
	for index := range cfg.Playlists {
		if cfg.Playlists[index].ID == request.Definition.ID {
			cfg.Playlists[index] = request.Definition
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Playlists = append(cfg.Playlists, request.Definition)
	}
	path, rpcErr := s.resolvePlaylistsWritePath()
	if rpcErr != nil {
		return nil, rpcErr
	}
	if err := playlists.Save(path, cfg); err != nil {
		return nil, playlistRPCError(err)
	}
	return map[string]any{"path": path, "definition": request.Definition, "created": !replaced}, nil
}

func (s *Server) readPlaylistsConfig() (any, *RPCError) {
	cfg, err := playlists.Load(playlists.LoadOptions{
		ExplicitPath: s.PlaylistsConfigPath, WorkingDir: s.WorkingDir,
	})
	if err != nil {
		return nil, playlistRPCError(err)
	}
	path, rpcErr := s.resolvePlaylistsWritePath()
	if rpcErr != nil {
		return nil, rpcErr
	}
	content := ""
	if payload, readErr := os.ReadFile(path); readErr == nil {
		content = string(payload)
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, playlistRPCError(readErr)
	}
	return playlistConfigResult{Path: path, Config: cfg, Content: content}, nil
}

func (s *Server) writePlaylistsConfig(params json.RawMessage) (any, *RPCError) {
	var request struct {
		Config playlists.Config `json:"config"`
	}
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "invalid playlists.config.write params", nil)
	}
	path, rpcErr := s.resolvePlaylistsWritePath()
	if rpcErr != nil {
		return nil, rpcErr
	}
	if err := playlists.Save(path, request.Config); err != nil {
		return nil, playlistRPCError(err)
	}
	payload, err := playlists.Marshal(request.Config)
	if err != nil {
		return nil, playlistRPCError(err)
	}
	return playlistConfigResult{Path: path, Config: request.Config, Content: string(payload)}, nil
}

func (s *Server) loadPlaylistConfigs() (config.Config, playlists.Config, *RPCError) {
	main, err := config.Load(config.LoadOptions{ExplicitPath: s.ConfigPath, WorkingDir: s.WorkingDir})
	if err != nil {
		return config.Config{}, playlists.Config{}, configRPCError(err)
	}
	if len(main.Sources) > 0 {
		if err := config.Validate(main); err != nil {
			return config.Config{}, playlists.Config{}, configRPCError(err)
		}
	}
	playlistConfig, err := playlists.Load(playlists.LoadOptions{
		ExplicitPath: s.PlaylistsConfigPath, WorkingDir: s.WorkingDir,
	})
	if err != nil {
		return config.Config{}, playlists.Config{}, playlistRPCError(err)
	}
	return main, playlistConfig, nil
}

func (s *Server) playlistService() playlists.Service {
	if s.PlaylistService != nil {
		return *s.PlaylistService
	}
	return playlists.Service{}
}

func (s *Server) resolvePlaylistsWritePath() (string, *RPCError) {
	path, err := playlists.ResolveWritePath(s.PlaylistsConfigPath, s.WorkingDir)
	if err != nil {
		return "", playlistRPCError(err)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", NewRPCError(CodeInvalidParams, "invalid playlists config path", nil)
	}
	absolute = filepath.Clean(absolute)
	allowed := map[string]bool{filepath.Clean(filepath.Join(s.WorkingDir, playlists.ProjectConfigName)): true}
	if userPath, userErr := playlists.UserConfigPath(); userErr == nil {
		if userAbsolute, absErr := filepath.Abs(userPath); absErr == nil {
			allowed[filepath.Clean(userAbsolute)] = true
		}
	}
	if strings.TrimSpace(s.PlaylistsConfigPath) != "" {
		expanded, expandErr := config.ExpandPath(s.PlaylistsConfigPath)
		if expandErr != nil {
			return "", NewRPCError(CodeInvalidParams, "invalid playlists config path", nil)
		}
		explicitAbsolute, absErr := filepath.Abs(expanded)
		if absErr != nil {
			return "", NewRPCError(CodeInvalidParams, "invalid playlists config path", nil)
		}
		allowed = map[string]bool{filepath.Clean(explicitAbsolute): true}
	}
	if !allowed[absolute] {
		return "", NewRPCError(CodeInvalidParams, "playlists config path is outside the initialized session scope", map[string]string{"path": absolute})
	}
	if info, statErr := os.Lstat(absolute); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", NewRPCError(CodeInvalidParams, "symbolic-link playlists config paths are not allowed", map[string]string{"path": absolute})
	}
	return absolute, nil
}

func parsePlaylistID(params json.RawMessage) (playlistIDParams, *RPCError) {
	var request playlistIDParams
	if err := json.Unmarshal(params, &request); err != nil || strings.TrimSpace(request.PlaylistID) == "" {
		return request, NewRPCError(CodeInvalidParams, "playlist_id must be set", nil)
	}
	request.PlaylistID = strings.TrimSpace(request.PlaylistID)
	return request, nil
}

func playlistExitCode(err error) int {
	if errors.Is(err, context.Canceled) {
		return exitcode.Interrupted
	}
	return exitcode.RuntimeFailure
}

func playlistRPCError(err error) *RPCError {
	return NewRPCError(CodeInvalidParams, err.Error(), nil)
}

func asRPCError(err error) *RPCError {
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) {
		return rpcErr
	}
	return NewRPCError(CodeInternalError, err.Error(), nil)
}
