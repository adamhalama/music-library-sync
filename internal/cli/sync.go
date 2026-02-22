package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/adapters/deemix"
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
	var progressMode string
	var preflightSummaryMode string
	var trackStatusMode string

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Run enabled sources in deterministic order",
		RunE: func(cmd *cobra.Command, args []string) error {
			parsedProgressMode, err := parseProgressMode(progressMode)
			if err != nil {
				return withExitCode(exitcode.InvalidUsage, err)
			}
			parsedPreflightSummaryMode, err := parsePreflightSummaryMode(preflightSummaryMode)
			if err != nil {
				return withExitCode(exitcode.InvalidUsage, err)
			}
			parsedTrackStatusMode, err := parseTrackStatusMode(trackStatusMode)
			if err != nil {
				return withExitCode(exitcode.InvalidUsage, err)
			}

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
			var compactWriter *output.CompactLogWriter
			if app.Opts.JSON {
				runnerStdout = app.IO.ErrOut
			} else if app.Opts.Quiet {
				runnerStdout = io.Discard
				runnerStderr = io.Discard
			} else if !app.Opts.Verbose {
				interactive := output.SupportsInPlaceUpdates(app.IO.Out)
				switch parsedProgressMode {
				case "always":
					interactive = true
				case "never":
					interactive = false
				}
				compactWriter = output.NewCompactLogWriterWithOptions(app.IO.Out, output.CompactLogOptions{
					Interactive:      interactive,
					PreflightSummary: parsedPreflightSummaryMode,
					TrackStatus:      string(parsedTrackStatusMode),
				})
				humanStdout = compactWriter
				runnerStdout = compactWriter
				runnerStderr = compactWriter
			}

			var emitter output.EventEmitter
			if app.Opts.JSON {
				emitter = output.NewJSONEmitter(app.IO.Out)
			} else {
				humanEmitter := output.NewHumanEmitter(humanStdout, humanStderr, app.Opts.Quiet, app.Opts.Verbose)
				if compactWriter != nil {
					emitter = output.NewObservingEmitter(compactWriter, humanEmitter)
				} else {
					emitter = humanEmitter
				}
			}
			runner := engine.NewSubprocessRunner(app.IO.In, runnerStdout, runnerStderr)

			registry := map[string]engine.Adapter{
				"deemix": deemix.New(),
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
				AllowPrompt:      !app.Opts.NoInput && !app.Opts.JSON && isTTY(os.Stdin),
				PromptOnExisting: func(sourceID string, preflight engine.SoundCloudPreflight) (bool, error) {
					return promptYesNo(app, fmt.Sprintf("[%s] Existing track found at position %d of %d. Continue scanning for gaps?", sourceID, preflight.FirstExistingIndex, preflight.RemoteTotal))
				},
				PromptOnSpotifyAuth: func(sourceID string) (bool, error) {
					return promptYesNoDefault(app, fmt.Sprintf("[%s] Spotify login required. Open browser now?", sourceID), true)
				},
				PromptOnDeemixARL: func(sourceID string) (string, error) {
					return promptLine(app, fmt.Sprintf("[%s] Enter your Deezer ARL for deemix", sourceID))
				},
				TrackStatus: parsedTrackStatusMode,
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
	cmd.Flags().BoolVar(&noPreflight, "no-preflight", false, "Skip remote preflight diff stage for supported adapters")
	cmd.Flags().StringVar(&progressMode, "progress", "auto", "Progress rendering mode: auto, always, or never")
	cmd.Flags().StringVar(&preflightSummaryMode, "preflight-summary", "auto", "Preflight summary output: auto, always, or never")
	cmd.Flags().StringVar(&trackStatusMode, "track-status", "names", "Per-track status output: names, count, or none")
	return cmd
}

func parseProgressMode(raw string) (string, error) {
	mode := strings.TrimSpace(strings.ToLower(raw))
	switch mode {
	case "", "auto", "always", "never":
		if mode == "" {
			return "auto", nil
		}
		return mode, nil
	default:
		return "", fmt.Errorf("invalid --progress mode %q (expected: auto, always, never)", raw)
	}
}

func parsePreflightSummaryMode(raw string) (string, error) {
	mode := strings.TrimSpace(strings.ToLower(raw))
	switch mode {
	case "", output.CompactPreflightAuto, output.CompactPreflightAlways, output.CompactPreflightNever:
		if mode == "" {
			return output.CompactPreflightAuto, nil
		}
		return mode, nil
	default:
		return "", fmt.Errorf("invalid --preflight-summary mode %q (expected: auto, always, never)", raw)
	}
}

func parseTrackStatusMode(raw string) (engine.TrackStatusMode, error) {
	mode := strings.TrimSpace(strings.ToLower(raw))
	switch mode {
	case "", string(engine.TrackStatusNames), string(engine.TrackStatusCount), string(engine.TrackStatusNone):
		if mode == "" {
			return engine.TrackStatusNames, nil
		}
		return engine.TrackStatusMode(mode), nil
	default:
		return "", fmt.Errorf("invalid --track-status mode %q (expected: names, count, none)", raw)
	}
}
