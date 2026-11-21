package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	workflows "github.com/jaa/update-downloads/internal/app"

	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/spf13/cobra"
)

type rekordboxAnalyseFlags struct {
	DBDir, BackupDir, PythonBin, PythonPath, Analyser, Mode, KeyNotation, Playlist string
	ContentIDs, PathPrefixes                                                       []string
	All, Reanalyse, Force, AllowRunningOnCopy                                      bool
}

func newRekordboxAnalyseCommand(app *AppContext) *cobra.Command {
	flags := rekordboxAnalyseFlags{Mode: "rbxport", KeyNotation: "auto"}
	cmd := &cobra.Command{Use: "analyse", Short: "Analyse Rekordbox tracks with RBxport's analyser", RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig(app)
		if err != nil {
			return withExitCode(exitcode.InvalidConfig, err)
		}
		rbCfg, err := loadRekordboxSyncConfig(app, cfg)
		if err != nil {
			return withExitCode(exitcode.InvalidConfig, err)
		}
		cfg = configWithRekordboxSyncDefaults(cfg, rbCfg)
		req := workflows.RekordboxAnalysisRequest{Config: cfg, DBDir: flags.DBDir, BackupDir: flags.BackupDir, PythonBin: flags.PythonBin, PythonPath: flags.PythonPath, Analyser: flags.Analyser, ContentIDs: flags.ContentIDs, PathPrefixes: flags.PathPrefixes, Playlist: flags.Playlist, All: flags.All, Reanalyse: flags.Reanalyse, DryRun: app.Opts.DryRun, Mode: flags.Mode, KeyNotation: flags.KeyNotation, AllowRunningOnCopy: flags.AllowRunningOnCopy}
		use := workflows.RekordboxAnalysisUseCase{}
		plan, err := use.Plan(context.Background(), req)
		if err != nil {
			return withExitCode(exitcode.InvalidUsage, err)
		}
		if app.Opts.DryRun {
			printAnalysis(app, plan)
			return nil
		}
		if !flags.Force {
			if app.Opts.NoInput || !isTTY(os.Stdin) {
				return withExitCode(exitcode.InvalidUsage, fmt.Errorf("--force is required when input is not interactive"))
			}
			ok, err := promptYesNoDefault(app, "Analyse the selected Rekordbox tracks?", false)
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
		}
		result, err := use.Apply(context.Background(), req, plan)
		if err != nil {
			return withExitCode(exitcode.RuntimeFailure, err)
		}
		printAnalysis(app, result)
		return nil
	}}
	f := cmd.Flags()
	f.StringVar(&flags.DBDir, "rekordbox-db-dir", "", "Rekordbox database directory containing master.db")
	f.StringVar(&flags.BackupDir, "backup-dir", "", "Directory for a full backup before analysis")
	f.StringVar(&flags.PythonBin, "python-bin", "", "Python executable with pyrekordbox available")
	f.StringVar(&flags.PythonPath, "python-path", "", "Extra PYTHONPATH for pyrekordbox")
	f.StringVar(&flags.Analyser, "analyser", "", "Path to udl-rbl-analyse")
	f.StringVar(&flags.Mode, "mode", "rbxport", "Analysis mode: rbxport or rekordbox")
	f.StringVar(&flags.KeyNotation, "key-notation", "auto", "Key notation: auto, classic, or alphanumeric")
	f.StringVar(&flags.Playlist, "playlist", "", "Playlist name or ID")
	f.StringSliceVar(&flags.ContentIDs, "content-id", nil, "Content ID (repeatable)")
	f.StringSliceVar(&flags.PathPrefixes, "path-prefix", nil, "Folder path prefix (repeatable)")
	f.BoolVar(&flags.All, "all", false, "Analyse every track in the library")
	f.BoolVar(&flags.Reanalyse, "reanalyse", false, "Analyse tracks that already have valid analysis")
	f.BoolVar(&flags.AllowRunningOnCopy, "allow-running-on-copy", false, "Ignore writer processes for an independent library copy only (sidecars still refused)")
	f.BoolVar(&flags.Force, "force", false, "Write without an interactive confirmation")
	return cmd
}
func printAnalysis(app *AppContext, r workflows.RekordboxAnalysisResult) {
	if app.Opts.JSON {
		_ = json.NewEncoder(app.IO.Out).Encode(r)
		return
	}
	if r.BackupPath != "" {
		fmt.Fprintf(app.IO.Out, "Backup written: %s\n", r.BackupPath)
	}
	counts := map[string]int{}
	for _, t := range r.Tracks {
		counts[t.Status]++
		switch t.Status {
		case "analysed":
			if t.Analysis != nil {
				fmt.Fprintf(app.IO.Out, "analysed: %s BPM %.2f, key %s, mode %s\n", t.Content.Path, t.Analysis.BPM, t.Analysis.Key, t.Analysis.Mode)
			}
		case "ready":
			fmt.Fprintf(app.IO.Out, "would analyse: %s\n", t.Content.Path)
		case "skipped":
			fmt.Fprintf(app.IO.Out, "skipped: %s (%s)\n", t.Content.Path, t.Reason)
		case "failed":
			fmt.Fprintf(app.IO.Out, "failed: %s (%s)\n", t.Content.Path, t.Reason)
		}
	}
	fmt.Fprintf(app.IO.Out, "Totals: analysed=%d skipped=%d failed=%d planned=%d\n", counts["analysed"], counts["skipped"], counts["failed"], counts["ready"])
	if r.KeyNotation != "" {
		fmt.Fprintf(app.IO.Out, "Key notation: %s\n", r.KeyNotation)
	}
}
