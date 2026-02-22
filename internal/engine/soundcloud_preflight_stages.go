package engine

import (
	"context"

	"github.com/jaa/update-downloads/internal/config"
)

type soundCloudEnumerateStageInput struct {
	Source config.Source
}

type soundCloudEnumerateStageResult struct {
	Tracks []soundCloudRemoteTrack
}

func enumerateSoundCloudStage(ctx context.Context, input soundCloudEnumerateStageInput) (soundCloudEnumerateStageResult, error) {
	tracks, err := enumerateSoundCloudTracks(ctx, input.Source)
	if err != nil {
		return soundCloudEnumerateStageResult{}, err
	}
	return soundCloudEnumerateStageResult{Tracks: tracks}, nil
}

type soundCloudStateStageInput struct {
	StateFilePath string
}

type soundCloudStateStageResult struct {
	State soundCloudSyncState
}

func loadSoundCloudStateStage(input soundCloudStateStageInput) (soundCloudStateStageResult, error) {
	state, err := parseSoundCloudSyncState(input.StateFilePath)
	if err != nil {
		return soundCloudStateStageResult{}, err
	}
	return soundCloudStateStageResult{State: state}, nil
}

type soundCloudArchiveStageInput struct {
	Source   config.Source
	Defaults config.Defaults
}

type soundCloudArchiveStageResult struct {
	ArchivePath string
	KnownIDs    idSet
}

func loadSoundCloudArchiveStage(input soundCloudArchiveStageInput) (soundCloudArchiveStageResult, error) {
	archivePath, err := resolveSoundCloudArchivePath(input.Source, input.Defaults)
	if err != nil {
		return soundCloudArchiveStageResult{}, err
	}
	known, err := parseSoundCloudArchive(archivePath)
	if err != nil {
		return soundCloudArchiveStageResult{}, err
	}
	return soundCloudArchiveStageResult{
		ArchivePath: archivePath,
		KnownIDs:    known,
	}, nil
}

type soundCloudLocalIndexStageInput struct {
	SourceID  string
	TargetDir string
	StateDir  string
	NeedScan  bool
	UseCache  bool
}

type soundCloudLocalIndexStageResult struct {
	Index    map[string]int
	CacheHit bool
	Scanned  bool
}

func loadSoundCloudLocalIndexStage(input soundCloudLocalIndexStageInput) (soundCloudLocalIndexStageResult, error) {
	result := soundCloudLocalIndexStageResult{Index: map[string]int{}}
	if !input.NeedScan {
		return result, nil
	}

	signature, err := targetDirSignature(input.TargetDir)
	if err != nil {
		signature = ""
	}

	if input.UseCache {
		if cached, hit := loadLocalIndexCache(input.StateDir, input.SourceID, input.TargetDir, signature); hit {
			result.Index = cached
			result.CacheHit = true
			return result, nil
		}
	}

	result.Index = scanLocalMediaTitleIndex(input.TargetDir)
	result.Scanned = true
	if input.UseCache {
		storeLocalIndexCache(input.StateDir, input.SourceID, input.TargetDir, signature, result.Index)
	}
	return result, nil
}

type soundCloudPlanStageInput struct {
	RemoteTracks   []soundCloudRemoteTrack
	State          soundCloudSyncState
	ArchiveKnownID idSet
	LocalIndex     map[string]int
	TargetDir      string
	Mode           SoundCloudMode
}

type soundCloudPlanStageResult struct {
	Preflight    SoundCloudPreflight
	ArchiveGapID map[string]struct{}
	KnownGapID   map[string]struct{}
	PlannedID    map[string]struct{}
}

func planSoundCloudPreflightStage(input soundCloudPlanStageInput) soundCloudPlanStageResult {
	knownGapIDs := map[string]struct{}{}
	archiveGapIDs := map[string]struct{}{}
	availableLocalTitles := copyTitleCountMap(input.LocalIndex)

	knownCount := 0
	firstExisting := 0
	for i, track := range input.RemoteTracks {
		entry, knownFromState := input.State.ByID[track.ID]
		_, knownFromArchive := input.ArchiveKnownID[track.ID]
		if !knownFromState && !knownFromArchive {
			archiveGapIDs[track.ID] = struct{}{}
			continue
		}

		knownCount++
		hasLocal := false
		if knownFromState {
			if stateEntryHasLocalFile(entry.FilePath, input.TargetDir) {
				hasLocal = true
			}
			if !hasLocal && consumeLocalTitleMatch(availableLocalTitles, track.Title) {
				hasLocal = true
			}
		} else if consumeLocalTitleMatch(availableLocalTitles, track.Title) {
			hasLocal = true
		}
		if hasLocal {
			if firstExisting == 0 {
				firstExisting = i + 1
			}
		} else {
			knownGapIDs[track.ID] = struct{}{}
		}
	}

	planned := map[string]struct{}{}
	switch input.Mode {
	case SoundCloudModeScanGaps:
		for id := range archiveGapIDs {
			planned[id] = struct{}{}
		}
		for id := range knownGapIDs {
			planned[id] = struct{}{}
		}
	default:
		limit := len(input.RemoteTracks)
		if firstExisting > 0 {
			limit = firstExisting - 1
		}
		for i := 0; i < limit; i++ {
			id := input.RemoteTracks[i].ID
			if _, gap := archiveGapIDs[id]; gap {
				planned[id] = struct{}{}
			}
			if _, knownGap := knownGapIDs[id]; knownGap {
				planned[id] = struct{}{}
			}
		}
	}

	return soundCloudPlanStageResult{
		Preflight: SoundCloudPreflight{
			RemoteTotal:          len(input.RemoteTracks),
			KnownCount:           knownCount,
			ArchiveGapCount:      len(archiveGapIDs),
			KnownGapCount:        len(knownGapIDs),
			FirstExistingIndex:   firstExisting,
			PlannedDownloadCount: len(planned),
			Mode:                 input.Mode,
		},
		ArchiveGapID: archiveGapIDs,
		KnownGapID:   knownGapIDs,
		PlannedID:    planned,
	}
}

func needsSoundCloudLocalIndex(remoteTracks []soundCloudRemoteTrack, state soundCloudSyncState, archiveKnownIDs idSet) bool {
	for _, track := range remoteTracks {
		_, knownFromState := state.ByID[track.ID]
		_, knownFromArchive := archiveKnownIDs[track.ID]
		if knownFromArchive && !knownFromState {
			return true
		}
	}
	return false
}
