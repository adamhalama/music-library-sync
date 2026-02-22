package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/jaa/update-downloads/internal/adapters/scdl"
	"github.com/jaa/update-downloads/internal/adapters/spotdl"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/jaa/update-downloads/internal/output"
	"github.com/spf13/cobra"
)

func newSyncCommand(app *AppContext) *cobra.Command {
	var sourceIDs []string
	var timeout time.Duration
	var askOnExisting bool
	var scanGaps bool
	var noPreflight bool

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Run enabled sources in deterministic order",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(app)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			if err := config.Validate(cfg); err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}

			humanStdout := app.IO.Out
			humanStderr := app.IO.ErrOut
			runnerStdout := app.IO.Out
			runnerStderr := app.IO.ErrOut
			if app.Opts.JSON {
				runnerStdout = app.IO.ErrOut
			} else if app.Opts.Quiet {
				runnerStdout = io.Discard
				runnerStderr = io.Discard
			} else if !app.Opts.Verbose {
				compact := output.NewCompactLogWriter(app.IO.Out)
				humanStdout = compact
				runnerStdout = compact
				runnerStderr = compact
			}

			var emitter output.EventEmitter
			if app.Opts.JSON {
				emitter = output.NewJSONEmitter(app.IO.Out)
			} else {
				emitter = output.NewHumanEmitter(humanStdout, humanStderr, app.Opts.Quiet, app.Opts.Verbose)
			}
			runner := engine.NewSubprocessRunner(runnerStdout, runnerStderr)

			registry := map[string]engine.Adapter{
				"spotdl": spotdl.New(),
				"scdl":   scdl.New(),
			}
			syncer := engine.NewSyncer(registry, runner, emitter)

			ctx, stop := signal.NotifyContext(context.Background(), interruptSignals()...)
			defer stop()

			result, runErr := syncer.Sync(ctx, cfg, engine.SyncOptions{
				SourceIDs:        sourceIDs,
				DryRun:           app.Opts.DryRun,
				TimeoutOverride:  timeout,
				AskOnExisting:    askOnExisting,
				AskOnExistingSet: cmd.Flags().Changed("ask-on-existing"),
				ScanGaps:         scanGaps,
				NoPreflight:      noPreflight,
				AllowPrompt:      !app.Opts.NoInput && isTTY(os.Stdin),
				PromptOnExisting: func(sourceID string, preflight engine.SoundCloudPreflight) (bool, error) {
					return promptYesNo(app, fmt.Sprintf("[%s] Existing track found at position %d of %d. Continue scanning for gaps?", sourceID, preflight.FirstExistingIndex, preflight.RemoteTotal))
				},
			})
			if runErr != nil {
				var selectionErr *engine.SelectionError
				switch {
				case errors.As(runErr, &selectionErr):
					return withExitCode(exitcode.InvalidUsage, runErr)
				case errors.Is(runErr, engine.ErrInterrupted):
					return withExitCode(exitcode.Interrupted, runErr)
				default:
					return withExitCode(exitcode.RuntimeFailure, runErr)
				}
			}

			if result.DependencyFailures > 0 {
				return withExitCode(exitcode.MissingDependency, fmt.Errorf("sync finished with dependency failures (%d)", result.DependencyFailures))
			}
			if result.Failed > 0 {
				return withExitCode(exitcode.PartialSuccess, fmt.Errorf("sync finished with failed sources (%d)", result.Failed))
			}
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&sourceIDs, "source", nil, "Run only selected source id (repeatable)")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "Override per-source command timeout (e.g. 10m, 1h)")
	cmd.Flags().BoolVar(&askOnExisting, "ask-on-existing", false, "Prompt once when first existing track is found and optionally continue with gap scan")
	cmd.Flags().BoolVar(&scanGaps, "scan-gaps", false, "Continue full remote scan to fill archive and local-file gaps")
	cmd.Flags().BoolVar(&noPreflight, "no-preflight", false, "Skip SoundCloud remote preflight diff stage")
	return cmd
}
