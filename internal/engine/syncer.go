package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/auth"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine/adapterlog"
	"github.com/jaa/update-downloads/internal/engine/progress"
	"github.com/jaa/update-downloads/internal/output"
)

var ErrInterrupted = errors.New("sync interrupted")

type SelectionError struct {
	Missing []string
}

func (e *SelectionError) Error() string {
	return fmt.Sprintf("unknown source id(s): %s", strings.Join(e.Missing, ", "))
}

type Syncer struct {
	Registry     map[string]Adapter
	Runner       ExecRunner
	Emitter      output.EventEmitter
	Progress     progress.Sink
	Parsers      *adapterlog.Registry
	PlanRegistry *PlanRegistry
	Now          func() time.Time
}

var (
	resolveSpotifyCredentialsFn           = auth.ResolveSpotifyCredentials
	resolveDeemixARLFn                    = auth.ResolveDeemixARL
	saveDeemixARLFn                       = auth.SaveDeemixARL
	enumerateSpotifyTracksFn              = enumerateSpotifyPlaylistTracks
	enumerateSpotifyViaPageFn             = enumerateSpotifyPlaylistTracksViaPage
	fetchSpotifyTrackMetadataFn           = fetchSpotifyTrackMetadataFromPage
	fetchSoundCloudFreeDownloadMetadataFn = fetchSoundCloudFreeDownloadMetadata
	applySoundCloudTrackMetadataFn        = applySoundCloudTrackMetadata
	deemixTitlePattern                    = regexp.MustCompile(`\[(.+?)\]\s+Download(?:ing:\s+[0-9]+(?:\.[0-9]+)?%| complete)`)
)

func NewSyncer(registry map[string]Adapter, runner ExecRunner, emitter output.EventEmitter) *Syncer {
	if emitter == nil {
		emitter = noOpEmitter{}
	}
	progressSink := progress.Sink(progress.NoopSink{})
	parserRegistry := adapterlog.NewRegistry()
	parserRegistry.RegisterFactory("scdl", adapterlog.NewSCDLParser)
	parserRegistry.RegisterFactory("deemix", adapterlog.NewDeemixParser)
	parserRegistry.RegisterFactory("spotdl", adapterlog.NewSpotDLParser)
	planRegistry := NewPlanRegistry()
	planRegistry.Register("scdl", NewSCDLPlanProvider())
	return &Syncer{
		Registry:     registry,
		Runner:       runner,
		Emitter:      emitter,
		Progress:     progressSink,
		Parsers:      parserRegistry,
		PlanRegistry: planRegistry,
		Now:          time.Now,
	}
}

type noOpEmitter struct{}

func (noOpEmitter) Emit(event output.Event) error {
	return nil
}

type sourceFlowContext struct {
	Progress progress.Sink
	Parser   adapterlog.Parser
}

type sourceRunOutcome struct {
	Attempted          int
	Succeeded          int
	Failed             int
	Skipped            int
	DependencyFailures int
	Interrupted        bool
	Stop               bool
}

func (s *Syncer) Sync(ctx context.Context, cfg config.Config, opts SyncOptions) (SyncResult, error) {
	result := SyncResult{}
	if s.Now == nil {
		s.Now = time.Now
	}
	originalEmitter := s.Emitter
	s.Emitter = output.NewFailureDiagnosticsEmitter(cfg.Defaults.StateDir, originalEmitter)
	defer func() {
		s.Emitter = originalEmitter
	}()

	selected, err := selectSources(cfg.Sources, opts.SourceIDs)
	if err != nil {
		return result, err
	}

	for _, source := range selected {
		if source.Enabled {
			result.Total++
		}
	}

	_ = s.Emitter.Emit(output.Event{
		Timestamp: s.Now(),
		Level:     output.LevelInfo,
		Event:     output.EventSyncStarted,
		Message:   fmt.Sprintf("sync started (%d source(s))", result.Total),
		Details: map[string]any{
			"total":   result.Total,
			"dry_run": opts.DryRun,
		},
	})

	for _, source := range selected {
		if !source.Enabled {
			result.Skipped++
			continue
		}

		if opts.Plan {
			provider := s.planProviderForSource(source)
			if provider == nil {
				result.Skipped++
				_ = s.Emitter.Emit(output.Event{
					Timestamp: s.Now(),
					Level:     output.LevelWarn,
					Event:     output.EventSourceFinished,
					SourceID:  source.ID,
					Message:   fmt.Sprintf("[%s] --plan only supports adapter.kind=scdl in this release; skipping source", source.ID),
					Details: map[string]any{
						"adapter_kind": source.Adapter.Kind,
						"skipped":      true,
						"mode":         "plan",
					},
				})
				continue
			}
		}

		adapter, ok := s.Registry[source.Adapter.Kind]
		if !ok {
			result.Failed++
			result.Attempted++
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelError,
				Event:     output.EventSourceFailed,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("adapter %q not registered", source.Adapter.Kind),
			})
			if !cfg.Defaults.ContinueOnError {
				break
			}
			continue
		}

		missingEnv := missingEnvVars(adapter.RequiredEnv(source))
		if len(missingEnv) > 0 {
			result.Failed++
			result.Attempted++
			result.DependencyFailures++
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelError,
				Event:     output.EventSourceFailed,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("missing required env var(s): %s", strings.Join(missingEnv, ", ")),
				Details: map[string]any{
					"missing_env": missingEnv,
				},
			})
			if !cfg.Defaults.ContinueOnError {
				break
			}
			continue
		}

		if err := adapter.Validate(source); err != nil {
			result.Failed++
			result.Attempted++
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelError,
				Event:     output.EventSourceFailed,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("source validation failed: %v", err),
			})
			if !cfg.Defaults.ContinueOnError {
				break
			}
			continue
		}

		if err := ensureSourcePaths(cfg, source); err != nil {
			result.Failed++
			result.Attempted++
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelError,
				Event:     output.EventSourceFailed,
				SourceID:  source.ID,
				Message:   err.Error(),
			})
			if !cfg.Defaults.ContinueOnError {
				break
			}
			continue
		}

		sourceForExec := source
		stateSwap := soundCloudStateSwap{}
		var sourcePreflight *SoundCloudPreflight
		plannedSoundCloudTracks := []soundCloudRemoteTrack{}
		downloadOrder := DownloadOrderNewestFirst
		if opts.Plan {
			sourcePlan, planErr := s.prepareSourcePlan(ctx, cfg, source, opts)
			if planErr != nil {
				if errors.Is(planErr, ErrInterrupted) {
					result.Interrupted = true
					break
				}
				result.Failed++
				result.Attempted++
				if errors.Is(planErr, exec.ErrNotFound) {
					result.DependencyFailures++
				}
				_ = s.Emitter.Emit(output.Event{
					Timestamp: s.Now(),
					Level:     output.LevelError,
					Event:     output.EventSourceFailed,
					SourceID:  source.ID,
					Message:   fmt.Sprintf("[%s] plan preflight failed: %v", source.ID, planErr),
				})
				if !cfg.Defaults.ContinueOnError {
					break
				}
				continue
			}
			sourceForExec = sourcePlan.SourceForExec
			stateSwap = sourcePlan.StateSwap
			sourcePreflight = sourcePlan.SourcePreflight
			plannedSoundCloudTracks = append([]soundCloudRemoteTrack{}, sourcePlan.PlannedSoundCloudTracks...)
			downloadOrder = sourcePlan.DownloadOrder
		} else if source.Type == config.SourceTypeSoundCloud {
			plan, planErr := s.prepareSoundCloudExecutionPlan(ctx, cfg, source, opts)
			if planErr != nil {
				result.Failed++
				result.Attempted++
				if errors.Is(planErr, exec.ErrNotFound) {
					result.DependencyFailures++
				}
				_ = s.Emitter.Emit(output.Event{
					Timestamp: s.Now(),
					Level:     output.LevelError,
					Event:     output.EventSourceFailed,
					SourceID:  source.ID,
					Message:   fmt.Sprintf("[%s] soundcloud preflight failed: %v", source.ID, planErr),
				})
				if !cfg.Defaults.ContinueOnError {
					break
				}
				continue
			}
			sourceForExec = plan.Source
			stateSwap = plan.StateSwap
			sourcePreflight = plan.Preflight
			plannedSoundCloudTracks = append([]soundCloudRemoteTrack{}, plan.PlannedTracks...)
			downloadOrder = plan.DownloadOrder
		}

		if sourcePreflight != nil {
			s.emitSourcePreflightSummary(source, sourcePreflight, downloadOrder)
		}

		flowOutcome := s.runSource(
			ctx,
			cfg,
			source,
			adapter,
			sourceForExec,
			sourcePreflight,
			plannedSoundCloudTracks,
			stateSwap,
			downloadOrder,
			opts,
		)
		applySourceOutcome(&result, flowOutcome)
		if flowOutcome.Stop {
			break
		}
	}

	if result.Interrupted {
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelError,
			Event:     output.EventSyncFinished,
			Message:   "sync interrupted",
			Details: map[string]any{
				"total":     result.Total,
				"attempted": result.Attempted,
				"succeeded": result.Succeeded,
				"failed":    result.Failed,
				"skipped":   result.Skipped,
			},
		})
		return result, ErrInterrupted
	}

	_ = s.Emitter.Emit(output.Event{
		Timestamp: s.Now(),
		Level:     output.LevelInfo,
		Event:     output.EventSyncFinished,
		Message:   fmt.Sprintf("sync finished: attempted=%d succeeded=%d failed=%d skipped=%d", result.Attempted, result.Succeeded, result.Failed, result.Skipped),
		Details: map[string]any{
			"total":     result.Total,
			"attempted": result.Attempted,
			"succeeded": result.Succeeded,
			"failed":    result.Failed,
			"skipped":   result.Skipped,
		},
	})

	return result, nil
}

func (s *Syncer) planProviderForSource(source config.Source) PlanProvider {
	if s.PlanRegistry == nil {
		return nil
	}
	return s.PlanRegistry.ProviderFor(source.Adapter.Kind)
}

func buildExecFailureDetails(source config.Source, spec ExecSpec, execResult ExecResult) map[string]any {
	details := map[string]any{
		"adapter_kind": source.Adapter.Kind,
		"command":      spec.DisplayCommand,
		"exit_code":    execResult.ExitCode,
		"duration_ms":  execResult.Duration.Milliseconds(),
		"timed_out":    execResult.TimedOut,
		"interrupted":  execResult.Interrupted,
	}
	if stdoutTail := strings.TrimSpace(execResult.StdoutTail); stdoutTail != "" {
		details["stdout_tail"] = stdoutTail
	}
	if stderrTail := strings.TrimSpace(execResult.StderrTail); stderrTail != "" {
		details["stderr_tail"] = stderrTail
	}
	return details
}

func (s *Syncer) prepareSourcePlan(
	ctx context.Context,
	cfg config.Config,
	source config.Source,
	opts SyncOptions,
) (sourcePlanExecution, error) {
	if opts.SelectPlanRows == nil {
		return sourcePlanExecution{}, fmt.Errorf("plan mode requires an interactive selector callback")
	}
	provider := s.planProviderForSource(source)
	if provider == nil {
		return sourcePlanExecution{}, fmt.Errorf("no plan provider registered for adapter %q", source.Adapter.Kind)
	}
	sourcePlan, err := provider.Build(ctx, cfg, source, opts)
	if err != nil {
		return sourcePlanExecution{}, err
	}
	selection, err := opts.SelectPlanRows(source.ID, sourcePlan.Rows())
	if err != nil {
		return sourcePlanExecution{}, err
	}
	if selection.Canceled {
		return sourcePlanExecution{}, ErrInterrupted
	}
	return sourcePlan.ApplySelection(selection.SelectedIndices, PlanApplyOptions{
		DryRun:        opts.DryRun,
		DownloadOrder: NormalizeDownloadOrder(selection.DownloadOrder),
	})
}

func (s *Syncer) emitSourcePreflightSummary(source config.Source, preflight *SoundCloudPreflight, downloadOrder DownloadOrder) {
	if preflight == nil {
		return
	}
	_ = s.Emitter.Emit(output.Event{
		Timestamp: s.Now(),
		Level:     output.LevelInfo,
		Event:     output.EventSourcePreflight,
		SourceID:  source.ID,
		Message: fmt.Sprintf(
			"[%s] preflight: remote=%d known=%d gaps=%d known_gaps=%d first_existing=%d planned=%d mode=%s download_order=%s",
			source.ID,
			preflight.RemoteTotal,
			preflight.KnownCount,
			preflight.ArchiveGapCount,
			preflight.KnownGapCount,
			preflight.FirstExistingIndex,
			preflight.PlannedDownloadCount,
			preflight.Mode,
			downloadOrder,
		),
		Details: map[string]any{
			"remote_total":           preflight.RemoteTotal,
			"known_count":            preflight.KnownCount,
			"archive_gap_count":      preflight.ArchiveGapCount,
			"known_gap_count":        preflight.KnownGapCount,
			"first_existing_index":   preflight.FirstExistingIndex,
			"planned_download_count": preflight.PlannedDownloadCount,
			"mode":                   preflight.Mode,
			"download_order":         string(downloadOrder),
		},
	})
}

func (s *Syncer) buildSourceFlowContext(source config.Source) sourceFlowContext {
	sink := s.Progress
	if sink == nil {
		sink = progress.NoopSink{}
	}

	var parser adapterlog.Parser = adapterlog.NoopParser{}
	if s.Parsers != nil {
		parser = s.Parsers.ParserFor(source.Adapter.Kind)
	}

	return sourceFlowContext{
		Progress: sink,
		Parser:   parser,
	}
}

func applySourceOutcome(result *SyncResult, outcome sourceRunOutcome) {
	if result == nil {
		return
	}
	result.Attempted += outcome.Attempted
	result.Succeeded += outcome.Succeeded
	result.Failed += outcome.Failed
	result.Skipped += outcome.Skipped
	result.DependencyFailures += outcome.DependencyFailures
	if outcome.Interrupted {
		result.Interrupted = true
	}
}

func (s *Syncer) applyFlowObservers(spec ExecSpec, flow sourceFlowContext, source config.Source) ExecSpec {
	if flow.Parser == nil {
		return spec
	}
	spec.StdoutObservers = append(spec.StdoutObservers, func(line string) {
		flow.Parser.OnStdoutLine(line)
		s.flushFlowParser(flow, source)
	})
	spec.StderrObservers = append(spec.StderrObservers, func(line string) {
		flow.Parser.OnStderrLine(line)
		s.flushFlowParser(flow, source)
	})
	return spec
}

func (s *Syncer) flushFlowParser(flow sourceFlowContext, source config.Source) {
	if flow.Parser == nil || flow.Progress == nil {
		return
	}
	events := flow.Parser.Flush()
	for _, event := range events {
		if strings.TrimSpace(event.SourceID) == "" {
			event.SourceID = source.ID
		}
		if strings.TrimSpace(event.AdapterKind) == "" {
			event.AdapterKind = source.Adapter.Kind
		}
		flow.Progress.RecordTrackEvent(event)
		s.emitTrackEvent(source, event)
	}
}

func (s *Syncer) emitTrackEvent(source config.Source, event progress.TrackEvent) {
	eventName := output.EventTrackProgress
	level := output.LevelInfo
	switch event.Kind {
	case progress.TrackStarted:
		eventName = output.EventTrackStarted
	case progress.TrackProgress:
		eventName = output.EventTrackProgress
	case progress.TrackDone:
		eventName = output.EventTrackDone
	case progress.TrackSkip:
		eventName = output.EventTrackSkip
		level = output.LevelWarn
	case progress.TrackFail:
		eventName = output.EventTrackFail
		level = output.LevelError
	default:
		return
	}
	trackLabel := strings.TrimSpace(event.TrackName)
	if trackLabel == "" {
		trackLabel = strings.TrimSpace(event.TrackID)
	}
	if trackLabel == "" {
		trackLabel = "track"
	}
	message := fmt.Sprintf("[%s] [%s] %s", source.ID, eventName, trackLabel)
	if event.Reason != "" {
		message = fmt.Sprintf("%s (%s)", message, event.Reason)
	}

	_ = s.Emitter.Emit(output.Event{
		Timestamp: s.Now(),
		Level:     level,
		Event:     eventName,
		SourceID:  source.ID,
		Message:   message,
		Details: map[string]any{
			"track_id":     strings.TrimSpace(event.TrackID),
			"track_name":   strings.TrimSpace(event.TrackName),
			"index":        event.Index,
			"total":        event.Total,
			"percent":      event.Percent,
			"reason":       strings.TrimSpace(event.Reason),
			"adapter_kind": strings.TrimSpace(event.AdapterKind),
		},
	})
}

func (s *Syncer) runSource(
	ctx context.Context,
	cfg config.Config,
	source config.Source,
	adapter Adapter,
	sourceForExec config.Source,
	sourcePreflight *SoundCloudPreflight,
	plannedSoundCloudTracks []soundCloudRemoteTrack,
	stateSwap soundCloudStateSwap,
	downloadOrder DownloadOrder,
	opts SyncOptions,
) sourceRunOutcome {
	if source.Type == config.SourceTypeSpotify && source.Adapter.Kind == "deemix" {
		return s.runSpotifyDeemix(ctx, cfg, source, adapter, sourceForExec, sourcePreflight, opts)
	}
	if source.Type == config.SourceTypeSoundCloud && source.Adapter.Kind == "scdl-freedl" {
		return s.runSoundCloudFreeDL(
			ctx,
			cfg,
			source,
			sourceForExec,
			sourcePreflight,
			plannedSoundCloudTracks,
			stateSwap,
			downloadOrder,
			opts,
		)
	}
	if source.Type == config.SourceTypeSoundCloud && source.Adapter.Kind == "scdl" {
		return s.runSoundCloudSCDL(ctx, cfg, source, adapter, sourceForExec, sourcePreflight, stateSwap, downloadOrder, opts)
	}
	return s.runGenericAdapter(ctx, cfg, source, adapter, sourceForExec, sourcePreflight, stateSwap, downloadOrder, opts)
}

func (s *Syncer) runSoundCloudSCDL(
	ctx context.Context,
	cfg config.Config,
	source config.Source,
	adapter Adapter,
	sourceForExec config.Source,
	sourcePreflight *SoundCloudPreflight,
	stateSwap soundCloudStateSwap,
	downloadOrder DownloadOrder,
	opts SyncOptions,
) sourceRunOutcome {
	return s.runGenericAdapter(ctx, cfg, source, adapter, sourceForExec, sourcePreflight, stateSwap, downloadOrder, opts)
}

func (s *Syncer) runGenericAdapter(
	ctx context.Context,
	cfg config.Config,
	source config.Source,
	adapter Adapter,
	sourceForExec config.Source,
	sourcePreflight *SoundCloudPreflight,
	stateSwap soundCloudStateSwap,
	downloadOrder DownloadOrder,
	opts SyncOptions,
) sourceRunOutcome {
	outcome := sourceRunOutcome{}
	flow := s.buildSourceFlowContext(source)

	timeout := time.Duration(cfg.Defaults.CommandTimeoutSeconds) * time.Second
	if opts.TimeoutOverride > 0 {
		timeout = opts.TimeoutOverride
	}

	spec, err := adapter.BuildExecSpec(sourceForExec, cfg.Defaults, timeout)
	if err != nil {
		outcome.Failed++
		outcome.Attempted++
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelError,
			Event:     output.EventSourceFailed,
			SourceID:  source.ID,
			Message:   fmt.Sprintf("cannot build command: %v", err),
		})
		outcome.Stop = !cfg.Defaults.ContinueOnError
		return outcome
	}
	spec = s.applyFlowObservers(spec, flow, source)

	if sourcePreflight != nil && sourcePreflight.PlannedDownloadCount == 0 && (!opts.DryRun || opts.Plan) {
		outcome.Attempted++
		outcome.Succeeded++
		if err := cleanupTempStateFiles(stateSwap); err != nil {
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelWarn,
				Event:     output.EventSourceFinished,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] unable to clean temporary state file: %v", source.ID, err),
			})
		}
		finishedMessage := fmt.Sprintf("[%s] up-to-date (no downloads planned)", source.ID)
		if sourcePreflight.KnownGapCount > 0 || sourcePreflight.ArchiveGapCount > 0 {
			finishedMessage = fmt.Sprintf(
				"[%s] no new downloads planned in %s mode (known_gaps=%d archive_gaps=%d)",
				source.ID,
				sourcePreflight.Mode,
				sourcePreflight.KnownGapCount,
				sourcePreflight.ArchiveGapCount,
			)
		}
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelInfo,
			Event:     output.EventSourceFinished,
			SourceID:  source.ID,
			Message:   finishedMessage,
			Details: map[string]any{
				"planned_download_count": 0,
				"mode":                   sourcePreflight.Mode,
				"known_gap_count":        sourcePreflight.KnownGapCount,
				"archive_gap_count":      sourcePreflight.ArchiveGapCount,
			},
		})
		return outcome
	}

	_ = s.Emitter.Emit(output.Event{
		Timestamp: s.Now(),
		Level:     output.LevelInfo,
		Event:     output.EventSourcePreflight,
		SourceID:  source.ID,
		Message:   fmt.Sprintf("[%s] downloading to %s", source.ID, spec.Dir),
		Details: map[string]any{
			"target_dir": spec.Dir,
		},
	})

	outcome.Attempted++
	_ = s.Emitter.Emit(output.Event{
		Timestamp: s.Now(),
		Level:     output.LevelInfo,
		Event:     output.EventSourceStarted,
		SourceID:  source.ID,
		Message:   fmt.Sprintf("[%s] running %s (download_order=%s)", source.ID, spec.DisplayCommand, downloadOrder),
		Details: map[string]any{
			"command":        spec.DisplayCommand,
			"dir":            spec.Dir,
			"download_order": string(downloadOrder),
		},
	})

	cleanupSuffixes := artifactSuffixesForAdapter(source.Adapter.Kind)
	preArtifacts := map[string]struct{}{}
	if len(cleanupSuffixes) > 0 {
		preArtifacts, err = snapshotArtifacts(spec.Dir, cleanupSuffixes)
		if err != nil {
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelWarn,
				Event:     output.EventSourceStarted,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] unable to snapshot artifacts before run: %v", source.ID, err),
			})
		}
	}

	if opts.DryRun {
		outcome.Succeeded++
		if err := cleanupTempStateFiles(stateSwap); err != nil {
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelWarn,
				Event:     output.EventSourceFinished,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] unable to clean temporary state file: %v", source.ID, err),
			})
		}
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelInfo,
			Event:     output.EventSourceFinished,
			SourceID:  source.ID,
			Message:   fmt.Sprintf("[%s] dry-run complete", source.ID),
			Details: map[string]any{
				"dry_run": true,
				"command": spec.DisplayCommand,
			},
		})
		return outcome
	}

	execResult := s.Runner.Run(ctx, spec)
	s.flushFlowParser(flow, source)
	if execResult.Interrupted {
		s.cleanupArtifactsOnFailure(source.ID, spec.Dir, preArtifacts, cleanupSuffixes)
		if err := cleanupTempStateFiles(stateSwap); err != nil {
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelWarn,
				Event:     output.EventSourceFailed,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] unable to clean temporary state file: %v", source.ID, err),
			})
		}
		outcome.Interrupted = true
		outcome.Stop = true
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelError,
			Event:     output.EventSourceFailed,
			SourceID:  source.ID,
			Message:   fmt.Sprintf("[%s] interrupted", source.ID),
			Details:   buildExecFailureDetails(source, spec, execResult),
		})
		return outcome
	}

	if shouldRetrySpotifyWithUserAuth(sourceForExec, execResult, opts) {
		retrySource, opensBrowser, promptErr := planSpotifyUserAuthRetry(sourceForExec, opts)
		if promptErr != nil {
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelWarn,
				Event:     output.EventSourcePreflight,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] unable to read Spotify auth prompt response: %v", source.ID, promptErr),
			})
			retrySource = withSpotDLUserAuth(sourceForExec)
			opensBrowser = false
		}
		retrySpec, retryErr := adapter.BuildExecSpec(retrySource, cfg.Defaults, timeout)
		if retryErr != nil {
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelWarn,
				Event:     output.EventSourceFailed,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] spotify auth retry setup failed: %v", source.ID, retryErr),
			})
		} else {
			retrySpec = s.applyFlowObservers(retrySpec, flow, source)
			retryHint := "paste redirected URL in terminal when prompted"
			retryMode := "manual"
			if opensBrowser {
				retryHint = "browser login enabled"
				retryMode = "browser"
			}
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelInfo,
				Event:     output.EventSourcePreflight,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] spotify login required; retrying once with --user-auth (%s)", source.ID, retryHint),
				Details: map[string]any{
					"command": retrySpec.DisplayCommand,
					"dir":     retrySpec.Dir,
					"retry":   true,
					"mode":    retryMode,
				},
			})
			execResult = s.Runner.Run(ctx, retrySpec)
			s.flushFlowParser(flow, source)
			spec = retrySpec
			sourceForExec = retrySource
			if execResult.Interrupted {
				s.cleanupArtifactsOnFailure(source.ID, spec.Dir, preArtifacts, cleanupSuffixes)
				if err := cleanupTempStateFiles(stateSwap); err != nil {
					_ = s.Emitter.Emit(output.Event{
						Timestamp: s.Now(),
						Level:     output.LevelWarn,
						Event:     output.EventSourceFailed,
						SourceID:  source.ID,
						Message:   fmt.Sprintf("[%s] unable to clean temporary state file: %v", source.ID, err),
					})
				}
				outcome.Interrupted = true
				outcome.Stop = true
				_ = s.Emitter.Emit(output.Event{
					Timestamp: s.Now(),
					Level:     output.LevelError,
					Event:     output.EventSourceFailed,
					SourceID:  source.ID,
					Message:   fmt.Sprintf("[%s] interrupted", source.ID),
					Details: map[string]any{
						"exit_code":   execResult.ExitCode,
						"duration_ms": execResult.Duration.Milliseconds(),
					},
				})
				return outcome
			}
		}
	}

	if execResult.ExitCode != 0 && isSpotifyUserAuthRequired(sourceForExec, execResult) {
		guidance := "spotify login required; rerun in an interactive terminal once with --user-auth to refresh ~/.spotdl/.spotipy"
		if opts.AllowPrompt {
			guidance = "spotify login required and retry did not complete; rerun sync and finish the OAuth prompt"
		}
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelWarn,
			Event:     output.EventSourceFailed,
			SourceID:  source.ID,
			Message:   fmt.Sprintf("[%s] %s", source.ID, guidance),
		})
	}
	if execResult.ExitCode != 0 && isSpotifyRateLimited(sourceForExec, execResult) {
		rateLimitMessage := fmt.Sprintf("[%s] spotify API rate limit detected; use your own Spotify app credentials and rerun", source.ID)
		if cachePath, ok := resolveSpotDLOAuthCachePath(); ok {
			rateLimitMessage = fmt.Sprintf(
				"[%s] spotify API rate limit detected; OAuth cache is present at %s, so auth is configured and this is likely app quota throttling. Use your own Spotify app credentials and rerun",
				source.ID,
				cachePath,
			)
		}
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelWarn,
			Event:     output.EventSourceFailed,
			SourceID:  source.ID,
			Message:   rateLimitMessage,
		})
	}
	if execResult.ExitCode != 0 {
		if clientIDMessage, ok := scdlClientIDFailureMessage(sourceForExec, execResult); ok {
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelWarn,
				Event:     output.EventSourceFailed,
				SourceID:  source.ID,
				Message:   clientIDMessage,
			})
		}
	}

	if execResult.ExitCode != 0 {
		if isGracefulBreakOnExistingStop(sourceForExec, sourcePreflight, execResult) {
			if err := commitTempStateFiles(stateSwap); err != nil {
				outcome.Failed++
				_ = s.Emitter.Emit(output.Event{
					Timestamp: s.Now(),
					Level:     output.LevelError,
					Event:     output.EventSourceFailed,
					SourceID:  source.ID,
					Message:   fmt.Sprintf("[%s] failed to finalize sync state file: %v", source.ID, err),
				})
				outcome.Stop = !cfg.Defaults.ContinueOnError
				return outcome
			}

			outcome.Succeeded++
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelInfo,
				Event:     output.EventSourceFinished,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] stopped at first existing track (break_on_existing)", source.ID),
				Details: map[string]any{
					"adapter_kind":        source.Adapter.Kind,
					"exit_code":           execResult.ExitCode,
					"duration_ms":         execResult.Duration.Milliseconds(),
					"stop_reason":         "break_on_existing",
					"stopped_on_existing": true,
				},
			})
			return outcome
		}

		s.cleanupArtifactsOnFailure(source.ID, spec.Dir, preArtifacts, cleanupSuffixes)
		if err := cleanupTempStateFiles(stateSwap); err != nil {
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelWarn,
				Event:     output.EventSourceFailed,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] unable to clean temporary state file: %v", source.ID, err),
			})
		}
		outcome.Failed++
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelError,
			Event:     output.EventSourceFailed,
			SourceID:  source.ID,
			Message:   fmt.Sprintf("[%s] command failed with exit code %d", source.ID, execResult.ExitCode),
			Details:   buildExecFailureDetails(source, spec, execResult),
		})
		outcome.Stop = !cfg.Defaults.ContinueOnError
		return outcome
	}

	if err := commitTempStateFiles(stateSwap); err != nil {
		outcome.Failed++
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelError,
			Event:     output.EventSourceFailed,
			SourceID:  source.ID,
			Message:   fmt.Sprintf("[%s] failed to finalize sync state file: %v", source.ID, err),
		})
		outcome.Stop = !cfg.Defaults.ContinueOnError
		return outcome
	}

	outcome.Succeeded++
	_ = s.Emitter.Emit(output.Event{
		Timestamp: s.Now(),
		Level:     output.LevelInfo,
		Event:     output.EventSourceFinished,
		SourceID:  source.ID,
		Message:   fmt.Sprintf("[%s] completed", source.ID),
		Details: map[string]any{
			"duration_ms": execResult.Duration.Milliseconds(),
		},
	})

	return outcome
}

func selectSources(sources []config.Source, requestedIDs []string) ([]config.Source, error) {
	if len(requestedIDs) == 0 {
		return sources, nil
	}

	required := map[string]struct{}{}
	for _, id := range requestedIDs {
		required[id] = struct{}{}
	}

	selected := []config.Source{}
	found := map[string]struct{}{}
	for _, source := range sources {
		if _, ok := required[source.ID]; ok {
			selected = append(selected, source)
			found[source.ID] = struct{}{}
		}
	}

	missing := []string{}
	for _, id := range requestedIDs {
		if _, ok := found[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return nil, &SelectionError{Missing: missing}
	}

	return selected, nil
}

func missingEnvVars(required []string) []string {
	missing := []string{}
	for _, key := range required {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			missing = append(missing, key)
		}
	}
	return missing
}

func scdlClientIDFailureMessage(source config.Source, execResult ExecResult) (string, bool) {
	if source.Type != config.SourceTypeSoundCloud {
		return "", false
	}
	if source.Adapter.Kind != "scdl" {
		return "", false
	}
	if execResult.ExitCode == 0 || execResult.Interrupted {
		return "", false
	}

	combined := strings.ToLower(execResult.StdoutTail + "\n" + execResult.StderrTail)
	if strings.Contains(combined, "invalid client_id specified by --client-id argument") &&
		strings.Contains(combined, "clientidgenerationerror") {
		return fmt.Sprintf(
			"[%s] scdl rejected the configured SCDL_CLIENT_ID, and automatic client_id generation also failed; refresh SCDL_CLIENT_ID and rerun",
			source.ID,
		), true
	}
	if strings.Contains(combined, "clientidgenerationerror") {
		return fmt.Sprintf(
			"[%s] scdl could not generate a SoundCloud client_id automatically; set SCDL_CLIENT_ID to a current value and rerun",
			source.ID,
		), true
	}
	return "", false
}
