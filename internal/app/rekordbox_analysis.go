package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/rekordbox/bridge"
	"github.com/jaa/update-downloads/internal/rekordbox/playlistsync"
	"github.com/jaa/update-downloads/internal/rekordbox/pyruntime"
)

type RekordboxAnalysisBridge interface {
	AnalysisInspect(context.Context, bridge.AnalysisInspectRequest) (bridge.AnalysisInspectResponse, error)
	AnalysisApply(context.Context, bridge.AnalysisApplyRequest) (bridge.AnalysisApplyResponse, error)
}

type RekordboxAnalysisRequest struct {
	Config                                            config.Config
	DBDir, BackupDir, PythonBin, PythonPath, Analyser string
	ContentIDs, PathPrefixes                          []string
	Playlist                                          string
	All, Reanalyse, DryRun, AllowRunningOnCopy        bool
	backupTaken                                       string
	Mode, KeyNotation                                 string
}

type AnalysisTrackResult struct {
	Content  bridge.AnalysisContent `json:"content"`
	Status   string                 `json:"status"`
	Reason   string                 `json:"reason,omitempty"`
	Analysis *bridge.AnalysisResult `json:"analysis,omitempty"`
}

type RekordboxAnalysisResult struct {
	DBDir       string                `json:"db_dir"`
	BackupPath  string                `json:"backup_path,omitempty"`
	KeyNotation string                `json:"key_notation"`
	Tracks      []AnalysisTrackResult `json:"tracks"`
}

func (u RekordboxAnalysisUseCase) normalized(req RekordboxAnalysisRequest) (RekordboxAnalysisRequest, error) {
	resolved, err := playlistsync.ResolveOptions(req.Config, playlistsync.Options{RekordboxDBDir: req.DBDir, BackupDir: req.BackupDir, PythonBin: req.PythonBin, PythonPath: req.PythonPath})
	if err != nil {
		return req, err
	}
	req.DBDir = resolved.RekordboxDBDir
	req.BackupDir = resolved.BackupDir
	if req.AllowRunningOnCopy {
		if err := playlistsync.ValidateLibraryCopy(req.DBDir); err != nil {
			return req, err
		}
	}
	if req.Mode == "" {
		req.Mode = "rbxport"
	}
	if req.Mode != "rbxport" && req.Mode != "rekordbox" {
		return req, fmt.Errorf("--mode must be rbxport or rekordbox")
	}
	if req.KeyNotation == "" {
		req.KeyNotation = "auto"
	}
	if req.KeyNotation != "auto" && req.KeyNotation != "classic" && req.KeyNotation != "alphanumeric" {
		return req, fmt.Errorf("--key-notation must be classic, alphanumeric, or auto")
	}
	if !req.All && len(req.ContentIDs) == 0 && len(req.PathPrefixes) == 0 && strings.TrimSpace(req.Playlist) == "" {
		return req, fmt.Errorf("at least one selector is required: --content-id, --path-prefix, --playlist, or --all")
	}
	for _, id := range req.ContentIDs {
		if strings.TrimSpace(id) == "" {
			return req, fmt.Errorf("--content-id must not be empty")
		}
	}
	for i, prefix := range req.PathPrefixes {
		if strings.TrimSpace(prefix) == "" {
			return req, fmt.Errorf("--path-prefix must not be empty")
		}
		expanded, err := config.ExpandPath(prefix)
		if err != nil {
			return req, err
		}
		req.PathPrefixes[i], err = filepath.Abs(expanded)
		if err != nil {
			return req, err
		}
	}
	return req, nil
}

type RekordboxAnalysisUseCase struct {
	Bridge       RekordboxAnalysisBridge
	CheckClosed  func(context.Context, string) error
	CreateBackup func(context.Context, string, string, time.Time) (string, error)
	RunAnalyser  func(context.Context, string, string, string, string, string, string) (bridge.AnalysisResult, error)
	Now          func() time.Time
}

func (u RekordboxAnalysisUseCase) Plan(ctx context.Context, req RekordboxAnalysisRequest) (RekordboxAnalysisResult, error) {
	req, err := u.normalized(req)
	if err != nil {
		return RekordboxAnalysisResult{}, err
	}
	client, err := u.bridge(ctx, req)
	if err != nil {
		return RekordboxAnalysisResult{}, err
	}
	notation := req.KeyNotation
	inspect, err := client.AnalysisInspect(ctx, bridge.AnalysisInspectRequest{DBDir: req.DBDir, ContentIDs: req.ContentIDs, PathPrefixes: req.PathPrefixes, Playlist: req.Playlist, All: req.All})
	if err != nil {
		return RekordboxAnalysisResult{}, err
	}
	if notation == "auto" {
		notation = inspect.KeyNotation
		if notation == "" {
			notation = "classic"
		}
	}
	result := RekordboxAnalysisResult{DBDir: req.DBDir, KeyNotation: notation}
	for _, track := range inspect.Contents {
		analysed := track.ANLZExists && strings.TrimSpace(track.AnalysisDataPath) != ""
		status := "ready"
		reason := ""
		if analysed && !req.Reanalyse {
			status = "skipped"
			reason = "already analysed"
		}
		if status == "ready" && track.Analysed&128 != 0 {
			status = "skipped"
			reason = "analysis is locked"
		}
		if status == "ready" && strings.TrimSpace(track.Path) == "" {
			status = "skipped"
			reason = "missing audio path"
		}
		result.Tracks = append(result.Tracks, AnalysisTrackResult{Content: track, Status: status, Reason: reason})
	}
	return result, nil
}

func (u RekordboxAnalysisUseCase) Apply(ctx context.Context, req RekordboxAnalysisRequest, planned RekordboxAnalysisResult) (RekordboxAnalysisResult, error) {
	req, err := u.normalized(req)
	if err != nil {
		return planned, err
	}
	if req.DryRun {
		return planned, nil
	}
	if planned.DBDir != req.DBDir {
		return planned, fmt.Errorf("analysis plan targets a different library")
	}
	ready := false
	for _, row := range planned.Tracks {
		if row.Status == "ready" {
			ready = true
			break
		}
	}
	if !ready {
		return planned, nil
	}
	if err := u.checkClosedRequest(ctx, req); err != nil {
		return planned, err
	}
	if err := checkAnalysisLibraryLinks(req.DBDir); err != nil {
		return planned, err
	}
	analyser, err := resolveAnalyser(req.Analyser)
	if err != nil {
		return planned, err
	}
	if req.backupTaken == "" {
		backup, err := u.backup(ctx, req.DBDir, req.BackupDir, u.now())
		if err != nil {
			return planned, err
		}
		planned.BackupPath = backup
	}
	tmpRoot, err := os.MkdirTemp("", "udl-rbl-analysis-")
	if err != nil {
		return planned, err
	}
	defer os.RemoveAll(tmpRoot)
	apply := bridge.AnalysisApplyRequest{DBDir: req.DBDir, Reanalyse: req.Reanalyse}
	for i := range planned.Tracks {
		row := &planned.Tracks[i]
		if row.Status != "ready" {
			continue
		}
		out := filepath.Join(tmpRoot, fmt.Sprintf("%d", i))
		if err := os.MkdirAll(out, 0700); err != nil {
			row.Status = "failed"
			row.Reason = err.Error()
			continue
		}
		result, err := u.runAnalyser(ctx, analyser, row.Content.Path, req.Mode, out, row.Content.ExistingDir, planned.KeyNotation)
		if err == nil {
			err = validateAnalysisResult(result, req.Mode)
		}
		if err != nil {
			row.Status = "failed"
			row.Reason = err.Error()
			continue
		}
		row.Analysis = &result
		apply.Rows = append(apply.Rows, bridge.AnalysisApplyRow{Content: row.Content, OutputDir: out, Result: result})
	}
	if len(apply.Rows) == 0 {
		return planned, nil
	}
	client, err := u.bridge(ctx, req)
	if err != nil {
		return planned, err
	}
	if err := u.checkClosedRequest(ctx, req); err != nil {
		return planned, err
	}
	if err := checkAnalysisLibraryLinks(req.DBDir); err != nil {
		return planned, err
	}
	applied, err := client.AnalysisApply(ctx, apply)
	if err != nil {
		return planned, err
	}
	for i := range planned.Tracks {
		if planned.Tracks[i].Status == "ready" {
			planned.Tracks[i].Status = "failed"
			planned.Tracks[i].Reason = "bridge did not confirm analysis write"
		}
	}
	for _, item := range applied.Rows {
		for i := range planned.Tracks {
			if planned.Tracks[i].Content.ID != item.ID || planned.Tracks[i].Analysis == nil {
				continue
			}
			if item.Error != "" {
				planned.Tracks[i].Status = "failed"
				planned.Tracks[i].Reason = item.Error
			} else {
				planned.Tracks[i].Status = "analysed"
				planned.Tracks[i].Reason = ""
			}
		}
	}
	return planned, nil
}

func (u RekordboxAnalysisUseCase) runAnalyser(ctx context.Context, bin, audio, mode, out, existing, notation string) (bridge.AnalysisResult, error) {
	if u.RunAnalyser != nil {
		return u.RunAnalyser(ctx, bin, audio, mode, out, existing, notation)
	}
	args := []string{"--audio", audio, "--mode", mode, "--out", out, "--key-notation", notation}
	if existing != "" {
		args = append(args, "--existing", existing)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return bridge.AnalysisResult{}, fmt.Errorf("analyser: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var result bridge.AnalysisResult
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		return result, fmt.Errorf("decode analyser output: %w", err)
	}
	return result, nil
}

func checkAnalysisLibraryLinks(dbDir string) error {
	for _, name := range []string{"master.db", "share"} {
		path := filepath.Join(dbDir, name)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect analysis library path %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("cannot write analysis: %q is a symlink; the backup would store the link instead of its contents", path)
		}
	}
	return nil
}

func resolveAnalyser(explicit string) (string, error) {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return exec.LookPath(explicit)
	}
	if env := strings.TrimSpace(os.Getenv("UDL_RBL_ANALYSE")); env != "" {
		return exec.LookPath(env)
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "udl-rbl-analyse")
		if runtime.GOOS == "windows" {
			candidate += ".exe"
		}
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			if p, err := exec.LookPath(candidate); err == nil {
				return p, nil
			}
		}
	}
	if p, err := exec.LookPath("udl-rbl-analyse"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("udl-rbl-analyse not found; set --analyser or UDL_RBL_ANALYSE")
}
func (u RekordboxAnalysisUseCase) bridge(ctx context.Context, req RekordboxAnalysisRequest) (RekordboxAnalysisBridge, error) {
	if u.Bridge != nil {
		return u.Bridge, nil
	}
	rt, err := (pyruntime.Resolver{}).Ensure(ctx, pyruntime.Request{Config: req.Config, PythonBin: req.PythonBin, PythonPath: req.PythonPath, ExplicitPython: req.PythonBin != "" || req.PythonPath != ""})
	if err != nil {
		return nil, err
	}
	return bridge.Client{PythonBin: rt.PythonBin, PythonPath: rt.PythonPath}, nil
}
func (u RekordboxAnalysisUseCase) backup(ctx context.Context, db, root string, now time.Time) (string, error) {
	if u.CreateBackup != nil {
		return u.CreateBackup(ctx, db, root, now)
	}
	return playlistsync.CreateBackup(ctx, db, root, now)
}
func (u RekordboxAnalysisUseCase) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now()
}

func (u RekordboxAnalysisUseCase) checkClosedRequest(ctx context.Context, req RekordboxAnalysisRequest) error {
	if u.CheckClosed != nil {
		return u.CheckClosed(ctx, req.DBDir)
	}
	return playlistsync.CheckRekordboxClosedForCopy(ctx, req.DBDir, req.AllowRunningOnCopy)
}

func validateAnalysisResult(result bridge.AnalysisResult, mode string) error {
	if result.Mode != mode || result.AnalyserVersion == "" || math.IsNaN(result.BPM) || math.IsInf(result.BPM, 0) || result.BPM < 0 || math.IsNaN(result.DurationSeconds) || math.IsInf(result.DurationSeconds, 0) || result.DurationSeconds <= 0 {
		return fmt.Errorf("invalid analyser result")
	}
	expected := []string{"ANLZ0000.DAT", "ANLZ0000.EXT", "ANLZ0000.2EX"}
	if len(result.Files) != len(expected) {
		return fmt.Errorf("analyser must produce all three ANLZ files")
	}
	for i, name := range expected {
		if result.Files[i] != name {
			return fmt.Errorf("unexpected analyser file %q", result.Files[i])
		}
	}
	return nil
}
