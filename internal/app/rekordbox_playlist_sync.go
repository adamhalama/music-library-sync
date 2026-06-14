package app

import (
	"context"
	"fmt"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/rekordbox/bridge"
	"github.com/jaa/update-downloads/internal/rekordbox/music"
	"github.com/jaa/update-downloads/internal/rekordbox/playlistsync"
	"github.com/jaa/update-downloads/internal/rekordbox/pyruntime"
)

type RekordboxMusicReader interface {
	ListPlaylists(ctx context.Context) ([]music.Playlist, error)
	ReadPlaylist(ctx context.Context, selector music.PlaylistSelector) (music.Playlist, []music.Track, error)
}

type RekordboxBridge interface {
	Inspect(ctx context.Context, dbDir string) (bridge.InspectResponse, error)
	Apply(ctx context.Context, req bridge.ApplyRequest) (bridge.ApplyResponse, error)
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
	Config  config.Config
	Options playlistsync.Options
}

type RekordboxPlaylistSyncPlanResult struct {
	Resolved playlistsync.ResolvedOptions
	Plan     playlistsync.Plan
	PlanPath string
}

type RekordboxPlaylistSyncApplyRequest struct {
	Config     config.Config
	Plan       playlistsync.Plan
	PythonBin  string
	PythonPath string
	BackupDir  string
	DryRun     bool
}

type RekordboxPlaylistSyncApplyResult struct {
	DryRun     bool
	BackupPath string
	Response   bridge.ApplyResponse
}

func (u RekordboxPlaylistSyncUseCase) Plan(ctx context.Context, req RekordboxPlaylistSyncPlanRequest) (RekordboxPlaylistSyncPlanResult, error) {
	if err := config.ValidateRekordbox(req.Config); err != nil {
		return RekordboxPlaylistSyncPlanResult{}, err
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

func (u RekordboxPlaylistSyncUseCase) Apply(ctx context.Context, req RekordboxPlaylistSyncApplyRequest) (RekordboxPlaylistSyncApplyResult, error) {
	if err := config.ValidateRekordbox(req.Config); err != nil {
		return RekordboxPlaylistSyncApplyResult{}, err
	}
	resolved, err := playlistsync.ResolveOptions(req.Config, playlistsync.Options{
		PythonBin:  req.PythonBin,
		PythonPath: req.PythonPath,
		BackupDir:  req.BackupDir,
	})
	if err != nil {
		return RekordboxPlaylistSyncApplyResult{}, err
	}
	resolved, err = u.ensureRuntime(ctx, resolved, pyruntime.Request{
		Config:         req.Config,
		PythonBin:      req.PythonBin,
		PythonPath:     req.PythonPath,
		ExplicitPython: req.PythonBin != "" || req.PythonPath != "",
	})
	if err != nil {
		return RekordboxPlaylistSyncApplyResult{}, err
	}
	plan := req.Plan
	if plan.BackupDir == "" {
		plan.BackupDir = resolved.BackupDir
	}
	if err := playlistsync.ValidatePlanForApply(plan); err != nil {
		return RekordboxPlaylistSyncApplyResult{}, err
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
		return RekordboxPlaylistSyncApplyResult{DryRun: true}, nil
	}

	backupPath, err := u.createBackup(ctx, plan.RekordboxDBDir, plan.BackupDir, u.now())
	if err != nil {
		return RekordboxPlaylistSyncApplyResult{}, err
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
	if !sameStringSlice(resp.FinalContentIDs, plan.FinalContentIDs) {
		return RekordboxPlaylistSyncApplyResult{}, fmt.Errorf("post-apply verification failed: final playlist order does not match plan")
	}
	return RekordboxPlaylistSyncApplyResult{BackupPath: backupPath, Response: resp}, nil
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

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
