package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/config"
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
	Registry map[string]Adapter
	Runner   ExecRunner
	Emitter  output.EventEmitter
	Now      func() time.Time
}

func NewSyncer(registry map[string]Adapter, runner ExecRunner, emitter output.EventEmitter) *Syncer {
	if emitter == nil {
		emitter = noOpEmitter{}
	}
	return &Syncer{
		Registry: registry,
		Runner:   runner,
		Emitter:  emitter,
		Now:      time.Now,
	}
}

type noOpEmitter struct{}

func (noOpEmitter) Emit(event output.Event) error {
	return nil
}

func (s *Syncer) Sync(ctx context.Context, cfg config.Config, opts SyncOptions) (SyncResult, error) {
	result := SyncResult{}
	if s.Now == nil {
		s.Now = time.Now
	}

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
		if source.Type == config.SourceTypeSoundCloud {
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

			if plan.Preflight != nil {
				_ = s.Emitter.Emit(output.Event{
					Timestamp: s.Now(),
					Level:     output.LevelInfo,
					Event:     output.EventSourcePreflight,
					SourceID:  source.ID,
					Message: fmt.Sprintf(
						"[%s] preflight: remote=%d known=%d gaps=%d known_gaps=%d first_existing=%d planned=%d mode=%s",
						source.ID,
						plan.Preflight.RemoteTotal,
						plan.Preflight.KnownCount,
						plan.Preflight.ArchiveGapCount,
						plan.Preflight.KnownGapCount,
						plan.Preflight.FirstExistingIndex,
						plan.Preflight.PlannedDownloadCount,
						plan.Preflight.Mode,
					),
					Details: map[string]any{
						"remote_total":           plan.Preflight.RemoteTotal,
						"known_count":            plan.Preflight.KnownCount,
						"archive_gap_count":      plan.Preflight.ArchiveGapCount,
						"known_gap_count":        plan.Preflight.KnownGapCount,
						"first_existing_index":   plan.Preflight.FirstExistingIndex,
						"planned_download_count": plan.Preflight.PlannedDownloadCount,
						"mode":                   plan.Preflight.Mode,
					},
				})
			}
		}

		timeout := time.Duration(cfg.Defaults.CommandTimeoutSeconds) * time.Second
		if opts.TimeoutOverride > 0 {
			timeout = opts.TimeoutOverride
		}

		spec, err := adapter.BuildExecSpec(sourceForExec, cfg.Defaults, timeout)
		if err != nil {
			result.Failed++
			result.Attempted++
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelError,
				Event:     output.EventSourceFailed,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("cannot build command: %v", err),
			})
			if !cfg.Defaults.ContinueOnError {
				break
			}
			continue
		}

		if !opts.DryRun && sourcePreflight != nil && sourcePreflight.PlannedDownloadCount == 0 {
			result.Attempted++
			result.Succeeded++
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
			continue
		}

		result.Attempted++
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelInfo,
			Event:     output.EventSourceStarted,
			SourceID:  source.ID,
			Message:   fmt.Sprintf("[%s] running %s", source.ID, spec.DisplayCommand),
			Details: map[string]any{
				"command": spec.DisplayCommand,
				"dir":     spec.Dir,
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
			result.Succeeded++
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
			continue
		}

		execResult := s.Runner.Run(ctx, spec)
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
			result.Interrupted = true
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
			break
		}

		if shouldRetrySpotifyWithUserAuth(sourceForExec, execResult, opts) {
			retrySource := withSpotDLUserAuth(sourceForExec)
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
				_ = s.Emitter.Emit(output.Event{
					Timestamp: s.Now(),
					Level:     output.LevelInfo,
					Event:     output.EventSourceStarted,
					SourceID:  source.ID,
					Message:   fmt.Sprintf("[%s] spotify API requires user auth, retrying with --user-auth", source.ID),
					Details: map[string]any{
						"command": retrySpec.DisplayCommand,
						"dir":     retrySpec.Dir,
						"retry":   true,
					},
				})
				execResult = s.Runner.Run(ctx, retrySpec)
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
					result.Interrupted = true
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
					break
				}
			}
		}

		if execResult.ExitCode != 0 && isSpotifyUserAuthRequired(sourceForExec, execResult) {
			guidance := "spotify API requires user authentication; rerun in an interactive terminal once with --user-auth to refresh ~/.spotdl/.spotipy"
			if opts.AllowPrompt {
				guidance = "spotify API requires user authentication and retry did not succeed; complete spotdl OAuth once and rerun sync"
			}
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelWarn,
				Event:     output.EventSourceFailed,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] %s", source.ID, guidance),
			})
		}

		if execResult.ExitCode != 0 {
			if isGracefulBreakOnExistingStop(sourceForExec, sourcePreflight, execResult) {
				if err := commitTempStateFiles(stateSwap); err != nil {
					result.Failed++
					_ = s.Emitter.Emit(output.Event{
						Timestamp: s.Now(),
						Level:     output.LevelError,
						Event:     output.EventSourceFailed,
						SourceID:  source.ID,
						Message:   fmt.Sprintf("[%s] failed to finalize sync state file: %v", source.ID, err),
					})
					if !cfg.Defaults.ContinueOnError {
						break
					}
					continue
				}

				result.Succeeded++
				_ = s.Emitter.Emit(output.Event{
					Timestamp: s.Now(),
					Level:     output.LevelInfo,
					Event:     output.EventSourceFinished,
					SourceID:  source.ID,
					Message:   fmt.Sprintf("[%s] stopped at first existing track (break_on_existing)", source.ID),
					Details: map[string]any{
						"exit_code":           execResult.ExitCode,
						"duration_ms":         execResult.Duration.Milliseconds(),
						"stopped_on_existing": true,
					},
				})
				continue
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
			result.Failed++
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelError,
				Event:     output.EventSourceFailed,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] command failed with exit code %d", source.ID, execResult.ExitCode),
				Details: map[string]any{
					"command":     spec.DisplayCommand,
					"exit_code":   execResult.ExitCode,
					"duration_ms": execResult.Duration.Milliseconds(),
					"timed_out":   execResult.TimedOut,
				},
			})
			if !cfg.Defaults.ContinueOnError {
				break
			}
			continue
		}

		if err := commitTempStateFiles(stateSwap); err != nil {
			result.Failed++
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelError,
				Event:     output.EventSourceFailed,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] failed to finalize sync state file: %v", source.ID, err),
			})
			if !cfg.Defaults.ContinueOnError {
				break
			}
			continue
		}

		result.Succeeded++
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

type soundCloudStateSwap struct {
	OriginalSyncPath    string
	TempSyncPath        string
	OriginalArchivePath string
	TempArchivePath     string
}

type soundCloudExecutionPlan struct {
	Source    config.Source
	Preflight *SoundCloudPreflight
	StateSwap soundCloudStateSwap
}

func (s *Syncer) prepareSoundCloudExecutionPlan(
	ctx context.Context,
	cfg config.Config,
	source config.Source,
	opts SyncOptions,
) (soundCloudExecutionPlan, error) {
	plan := soundCloudExecutionPlan{Source: source}

	stateFilePath, err := config.ResolveStateFile(cfg.Defaults.StateDir, source.StateFile)
	if err != nil {
		return plan, fmt.Errorf("resolve state_file: %w", err)
	}

	mode := determineSoundCloudMode(source, opts)
	askOnExisting := resolveAskOnExisting(source, opts)

	plan.Source.StateFile = stateFilePath
	breakOnExisting := mode == SoundCloudModeBreak
	plan.Source.Sync.BreakOnExisting = &breakOnExisting

	if opts.NoPreflight {
		if askOnExisting {
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelWarn,
				Event:     output.EventSourcePreflight,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] ask-on-existing ignored because preflight is disabled", source.ID),
			})
		}
		if mode == SoundCloudModeScanGaps {
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelWarn,
				Event:     output.EventSourcePreflight,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] scan-gaps without preflight disables remote diff planning", source.ID),
			})
		}
		return plan, nil
	}

	tracks, err := enumerateSoundCloudTracks(ctx, source)
	if err != nil {
		return plan, err
	}

	targetDir, err := config.ExpandPath(source.TargetDir)
	if err != nil {
		return plan, fmt.Errorf("resolve target_dir: %w", err)
	}

	state, err := parseSoundCloudSyncState(stateFilePath)
	if err != nil {
		return plan, fmt.Errorf("parse sync state file: %w", err)
	}

	archivePath, err := resolveSoundCloudArchivePath(source, cfg.Defaults)
	if err != nil {
		return plan, fmt.Errorf("resolve archive file: %w", err)
	}
	archiveKnownIDs, err := parseSoundCloudArchive(archivePath)
	if err != nil {
		return plan, fmt.Errorf("parse archive file: %w", err)
	}

	preflight, _, knownGapIDs, plannedIDs := buildSoundCloudPreflight(tracks, state, archiveKnownIDs, targetDir, mode)
	if askOnExisting &&
		mode == SoundCloudModeBreak &&
		preflight.FirstExistingIndex > 0 &&
		opts.AllowPrompt &&
		opts.PromptOnExisting != nil {
		shouldScanGaps, promptErr := opts.PromptOnExisting(source.ID, preflight)
		if promptErr != nil {
			return plan, promptErr
		}
		if shouldScanGaps {
			mode = SoundCloudModeScanGaps
			preflight, _, knownGapIDs, plannedIDs = buildSoundCloudPreflight(tracks, state, archiveKnownIDs, targetDir, mode)
		}
	}

	plan.Preflight = &preflight
	breakOnExisting = mode == SoundCloudModeBreak
	plan.Source.Sync.BreakOnExisting = &breakOnExisting

	if opts.DryRun {
		return plan, nil
	}

	plannedKnownGapIDs := map[string]struct{}{}
	for id := range plannedIDs {
		if _, isKnownGap := knownGapIDs[id]; isKnownGap {
			plannedKnownGapIDs[id] = struct{}{}
		}
	}
	if len(plannedKnownGapIDs) > 0 {
		tempStateFile, tempErr := writeFilteredSyncStateFile(stateFilePath, state, plannedKnownGapIDs)
		if tempErr != nil {
			return plan, fmt.Errorf("prepare temporary sync state file: %w", tempErr)
		}
		tempArchiveFile, archiveErr := writeFilteredArchiveFile(archivePath, plannedKnownGapIDs)
		if archiveErr != nil {
			_ = cleanupTempFile(tempStateFile)
			return plan, fmt.Errorf("prepare temporary archive file: %w", archiveErr)
		}
		plan.Source.StateFile = tempStateFile
		plan.Source.DownloadArchivePath = tempArchiveFile
		plan.StateSwap = soundCloudStateSwap{
			OriginalSyncPath:    stateFilePath,
			TempSyncPath:        tempStateFile,
			OriginalArchivePath: archivePath,
			TempArchivePath:     tempArchiveFile,
		}
	}

	return plan, nil
}

func determineSoundCloudMode(source config.Source, opts SyncOptions) SoundCloudMode {
	if opts.ScanGaps {
		return SoundCloudModeScanGaps
	}

	breakOnExisting := true
	if source.Sync.BreakOnExisting != nil {
		breakOnExisting = *source.Sync.BreakOnExisting
	}
	if breakOnExisting {
		return SoundCloudModeBreak
	}
	return SoundCloudModeScanGaps
}

func resolveAskOnExisting(source config.Source, opts SyncOptions) bool {
	value := false
	if source.Sync.AskOnExisting != nil {
		value = *source.Sync.AskOnExisting
	}
	if opts.AskOnExistingSet {
		value = opts.AskOnExisting
	}
	return value
}

func isGracefulBreakOnExistingStop(
	source config.Source,
	preflight *SoundCloudPreflight,
	execResult ExecResult,
) bool {
	if source.Type != config.SourceTypeSoundCloud {
		return false
	}
	breakOnExisting := true
	if source.Sync.BreakOnExisting != nil {
		breakOnExisting = *source.Sync.BreakOnExisting
	}
	if !breakOnExisting {
		return false
	}
	if execResult.ExitCode == 0 || execResult.Interrupted || execResult.TimedOut {
		return false
	}

	combined := strings.ToLower(execResult.StdoutTail + "\n" + execResult.StderrTail)
	if strings.Contains(combined, "existingvideoreached") ||
		strings.Contains(combined, "stopping due to --break-on-existing") ||
		strings.Contains(combined, "has already been recorded in the archive") {
		return true
	}

	if preflight != nil &&
		preflight.Mode == SoundCloudModeBreak &&
		preflight.FirstExistingIndex > 0 &&
		preflight.PlannedDownloadCount == 0 {
		return true
	}

	return false
}

func shouldRetrySpotifyWithUserAuth(source config.Source, execResult ExecResult, opts SyncOptions) bool {
	if !opts.AllowPrompt {
		return false
	}
	if source.Type != config.SourceTypeSpotify {
		return false
	}
	if execResult.ExitCode == 0 || execResult.Interrupted || execResult.TimedOut {
		return false
	}
	if hasSpotDLUserAuthArg(source.Adapter.ExtraArgs) {
		return false
	}
	return isSpotifyUserAuthRequired(source, execResult)
}

func isSpotifyUserAuthRequired(source config.Source, execResult ExecResult) bool {
	if source.Type != config.SourceTypeSpotify {
		return false
	}
	if execResult.ExitCode == 0 || execResult.Interrupted {
		return false
	}
	combined := strings.ToLower(execResult.StdoutTail + "\n" + execResult.StderrTail)
	return strings.Contains(combined, "valid user authentication required") ||
		(strings.Contains(combined, "user authentication required") && strings.Contains(combined, "api.spotify.com"))
}

func hasSpotDLUserAuthArg(args []string) bool {
	for _, candidate := range args {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "--user-auth" {
			return true
		}
	}
	return false
}

func withSpotDLUserAuth(source config.Source) config.Source {
	if hasSpotDLUserAuthArg(source.Adapter.ExtraArgs) {
		return source
	}
	cloned := append([]string{}, source.Adapter.ExtraArgs...)
	cloned = append(cloned, "--user-auth")
	source.Adapter.ExtraArgs = cloned
	return source
}

func cleanupTempFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func cleanupTempStateFiles(stateSwap soundCloudStateSwap) error {
	paths := []string{stateSwap.TempSyncPath, stateSwap.TempArchivePath}
	problems := []string{}
	for _, path := range paths {
		if err := cleanupTempFile(path); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func commitTempStateFiles(stateSwap soundCloudStateSwap) error {
	if strings.TrimSpace(stateSwap.TempSyncPath) != "" {
		if strings.TrimSpace(stateSwap.OriginalSyncPath) == "" {
			if err := cleanupTempFile(stateSwap.TempSyncPath); err != nil {
				return err
			}
		} else if err := os.Rename(stateSwap.TempSyncPath, stateSwap.OriginalSyncPath); err != nil {
			_ = cleanupTempFile(stateSwap.TempSyncPath)
			return err
		}
	}
	if strings.TrimSpace(stateSwap.TempArchivePath) != "" {
		if strings.TrimSpace(stateSwap.OriginalArchivePath) == "" {
			if err := cleanupTempFile(stateSwap.TempArchivePath); err != nil {
				return err
			}
		} else if err := os.Rename(stateSwap.TempArchivePath, stateSwap.OriginalArchivePath); err != nil {
			_ = cleanupTempFile(stateSwap.TempArchivePath)
			return err
		}
	}
	return nil
}

func ensureSourcePaths(cfg config.Config, source config.Source) error {
	targetDir, err := config.ExpandPath(source.TargetDir)
	if err != nil {
		return fmt.Errorf("[%s] invalid target_dir: %w", source.ID, err)
	}
	info, err := os.Stat(targetDir)
	if err != nil {
		return fmt.Errorf("[%s] target_dir does not exist: %s", source.ID, targetDir)
	}
	if !info.IsDir() {
		return fmt.Errorf("[%s] target_dir is not a directory: %s", source.ID, targetDir)
	}

	if source.Type == config.SourceTypeSpotify || source.Type == config.SourceTypeSoundCloud {
		stateFile, err := config.ResolveStateFile(cfg.Defaults.StateDir, source.StateFile)
		if err != nil {
			return fmt.Errorf("[%s] invalid state_file: %w", source.ID, err)
		}
		stateDir := filepath.Dir(stateFile)
		stateInfo, stateErr := os.Stat(stateDir)
		if stateErr != nil {
			return fmt.Errorf("[%s] state directory does not exist: %s", source.ID, stateDir)
		}
		if !stateInfo.IsDir() {
			return fmt.Errorf("[%s] state directory is not a directory: %s", source.ID, stateDir)
		}
	}

	return nil
}

func artifactSuffixesForAdapter(adapterKind string) []string {
	suffixes := []string{".part", ".ytdl"}
	if adapterKind == "scdl" {
		suffixes = append(suffixes, ".scdl.lock", ".jpg", ".jpeg", ".png", ".webp")
	}
	return suffixes
}

func snapshotArtifacts(dir string, suffixes []string) (map[string]struct{}, error) {
	seen := map[string]struct{}{}
	if len(suffixes) == 0 {
		return seen, nil
	}

	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return seen, nil
		}
		return nil, err
	}

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		for _, suffix := range suffixes {
			if strings.HasSuffix(name, suffix) {
				seen[path] = struct{}{}
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return seen, nil
}

func cleanupNewArtifacts(dir string, baseline map[string]struct{}, suffixes []string) ([]string, error) {
	current, err := snapshotArtifacts(dir, suffixes)
	if err != nil {
		return nil, err
	}

	removed := make([]string, 0)
	for path := range current {
		if _, existed := baseline[path]; existed {
			continue
		}
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return removed, removeErr
		}
		removed = append(removed, path)
	}
	slices.Sort(removed)
	return removed, nil
}

func (s *Syncer) cleanupArtifactsOnFailure(sourceID string, dir string, preArtifacts map[string]struct{}, suffixes []string) {
	if len(suffixes) == 0 {
		return
	}

	removed, err := cleanupNewArtifacts(dir, preArtifacts, suffixes)
	if err != nil {
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelWarn,
			Event:     output.EventSourceFailed,
			SourceID:  sourceID,
			Message:   fmt.Sprintf("[%s] artifact cleanup failed: %v", sourceID, err),
		})
		return
	}
	if len(removed) == 0 {
		return
	}

	_ = s.Emitter.Emit(output.Event{
		Timestamp: s.Now(),
		Level:     output.LevelInfo,
		Event:     output.EventSourceFailed,
		SourceID:  sourceID,
		Message:   fmt.Sprintf("[%s] cleaned %d partial artifact(s)", sourceID, len(removed)),
		Details: map[string]any{
			"removed_artifacts": removed,
		},
	})
}
