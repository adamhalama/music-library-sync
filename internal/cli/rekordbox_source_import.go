package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	workflows "github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/jaa/update-downloads/internal/rekordbox/sourceimport"
	"github.com/spf13/cobra"
)

type rekordboxImportSourceFlags struct {
	DBDir       string
	BackupDir   string
	PythonBin   string
	PythonPath  string
	Genre       string
	StepSeconds int
	Force       bool
}

func newRekordboxImportSourceCommand(app *AppContext) *cobra.Command {
	flags := rekordboxImportSourceFlags{StepSeconds: 1}
	cmd := &cobra.Command{
		Use:   "import-source <source-id>",
		Short: "Import a Spotify source's local files into Rekordbox with Date Added in playlist order",
		Long: strings.TrimSpace(`
Imports the local files of a configured Spotify source into the Rekordbox
collection and sets Date Added so that sorting by Date Added follows the
Spotify playlist order, oldest (first in the playlist) to newest.

Tracks already in the collection keep their Date Added when it already sorts
correctly and move by whole seconds when it does not. New tracks after the
last known one get the current time. Re-running on an ordered collection
changes nothing.

Use --dry-run to print the planned order without writing. A real run needs
Rekordbox and RBxport closed and writes a full backup of the Rekordbox
directory first.`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			cfg, err := loadConfig(app)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			if flags.StepSeconds < 1 {
				return withExitCode(exitcode.InvalidUsage, fmt.Errorf("--step-seconds must be at least 1"))
			}
			rbCfg, err := loadRekordboxSyncConfig(app, cfg)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			cfg = configWithRekordboxSyncDefaults(cfg, rbCfg)

			req := workflows.RekordboxSourceImportRequest{
				Config:        cfg,
				SourceID:      args[0],
				DBDir:         flags.DBDir,
				BackupDir:     flags.BackupDir,
				PythonBin:     flags.PythonBin,
				PythonPath:    flags.PythonPath,
				GenreOverride: flags.Genre,
				Step:          time.Duration(flags.StepSeconds) * time.Second,
				DryRun:        app.Opts.DryRun,
			}
			useCase := workflows.RekordboxSourceImportUseCase{}
			plan, err := useCase.Plan(ctx, req)
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			if app.Opts.DryRun || !plan.HasChanges() {
				printSourceImportPlan(app, plan, true)
				return nil
			}
			printSourceImportPlan(app, plan, false)

			if app.Opts.NoInput && !flags.Force {
				return withExitCode(exitcode.InvalidUsage, fmt.Errorf("--force is required with --no-input"))
			}
			if !flags.Force {
				if !isTTY(os.Stdin) {
					return withExitCode(exitcode.InvalidUsage, fmt.Errorf("--force is required when stdin is not an interactive TTY"))
				}
				ok, err := promptYesNoDefault(app, fmt.Sprintf("Write %d imports and %d Date Added changes to Rekordbox?", plan.Summary.Import, plan.Summary.DateChanges), false)
				if err != nil {
					return withExitCode(exitcode.RuntimeFailure, err)
				}
				if !ok {
					fmt.Fprintln(app.IO.Out, "Import canceled.")
					return nil
				}
			}

			result, err := useCase.Apply(ctx, req, plan)
			if err != nil {
				if result.BackupPath != "" {
					fmt.Fprintf(app.IO.ErrOut, "Backup written: %s\n", result.BackupPath)
				}
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			printSourceImportResult(app, result)
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.DBDir, "rekordbox-db-dir", "", "Rekordbox database directory containing master.db")
	cmd.Flags().StringVar(&flags.BackupDir, "backup-dir", "", "Directory where the full Rekordbox backup is written before writing")
	cmd.Flags().StringVar(&flags.PythonBin, "python-bin", "", "Python executable with pyrekordbox available; defaults to UDL's managed runtime")
	cmd.Flags().StringVar(&flags.PythonPath, "python-path", "", "Extra PYTHONPATH used when running the pyrekordbox helper")
	cmd.Flags().StringVar(&flags.Genre, "genre", "", "Genre written on newly imported tracks instead of the file's genre tag")
	cmd.Flags().IntVar(&flags.StepSeconds, "step-seconds", 1, "Seconds between consecutive tracks' Date Added")
	cmd.Flags().BoolVar(&flags.Force, "force", false, "Write without an interactive confirmation")
	return cmd
}

func printSourceImportPlan(app *AppContext, plan sourceimport.Plan, final bool) {
	if app.Opts.JSON {
		if final {
			_ = json.NewEncoder(app.IO.Out).Encode(map[string]any{"dry_run": app.Opts.DryRun, "plan": plan})
		}
		return
	}
	out := app.IO.Out
	fmt.Fprintf(out, "Source: %s (%s)\n", plan.SourceID, plan.PlaylistURL)
	fmt.Fprintf(out, "Rekordbox: %s\n", plan.DBDir)
	fmt.Fprintf(out, "Order: Spotify playlist order, oldest first; Date Added %ds apart\n", plan.StepMillis/1000)
	if plan.GenreOverride != "" {
		fmt.Fprintf(out, "Genre for new tracks: %s\n", plan.GenreOverride)
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %3s  %-8s  %-19s  %s\n", "#", "action", "date added (local)", "track")
	for _, row := range plan.Rows {
		action := ""
		switch row.Status {
		case sourceimport.StatusImport:
			action = "import"
		case sourceimport.StatusExisting:
			action = "keep"
			if row.Changes() {
				action = "redate"
			}
		default:
			action = "skip"
		}
		name := firstNonEmpty(strings.TrimSpace(strings.TrimSpace(row.Artist)+" – "+strings.TrimSpace(row.Title)), row.SpotifyID)
		if row.Status != sourceimport.StatusExisting && row.Status != sourceimport.StatusImport {
			fmt.Fprintf(out, "  %3d  %-8s  %-19s  %s [%s]\n", row.Position, action, "", name, row.Status)
			continue
		}
		fmt.Fprintf(out, "  %3d  %-8s  %-19s  %s\n", row.Position, action, localDateAdded(row.NewCreatedAt), name)
	}
	fmt.Fprintln(out)
	s := plan.Summary
	fmt.Fprintf(out, "Spotify tracks: %d  in collection: %d  to import: %d  Date Added changes: %d  skipped: %d\n",
		s.SourceTracks, s.Existing, s.Import, s.DateChanges, s.NotDownloaded+s.FileMissing+s.DuplicatePath)
	for _, warning := range plan.Warnings {
		fmt.Fprintf(app.IO.ErrOut, "WARN: %s\n", warning)
	}
	if !final {
		return
	}
	switch {
	case !plan.HasChanges():
		fmt.Fprintln(out, "Rekordbox already matches the Spotify order; nothing to write.")
	case app.Opts.DryRun:
		fmt.Fprintf(out, "Dry run: no backup or database changes written. A real run backs up %s under %s first.\n", plan.DBDir, plan.BackupDir)
	}
}

func printSourceImportResult(app *AppContext, result workflows.RekordboxSourceImportResult) {
	if app.Opts.JSON {
		_ = json.NewEncoder(app.IO.Out).Encode(map[string]any{
			"applied":     result.Applied,
			"backup_path": result.BackupPath,
			"plan":        result.Plan,
			"rows":        result.AppliedRows,
		})
		return
	}
	fmt.Fprintf(app.IO.Out, "Backup written: %s\n", result.BackupPath)
	fmt.Fprintf(app.IO.Out, "Imported %d tracks and updated Date Added on %d; verified %d rows.\n",
		result.Plan.Summary.Import, result.Plan.Summary.DateChanges, len(result.AppliedRows))
}

// localDateAdded renders a planned created_at in local time, the way
// Rekordbox shows it.
func localDateAdded(raw string) string {
	t, err := sourceimport.ParseTimestamp(raw)
	if err != nil || t.IsZero() {
		return raw
	}
	return t.Local().Format("2006-01-02 15:04:05")
}
