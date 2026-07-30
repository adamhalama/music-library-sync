package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/playlists"
	"github.com/jaa/update-downloads/internal/rekordbox/bridge"
	"github.com/jaa/update-downloads/internal/rekordbox/music"
	"github.com/jaa/update-downloads/internal/rekordbox/playlistsync"
	"github.com/jaa/update-downloads/internal/rekordbox/pyruntime"
	"github.com/jaa/update-downloads/internal/rekordbox/syncconfig"
)

type RekordboxMusicReader interface {
	ListPlaylists(ctx context.Context) ([]music.Playlist, error)
	ReadPlaylist(ctx context.Context, selector music.PlaylistSelector) (music.Playlist, []music.Track, error)
}

type RekordboxBridge interface {
	Inspect(ctx context.Context, dbDir string) (bridge.InspectResponse, error)
	Apply(ctx context.Context, req bridge.ApplyRequest) (bridge.ApplyResponse, error)
	ApplyBatch(ctx context.Context, req bridge.ApplyBatchRequest) (bridge.ApplyBatchResponse, error)
}

type RekordboxPlaylistSyncUseCase struct {
	MusicReader   RekordboxMusicReader
	Bridge        RekordboxBridge
	Now           func() time.Time
	CheckClosed   func(context.Context, string) error
	CreateBackup  func(context.Context, string, string, time.Time) (string, error)
	EnsureRuntime func(context.Context, pyruntime.Request) (pyruntime.Runtime, error)
}

type RekordboxPlaylistSyncPlanRequest struct {
	Config     config.Config
	SyncConfig *syncconfig.Config
	MappingID  string
	Options    playlistsync.Options
	Snapshot   *playlists.Snapshot
}

type RekordboxPlaylistSyncPlanResult struct {
	Resolved playlistsync.ResolvedOptions `json:"resolved"`
	Plan     playlistsync.Plan            `json:"plan"`
	PlanPath string                       `json:"plan_path"`
}

type RekordboxPlaylistSyncApplyRequest struct {
	Config     config.Config
	SyncConfig *syncconfig.Config
	Plan       playlistsync.Plan
	PythonBin  string
	PythonPath string
	BackupDir  string
	DryRun     bool
}

type RekordboxPlaylistSyncApplyResult struct {
	DryRun             bool                      `json:"dry_run"`
	EffectiveBackupDir string                    `json:"effective_backup_dir"`
	BackupPath         string                    `json:"backup_path,omitempty"`
	Response           bridge.ApplyResponse      `json:"response,omitempty"`
	BatchResponse      bridge.ApplyBatchResponse `json:"batch_response,omitempty"`
}

func (u RekordboxPlaylistSyncUseCase) Plan(ctx context.Context, req RekordboxPlaylistSyncPlanRequest) (RekordboxPlaylistSyncPlanResult, error) {
	if err := config.ValidateRekordbox(req.Config); err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	if req.Snapshot != nil {
		return u.planSnapshot(ctx, req)
	}
	if req.SyncConfig != nil && req.SyncConfig.HasFolderMappings() {
		return u.planFolder(ctx, req)
	}
	resolved, err := playlistsync.ResolveOptions(req.Config, req.Options)
	if err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	resolved, err = u.ensureRuntime(ctx, resolved, pyruntime.Request{
		Config:         req.Config,
		PythonBin:      req.Options.PythonBin,
		PythonPath:     req.Options.PythonPath,
		ExplicitPython: req.Options.PythonBin != "" || req.Options.PythonPath != "",
	})
	if err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	if err := u.checkClosed(ctx, resolved.RekordboxDBDir); err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}

	reader := u.musicReader()
	playlists, err := reader.ListPlaylists(ctx)
	if err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	selected, err := playlistsync.SelectMusicPlaylist(playlists, resolved.MusicPlaylist, resolved.MusicPlaylistID)
	if err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	selector := music.PlaylistSelector{Name: selected.Name, PersistentID: selected.PersistentID}
	if resolved.MusicPlaylistID == "" {
		selector.PersistentID = ""
	}
	playlist, tracks, err := reader.ReadPlaylist(ctx, selector)
	if err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}

	inspect, err := u.bridge(resolved).Inspect(ctx, resolved.RekordboxDBDir)
	if err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	now := u.now()
	outPath, err := playlistsync.DefaultOutPath(req.Config, resolved, now)
	if err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	resolved.OutPath = outPath
	plan, err := playlistsync.BuildPlan(playlistsync.BuildRequest{
		Options:       resolved,
		MusicPlaylist: playlist,
		MusicTracks:   tracks,
		Inspect:       inspect,
	}, now)
	if err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	if err := playlistsync.WritePlan(outPath, plan); err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	return RekordboxPlaylistSyncPlanResult{
		Resolved: resolved,
		Plan:     plan,
		PlanPath: outPath,
	}, nil
}

func (u RekordboxPlaylistSyncUseCase) planSnapshot(ctx context.Context, req RekordboxPlaylistSyncPlanRequest) (RekordboxPlaylistSyncPlanResult, error) {
	snapshot := *req.Snapshot
	if err := playlists.ValidateSnapshot(snapshot); err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	resolved, err := playlistsync.ResolveOptions(req.Config, req.Options)
	if err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	resolved.MusicPlaylist = snapshot.Name
	resolved.MusicPlaylistID = snapshot.PlaylistID
	resolved, err = u.ensureRuntime(ctx, resolved, pyruntime.Request{
		Config:         req.Config,
		PythonBin:      req.Options.PythonBin,
		PythonPath:     req.Options.PythonPath,
		ExplicitPython: req.Options.PythonBin != "" || req.Options.PythonPath != "",
	})
	if err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	if err := u.checkClosed(ctx, resolved.RekordboxDBDir); err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	inspect, err := u.bridge(resolved).Inspect(ctx, resolved.RekordboxDBDir)
	if err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	tracks := make([]music.Track, 0, len(snapshot.Tracks))
	for _, track := range snapshot.Tracks {
		tracks = append(tracks, music.Track{
			Index: track.Index, PersistentID: track.ProviderID, DatabaseID: track.DatabaseID,
			Artist: track.Artist, Title: track.Title, Album: track.Album,
			Duration: track.Duration, Path: track.Path,
		})
	}
	now := u.now()
	outPath, err := playlistsync.DefaultOutPath(req.Config, resolved, now)
	if err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	resolved.OutPath = outPath
	plan, err := playlistsync.BuildPlan(playlistsync.BuildRequest{
		Options: resolved,
		MusicPlaylist: music.Playlist{
			Name: snapshot.Name, PersistentID: snapshot.PlaylistID, TrackCount: len(tracks),
		},
		MusicTracks: tracks,
		Inspect:     inspect,
	}, now)
	if err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	if err := playlistsync.WritePlan(outPath, plan); err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	return RekordboxPlaylistSyncPlanResult{Resolved: resolved, Plan: plan, PlanPath: outPath}, nil
}

func (u RekordboxPlaylistSyncUseCase) planFolder(ctx context.Context, req RekordboxPlaylistSyncPlanRequest) (RekordboxPlaylistSyncPlanResult, error) {
	mapping, ok := req.SyncConfig.FolderMapping(req.MappingID)
	if !ok {
		return RekordboxPlaylistSyncPlanResult{}, fmt.Errorf("Rekordbox folder mapping %q not found", req.MappingID)
	}
	cfg := configForSyncConfig(req.Config, *req.SyncConfig)
	resolved, err := playlistsync.ResolveOptions(cfg, playlistsync.Options{
		MappingID: req.MappingID,
		OutPath:   req.Options.OutPath,
	})
	if err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	resolved, err = u.ensureRuntime(ctx, resolved, pyruntime.Request{Config: cfg})
	if err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	if err := u.checkClosed(ctx, resolved.RekordboxDBDir); err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	reader := u.musicReader()
	playlists, err := reader.ListPlaylists(ctx)
	if err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	folder, children, err := playlistsync.SelectMusicFolderChildren(playlists, mapping)
	if err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	childTracks := make([]playlistsync.FolderMusicPlaylistTracks, 0, len(children))
	for _, child := range children {
		selector := music.PlaylistSelector{Name: child.Name, PersistentID: child.PersistentID}
		playlist, tracks, err := reader.ReadPlaylist(ctx, selector)
		if err != nil {
			return RekordboxPlaylistSyncPlanResult{}, err
		}
		childTracks = append(childTracks, playlistsync.FolderMusicPlaylistTracks{Playlist: playlist, Tracks: tracks})
	}
	inspect, err := u.bridge(resolved).Inspect(ctx, resolved.RekordboxDBDir)
	if err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	now := u.now()
	outPath, err := playlistsync.DefaultOutPath(cfg, resolved, now)
	if err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	resolved.OutPath = outPath
	plan, err := playlistsync.BuildFolderPlan(playlistsync.FolderBuildRequest{
		Options:       resolved,
		Mapping:       mapping,
		MusicFolder:   folder,
		MusicChildren: childTracks,
		Inspect:       inspect,
	}, now)
	if err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	if err := playlistsync.WritePlan(outPath, plan); err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
	}
	return RekordboxPlaylistSyncPlanResult{Resolved: resolved, Plan: plan, PlanPath: outPath}, nil
}

func (u RekordboxPlaylistSyncUseCase) Apply(ctx context.Context, req RekordboxPlaylistSyncApplyRequest) (RekordboxPlaylistSyncApplyResult, error) {
	cfg := req.Config
	if req.SyncConfig != nil {
		cfg = configForSyncConfig(req.Config, *req.SyncConfig)
	}
	if err := config.ValidateRekordbox(cfg); err != nil {
		return RekordboxPlaylistSyncApplyResult{}, err
	}
	plan := req.Plan
	if err := playlistsync.ValidatePlanForApply(plan); err != nil {
		return RekordboxPlaylistSyncApplyResult{}, err
	}
	resolved, err := playlistsync.ResolveOptions(cfg, playlistsync.Options{
		PythonBin:  req.PythonBin,
		PythonPath: req.PythonPath,
		BackupDir:  req.BackupDir,
	})
	if err != nil {
		return RekordboxPlaylistSyncApplyResult{}, err
	}
	resolved, err = u.ensureRuntime(ctx, resolved, pyruntime.Request{
		Config:         cfg,
		PythonBin:      req.PythonBin,
		PythonPath:     req.PythonPath,
		ExplicitPython: req.PythonBin != "" || req.PythonPath != "",
	})
	if err != nil {
		return RekordboxPlaylistSyncApplyResult{}, err
	}
	effectiveBackupDir := resolved.BackupDir
	if strings.TrimSpace(req.BackupDir) == "" && strings.TrimSpace(plan.BackupDir) != "" {
		effectiveBackupDir, err = config.ExpandPath(plan.BackupDir)
		if err != nil {
			return RekordboxPlaylistSyncApplyResult{}, fmt.Errorf("resolve plan backup dir: %w", err)
		}
	}
	if err := u.checkClosed(ctx, plan.RekordboxDBDir); err != nil {
		return RekordboxPlaylistSyncApplyResult{}, err
	}
	client := u.bridge(resolved)
	inspect, err := client.Inspect(ctx, plan.RekordboxDBDir)
	if err != nil {
		return RekordboxPlaylistSyncApplyResult{}, err
	}
	if err := playlistsync.ValidatePreconditions(plan, inspect); err != nil {
		return RekordboxPlaylistSyncApplyResult{}, err
	}
	if req.DryRun {
		return RekordboxPlaylistSyncApplyResult{DryRun: true, EffectiveBackupDir: effectiveBackupDir}, nil
	}

	backupPath, err := u.createBackup(ctx, plan.RekordboxDBDir, effectiveBackupDir, u.now())
	if err != nil {
		return RekordboxPlaylistSyncApplyResult{}, err
	}
	if plan.Version == playlistsync.PlanVersionFolder {
		ops := make([]bridge.ApplyRequest, 0, len(plan.Operations))
		for _, op := range plan.Operations {
			ops = append(ops, bridge.ApplyRequest{
				DBDir:                     plan.RekordboxDBDir,
				TargetPlaylistID:          op.RekordboxPlaylist.ID,
				TargetPlaylistName:        op.RekordboxPlaylist.Name,
				CreatePlaylistIfMissing:   op.RekordboxPlaylist.CreatePlanned,
				ExpectedCurrentContentIDs: op.Preconditions.ExpectedCurrentContentIDs,
				FinalContentIDs:           op.FinalContentIDs,
			})
		}
		resp, err := client.ApplyBatch(ctx, bridge.ApplyBatchRequest{
			DBDir:                 plan.RekordboxDBDir,
			TargetFolderID:        plan.RekordboxFolder.ID,
			TargetFolderName:      plan.RekordboxFolder.Name,
			CreateFolderIfMissing: plan.RekordboxFolder.CreatePlanned,
			Operations:            ops,
		})
		if err != nil {
			return RekordboxPlaylistSyncApplyResult{}, err
		}
		if len(resp.Responses) != len(plan.Operations) {
			return RekordboxPlaylistSyncApplyResult{}, fmt.Errorf("post-apply verification failed: final playlist count does not match plan")
		}
		for idx, op := range plan.Operations {
			if !playlistsync.SameStrings(resp.Responses[idx].FinalContentIDs, op.FinalContentIDs) {
				return RekordboxPlaylistSyncApplyResult{}, fmt.Errorf("post-apply verification failed: final playlist order does not match plan for %q", op.RekordboxPlaylist.Name)
			}
		}
		return RekordboxPlaylistSyncApplyResult{EffectiveBackupDir: effectiveBackupDir, BackupPath: backupPath, BatchResponse: resp}, nil
	}

	resp, err := client.Apply(ctx, bridge.ApplyRequest{
		DBDir:                     plan.RekordboxDBDir,
		TargetPlaylistID:          plan.RekordboxPlaylist.ID,
		TargetPlaylistName:        plan.RekordboxPlaylist.Name,
		CreatePlaylistIfMissing:   plan.RekordboxPlaylist.CreatePlanned,
		ExpectedCurrentContentIDs: plan.Preconditions.ExpectedCurrentContentIDs,
		FinalContentIDs:           plan.FinalContentIDs,
	})
	if err != nil {
		return RekordboxPlaylistSyncApplyResult{}, err
	}
	if !playlistsync.SameStrings(resp.FinalContentIDs, plan.FinalContentIDs) {
		return RekordboxPlaylistSyncApplyResult{}, fmt.Errorf("post-apply verification failed: final playlist order does not match plan")
	}
	return RekordboxPlaylistSyncApplyResult{EffectiveBackupDir: effectiveBackupDir, BackupPath: backupPath, Response: resp}, nil
}

func configForSyncConfig(base config.Config, rb syncconfig.Config) config.Config {
	cfg := base
	cfg.Rekordbox = &config.RekordboxConfig{
		DBDir:      rb.Defaults.DBDir,
		PythonBin:  rb.Defaults.PythonBin,
		PythonPath: rb.Defaults.PythonPath,
		BackupDir:  rb.Defaults.BackupDir,
	}
	return cfg
}

func (u RekordboxPlaylistSyncUseCase) musicReader() RekordboxMusicReader {
	if u.MusicReader != nil {
		return u.MusicReader
	}
	return music.Reader{}
}

func (u RekordboxPlaylistSyncUseCase) bridge(opts playlistsync.ResolvedOptions) RekordboxBridge {
	if u.Bridge != nil {
		return u.Bridge
	}
	return bridge.Client{PythonBin: opts.PythonBin, PythonPath: opts.PythonPath}
}

func (u RekordboxPlaylistSyncUseCase) ensureRuntime(ctx context.Context, opts playlistsync.ResolvedOptions, req pyruntime.Request) (playlistsync.ResolvedOptions, error) {
	if u.Bridge != nil {
		return opts, nil
	}
	if u.EnsureRuntime != nil {
		rt, err := u.EnsureRuntime(ctx, req)
		if err != nil {
			return playlistsync.ResolvedOptions{}, err
		}
		opts.PythonBin = rt.PythonBin
		opts.PythonPath = rt.PythonPath
		return opts, nil
	}
	rt, err := (pyruntime.Resolver{}).Ensure(ctx, req)
	if err != nil {
		return playlistsync.ResolvedOptions{}, err
	}
	opts.PythonBin = rt.PythonBin
	opts.PythonPath = rt.PythonPath
	return opts, nil
}

func (u RekordboxPlaylistSyncUseCase) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now()
}

func (u RekordboxPlaylistSyncUseCase) checkClosed(ctx context.Context, dbDir string) error {
	if u.CheckClosed != nil {
		return u.CheckClosed(ctx, dbDir)
	}
	return playlistsync.CheckRekordboxClosed(ctx, dbDir)
}

func (u RekordboxPlaylistSyncUseCase) createBackup(ctx context.Context, dbDir, backupRoot string, now time.Time) (string, error) {
	if u.CreateBackup != nil {
		return u.CreateBackup(ctx, dbDir, backupRoot, now)
	}
	return playlistsync.CreateBackup(ctx, dbDir, backupRoot, now)
}
