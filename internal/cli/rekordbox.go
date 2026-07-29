package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	workflows "github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/jaa/update-downloads/internal/rekordbox/bridge"
	"github.com/jaa/update-downloads/internal/rekordbox/playlistsync"
	"github.com/jaa/update-downloads/internal/rekordbox/pyruntime"
	"github.com/jaa/update-downloads/internal/rekordbox/syncconfig"
	"github.com/spf13/cobra"
)

type rekordboxPlaylistSyncFlags struct {
	JobID               string
	MusicPlaylist       string
	MusicPlaylistID     string
	RekordboxPlaylist   string
	RekordboxPlaylistID string
	RekordboxDBDir      string
	PythonBin           string
	PythonPath          string
	BackupDir           string
	OutPath             string
	PlanFile            string
	Mode                string
	CreatePlaylist      bool
	Force               bool
	MappingID           string
}

func newRekordboxCommand(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rekordbox",
		Short: "Inspect and update Rekordbox library metadata",
	}
	cmd.AddCommand(newRekordboxConfigCommand(app))
	cmd.AddCommand(newRekordboxDepsCommand(app))
	cmd.AddCommand(newRekordboxPlaylistSyncCommand(app))
	return cmd
}

func newRekordboxConfigCommand(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect Rekordbox sync feature config",
	}
	cmd.AddCommand(newRekordboxConfigPathCommand(app))
	cmd.AddCommand(newRekordboxConfigShowCommand(app))
	return cmd
}

func newRekordboxConfigPathCommand(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "path",
		Short: "Show the Rekordbox sync config path UDL will write",
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, err := os.Getwd()
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, fmt.Errorf("resolve working directory: %w", err))
			}
			resolved, err := syncconfig.ResolveWritePath(syncconfig.WritePathOptions{
				ExplicitPath: strings.TrimSpace(app.Opts.RekordboxConfigPath),
				WorkingDir:   wd,
			})
			if err != nil {
				return withExitCode(exitcode.InvalidUsage, err)
			}
			if app.Opts.JSON {
				_ = json.NewEncoder(app.IO.Out).Encode(resolved)
				return nil
			}
			fmt.Fprintln(app.IO.Out, resolved.Path)
			return nil
		},
	}
	return cmd
}

func newRekordboxConfigShowCommand(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show merged Rekordbox sync feature config",
		RunE: func(cmd *cobra.Command, args []string) error {
			base, err := loadConfig(app)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			rbCfg, err := loadRekordboxSyncConfig(app, base)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			wd, err := os.Getwd()
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, fmt.Errorf("resolve working directory: %w", err))
			}
			resolved, err := syncconfig.ResolveWritePath(syncconfig.WritePathOptions{
				ExplicitPath: strings.TrimSpace(app.Opts.RekordboxConfigPath),
				WorkingDir:   wd,
			})
			if err != nil {
				return withExitCode(exitcode.InvalidUsage, err)
			}
			if app.Opts.JSON {
				_ = json.NewEncoder(app.IO.Out).Encode(map[string]any{
					"path":   resolved,
					"config": rbCfg,
				})
				return nil
			}
			fmt.Fprintf(app.IO.Out, "Path: %s\n", resolved.Path)
			fmt.Fprintf(app.IO.Out, "Path kind: %s\n", resolved.Kind)
			fmt.Fprintf(app.IO.Out, "Folder mappings: %d\n", len(rbCfg.Sync.Folders))
			fmt.Fprintf(app.IO.Out, "Legacy jobs: %d\n", len(rbCfg.Sync.Jobs))
			payload, err := syncconfig.Marshal(rbCfg)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			fmt.Fprintln(app.IO.Out)
			fmt.Fprint(app.IO.Out, string(payload))
			for _, warning := range rbCfg.Warnings {
				fmt.Fprintf(app.IO.ErrOut, "WARN: %s\n", warning)
			}
			return nil
		},
	}
	return cmd
}

func newRekordboxDepsCommand(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deps",
		Short: "Manage the Rekordbox Python dependency runtime",
	}
	cmd.AddCommand(newRekordboxDepsStatusCommand(app))
	cmd.AddCommand(newRekordboxDepsEnsureCommand(app))
	cmd.AddCommand(newRekordboxDepsResetCommand(app))
	return cmd
}

func newRekordboxDepsStatusCommand(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show Rekordbox Python runtime status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(app)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			rbCfg, err := loadRekordboxSyncConfig(app, cfg)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			cfg = configWithRekordboxSyncDefaults(cfg, rbCfg)
			status := (pyruntime.Resolver{}).Status(context.Background(), pyruntime.Request{Config: cfg})
			printRekordboxDepsStatus(app, status)
			if !status.Healthy {
				return withExitCode(exitcode.MissingDependency, errors.New(status.Message))
			}
			return nil
		},
	}
	return cmd
}

func newRekordboxDepsEnsureCommand(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ensure",
		Short: "Install or repair the managed Rekordbox Python runtime",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(app)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			rbCfg, err := loadRekordboxSyncConfig(app, cfg)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			cfg = configWithRekordboxSyncDefaults(cfg, rbCfg)
			rt, err := (pyruntime.Resolver{}).Ensure(context.Background(), pyruntime.Request{Config: cfg})
			if err != nil {
				return withExitCode(exitcode.MissingDependency, err)
			}
			printRekordboxDepsEnsure(app, rt)
			return nil
		},
	}
	return cmd
}

func newRekordboxDepsResetCommand(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Remove the managed Rekordbox Python runtime",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(app)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			rbCfg, err := loadRekordboxSyncConfig(app, cfg)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			cfg = configWithRekordboxSyncDefaults(cfg, rbCfg)
			if err := (pyruntime.Resolver{}).Reset(pyruntime.Request{Config: cfg}); err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			if app.Opts.JSON {
				_ = json.NewEncoder(app.IO.Out).Encode(map[string]any{"reset": true})
				return nil
			}
			fmt.Fprintln(app.IO.Out, "Managed Rekordbox Python runtime removed.")
			return nil
		},
	}
	return cmd
}

func newRekordboxPlaylistSyncCommand(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "playlist-sync",
		Short: "Mirror a Music.app playlist into a Rekordbox playlist",
	}
	cmd.AddCommand(newRekordboxPlaylistSyncPlanCommand(app))
	cmd.AddCommand(newRekordboxPlaylistSyncShowCommand(app))
	cmd.AddCommand(newRekordboxPlaylistSyncApplyCommand(app))
	return cmd
}

func newRekordboxPlaylistSyncPlanCommand(app *AppContext) *cobra.Command {
	flags := defaultRekordboxPlaylistSyncFlags()
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Create a dry-run plan for Music.app to Rekordbox playlist sync",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			cfg, err := loadConfig(app)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			if err := config.ValidateRekordbox(cfg); err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			rbCfg, err := loadRekordboxSyncConfig(app, cfg)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			cfg = configWithRekordboxSyncDefaults(cfg, rbCfg)

			result, err := (workflows.RekordboxPlaylistSyncUseCase{}).Plan(ctx, workflows.RekordboxPlaylistSyncPlanRequest{
				Config:     cfg,
				SyncConfig: &rbCfg,
				MappingID:  flags.MappingID,
				Options: playlistsync.Options{
					JobID:               flags.JobID,
					MusicPlaylist:       flagValueIfChanged(cmd, "music-playlist", flags.MusicPlaylist),
					MusicPlaylistID:     flags.MusicPlaylistID,
					RekordboxPlaylist:   flagValueIfChanged(cmd, "rekordbox-playlist", flags.RekordboxPlaylist),
					RekordboxPlaylistID: flags.RekordboxPlaylistID,
					RekordboxDBDir:      flags.RekordboxDBDir,
					PythonBin:           flags.PythonBin,
					PythonPath:          flags.PythonPath,
					BackupDir:           flags.BackupDir,
					Mode:                flags.Mode,
					CreatePlaylist:      flags.CreatePlaylist,
					CreatePlaylistSet:   cmd.Flags().Changed("create-playlist"),
					OutPath:             flags.OutPath,
				},
			})
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			printPlaylistSyncPlan(app, result.Plan, result.PlanPath)
			return nil
		},
	}
	addPlaylistSyncPlanFlags(cmd, &flags)
	return cmd
}

func newRekordboxPlaylistSyncShowCommand(app *AppContext) *cobra.Command {
	flags := defaultRekordboxPlaylistSyncFlags()
	cmd := &cobra.Command{
		Use:   "show --plan-file <path>",
		Short: "Show a saved Rekordbox playlist sync plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(flags.PlanFile) == "" {
				return withExitCode(exitcode.InvalidUsage, fmt.Errorf("--plan-file is required"))
			}
			plan, err := playlistsync.ReadPlan(flags.PlanFile)
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			if err := playlistsync.VerifyPlanChecksum(plan); err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			printPlaylistSyncPlan(app, plan, flags.PlanFile)
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.PlanFile, "plan-file", "", "Path to a playlist sync plan JSON file")
	return cmd
}

func newRekordboxPlaylistSyncApplyCommand(app *AppContext) *cobra.Command {
	flags := defaultRekordboxPlaylistSyncFlags()
	cmd := &cobra.Command{
		Use:   "apply --plan-file <path>",
		Short: "Apply a saved Music.app to Rekordbox playlist sync plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(flags.PlanFile) == "" {
				return withExitCode(exitcode.InvalidUsage, fmt.Errorf("--plan-file is required"))
			}
			if app.Opts.NoInput && !flags.Force {
				return withExitCode(exitcode.InvalidUsage, fmt.Errorf("--force is required with --no-input"))
			}
			if !flags.Force && !app.Opts.NoInput && !isTTY(os.Stdin) {
				return withExitCode(exitcode.InvalidUsage, fmt.Errorf("--force is required when stdin is not an interactive TTY"))
			}

			cfg, err := loadConfig(app)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			if err := config.ValidateRekordbox(cfg); err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			rbCfg, err := loadRekordboxSyncConfig(app, cfg)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			cfg = configWithRekordboxSyncDefaults(cfg, rbCfg)

			plan, err := playlistsync.ReadPlan(flags.PlanFile)
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			if err := playlistsync.ValidatePlanForApply(plan); err != nil {
				printPlaylistSyncBlockers(app, plan)
				return withExitCode(exitcode.RuntimeFailure, err)
			}

			if !flags.Force && !app.Opts.NoInput {
				target := plan.RekordboxPlaylist.Name
				if plan.Version == playlistsync.PlanVersionFolder {
					target = "folder " + plan.RekordboxFolder.Name
				}
				ok, err := promptYesNoDefault(app, fmt.Sprintf("Apply playlist sync to Rekordbox %s?", target), false)
				if err != nil {
					return withExitCode(exitcode.RuntimeFailure, err)
				}
				if !ok {
					fmt.Fprintln(app.IO.Out, "Apply canceled.")
					return nil
				}
			}

			result, err := (workflows.RekordboxPlaylistSyncUseCase{}).Apply(context.Background(), workflows.RekordboxPlaylistSyncApplyRequest{
				Config:     cfg,
				SyncConfig: &rbCfg,
				Plan:       plan,
				PythonBin:  flags.PythonBin,
				PythonPath: flags.PythonPath,
				BackupDir:  flags.BackupDir,
				DryRun:     app.Opts.DryRun,
			})
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			if result.DryRun {
				printPlaylistSyncApplyDryRun(app, plan, result.EffectiveBackupDir)
				return nil
			}
			if plan.Version == playlistsync.PlanVersionFolder {
				printPlaylistSyncApplyBatchResult(app, plan, result.BatchResponse, result.BackupPath)
				return nil
			}

			printPlaylistSyncApplyResult(app, plan, result.Response, result.BackupPath)
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.PlanFile, "plan-file", "", "Path to a playlist sync plan JSON file")
	cmd.Flags().StringVar(&flags.PythonBin, "python-bin", "", "Python executable with pyrekordbox available")
	cmd.Flags().StringVar(&flags.PythonPath, "python-path", "", "Extra PYTHONPATH used when running the pyrekordbox helper")
	cmd.Flags().StringVar(&flags.BackupDir, "backup-dir", "", "Directory where a full Rekordbox backup is written before apply")
	cmd.Flags().BoolVar(&flags.Force, "force", false, "Apply without an interactive confirmation")
	return cmd
}

func defaultRekordboxPlaylistSyncFlags() rekordboxPlaylistSyncFlags {
	return rekordboxPlaylistSyncFlags{
		MusicPlaylist:     playlistsync.DefaultMusicPlaylist,
		RekordboxPlaylist: playlistsync.DefaultRekordboxPlaylist,
		RekordboxDBDir:    playlistsync.DefaultRekordboxDBDir,
		BackupDir:         playlistsync.DefaultBackupDir,
		Mode:              playlistsync.DefaultMode,
		CreatePlaylist:    true,
	}
}

func addPlaylistSyncPlanFlags(cmd *cobra.Command, flags *rekordboxPlaylistSyncFlags) {
	cmd.Flags().StringVar(&flags.JobID, "job", "", "Configured rekordbox.playlist_sync job id")
	cmd.Flags().StringVar(&flags.MappingID, "mapping", "", "Configured Rekordbox folder mapping id")
	cmd.Flags().StringVar(&flags.MusicPlaylist, "music-playlist", playlistsync.DefaultMusicPlaylist, "Exact Music.app playlist name")
	cmd.Flags().StringVar(&flags.MusicPlaylistID, "music-playlist-id", "", "Music.app playlist persistent ID")
	cmd.Flags().StringVar(&flags.RekordboxPlaylist, "rekordbox-playlist", playlistsync.DefaultRekordboxPlaylist, "Exact Rekordbox target playlist name")
	cmd.Flags().StringVar(&flags.RekordboxPlaylistID, "rekordbox-playlist-id", "", "Rekordbox target playlist ID")
	cmd.Flags().StringVar(&flags.RekordboxDBDir, "rekordbox-db-dir", "", "Rekordbox database directory containing master.db")
	cmd.Flags().StringVar(&flags.PythonBin, "python-bin", "", "Python executable with pyrekordbox available; defaults to UDL's managed runtime")
	cmd.Flags().StringVar(&flags.PythonPath, "python-path", "", "Extra PYTHONPATH used when running the pyrekordbox helper")
	cmd.Flags().StringVar(&flags.BackupDir, "backup-dir", "", "Directory where apply writes full Rekordbox backups")
	cmd.Flags().StringVar(&flags.OutPath, "out", "", "Plan output path")
	cmd.Flags().StringVar(&flags.Mode, "mode", playlistsync.DefaultMode, "Sync mode: mirror")
	cmd.Flags().BoolVar(&flags.CreatePlaylist, "create-playlist", true, "Create target Rekordbox playlist if missing")
}

func loadRekordboxSyncConfig(app *AppContext, base config.Config) (syncconfig.Config, error) {
	wd, err := os.Getwd()
	if err != nil {
		return syncconfig.Config{}, fmt.Errorf("resolve working directory: %w", err)
	}
	return syncconfig.Load(syncconfig.LoadOptions{
		ExplicitPath: strings.TrimSpace(app.Opts.RekordboxConfigPath),
		WorkingDir:   wd,
		BaseConfig:   base,
	})
}

func configWithRekordboxSyncDefaults(base config.Config, rb syncconfig.Config) config.Config {
	base.Rekordbox = &config.RekordboxConfig{
		DBDir:      rb.Defaults.DBDir,
		PythonBin:  rb.Defaults.PythonBin,
		PythonPath: rb.Defaults.PythonPath,
		BackupDir:  rb.Defaults.BackupDir,
	}
	return base
}

func printRekordboxDepsStatus(app *AppContext, status pyruntime.Status) {
	if app.Opts.JSON {
		_ = json.NewEncoder(app.IO.Out).Encode(status)
		return
	}
	state := "not ready"
	if status.Healthy {
		state = "ready"
	}
	fmt.Fprintf(app.IO.Out, "Rekordbox Python runtime: %s\n", state)
	fmt.Fprintf(app.IO.Out, "Python: %s\n", status.PythonBin)
	if status.Managed {
		fmt.Fprintf(app.IO.Out, "Managed venv: %s\n", status.VenvDir)
	}
	if status.Version != "" {
		fmt.Fprintf(app.IO.Out, "pyrekordbox: %s\n", status.Version)
	}
	if status.Message != "" {
		fmt.Fprintf(app.IO.Out, "Status: %s\n", status.Message)
	}
}

func printRekordboxDepsEnsure(app *AppContext, rt pyruntime.Runtime) {
	if app.Opts.JSON {
		_ = json.NewEncoder(app.IO.Out).Encode(rt)
		return
	}
	fmt.Fprintln(app.IO.Out, "Rekordbox Python runtime ready.")
	fmt.Fprintf(app.IO.Out, "Python: %s\n", rt.PythonBin)
	if rt.Managed {
		fmt.Fprintf(app.IO.Out, "Managed venv: %s\n", rt.VenvDir)
	}
	if rt.Version != "" {
		fmt.Fprintf(app.IO.Out, "pyrekordbox: %s\n", rt.Version)
	}
}

func flagValueIfChanged(cmd *cobra.Command, name string, value string) string {
	if cmd.Flags().Changed(name) {
		return value
	}
	return ""
}

func printPlaylistSyncPlan(app *AppContext, plan playlistsync.Plan, path string) {
	if app.Opts.JSON {
		payload := map[string]any{
			"plan_file": path,
			"plan":      plan,
		}
		_ = json.NewEncoder(app.IO.Out).Encode(payload)
		return
	}
	if plan.Version == playlistsync.PlanVersionFolder {
		fmt.Fprintf(app.IO.Out, "Music folder: %s (%d playlists, %d tracks)\n", plan.MusicFolder.Name, plan.MusicFolder.ChildCount, plan.Summary.MusicTotal)
		if plan.RekordboxFolder.ID != "" {
			fmt.Fprintf(app.IO.Out, "Rekordbox folder: %s (ID %s)\n", plan.RekordboxFolder.Name, plan.RekordboxFolder.ID)
		} else {
			fmt.Fprintf(app.IO.Out, "Rekordbox folder: %s (will be created)\n", plan.RekordboxFolder.Name)
		}
		fmt.Fprintf(app.IO.Out, "Matched by path: %d\n", plan.Summary.MatchedByPath)
		fmt.Fprintf(app.IO.Out, "Missing in RB: %d\n", plan.Summary.MissingInRekordbox)
		fmt.Fprintf(app.IO.Out, "Final playlists: %d\n", len(plan.Operations))
		fmt.Fprintf(app.IO.Out, "Final tracks: %d\n", plan.Summary.FinalTargetCount)
		for _, op := range plan.Operations {
			fmt.Fprintf(app.IO.Out, "- %s -> %s: add=%d move=%d remove=%d final=%d\n",
				op.MusicPlaylist.Name,
				op.RekordboxPlaylist.Name,
				op.Summary.WillAdd,
				op.Summary.WillMove,
				op.Summary.WillRemove,
				op.Summary.FinalTargetCount,
			)
		}
		if path != "" {
			fmt.Fprintf(app.IO.Out, "Plan written: %s\n", path)
		}
		printPlaylistSyncBlockers(app, plan)
		for _, warning := range plan.Warnings {
			fmt.Fprintf(app.IO.ErrOut, "WARN: %s\n", warning)
		}
		return
	}
	fmt.Fprintf(app.IO.Out, "Music playlist: %s (%d tracks)\n", plan.MusicPlaylist.Name, plan.Summary.MusicTotal)
	if plan.RekordboxPlaylist.ID != "" {
		fmt.Fprintf(app.IO.Out, "Rekordbox playlist: %s (ID %s)\n", plan.RekordboxPlaylist.Name, plan.RekordboxPlaylist.ID)
	} else {
		fmt.Fprintf(app.IO.Out, "Rekordbox playlist: %s (will be created)\n", plan.RekordboxPlaylist.Name)
	}
	fmt.Fprintf(app.IO.Out, "Matched by path: %d\n", plan.Summary.MatchedByPath)
	fmt.Fprintf(app.IO.Out, "Missing in RB: %d\n", plan.Summary.MissingInRekordbox)
	fmt.Fprintf(app.IO.Out, "Current target count: %d\n", plan.Summary.CurrentTargetCount)
	fmt.Fprintf(app.IO.Out, "Final target count: %d\n", plan.Summary.FinalTargetCount)
	if path != "" {
		fmt.Fprintf(app.IO.Out, "Plan written: %s\n", path)
	}
	printPlaylistSyncBlockers(app, plan)
	for _, warning := range plan.Warnings {
		fmt.Fprintf(app.IO.ErrOut, "WARN: %s\n", warning)
	}
}

type playlistSyncBlocker struct {
	Playlist    string `json:"playlist,omitempty"`
	Artist      string `json:"artist,omitempty"`
	Title       string `json:"title"`
	Path        string `json:"path,omitempty"`
	MatchStatus string `json:"match_status"`
}

func playlistSyncBlockers(plan playlistsync.Plan) []playlistSyncBlocker {
	blockers := []playlistSyncBlocker{}
	appendRows := func(playlist string, rows []playlistsync.PlanRow) {
		for _, row := range rows {
			if row.MatchStatus == "matched_path" {
				continue
			}
			blockers = append(blockers, playlistSyncBlocker{
				Playlist:    playlist,
				Artist:      strings.TrimSpace(row.Artist),
				Title:       firstNonEmpty(row.Title, row.RekordboxTitle, "(untitled track)"),
				Path:        strings.TrimSpace(row.Path),
				MatchStatus: firstNonEmpty(row.MatchStatus, "blocked"),
			})
		}
	}
	if plan.Version == playlistsync.PlanVersionFolder {
		for _, op := range plan.Operations {
			appendRows(op.MusicPlaylist.Name, op.Rows)
		}
		return blockers
	}
	appendRows("", plan.Rows)
	return blockers
}

func printPlaylistSyncBlockers(app *AppContext, plan playlistsync.Plan) {
	blockers := playlistSyncBlockers(plan)
	if len(blockers) == 0 {
		return
	}
	if app.Opts.JSON {
		_ = json.NewEncoder(app.IO.ErrOut).Encode(map[string]any{"apply_blockers": blockers})
		return
	}
	fmt.Fprintln(app.IO.ErrOut, "Apply blockers:")
	for _, blocker := range blockers {
		playlist := ""
		if blocker.Playlist != "" {
			playlist = blocker.Playlist + ": "
		}
		identity := strings.TrimSpace(strings.TrimSpace(blocker.Artist) + " — " + strings.TrimSpace(blocker.Title))
		fmt.Fprintf(app.IO.ErrOut, "- [%s] %s%s\n", blocker.MatchStatus, playlist, identity)
		fmt.Fprintf(app.IO.ErrOut, "  path: %s\n", firstNonEmpty(blocker.Path, "(no local path)"))
	}
}

func printPlaylistSyncApplyDryRun(app *AppContext, plan playlistsync.Plan, effectiveBackupDir string) {
	if app.Opts.JSON {
		payload := map[string]any{
			"dry_run":              true,
			"effective_backup_dir": effectiveBackupDir,
			"plan":                 plan,
		}
		_ = json.NewEncoder(app.IO.Out).Encode(payload)
		return
	}
	if plan.Version == playlistsync.PlanVersionFolder {
		fmt.Fprintf(app.IO.Out, "Dry run: validated plan for folder %s; backup would be written under %s; no backup or DB changes written.\n", plan.RekordboxFolder.Name, effectiveBackupDir)
		return
	}
	fmt.Fprintf(app.IO.Out, "Dry run: validated plan for %s; backup would be written under %s; no backup or DB changes written.\n", plan.RekordboxPlaylist.Name, effectiveBackupDir)
}

func printPlaylistSyncApplyResult(app *AppContext, plan playlistsync.Plan, resp bridge.ApplyResponse, backupPath string) {
	if app.Opts.JSON {
		payload := map[string]any{
			"applied":     true,
			"backup_path": backupPath,
			"result":      resp,
		}
		_ = json.NewEncoder(app.IO.Out).Encode(payload)
		return
	}
	fmt.Fprintf(app.IO.Out, "Backup written: %s\n", backupPath)
	fmt.Fprintf(app.IO.Out, "Rekordbox playlist updated: %s (ID %s)\n", resp.PlaylistName, resp.PlaylistID)
	fmt.Fprintf(app.IO.Out, "Final target count: %d\n", resp.FinalTrackCount)
	if plan.Summary.WillRemove > 0 || plan.Summary.WillAdd > 0 || plan.Summary.WillMove > 0 {
		fmt.Fprintf(app.IO.Out, "Applied changes: add=%d move=%d remove=%d keep=%d\n", plan.Summary.WillAdd, plan.Summary.WillMove, plan.Summary.WillRemove, plan.Summary.WillKeep)
	}
}

func printPlaylistSyncApplyBatchResult(app *AppContext, plan playlistsync.Plan, resp bridge.ApplyBatchResponse, backupPath string) {
	if app.Opts.JSON {
		payload := map[string]any{
			"applied":     true,
			"backup_path": backupPath,
			"result":      resp,
		}
		_ = json.NewEncoder(app.IO.Out).Encode(payload)
		return
	}
	fmt.Fprintf(app.IO.Out, "Backup written: %s\n", backupPath)
	fmt.Fprintf(app.IO.Out, "Rekordbox folder updated: %s (ID %s)\n", resp.FolderName, resp.FolderID)
	fmt.Fprintf(app.IO.Out, "Final playlists: %d\n", resp.FinalPlaylistCount)
	fmt.Fprintf(app.IO.Out, "Final tracks: %d\n", resp.FinalTrackCount)
	if plan.Summary.WillRemove > 0 || plan.Summary.WillAdd > 0 || plan.Summary.WillMove > 0 {
		fmt.Fprintf(app.IO.Out, "Applied changes: add=%d move=%d remove=%d keep=%d\n", plan.Summary.WillAdd, plan.Summary.WillMove, plan.Summary.WillRemove, plan.Summary.WillKeep)
	}
}
