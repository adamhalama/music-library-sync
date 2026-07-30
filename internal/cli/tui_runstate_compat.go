package cli

import "github.com/jaa/update-downloads/internal/runstate"

type tuiStatusFilter = runstate.StatusFilter

const (
	tuiTrackFilterAll         = runstate.FilterAll
	tuiTrackFilterWillSync    = runstate.FilterWillSync
	tuiTrackFilterMissingNew  = runstate.FilterMissingNew
	tuiTrackFilterKnownGap    = runstate.FilterKnownGap
	tuiTrackFilterAlreadyHave = runstate.FilterAlreadyHave
	tuiTrackFilterInRun       = runstate.FilterInRun
	tuiTrackFilterRemaining   = runstate.FilterRemaining
	tuiTrackFilterDownloaded  = runstate.FilterDownloaded
	tuiTrackFilterSkipped     = runstate.FilterSkipped
	tuiTrackFilterFailed      = runstate.FilterFailed
)

type tuiTrackPlanClass = runstate.TrackPlanClass

const (
	tuiTrackPlanClassNew         = runstate.TrackPlanClassNew
	tuiTrackPlanClassKnownGap    = runstate.TrackPlanClassKnownGap
	tuiTrackPlanClassAlreadyHave = runstate.TrackPlanClassAlreadyHave
)

type tuiTrackRunScope = runstate.TrackRunScope

const (
	tuiTrackRunScopeIncluded = runstate.TrackRunScopeIncluded
	tuiTrackRunScopeExcluded = runstate.TrackRunScopeExcluded
	tuiTrackRunScopeLocked   = runstate.TrackRunScopeLocked
)

type tuiPlanTrackRow = runstate.PlanTrackRow
type tuiTrackRowState = runstate.TrackRow
type tuiTrackRuntimeStatus = runstate.TrackRuntimeStatus
type tuiActivityEntry = runstate.ActivityEntry
type tuiSyncFailureState = runstate.FailureState
type tuiInteractiveSourceLifecycle = runstate.SourceLifecycle

const (
	tuiTrackStatusIdle        = runstate.TrackStatusIdle
	tuiTrackStatusQueued      = runstate.TrackStatusQueued
	tuiTrackStatusDownloading = runstate.TrackStatusDownloading
	tuiTrackStatusDownloaded  = runstate.TrackStatusDownloaded
	tuiTrackStatusSkipped     = runstate.TrackStatusSkipped
	tuiTrackStatusFailed      = runstate.TrackStatusFailed

	tuiSourceLifecycleIdle      = runstate.SourceLifecycleIdle
	tuiSourceLifecyclePreflight = runstate.SourceLifecyclePreflight
	tuiSourceLifecycleRunning   = runstate.SourceLifecycleRunning
	tuiSourceLifecycleFinished  = runstate.SourceLifecycleFinished
	tuiSourceLifecycleFailed    = runstate.SourceLifecycleFailed
)
