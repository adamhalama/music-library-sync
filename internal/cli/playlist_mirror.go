package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	workflows "github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/jaa/update-downloads/internal/navidrome"
	"github.com/jaa/update-downloads/internal/playlistmirror"
	"github.com/jaa/update-downloads/internal/playlists"
	"github.com/jaa/update-downloads/internal/rekordbox/bridge"
	legacy "github.com/jaa/update-downloads/internal/rekordbox/playlistsync"
	"github.com/jaa/update-downloads/internal/rekordbox/pyruntime"
	"github.com/spf13/cobra"
)

type playlistMirrorFlags struct {
	Job       string
	Direction string
	Out       string
	PlanFile  string
	Force     bool
}

func newPlaylistMirrorCommand(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Plan and apply an explicit directional Rekordbox/Navidrome playlist mirror",
		Long:  "Mirror one configured playlist pair in an explicit direction. The source controls exact membership and order; every mutation requires a saved plan.",
		Example: `  # Initial seed to the phone
  udl playlist sync plan --job favs-august --direction rekordbox-to-navidrome --out /tmp/favs-plan.json
  udl playlist sync show --plan-file /tmp/favs-plan.json
  udl playlist sync apply --plan-file /tmp/favs-plan.json

  # Bring phone membership and order back
  udl playlist sync plan --job favs-august --direction navidrome-to-rekordbox

  # Automation and complete validation without backup/write
  udl --json playlist sync plan --job favs-august --direction rekordbox-to-navidrome
  udl --dry-run playlist sync apply --plan-file /tmp/favs-plan.json --force`,
	}
	cmd.AddCommand(newPlaylistMirrorListCommand(app))
	cmd.AddCommand(newPlaylistMirrorPlanCommand(app))
	cmd.AddCommand(newPlaylistMirrorShowCommand(app))
	cmd.AddCommand(newPlaylistMirrorApplyCommand(app))
	return cmd
}

func newPlaylistMirrorListCommand(app *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured directional playlist sync jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			main, cfg, err := loadPlaylistConfigs(app)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			type row struct {
				Job        playlists.SyncJob    `json:"job"`
				State      *playlists.PairState `json:"state,omitempty"`
				StateError string               `json:"state_error,omitempty"`
			}
			rows := make([]row, 0, len(cfg.SyncJobs))
			for _, job := range cfg.SyncJobs {
				item := row{Job: job}
				state, stateErr := playlists.LoadPairState(main.Defaults.StateDir, job.ID)
				if stateErr == nil {
					if trusted, ok, trustErr := playlists.PairStateForJob(state, job); trustErr != nil {
						item.StateError = trustErr.Error()
					} else if ok {
						item.State = &trusted
					} else {
						item.StateError = "saved binding belongs to an older job configuration"
					}
				} else if !errors.Is(stateErr, os.ErrNotExist) {
					item.StateError = stateErr.Error()
				}
				rows = append(rows, item)
			}
			if app.Opts.JSON {
				return json.NewEncoder(app.IO.Out).Encode(rows)
			}
			if len(rows) == 0 {
				fmt.Fprintln(app.IO.Out, "No playlist sync jobs configured in playlists.yaml.")
				return nil
			}
			for _, item := range rows {
				status := "never verified"
				if item.State != nil {
					status = fmt.Sprintf("last %s at %s", item.State.LastDirection, item.State.LastVerifiedAt.Format("2006-01-02 15:04 MST"))
				} else if item.StateError != "" {
					status = "state warning: " + item.StateError
				}
				fmt.Fprintf(app.IO.Out, "%s\tRekordbox %s ⇄ Navidrome %s\t%s\n", item.Job.ID, item.Job.Rekordbox.Playlist, item.Job.Navidrome.Playlist, status)
			}
			return nil
		},
	}
}

func newPlaylistMirrorPlanCommand(app *AppContext) *cobra.Command {
	flags := playlistMirrorFlags{}
	cmd := &cobra.Command{
		Use:     "plan --job <id> --direction <direction>",
		Short:   "Build and save a directional playlist mirror plan",
		Example: "udl playlist sync plan --job favs-august --direction rekordbox-to-navidrome --out /tmp/favs-plan.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(flags.Job) == "" {
				return withExitCode(exitcode.InvalidUsage, errors.New("--job is required"))
			}
			direction := playlists.SyncDirection(strings.TrimSpace(flags.Direction))
			if !direction.Valid() {
				return withExitCode(exitcode.InvalidUsage, fmt.Errorf("--direction must be %q or %q", playlists.DirectionRekordboxToNavidrome, playlists.DirectionNavidromeToRekordbox))
			}
			main, cfg, err := loadPlaylistConfigs(app)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			useCase, main, dbDir, username, err := buildPlaylistMirrorUseCase(cmd.Context(), app, main)
			if err != nil {
				return err
			}
			result, err := useCase.Plan(cmd.Context(), workflows.PlaylistMirrorPlanRequest{
				Config: main, PlaylistConfig: cfg, JobID: flags.Job, Direction: direction,
				RekordboxDBDir: dbDir, NavidromeUser: username, OutPath: flags.Out,
			})
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			printPlaylistMirrorPlan(app, result.Plan, result.PlanPath)
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.Job, "job", "", "Configured sync_jobs id")
	cmd.Flags().StringVar(&flags.Direction, "direction", "", "rekordbox-to-navidrome or navidrome-to-rekordbox")
	cmd.Flags().StringVar(&flags.Out, "out", "", "Plan output path")
	return cmd
}

func newPlaylistMirrorShowCommand(app *AppContext) *cobra.Command {
	flags := playlistMirrorFlags{}
	cmd := &cobra.Command{
		Use:   "show --plan-file <path>",
		Short: "Show a saved directional playlist mirror plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(flags.PlanFile) == "" {
				return withExitCode(exitcode.InvalidUsage, errors.New("--plan-file is required"))
			}
			plan, err := playlistmirror.ReadPlan(flags.PlanFile)
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			printPlaylistMirrorPlan(app, plan, flags.PlanFile)
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.PlanFile, "plan-file", "", "Path to a directional playlist mirror plan")
	return cmd
}

func newPlaylistMirrorApplyCommand(app *AppContext) *cobra.Command {
	flags := playlistMirrorFlags{}
	cmd := &cobra.Command{
		Use:     "apply --plan-file <path>",
		Short:   "Revalidate and apply a saved directional playlist mirror plan",
		Example: "udl playlist sync apply --plan-file /tmp/favs-plan.json --force",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(flags.PlanFile) == "" {
				return withExitCode(exitcode.InvalidUsage, errors.New("--plan-file is required"))
			}
			if app.Opts.NoInput && !flags.Force {
				return withExitCode(exitcode.InvalidUsage, errors.New("--force is required with --no-input"))
			}
			if !flags.Force && !app.Opts.NoInput && !isTTY(os.Stdin) {
				return withExitCode(exitcode.InvalidUsage, errors.New("--force is required when stdin is not an interactive TTY"))
			}
			plan, err := playlistmirror.ReadPlan(flags.PlanFile)
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			if err := playlistmirror.VerifyPlanApplicable(plan); err != nil {
				printPlaylistMirrorPlan(app, plan, flags.PlanFile)
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			main, cfg, err := loadPlaylistConfigs(app)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			useCase, main, dbDir, username, err := buildPlaylistMirrorUseCase(cmd.Context(), app, main)
			if err != nil {
				return err
			}
			if !flags.Force && !app.Opts.NoInput {
				message := fmt.Sprintf("Replace %s playlist %q with %d tracks (%d removals)?", plan.Destination.Provider, plan.Destination.Name, plan.Summary.FinalTotal, plan.Summary.WillRemove)
				ok, err := promptYesNoDefault(app, message, false)
				if err != nil {
					return withExitCode(exitcode.RuntimeFailure, err)
				}
				if !ok {
					fmt.Fprintln(app.IO.Out, "Apply canceled.")
					return nil
				}
			}
			result, err := useCase.Apply(cmd.Context(), workflows.PlaylistMirrorApplyRequest{
				Config: main, PlaylistConfig: cfg, Plan: plan, RekordboxDBDir: dbDir,
				NavidromeUser: username, DryRun: app.Opts.DryRun,
			})
			if err != nil {
				code := exitcode.RuntimeFailure
				if errors.Is(err, navidrome.ErrMutationUncertain) || result.BackupPath != "" {
					code = exitcode.PartialSuccess
				}
				return withExitCode(code, err)
			}
			if app.Opts.JSON {
				return json.NewEncoder(app.IO.Out).Encode(result)
			}
			switch {
			case result.DryRun:
				fmt.Fprintln(app.IO.Out, "Dry run validated the plan; no backup or playlist write was performed.")
			case result.NoOp:
				fmt.Fprintf(app.IO.Out, "Verified parity; no playlist write or backup was needed.\nState: %s\n", result.PairStatePath)
			default:
				fmt.Fprintf(app.IO.Out, "Playlist mirror applied and verified.\nBackup: %s\nState: %s\n", result.BackupPath, result.PairStatePath)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.PlanFile, "plan-file", "", "Path to a directional playlist mirror plan")
	cmd.Flags().BoolVar(&flags.Force, "force", false, "Apply without an interactive confirmation")
	return cmd
}

func buildPlaylistMirrorUseCase(ctx context.Context, app *AppContext, main config.Config) (workflows.PlaylistMirrorUseCase, config.Config, string, string, error) {
	rbCfg, err := loadRekordboxSyncConfig(app, main)
	if err != nil {
		return workflows.PlaylistMirrorUseCase{}, main, "", "", withExitCode(exitcode.InvalidConfig, err)
	}
	main = configWithRekordboxSyncDefaults(main, rbCfg)
	if err := config.ValidateRekordbox(main); err != nil {
		return workflows.PlaylistMirrorUseCase{}, main, "", "", withExitCode(exitcode.InvalidConfig, err)
	}
	runtime, err := (pyruntime.Resolver{}).Ensure(ctx, pyruntime.Request{Config: main})
	if err != nil {
		return workflows.PlaylistMirrorUseCase{}, main, "", "", withExitCode(exitcode.MissingDependency, err)
	}
	dbDir, err := config.ExpandPath(rbCfg.Defaults.DBDir)
	if err != nil {
		return workflows.PlaylistMirrorUseCase{}, main, "", "", withExitCode(exitcode.InvalidConfig, err)
	}
	backupDir, err := config.ExpandPath(rbCfg.Defaults.BackupDir)
	if err != nil {
		return workflows.PlaylistMirrorUseCase{}, main, "", "", withExitCode(exitcode.InvalidConfig, err)
	}
	wd, err := os.Getwd()
	if err != nil {
		return workflows.PlaylistMirrorUseCase{}, main, "", "", withExitCode(exitcode.RuntimeFailure, err)
	}
	manager, err := navidrome.NewManager(navidrome.ManagerOptions{
		ConfigPath: strings.TrimSpace(app.Opts.NavidromeConfigPath), WorkingDir: wd,
	})
	if err != nil {
		return workflows.PlaylistMirrorUseCase{}, main, "", "", withExitCode(exitcode.InvalidConfig, err)
	}
	client, err := manager.Client()
	if err != nil {
		return workflows.PlaylistMirrorUseCase{}, main, "", "", withExitCode(exitcode.MissingDependency, err)
	}
	useCase := workflows.PlaylistMirrorUseCase{
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
	return useCase, main, dbDir, manager.Config.Server.Username, nil
}

func printPlaylistMirrorPlan(app *AppContext, plan playlistmirror.Plan, path string) {
	if app.Opts.JSON {
		_ = json.NewEncoder(app.IO.Out).Encode(map[string]any{"plan": plan, "plan_path": path})
		return
	}
	fmt.Fprintf(app.IO.Out, "%s → %s\n", plan.Source.Provider, plan.Destination.Provider)
	fmt.Fprintf(app.IO.Out, "Source: %s (%s), %d tracks\n", plan.Source.Name, plan.Source.ID, plan.Source.TrackCount)
	destination := fmt.Sprintf("%s (%s)", plan.Destination.Name, plan.Destination.ID)
	if plan.DestinationCreate {
		destination = plan.Destination.Name + " (will be created)"
	}
	fmt.Fprintf(app.IO.Out, "Destination: %s\n", destination)
	fmt.Fprintf(app.IO.Out, "Changes: +%d -%d move=%d keep=%d blocked=%d final=%d\n",
		plan.Summary.WillAdd, plan.Summary.WillRemove, plan.Summary.WillMove,
		plan.Summary.WillKeep, plan.Summary.Blocked, plan.Summary.FinalTotal)
	for _, row := range plan.Rows {
		detail := row.Blocker
		if detail == "" {
			detail = row.NormalizedPath
		}
		fmt.Fprintf(app.IO.Out, "%s\t%s — %s\t%s\n", row.Action, row.Artist, row.Title, detail)
	}
	for _, blocker := range plan.Blockers {
		fmt.Fprintf(app.IO.Out, "BLOCKED: %s\n", blocker)
	}
	fmt.Fprintf(app.IO.Out, "Checksum: %s\nPlan: %s\n", plan.ChecksumSHA256, path)
}
