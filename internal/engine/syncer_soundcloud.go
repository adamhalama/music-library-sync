package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/output"
)

type soundCloudStateSwap struct {
	OriginalSyncPath    string
	TempSyncPath        string
	OriginalArchivePath string
	TempArchivePath     string
}

type soundCloudExecutionPlan struct {
	Source        config.Source
	Preflight     *SoundCloudPreflight
	StateSwap     soundCloudStateSwap
	PlannedTracks []soundCloudRemoteTrack
	DownloadOrder DownloadOrder
}

type soundCloudFreeDownloadOutcome struct {
	Attempted         bool
	Succeeded         bool
	DependencyFailure bool
	Interrupted       bool
}

func (s *Syncer) runSoundCloudFreeDL(
	ctx context.Context,
	cfg config.Config,
	source config.Source,
	sourceForExec config.Source,
	sourcePreflight *SoundCloudPreflight,
	plannedSoundCloudTracks []soundCloudRemoteTrack,
	stateSwap soundCloudStateSwap,
	downloadOrder DownloadOrder,
	opts SyncOptions,
) sourceRunOutcome {
	outcome := sourceRunOutcome{}
	flowResult, runErr := s.runSoundCloudFreeDownloadSource(
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
	if flowResult.Attempted {
		outcome.Attempted++
	}
	if flowResult.DependencyFailure {
		outcome.DependencyFailures++
	}
	if flowResult.Interrupted {
		outcome.Interrupted = true
		outcome.Stop = true
		return outcome
	}
	if runErr != nil {
		outcome.Failed++
		outcome.Stop = !cfg.Defaults.ContinueOnError
		return outcome
	}
	if flowResult.Succeeded {
		outcome.Succeeded++
	}
	return outcome
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
		if source.Adapter.Kind == "scdl-freedl" {
			return plan, fmt.Errorf("soundcloud adapter.kind=scdl-freedl requires preflight planning; remove --no-preflight")
		}
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

	enumerateStage, err := enumerateSoundCloudStage(ctx, soundCloudEnumerateStageInput{Source: source})
	if err != nil {
		return plan, err
	}
	tracks := enumerateStage.Tracks

	targetDir, err := config.ExpandPath(source.TargetDir)
	if err != nil {
		return plan, fmt.Errorf("resolve target_dir: %w", err)
	}

	stateStage, err := loadSoundCloudStateStage(soundCloudStateStageInput{StateFilePath: stateFilePath})
	if err != nil {
		return plan, fmt.Errorf("parse sync state file: %w", err)
	}
	state := stateStage.State

	archiveStage, err := loadSoundCloudArchiveStage(soundCloudArchiveStageInput{
		Source:   source,
		Defaults: cfg.Defaults,
	})
	if err != nil {
		return plan, fmt.Errorf("parse archive file: %w", err)
	}
	archivePath := archiveStage.ArchivePath
	archiveKnownIDs := archiveStage.KnownIDs

	cacheEnabled := source.Sync.LocalIndexCache != nil && *source.Sync.LocalIndexCache
	needsLocalIndex := needsSoundCloudLocalIndex(tracks, state, archiveKnownIDs, targetDir)
	localIndexStage, err := loadSoundCloudLocalIndexStage(soundCloudLocalIndexStageInput{
		SourceID:  source.ID,
		TargetDir: targetDir,
		StateDir:  cfg.Defaults.StateDir,
		NeedScan:  needsLocalIndex,
		UseCache:  cacheEnabled,
	})
	if err != nil {
		return plan, fmt.Errorf("build local index: %w", err)
	}

	planStage := planSoundCloudPreflightStage(soundCloudPlanStageInput{
		RemoteTracks:   tracks,
		State:          state,
		ArchiveKnownID: archiveKnownIDs,
		LocalIndex:     localIndexStage.Index,
		TargetDir:      targetDir,
		Mode:           mode,
	})
	preflight := planStage.Preflight
	knownGapIDs := planStage.KnownGapID
	plannedIDs := planStage.PlannedID
	plan.DownloadOrder = DownloadOrderNewestFirst
	plan.PlannedTracks = orderForExecution(orderPlannedSoundCloudTracks(tracks, plannedIDs), plan.DownloadOrder)
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
			planStage = planSoundCloudPreflightStage(soundCloudPlanStageInput{
				RemoteTracks:   tracks,
				State:          state,
				ArchiveKnownID: archiveKnownIDs,
				LocalIndex:     localIndexStage.Index,
				TargetDir:      targetDir,
				Mode:           mode,
			})
			preflight = planStage.Preflight
			knownGapIDs = planStage.KnownGapID
			plannedIDs = planStage.PlannedID
			plan.PlannedTracks = orderForExecution(orderPlannedSoundCloudTracks(tracks, plannedIDs), plan.DownloadOrder)
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

func orderPlannedSoundCloudTracks(tracks []soundCloudRemoteTrack, plannedIDs map[string]struct{}) []soundCloudRemoteTrack {
	if len(tracks) == 0 || len(plannedIDs) == 0 {
		return []soundCloudRemoteTrack{}
	}
	ordered := make([]soundCloudRemoteTrack, 0, len(plannedIDs))
	for _, track := range tracks {
		if _, ok := plannedIDs[track.ID]; !ok {
			continue
		}
		ordered = append(ordered, track)
	}
	return ordered
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
