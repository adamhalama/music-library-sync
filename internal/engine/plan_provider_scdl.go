package engine

import (
	"context"
	"fmt"
	"sort"

	"github.com/jaa/update-downloads/internal/config"
)

type SCDLPlanProvider struct{}

func NewSCDLPlanProvider() *SCDLPlanProvider {
	return &SCDLPlanProvider{}
}

type scdlSourcePlan struct {
	rows          []PlanRow
	sourceForExec config.Source
	preflight     SoundCloudPreflight
	tracks        []soundCloudRemoteTrack
	knownGapIDs   map[string]struct{}
	archivePath   string
}

func (p *SCDLPlanProvider) Build(
	ctx context.Context,
	cfg config.Config,
	source config.Source,
	opts SyncOptions,
) (SourcePlan, error) {
	if source.Type != config.SourceTypeSoundCloud {
		return nil, fmt.Errorf("scdl plan provider only supports soundcloud sources")
	}
	if source.Adapter.Kind != "scdl" {
		return nil, fmt.Errorf("scdl plan provider only supports adapter.kind=scdl")
	}

	stateFilePath, err := config.ResolveStateFile(cfg.Defaults.StateDir, source.StateFile)
	if err != nil {
		return nil, fmt.Errorf("resolve state_file: %w", err)
	}

	mode := determineSoundCloudMode(source, opts)
	sourceForExec := source
	sourceForExec.StateFile = stateFilePath
	breakOnExisting := mode == SoundCloudModeBreak
	sourceForExec.Sync.BreakOnExisting = &breakOnExisting

	enumerateStage, err := enumerateSoundCloudStage(ctx, soundCloudEnumerateStageInput{
		Source: source,
		Limit:  opts.PlanLimit,
	})
	if err != nil {
		return nil, err
	}
	tracks := enumerateStage.Tracks

	targetDir, err := config.ExpandPath(source.TargetDir)
	if err != nil {
		return nil, fmt.Errorf("resolve target_dir: %w", err)
	}

	stateStage, err := loadSoundCloudStateStage(soundCloudStateStageInput{StateFilePath: stateFilePath})
	if err != nil {
		return nil, fmt.Errorf("parse sync state file: %w", err)
	}

	archiveStage, err := loadSoundCloudArchiveStage(soundCloudArchiveStageInput{
		Source:   source,
		Defaults: cfg.Defaults,
	})
	if err != nil {
		return nil, fmt.Errorf("parse archive file: %w", err)
	}

	cacheEnabled := source.Sync.LocalIndexCache != nil && *source.Sync.LocalIndexCache
	needsLocalIndex := needsSoundCloudLocalIndex(tracks, stateStage.State, archiveStage.KnownIDs, targetDir)
	localIndexStage, err := loadSoundCloudLocalIndexStage(soundCloudLocalIndexStageInput{
		SourceID:  source.ID,
		TargetDir: targetDir,
		StateDir:  cfg.Defaults.StateDir,
		NeedScan:  needsLocalIndex,
		UseCache:  cacheEnabled,
	})
	if err != nil {
		return nil, fmt.Errorf("build local index: %w", err)
	}

	planStage := planSoundCloudPreflightStage(soundCloudPlanStageInput{
		RemoteTracks:   tracks,
		State:          stateStage.State,
		ArchiveKnownID: archiveStage.KnownIDs,
		LocalIndex:     localIndexStage.Index,
		TargetDir:      targetDir,
		Mode:           mode,
	})

	rows := make([]PlanRow, 0, len(tracks))
	for i, track := range tracks {
		status := PlanRowAlreadyDownloaded
		if _, ok := planStage.ArchiveGapID[track.ID]; ok {
			status = PlanRowMissingNew
		} else if _, ok := planStage.KnownGapID[track.ID]; ok {
			status = PlanRowMissingKnownGap
		}
		toggleable := status != PlanRowAlreadyDownloaded
		rows = append(rows, PlanRow{
			Index:             i + 1,
			RemoteID:          track.ID,
			Title:             track.Title,
			Status:            status,
			Toggleable:        toggleable,
			SelectedByDefault: toggleable,
		})
	}

	return &scdlSourcePlan{
		rows:          rows,
		sourceForExec: sourceForExec,
		preflight:     planStage.Preflight,
		tracks:        append([]soundCloudRemoteTrack{}, tracks...),
		knownGapIDs:   copyStringSet(planStage.KnownGapID),
		archivePath:   archiveStage.ArchivePath,
	}, nil
}

func (p *scdlSourcePlan) Rows() []PlanRow {
	return append([]PlanRow{}, p.rows...)
}

func (p *scdlSourcePlan) ApplySelection(selectedIndices []int, dryRun bool) (sourcePlanExecution, error) {
	indexSet := map[int]struct{}{}
	for _, idx := range selectedIndices {
		if idx <= 0 {
			return sourcePlanExecution{}, fmt.Errorf("invalid selected index %d", idx)
		}
		indexSet[idx] = struct{}{}
	}

	selectedPlaylistIDs := make([]int, 0, len(indexSet))
	selectedTrackIDs := map[string]struct{}{}
	validSelectionIndices := map[int]struct{}{}
	for _, row := range p.rows {
		if !row.Toggleable {
			continue
		}
		validSelectionIndices[row.Index] = struct{}{}
		if _, ok := indexSet[row.Index]; !ok {
			continue
		}
		selectedPlaylistIDs = append(selectedPlaylistIDs, row.Index)
		selectedTrackIDs[row.RemoteID] = struct{}{}
	}

	for idx := range indexSet {
		if _, ok := validSelectionIndices[idx]; !ok {
			return sourcePlanExecution{}, fmt.Errorf("selected index %d is not toggleable for this source", idx)
		}
	}

	sort.Ints(selectedPlaylistIDs)

	plannedTracks := make([]soundCloudRemoteTrack, 0, len(selectedTrackIDs))
	selectedKnownGapIDs := map[string]struct{}{}
	for _, track := range p.tracks {
		if _, ok := selectedTrackIDs[track.ID]; !ok {
			continue
		}
		plannedTracks = append(plannedTracks, track)
		if _, knownGap := p.knownGapIDs[track.ID]; knownGap {
			selectedKnownGapIDs[track.ID] = struct{}{}
		}
	}

	preflight := p.preflight
	preflight.PlannedDownloadCount = len(selectedPlaylistIDs)

	sourceForExec := p.sourceForExec
	sourceForExec.SelectedPlaylistIDs = selectedPlaylistIDs
	sourceForExec.DisableSyncMode = len(selectedPlaylistIDs) > 0
	out := sourcePlanExecution{
		SourceForExec:           sourceForExec,
		SourcePreflight:         &preflight,
		PlannedSoundCloudTracks: plannedTracks,
	}

	if dryRun || len(selectedKnownGapIDs) == 0 {
		return out, nil
	}

	tempArchiveFile, err := writeFilteredArchiveFile(p.archivePath, selectedKnownGapIDs)
	if err != nil {
		return sourcePlanExecution{}, fmt.Errorf("prepare temporary archive file: %w", err)
	}

	out.SourceForExec.DownloadArchivePath = tempArchiveFile
	out.StateSwap = soundCloudStateSwap{
		OriginalArchivePath: p.archivePath,
		TempArchivePath:     tempArchiveFile,
	}
	return out, nil
}

func copyStringSet(source map[string]struct{}) map[string]struct{} {
	out := map[string]struct{}{}
	for key := range source {
		out[key] = struct{}{}
	}
	return out
}
