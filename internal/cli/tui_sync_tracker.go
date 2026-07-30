package cli

import (
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/runstate"
)

// tuiSyncRunTracker preserves the frozen TUI-facing API while delegating all
// reducer behavior to the frontend-neutral runstate package.
type tuiSyncRunTracker struct {
	*runstate.Tracker
	startedAt time.Time
}

type tuiTrackedSourceSnapshot struct {
	lifecycle tuiInteractiveSourceLifecycle
	confirmed bool
	rows      []tuiTrackRowState
	activity  []tuiActivityEntry
}

func newTUISyncRunTracker() *tuiSyncRunTracker {
	return &tuiSyncRunTracker{Tracker: runstate.NewTracker()}
}

func (t *tuiSyncRunTracker) Reset(sources []config.Source) {
	if t == nil || t.Tracker == nil {
		return
	}
	t.startedAt = time.Time{}
	t.Tracker.Reset(sources)
}

func (t *tuiSyncRunTracker) MarkRuntimeStarted(at time.Time) {
	if t == nil || t.Tracker == nil {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	t.startedAt = at
	t.Tracker.MarkRuntimeStarted(at)
}

func (t *tuiSyncRunTracker) ConfirmSelection(state *tuiInteractiveSelectionState) {
	if t == nil || t.Tracker == nil || state == nil {
		return
	}
	t.Tracker.ConfirmSelection(state.sourceID, state.rows, state.manifest, state.isSelected)
}

func (t *tuiSyncRunTracker) SourceSnapshot(sourceID string) tuiTrackedSourceSnapshot {
	if t == nil || t.Tracker == nil {
		return tuiTrackedSourceSnapshot{lifecycle: tuiSourceLifecycleIdle}
	}
	snapshot := t.Tracker.SourceSnapshot(sourceID)
	return tuiTrackedSourceSnapshot{
		lifecycle: snapshot.Lifecycle,
		confirmed: snapshot.Confirmed,
		rows:      snapshot.Rows,
		activity:  snapshot.Activity,
	}
}
