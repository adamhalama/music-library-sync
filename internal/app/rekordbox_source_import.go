package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/rekordbox/bridge"
	"github.com/jaa/update-downloads/internal/rekordbox/playlistsync"
	"github.com/jaa/update-downloads/internal/rekordbox/pyruntime"
	"github.com/jaa/update-downloads/internal/rekordbox/sourceimport"
)

// RekordboxSourceImportBridge is the part of the pyrekordbox bridge the source
// import needs.
type RekordboxSourceImportBridge interface {
	SourceImportInspect(ctx context.Context, req bridge.SourceImportInspectRequest) (bridge.SourceImportInspectResponse, error)
	SourceImportApply(ctx context.Context, req bridge.SourceImportApplyRequest) (bridge.SourceImportApplyResponse, error)
}

// RekordboxSourceImportUseCase imports a udl Spotify source's local files into
// the Rekordbox collection with Date Added in the playlist's own order. The CLI
// and the TUI share it; every side effect has a seam for tests.
type RekordboxSourceImportUseCase struct {
	Bridge        RekordboxSourceImportBridge
	FetchOrder    func(ctx context.Context, playlistURL string) ([]engine.SpotifyPlaylistTrack, error)
	LoadState     func(stateFilePath string) (map[string]engine.SpotifyStateEntry, error)
	TagReader     sourceimport.TagReader
	FileExists    func(path string) bool
	Now           func() time.Time
	Location      *time.Location
	CheckClosed   func(context.Context, string) error
	CreateBackup  func(context.Context, string, string, time.Time) (string, error)
	EnsureRuntime func(context.Context, pyruntime.Request) (pyruntime.Runtime, error)
	Analyse       func(context.Context, RekordboxAnalysisRequest) (RekordboxAnalysisResult, error)
}

type RekordboxSourceImportRequest struct {
	Config             config.Config
	SourceID           string
	DBDir              string
	BackupDir          string
	PythonBin          string
	PythonPath         string
	GenreOverride      string
	Step               time.Duration
	Analyse            bool
	AnalysisMode       string
	Analyser           string
	KeyNotation        string
	AllowRunningOnCopy bool
	DryRun             bool
}

type RekordboxSourceImportResult struct {
	Plan        sourceimport.Plan
	DryRun      bool
	Applied     bool
	BackupPath  string
	Analysis    *RekordboxAnalysisResult
	AppliedRows []bridge.SourceImportAppliedRow
}

// Plan builds the import plan without writing anything. Rekordbox and RBxport
// may be open.
func (u RekordboxSourceImportUseCase) Plan(ctx context.Context, req RekordboxSourceImportRequest) (sourceimport.Plan, error) {
	plan, _, err := u.plan(ctx, req)
	return plan, err
}

// Apply writes a plan built by Plan: it checks that Rekordbox and RBxport are
// closed, backs up the Rekordbox directory and applies every changing row in
// one transaction. The helper re-checks each row against the state it was
// planned from, so a collection edited since planning aborts the write.
func (u RekordboxSourceImportUseCase) Apply(ctx context.Context, req RekordboxSourceImportRequest, plan sourceimport.Plan) (RekordboxSourceImportResult, error) {
	result := RekordboxSourceImportResult{Plan: plan, DryRun: req.DryRun}
	if err := sourceimport.VerifyTimeline(plan.TimelineRows()); err != nil {
		return result, err
	}
	if req.DryRun || !plan.HasChanges() {
		return result, nil
	}
	if err := u.checkImportClosed(ctx, plan.DBDir, req.AllowRunningOnCopy); err != nil {
		return result, err
	}
	client, err := u.bridge(ctx, req.Config, req)
	if err != nil {
		return result, err
	}
	applyReq := sourceImportApplyRequest(plan)
	backupPath, err := u.createBackup(ctx, plan.DBDir, plan.BackupDir, u.now())
	if err != nil {
		return result, err
	}
	result.BackupPath = backupPath
	if err := u.checkImportClosed(ctx, plan.DBDir, req.AllowRunningOnCopy); err != nil {
		return result, err
	}
	resp, err := client.SourceImportApply(ctx, applyReq)
	if err != nil {
		return result, fmt.Errorf("apply source import: %w", err)
	}
	if err := verifySourceImport(applyReq, resp); err != nil {
		return result, err
	}
	result.Applied = true
	result.AppliedRows = resp.Rows
	if req.Analyse {
		analysisReq := RekordboxAnalysisRequest{Config: req.Config, DBDir: plan.DBDir, BackupDir: plan.BackupDir, PythonBin: req.PythonBin, PythonPath: req.PythonPath, Mode: req.AnalysisMode, Analyser: req.Analyser, KeyNotation: req.KeyNotation, AllowRunningOnCopy: req.AllowRunningOnCopy, backupTaken: backupPath}
		for i, row := range applyReq.Rows {
			if row.Action == bridge.SourceImportActionImport {
				analysisReq.ContentIDs = append(analysisReq.ContentIDs, resp.Rows[i].ContentID)
			}
		}
		if len(analysisReq.ContentIDs) > 0 {
			var analysed RekordboxAnalysisResult
			if u.Analyse != nil {
				analysed, err = u.Analyse(ctx, analysisReq)
			} else {
				use := RekordboxAnalysisUseCase{CheckClosed: u.CheckClosed}
				var planned RekordboxAnalysisResult
				planned, err = use.Plan(ctx, analysisReq)
				if err == nil {
					analysed, err = use.Apply(ctx, analysisReq, planned)
				}
			}
			result.Analysis = &analysed
			if err != nil {
				return result, fmt.Errorf("import committed; analysis failed: %w", err)
			}
		}
	}
	return result, nil
}

func sourceImportApplyRequest(plan sourceimport.Plan) bridge.SourceImportApplyRequest {
	applyReq := bridge.SourceImportApplyRequest{DBDir: plan.DBDir}
	for _, row := range plan.Rows {
		if !row.Changes() {
			continue
		}
		applyRow := bridge.SourceImportApplyRow{
			Key:       row.SpotifyID,
			Path:      row.Path,
			CreatedAt: row.NewCreatedAt,
			StockDate: row.NewStockDate,
		}
		switch row.Status {
		case sourceimport.StatusExisting:
			applyRow.Action = bridge.SourceImportActionUpdate
			applyRow.ContentID = row.ContentID
			applyRow.ExpectedCreatedAt = row.OldCreatedAt
		case sourceimport.StatusImport:
			applyRow.Action = bridge.SourceImportActionImport
			applyRow.Tags = bridgeTags(row.Tags)
		}
		applyReq.Rows = append(applyReq.Rows, applyRow)
	}
	return applyReq
}

func (u RekordboxSourceImportUseCase) plan(ctx context.Context, req RekordboxSourceImportRequest) (sourceimport.Plan, RekordboxSourceImportBridge, error) {
	if req.Analyse {
		if req.AnalysisMode != "" && req.AnalysisMode != "rbxport" && req.AnalysisMode != "rekordbox" {
			return sourceimport.Plan{}, nil, fmt.Errorf("--analysis-mode must be rbxport or rekordbox")
		}
		if req.KeyNotation != "" && req.KeyNotation != "auto" && req.KeyNotation != "classic" && req.KeyNotation != "alphanumeric" {
			return sourceimport.Plan{}, nil, fmt.Errorf("invalid key notation")
		}
	}
	cfg := req.Config
	source, err := findSpotifySource(cfg, req.SourceID)
	if err != nil {
		return sourceimport.Plan{}, nil, err
	}
	targetDir, err := config.ExpandPath(source.TargetDir)
	if err != nil {
		return sourceimport.Plan{}, nil, fmt.Errorf("resolve target_dir for %s: %w", source.ID, err)
	}
	statePath, err := config.ResolveStateFile(cfg.Defaults.StateDir, source.StateFile)
	if err != nil {
		return sourceimport.Plan{}, nil, fmt.Errorf("resolve state_file for %s: %w", source.ID, err)
	}

	resolved, err := playlistsync.ResolveOptions(cfg, playlistsync.Options{
		RekordboxDBDir: req.DBDir,
		PythonBin:      req.PythonBin,
		PythonPath:     req.PythonPath,
		BackupDir:      req.BackupDir,
	})
	if err != nil {
		return sourceimport.Plan{}, nil, err
	}
	dbDir, err := config.ExpandPath(firstNonEmptyString(req.DBDir, resolved.RekordboxDBDir))
	if err != nil {
		return sourceimport.Plan{}, nil, fmt.Errorf("resolve Rekordbox db dir: %w", err)
	}
	backupDir, err := config.ExpandPath(resolved.BackupDir)
	if err != nil {
		return sourceimport.Plan{}, nil, fmt.Errorf("resolve backup dir: %w", err)
	}

	client, err := u.bridge(ctx, cfg, req)
	if err != nil {
		return sourceimport.Plan{}, nil, err
	}

	order, err := u.fetchOrder(ctx, source.URL)
	if err != nil {
		return sourceimport.Plan{}, nil, fmt.Errorf("read Spotify playlist order: %w", err)
	}
	state, err := u.loadState(statePath)
	if err != nil {
		return sourceimport.Plan{}, nil, fmt.Errorf("read sync state %s: %w", statePath, err)
	}
	tracks := make([]sourceimport.SourceTrack, 0, len(order))
	for _, item := range order {
		track := sourceimport.SourceTrack{
			Position:  item.Position,
			SpotifyID: item.ID,
			Title:     item.Title,
			Artist:    item.Artist,
		}
		if entry, ok := state[item.ID]; ok && strings.TrimSpace(entry.LocalPath) != "" {
			track.Path = entry.LocalPath
			if !filepath.IsAbs(track.Path) {
				track.Path = filepath.Join(targetDir, track.Path)
			}
		}
		tracks = append(tracks, track)
	}

	inspect, err := client.SourceImportInspect(ctx, bridge.SourceImportInspectRequest{DBDir: dbDir, FolderPrefix: targetDir})
	if err != nil {
		return sourceimport.Plan{}, nil, err
	}
	contents := make([]sourceimport.Content, 0, len(inspect.Contents))
	for _, row := range inspect.Contents {
		created, err := sourceimport.ParseTimestamp(row.CreatedAt)
		if err != nil {
			return sourceimport.Plan{}, nil, fmt.Errorf("Rekordbox track %s: %w", row.ID, err)
		}
		contents = append(contents, sourceimport.Content{
			ID:         row.ID,
			FolderPath: row.FolderPath,
			CreatedAt:  created,
			StockDate:  row.StockDate,
		})
	}

	step := req.Step
	if step == 0 {
		step = time.Second
	}
	plan, err := sourceimport.BuildPlan(ctx, sourceimport.Input{
		SourceID:      source.ID,
		PlaylistURL:   source.URL,
		TargetDir:     targetDir,
		DBDir:         dbDir,
		BackupDir:     backupDir,
		Tracks:        tracks,
		Contents:      contents,
		ImportTime:    u.now(),
		Step:          step,
		Location:      u.Location,
		GenreOverride: req.GenreOverride,
		FileExists:    u.fileExists(),
		TagReader:     u.tagReader(),
	})
	if err != nil {
		return sourceimport.Plan{}, nil, err
	}
	if err := sourceimport.VerifyTimeline(plan.TimelineRows()); err != nil {
		return sourceimport.Plan{}, nil, fmt.Errorf("internal planning error: %w", err)
	}
	return plan, client, nil
}

func findSpotifySource(cfg config.Config, id string) (config.Source, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return config.Source{}, fmt.Errorf("source id is required")
	}
	for _, source := range cfg.Sources {
		if source.ID != id {
			continue
		}
		if source.Type != config.SourceTypeSpotify {
			return config.Source{}, fmt.Errorf("source %q is %s; only Spotify sources can be imported in playlist order", id, source.Type)
		}
		if strings.TrimSpace(source.StateFile) == "" {
			return config.Source{}, fmt.Errorf("source %q has no state_file", id)
		}
		return source, nil
	}
	return config.Source{}, fmt.Errorf("source %q not found in config", id)
}

// verifySourceImport checks the helper's read-back against what was sent:
// every row landed, at the planned Date Added.
func verifySourceImport(req bridge.SourceImportApplyRequest, resp bridge.SourceImportApplyResponse) error {
	if len(resp.Rows) != len(req.Rows) {
		return fmt.Errorf("post-apply verification failed: %d rows written, %d planned", len(resp.Rows), len(req.Rows))
	}
	for i, sent := range req.Rows {
		got := resp.Rows[i]
		if got.Key != sent.Key || !sourceimport.SamePath(got.Path, sent.Path) {
			return fmt.Errorf("post-apply verification failed: row %d is %s, planned %s", i+1, got.Path, sent.Path)
		}
		if got.CreatedAt != sent.CreatedAt || got.StockDate != sent.StockDate {
			return fmt.Errorf("post-apply verification failed: %s has Date Added %s/%s, planned %s/%s", got.Path, got.StockDate, got.CreatedAt, sent.StockDate, sent.CreatedAt)
		}
	}
	return nil
}

func bridgeTags(tags *sourceimport.Tags) *bridge.SourceImportTags {
	if tags == nil {
		return nil
	}
	return &bridge.SourceImportTags{
		Title:       tags.Title,
		Artist:      tags.Artist,
		Album:       tags.Album,
		Genre:       tags.Genre,
		Label:       tags.Label,
		Comment:     tags.Comment,
		Year:        tags.Year,
		TrackNo:     tags.TrackNo,
		DiscNo:      tags.DiscNo,
		DurationSec: tags.DurationSec,
		BitRateKbps: tags.BitRateKbps,
		SampleRate:  tags.SampleRate,
		BitDepth:    tags.BitDepth,
	}
}

func (u RekordboxSourceImportUseCase) bridge(ctx context.Context, cfg config.Config, req RekordboxSourceImportRequest) (RekordboxSourceImportBridge, error) {
	if u.Bridge != nil {
		return u.Bridge, nil
	}
	runtimeReq := pyruntime.Request{
		Config:         cfg,
		PythonBin:      req.PythonBin,
		PythonPath:     req.PythonPath,
		ExplicitPython: req.PythonBin != "" || req.PythonPath != "",
	}
	ensure := u.EnsureRuntime
	if ensure == nil {
		ensure = (pyruntime.Resolver{}).Ensure
	}
	rt, err := ensure(ctx, runtimeReq)
	if err != nil {
		return nil, err
	}
	return bridge.Client{PythonBin: rt.PythonBin, PythonPath: rt.PythonPath}, nil
}

func (u RekordboxSourceImportUseCase) fetchOrder(ctx context.Context, playlistURL string) ([]engine.SpotifyPlaylistTrack, error) {
	if u.FetchOrder != nil {
		return u.FetchOrder(ctx, playlistURL)
	}
	return engine.FetchSpotifyPlaylistOrder(ctx, playlistURL)
}

func (u RekordboxSourceImportUseCase) loadState(path string) (map[string]engine.SpotifyStateEntry, error) {
	if u.LoadState != nil {
		return u.LoadState(path)
	}
	return engine.LoadSpotifyStateEntries(path)
}

func (u RekordboxSourceImportUseCase) tagReader() sourceimport.TagReader {
	if u.TagReader != nil {
		return u.TagReader
	}
	return sourceimport.FFProbeTagReader{}
}

func (u RekordboxSourceImportUseCase) fileExists() func(string) bool {
	if u.FileExists != nil {
		return u.FileExists
	}
	return func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && info.Mode().IsRegular()
	}
}

func (u RekordboxSourceImportUseCase) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now()
}

func (u RekordboxSourceImportUseCase) checkClosed(ctx context.Context, dbDir string) error {
	if u.CheckClosed != nil {
		return u.CheckClosed(ctx, dbDir)
	}
	return playlistsync.CheckRekordboxClosed(ctx, dbDir)
}

func (u RekordboxSourceImportUseCase) createBackup(ctx context.Context, dbDir, backupRoot string, now time.Time) (string, error) {
	if u.CreateBackup != nil {
		return u.CreateBackup(ctx, dbDir, backupRoot, now)
	}
	return playlistsync.CreateBackup(ctx, dbDir, backupRoot, now)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (u RekordboxSourceImportUseCase) checkImportClosed(ctx context.Context, dbDir string, allowCopy bool) error {
	if u.CheckClosed != nil {
		return u.CheckClosed(ctx, dbDir)
	}
	return playlistsync.CheckRekordboxClosedForCopy(ctx, dbDir, allowCopy)
}
