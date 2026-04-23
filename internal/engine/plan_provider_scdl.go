package engine

import (
	"context"
	"fmt"

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
	state         soundCloudSyncState
	statePath     string
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
		state:         stateStage.State,
		statePath:     stateFilePath,
		knownGapIDs:   copyStringSet(planStage.KnownGapID),
		archivePath:   archiveStage.ArchivePath,
	}, nil
}

func (p *scdlSourcePlan) Rows() []PlanRow {
	return append([]PlanRow{}, p.rows...)
}

func (p *scdlSourcePlan) ApplySelection(manifest ExecutionManifest, opts PlanApplyOptions) (sourcePlanExecution, error) {
	manifest, err := CanonicalizeExecutionManifest(p.sourceForExec.ID, p.rows, manifest)
	if err != nil {
		return sourcePlanExecution{}, err
	}

	indexSet := map[int]struct{}{}
	selectedTrackIDs := map[string]struct{}{}
	for _, idx := range manifest.SelectedIndices {
		indexSet[idx] = struct{}{}
	}
	for _, entry := range manifest.Execution {
		selectedTrackIDs[entry.RemoteID] = struct{}{}
	}

	orderedSelectedPlaylistIDs := make([]int, 0, len(manifest.Execution))
	for _, entry := range manifest.Execution {
		orderedSelectedPlaylistIDs = append(orderedSelectedPlaylistIDs, entry.Index)
	}
	downloadOrder := NormalizeDownloadOrder(manifest.DownloadOrder)

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
	plannedTracks = orderForExecution(plannedTracks, downloadOrder)

	preflight := p.preflight
	preflight.PlannedDownloadCount = len(orderedSelectedPlaylistIDs)

	sourceForExec := p.sourceForExec
	sourceForExec.SelectedPlaylistIDs = orderedSelectedPlaylistIDs
	out := sourcePlanExecution{
		SourceForExec:           sourceForExec,
		SourcePreflight:         &preflight,
		PlannedSoundCloudTracks: plannedTracks,
		DownloadOrder:           downloadOrder,
	}

	if opts.DryRun || len(orderedSelectedPlaylistIDs) == 0 {
		return out, nil
	}

	allStateIDs := map[string]struct{}{}
	for id := range p.state.ByID {
		allStateIDs[id] = struct{}{}
	}
	tempSyncFile, err := writeFilteredSyncStateFile(p.statePath, p.state, allStateIDs)
	if err != nil {
		return sourcePlanExecution{}, fmt.Errorf("prepare temporary sync state file: %w", err)
	}
	out.SourceForExec.StateFile = tempSyncFile
	out.StateSwap.OriginalSyncPath = p.statePath
	out.StateSwap.TempSyncPath = tempSyncFile

	if len(selectedKnownGapIDs) == 0 {
		return out, nil
	}

	tempArchiveFile, err := writeFilteredArchiveFile(p.archivePath, selectedKnownGapIDs)
	if err != nil {
		_ = cleanupTempFile(tempSyncFile)
		return sourcePlanExecution{}, fmt.Errorf("prepare temporary archive file: %w", err)
	}

	out.SourceForExec.DownloadArchivePath = tempArchiveFile
	out.StateSwap.OriginalArchivePath = p.archivePath
	out.StateSwap.TempArchivePath = tempArchiveFile
	return out, nil
}

func copyStringSet(source map[string]struct{}) map[string]struct{} {
	out := map[string]struct{}{}
	for key := range source {
		out[key] = struct{}{}
	}
	return out
}
