package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/output"
)

type tuiSyncRunTracker struct {
	sources     map[string]*tuiTrackedSourceState
	lastFailure *tuiSyncFailureState
	startedAt   time.Time
	finishedAt  time.Time
}

type tuiTrackedSourceState struct {
	lifecycle tuiInteractiveSourceLifecycle
	confirmed bool
	rows      []tuiTrackRowState
	activity  []tuiActivityEntry
}

type tuiTrackedSourceSnapshot struct {
	lifecycle tuiInteractiveSourceLifecycle
	confirmed bool
	rows      []tuiTrackRowState
	activity  []tuiActivityEntry
}

func newTUISyncRunTracker() *tuiSyncRunTracker {
	return &tuiSyncRunTracker{
		sources: map[string]*tuiTrackedSourceState{},
	}
}

func (t *tuiSyncRunTracker) Reset(sources []config.Source) {
	if t == nil {
		return
	}
	t.sources = map[string]*tuiTrackedSourceState{}
	for _, source := range sources {
		t.sources[source.ID] = &tuiTrackedSourceState{lifecycle: tuiSourceLifecycleIdle}
	}
	t.lastFailure = nil
	t.startedAt = time.Time{}
	t.finishedAt = time.Time{}
}

func (t *tuiSyncRunTracker) ensureSource(sourceID string) *tuiTrackedSourceState {
	if t == nil {
		return nil
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil
	}
	if t.sources == nil {
		t.sources = map[string]*tuiTrackedSourceState{}
	}
	if state, ok := t.sources[sourceID]; ok {
		return state
	}
	state := &tuiTrackedSourceState{lifecycle: tuiSourceLifecycleIdle}
	t.sources[sourceID] = state
	return state
}

func (t *tuiSyncRunTracker) ConfirmSelection(state *tuiInteractiveSelectionState) {
	if t == nil || state == nil {
		return
	}
	source := t.ensureSource(state.sourceID)
	if source == nil {
		return
	}
	source.confirmed = true
	source.rows = buildTrackedRowsForSelection(state)
}

func (t *tuiSyncRunTracker) SetSourceLifecycle(sourceID string, lifecycle tuiInteractiveSourceLifecycle) {
	source := t.ensureSource(sourceID)
	if source == nil {
		return
	}
	source.lifecycle = lifecycle
}

func (t *tuiSyncRunTracker) SourceLifecycle(sourceID string) tuiInteractiveSourceLifecycle {
	source := t.ensureSource(sourceID)
	if source == nil {
		return tuiSourceLifecycleIdle
	}
	return source.lifecycle
}

func (t *tuiSyncRunTracker) SourceSnapshot(sourceID string) tuiTrackedSourceSnapshot {
	source := t.ensureSource(sourceID)
	if source == nil {
		return tuiTrackedSourceSnapshot{lifecycle: tuiSourceLifecycleIdle}
	}
	return tuiTrackedSourceSnapshot{
		lifecycle: source.lifecycle,
		confirmed: source.confirmed,
		rows:      cloneTUITrackRows(source.rows),
		activity:  cloneTUIActivityEntries(source.activity),
	}
}

func (t *tuiSyncRunTracker) MarkRuntimeStarted(at time.Time) {
	if t == nil {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	t.startedAt = at
	t.finishedAt = time.Time{}
}

func (t *tuiSyncRunTracker) MarkRunFinished(at time.Time) {
	if t == nil {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	t.finishedAt = at
}

func (t *tuiSyncRunTracker) ObserveEvent(event output.Event, outcomes []output.StructuredTrackOutcome, historyLine string, historyOK bool) {
	if t == nil {
		return
	}
	sourceID := strings.TrimSpace(event.SourceID)
	source := t.ensureSource(sourceID)
	if source == nil {
		return
	}
	switch event.Event {
	case output.EventSourcePreflight:
		source.lifecycle = tuiSourceLifecyclePreflight
	case output.EventSourceStarted:
		source.lifecycle = tuiSourceLifecycleRunning
	case output.EventSourceFinished:
		source.lifecycle = tuiSourceLifecycleFinished
	case output.EventSourceFailed:
		source.lifecycle = tuiSourceLifecycleFailed
	}
	if row := source.resolveRowForEvent(event); row != nil {
		observeTrackedRowEvent(row, event)
	}
	for _, outcome := range outcomes {
		level := output.LevelInfo
		switch outcome.Kind {
		case output.StructuredTrackOutcomeSkip:
			level = output.LevelWarn
		case output.StructuredTrackOutcomeFail:
			level = output.LevelError
		}
		source.appendActivity(tuiActivityEntry{
			Timestamp: event.Timestamp,
			Level:     level,
			Message:   output.FormatCompactTrackOutcome(outcome, output.CompactTrackStatusNames),
			SourceID:  sourceID,
		})
	}
	if historyOK {
		source.appendActivity(tuiActivityEntry{
			Timestamp: event.Timestamp,
			Level:     event.Level,
			Message:   historyLine,
			SourceID:  sourceID,
		})
	}
	if failure := tuiFailureStateFromEvent(event); failure != nil {
		t.lastFailure = failure
	}
}

func (t *tuiSyncRunTracker) AggregateCounts(doneWithoutError bool) (selected, completed, skipped, failed int, progressPercent float64) {
	if t == nil {
		return 0, 0, 0, 0, 0
	}
	for _, source := range t.sources {
		if source == nil || !source.confirmed {
			continue
		}
		for _, row := range source.rows {
			if row.RunScope != tuiTrackRunScopeIncluded {
				continue
			}
			selected++
			switch row.RuntimeStatus {
			case tuiTrackStatusDownloaded:
				completed++
				progressPercent += 1
			case tuiTrackStatusSkipped:
				skipped++
				progressPercent += 1
			case tuiTrackStatusFailed:
				failed++
				progressPercent += 1
			case tuiTrackStatusDownloading:
				if row.ProgressKnown {
					progressPercent += row.ProgressPercent / 100.0
				}
			}
		}
	}
	if selected > 0 {
		progressPercent = (progressPercent / float64(selected)) * 100
	}
	if progressPercent < 0 {
		progressPercent = 0
	}
	if progressPercent > 100 {
		progressPercent = 100
	}
	if doneWithoutError {
		progressPercent = 100
	}
	return selected, completed, skipped, failed, progressPercent
}

func (t *tuiSyncRunTracker) ElapsedLabel(now time.Time) string {
	if t == nil || t.startedAt.IsZero() {
		return "0:00"
	}
	end := t.finishedAt
	if end.IsZero() {
		end = now
	}
	if end.Before(t.startedAt) {
		end = t.startedAt
	}
	elapsed := end.Sub(t.startedAt).Round(time.Second)
	if elapsed < 0 {
		elapsed = 0
	}
	totalSeconds := int(elapsed / time.Second)
	minutes := totalSeconds / 60
	seconds := totalSeconds % 60
	return formatElapsedLabel(minutes, seconds)
}

func (t *tuiSyncRunTracker) LastFailure() *tuiSyncFailureState {
	if t == nil || t.lastFailure == nil {
		return nil
	}
	failure := *t.lastFailure
	return &failure
}

func formatElapsedLabel(minutes, seconds int) string {
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}

func buildTrackedRowsForSelection(state *tuiInteractiveSelectionState) []tuiTrackRowState {
	if state == nil || len(state.rows) == 0 {
		return nil
	}
	rows := make([]tuiTrackRowState, 0, len(state.rows))
	for _, row := range state.rows {
		rows = append(rows, tuiDisplayRowFromPlanRow(row, state.isSelected(row.Index)))
	}
	return rows
}

func cloneTUITrackRows(rows []tuiTrackRowState) []tuiTrackRowState {
	if len(rows) == 0 {
		return nil
	}
	cloned := make([]tuiTrackRowState, len(rows))
	copy(cloned, rows)
	return cloned
}

func cloneTUIActivityEntries(entries []tuiActivityEntry) []tuiActivityEntry {
	if len(entries) == 0 {
		return nil
	}
	cloned := make([]tuiActivityEntry, len(entries))
	copy(cloned, entries)
	return cloned
}

func (s *tuiTrackedSourceState) appendActivity(entry tuiActivityEntry) {
	if s == nil || strings.TrimSpace(entry.Message) == "" {
		return
	}
	s.activity = append(s.activity, entry)
	const maxEntries = 18
	if len(s.activity) > maxEntries {
		s.activity = append([]tuiActivityEntry(nil), s.activity[len(s.activity)-maxEntries:]...)
	}
}

func (s *tuiTrackedSourceState) resolveRowForEvent(event output.Event) *tuiTrackRowState {
	if s == nil {
		return nil
	}
	if idx, ok := tuiDetailInt(event.Details, "index"); ok {
		for i := range s.rows {
			if s.rows[i].Index == idx {
				return &s.rows[i]
			}
		}
	}
	trackID := strings.TrimSpace(tuiDetailString(event.Details, "track_id"))
	if trackID != "" {
		for i := range s.rows {
			if strings.TrimSpace(s.rows[i].RemoteID) == trackID {
				return &s.rows[i]
			}
		}
	}
	trackName := strings.TrimSpace(tuiDetailString(event.Details, "track_name"))
	if trackName != "" {
		for i := range s.rows {
			if strings.TrimSpace(s.rows[i].Title) == trackName {
				return &s.rows[i]
			}
		}
	}
	return nil
}

func observeTrackedRowEvent(row *tuiTrackRowState, event output.Event) {
	if row == nil {
		return
	}
	reason := strings.TrimSpace(tuiDetailString(event.Details, "reason"))
	switch event.Event {
	case output.EventTrackStarted:
		row.RuntimeStatus = tuiTrackStatusDownloading
		row.ProgressKnown = false
		row.ProgressPercent = 0
		row.FailureDetail = ""
	case output.EventTrackProgress:
		row.RuntimeStatus = tuiTrackStatusDownloading
		if percent, ok := tuiDetailFloat(event.Details, "percent"); ok {
			row.ProgressKnown = true
			row.ProgressPercent = percent
		}
	case output.EventTrackDone:
		row.RuntimeStatus = tuiTrackStatusDownloaded
		row.ProgressKnown = true
		row.ProgressPercent = 100
		row.FailureDetail = ""
	case output.EventTrackSkip:
		row.RuntimeStatus = tuiTrackStatusSkipped
		row.ProgressKnown = false
		row.ProgressPercent = 0
		row.FailureDetail = reason
	case output.EventTrackFail:
		row.RuntimeStatus = tuiTrackStatusFailed
		row.ProgressKnown = false
		row.ProgressPercent = 0
		row.FailureDetail = reason
	default:
		return
	}
	row.StatusLabel = tuiTrackStatusLabel(row.RuntimeStatus, row.ProgressPercent, row.ProgressKnown, row.FailureDetail)
}
