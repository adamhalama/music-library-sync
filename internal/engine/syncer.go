package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
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

		timeout := time.Duration(cfg.Defaults.CommandTimeoutSeconds) * time.Second
		if opts.TimeoutOverride > 0 {
			timeout = opts.TimeoutOverride
		}

		spec, err := adapter.BuildExecSpec(source, cfg.Defaults, timeout)
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

		if execResult.ExitCode != 0 {
			s.cleanupArtifactsOnFailure(source.ID, spec.Dir, preArtifacts, cleanupSuffixes)
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

	if source.Type == config.SourceTypeSpotify {
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
		suffixes = append(suffixes, ".scdl.lock")
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
