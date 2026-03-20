package engine

import "github.com/jaa/update-downloads/internal/config"

type sourcePlanExecution struct {
	SourceForExec           config.Source
	SourcePreflight         *SoundCloudPreflight
	PlannedSoundCloudTracks []soundCloudRemoteTrack
	StateSwap               soundCloudStateSwap
}
