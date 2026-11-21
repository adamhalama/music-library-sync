package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
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
	NewOnly     bool
}

func newRekordboxImportSourceCommand(app *AppContext) *cobra.Command {
	flags := rekordboxImportSourceFlags{StepSeconds: 1}
	cmd := &cobra.Command{
		Use:   "import-source <source-id>",
		Short: "Import a Spotify or SoundCloud source into Rekordbox in source order",
		Long: strings.TrimSpace(`
Imports local files of a configured Spotify or SoundCloud (scdl) source into
Rekordbox. Date Added follows Spotify playlist order or SoundCloud added order:
likes, tracks and reposts are reversed; sets retain playlist position order.
scdl-freedl sources are not supported.

Existing tracks keep Date Added when correctly ordered; otherwise it moves by
whole seconds. Trailing new tracks get the current time. With sync.append_only,
existing collection rows remain untouched and new imports follow both the
current time and every existing source row, one step apart. --new-only applies
the same behavior for this run. Local tracks no longer listed remotely remain
untouched. Re-running changes nothing.

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
				NewOnly:       flags.NewOnly,
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
	cmd.Flags().BoolVar(&flags.NewOnly, "new-only", false, "Import only tracks not already in the collection")
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
	order := "Spotify playlist order"
	if plan.SourceType == "soundcloud" {
		order = "SoundCloud added order (sets: position order; user streams: reversed)"
	}
	fmt.Fprintf(out, "Order: %s, oldest first; Date Added %ds apart\n", order, plan.StepMillis/1000)
	if plan.AppendOnly {
		fmt.Fprintln(out, "Existing collection rows remain unchanged; new imports follow all existing source rows and the current time.")
	}
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
		name := sourceImportTrackName(row)
		if row.Status != sourceimport.StatusExisting && row.Status != sourceimport.StatusImport {
			fmt.Fprintf(out, "  %3d  %-8s  %-19s  %s [%s]\n", row.Position, action, "", name, row.Status)
			continue
		}
		fmt.Fprintf(out, "  %3d  %-8s  %-19s  %s\n", row.Position, action, localDateAdded(row.NewCreatedAt), name)
	}
	fmt.Fprintln(out)
	s := plan.Summary
	fmt.Fprintf(out, "Source tracks: %d  existing: %d  import: %d  Date Added changes: %d\n", s.SourceTracks, s.Existing, s.Import, s.DateChanges)
	fmt.Fprintf(out, "Skipped: not_downloaded=%d  file_missing=%d  duplicate_path=%d\n", s.NotDownloaded, s.FileMissing, s.DuplicatePath)
	fmt.Fprintf(out, "Source rows already in collection: %d\nLocal files no longer remote (left alone): %d\n", s.CollectionExisting, s.LocalNotRemote)
	imports := []sourceimport.Row{}
	genres := map[string]int{}
	for _, row := range plan.Rows {
		if row.Status != sourceimport.StatusImport {
			continue
		}
		imports = append(imports, row)
		genre := "(empty)"
		if row.Tags != nil && strings.TrimSpace(row.Tags.Genre) != "" {
			genre = row.Tags.Genre
		}
		genres[genre]++
	}
	if len(imports) > 0 {
		fmt.Fprintf(out, "First to import: %s\nLast to import: %s\n", sourceImportTrackName(imports[0]), sourceImportTrackName(imports[len(imports)-1]))
		names := make([]string, 0, len(genres))
		for name := range genres {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintf(out, "Genre to import: %s (%d)\n", name, genres[name])
		}
	} else {
		fmt.Fprintln(out, "First/last to import: none\nGenres to import: none")
	}
	for _, warning := range plan.Warnings {
		fmt.Fprintf(app.IO.ErrOut, "WARN: %s\n", warning)
	}
	if !final {
		return
	}
	switch {
	case !plan.HasChanges():
		fmt.Fprintln(out, "Rekordbox already matches the source import plan; nothing to write.")
	case app.Opts.DryRun:
		fmt.Fprintf(out, "Dry run: no backup or database changes written. A real run backs up %s under %s first.\n", plan.DBDir, plan.BackupDir)
	}
}

func sourceImportTrackName(row sourceimport.Row) string {
	title, artist := strings.TrimSpace(row.Title), strings.TrimSpace(row.Artist)
	if title == "" && row.Tags != nil {
		title = strings.TrimSpace(row.Tags.Title)
	}
	if artist == "" && row.Tags != nil {
		artist = strings.TrimSpace(row.Tags.Artist)
	}
	if artist != "" && title != "" {
		return artist + " – " + title
	}
	return firstNonEmpty(title, artist, row.ID)
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
