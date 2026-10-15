package runstate

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/output"
)

type Tracker struct {
	sources     map[string]*trackedSourceState
	lastFailure *FailureState
	startedAt   time.Time
	finishedAt  time.Time
}

type trackedSourceState struct {
	lifecycle      SourceLifecycle
	confirmed      bool
	rows           []TrackRow
	rowIndexBySlot map[int]int
	activity       []ActivityEntry
}

func NewTracker() *Tracker {
	return &Tracker{sources: map[string]*trackedSourceState{}}
}

func (t *Tracker) Reset(sources []config.Source) {
	if t == nil {
		return
	}
	t.sources = map[string]*trackedSourceState{}
	for _, source := range sources {
		t.sources[source.ID] = &trackedSourceState{lifecycle: SourceLifecycleIdle}
	}
	t.lastFailure = nil
	t.startedAt = time.Time{}
	t.finishedAt = time.Time{}
}

func (t *Tracker) ensureSource(sourceID string) *trackedSourceState {
	if t == nil {
		return nil
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil
	}
	if t.sources == nil {
		t.sources = map[string]*trackedSourceState{}
	}
	if state, ok := t.sources[sourceID]; ok {
		return state
	}
	state := &trackedSourceState{lifecycle: SourceLifecycleIdle}
	t.sources[sourceID] = state
	return state
}

func (t *Tracker) ConfirmSelection(sourceID string, rows []PlanTrackRow, manifest engine.ExecutionManifest, selected func(int) bool) {
	if t == nil {
		return
	}
	source := t.ensureSource(sourceID)
	if source == nil {
		return
	}
	source.confirmed = true
	source.rows, source.rowIndexBySlot = buildTrackedRowsForSelection(rows, manifest, selected)
}

func (t *Tracker) SetSourceLifecycle(sourceID string, lifecycle SourceLifecycle) {
	if source := t.ensureSource(sourceID); source != nil {
		source.lifecycle = lifecycle
	}
}

func (t *Tracker) SourceLifecycle(sourceID string) SourceLifecycle {
	if source := t.ensureSource(sourceID); source != nil {
		return source.lifecycle
	}
	return SourceLifecycleIdle
}

func (t *Tracker) SourceSnapshot(sourceID string) SourceSnapshot {
	source := t.ensureSource(sourceID)
	if source == nil {
		return SourceSnapshot{Lifecycle: SourceLifecycleIdle}
	}
	return SourceSnapshot{
		Lifecycle: source.lifecycle,
		Confirmed: source.confirmed,
		Rows:      append([]TrackRow(nil), source.rows...),
		Activity:  append([]ActivityEntry(nil), source.activity...),
	}
}

func (t *Tracker) MarkRuntimeStarted(at time.Time) {
	if t == nil {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	t.startedAt, t.finishedAt = at, time.Time{}
}

func (t *Tracker) MarkRunFinished(at time.Time) {
	if t == nil {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	t.finishedAt = at
}

func (t *Tracker) ObserveEvent(event output.Event, outcomes []output.StructuredTrackOutcome, historyLine string, historyOK bool) {
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
		source.lifecycle = SourceLifecyclePreflight
	case output.EventSourceStarted:
		source.lifecycle = SourceLifecycleRunning
	case output.EventSourceFinished:
		source.lifecycle = SourceLifecycleFinished
	case output.EventSourceFailed:
		source.lifecycle = SourceLifecycleFailed
	}
	row := source.resolveRowForEvent(event)
	if row == nil {
		row = source.resolveRowForOutcomes(outcomes)
	}
	if row != nil {
		observeTrackedRowEvent(row, event)
	}
	for _, outcome := range outcomes {
		level := output.LevelInfo
		if outcome.Kind == output.StructuredTrackOutcomeSkip {
			level = output.LevelWarn
		} else if outcome.Kind == output.StructuredTrackOutcomeFail {
			level = output.LevelError
		}
		source.appendActivity(ActivityEntry{Timestamp: event.Timestamp, Level: level, Message: output.FormatCompactTrackOutcome(outcome, output.CompactTrackStatusNames), SourceID: sourceID})
	}
	if historyOK {
		source.appendActivity(ActivityEntry{Timestamp: event.Timestamp, Level: event.Level, Message: historyLine, SourceID: sourceID})
	}
	if failure := FailureStateFromEvent(event); failure != nil {
		t.lastFailure = failure
	}
}

// RowForEvent returns the fully resolved plan row after ObserveEvent has
// applied an adapter event. Adapter indices are execution counters, so callers
// must not construct a row identity directly from them.
func (t *Tracker) RowForEvent(event output.Event) *TrackRow {
	if t == nil {
		return nil
	}
	source := t.ensureSource(strings.TrimSpace(event.SourceID))
	if source == nil {
		return nil
	}
	row := source.resolveRowForEvent(event)
	if row == nil {
		return nil
	}
	copy := *row
	return &copy
}

func (t *Tracker) AggregateCounts(doneWithoutError bool) (selected, completed, skipped, failed int, progressPercent float64) {
	if t == nil {
		return
	}
	for _, source := range t.sources {
		if source == nil || !source.confirmed {
			continue
		}
		for _, row := range source.rows {
			if row.RunScope != TrackRunScopeIncluded {
				continue
			}
			selected++
			switch row.RuntimeStatus {
			case TrackStatusDownloaded:
				completed++
				progressPercent++
			case TrackStatusSkipped:
				skipped++
				progressPercent++
			case TrackStatusFailed:
				failed++
				progressPercent++
			case TrackStatusDownloading:
				if row.ProgressKnown {
					progressPercent += row.ProgressPercent / 100
				}
			}
		}
	}
	if selected > 0 {
		progressPercent = progressPercent / float64(selected) * 100
	}
	if progressPercent < 0 {
		progressPercent = 0
	} else if progressPercent > 100 {
		progressPercent = 100
	}
	if doneWithoutError && selected > 0 && completed+skipped+failed == selected {
		progressPercent = 100
	}
	return
}

func (t *Tracker) ElapsedLabel(now time.Time) string {
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
	totalSeconds := int(end.Sub(t.startedAt).Round(time.Second) / time.Second)
	if totalSeconds < 0 {
		totalSeconds = 0
	}
	return fmt.Sprintf("%d:%02d", totalSeconds/60, totalSeconds%60)
}

func (t *Tracker) LastFailure() *FailureState {
	if t == nil || t.lastFailure == nil {
		return nil
	}
	failure := *t.lastFailure
	return &failure
}

func buildTrackedRowsForSelection(rowsIn []PlanTrackRow, manifest engine.ExecutionManifest, selected func(int) bool) ([]TrackRow, map[int]int) {
	if len(rowsIn) == 0 {
		return nil, nil
	}
	rows := make([]TrackRow, 0, len(rowsIn))
	for _, row := range rowsIn {
		rows = append(rows, DisplayRowFromPlanRow(row, selected(row.Index)))
	}
	rowIndexBySlot := make(map[int]int, len(manifest.Execution))
	for _, entry := range manifest.Execution {
		for i := range rows {
			if rows[i].Index == entry.Index {
				rows[i].ExecutionSlot = entry.ExecutionSlot
				rowIndexBySlot[entry.ExecutionSlot] = i
				break
			}
		}
	}
	return rows, rowIndexBySlot
}

func (s *trackedSourceState) appendActivity(entry ActivityEntry) {
	if s == nil || strings.TrimSpace(entry.Message) == "" {
		return
	}
	s.activity = append(s.activity, entry)
	const maxEntries = 18
	if len(s.activity) > maxEntries {
		s.activity = append([]ActivityEntry(nil), s.activity[len(s.activity)-maxEntries:]...)
	}
}

func (s *trackedSourceState) resolveRowForEvent(event output.Event) *TrackRow {
	if s == nil {
		return nil
	}
	if trackID := strings.TrimSpace(DetailString(event.Details, "track_id")); trackID != "" {
		for i := range s.rows {
			if strings.TrimSpace(s.rows[i].RemoteID) == trackID {
				return &s.rows[i]
			}
		}
	}
	if trackName := strings.TrimSpace(DetailString(event.Details, "track_name")); trackName != "" {
		normalized := NormalizeTrackMatchKey(trackName)
		for i := range s.rows {
			if strings.TrimSpace(s.rows[i].Title) == trackName {
				return &s.rows[i]
			}
		}
		if normalized != "" {
			for i := range s.rows {
				if NormalizeTrackMatchKey(s.rows[i].Title) == normalized {
					return &s.rows[i]
				}
			}
		}
	}
	if idx, ok := DetailInt(event.Details, "index"); ok {
		if rowPos, found := s.rowIndexBySlot[idx]; found && rowPos >= 0 && rowPos < len(s.rows) {
			return &s.rows[rowPos]
		}
		if !s.confirmed {
			for i := range s.rows {
				if s.rows[i].Index == idx {
					return &s.rows[i]
				}
			}
		}
	}
	return nil
}

func (s *trackedSourceState) resolveRowForOutcomes(outcomes []output.StructuredTrackOutcome) *TrackRow {
	for _, outcome := range outcomes {
		name := strings.TrimSpace(outcome.Name)
		if name == "" {
			continue
		}
		for i := range s.rows {
			if strings.TrimSpace(s.rows[i].Title) == name {
				return &s.rows[i]
			}
		}
		normalized := NormalizeTrackMatchKey(name)
		for i := range s.rows {
			if normalized != "" && NormalizeTrackMatchKey(s.rows[i].Title) == normalized {
				return &s.rows[i]
			}
		}
	}
	return nil
}

func NormalizeTrackMatchKey(raw string) string {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	prevSpace := false
	for _, r := range trimmed {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevSpace = false
		case unicode.IsSpace(r):
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
		default:
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func observeTrackedRowEvent(row *TrackRow, event output.Event) {
	reason := strings.TrimSpace(DetailString(event.Details, "reason"))
	switch event.Event {
	case output.EventTrackStarted:
		row.RuntimeStatus, row.ProgressKnown, row.ProgressPercent, row.FailureDetail = TrackStatusDownloading, false, 0, ""
	case output.EventTrackProgress:
		row.RuntimeStatus = TrackStatusDownloading
		if percent, ok := DetailFloat(event.Details, "percent"); ok {
			row.ProgressKnown, row.ProgressPercent = true, percent
		}
	case output.EventTrackDone:
		row.RuntimeStatus, row.ProgressKnown, row.ProgressPercent, row.FailureDetail = TrackStatusDownloaded, true, 100, ""
	case output.EventTrackSkip:
		row.RuntimeStatus, row.ProgressKnown, row.ProgressPercent, row.FailureDetail = TrackStatusSkipped, false, 0, reason
	case output.EventTrackFail:
		row.RuntimeStatus, row.ProgressKnown, row.ProgressPercent, row.FailureDetail = TrackStatusFailed, false, 0, reason
	default:
		return
	}
	row.StatusLabel = TrackStatusLabel(row.RuntimeStatus, row.ProgressPercent, row.ProgressKnown, row.FailureDetail)
}

func FailureStateFromEvent(event output.Event) *FailureState {
	if event.Event != output.EventSourceFailed || event.Level != output.LevelError {
		return nil
	}
	failure := &FailureState{
		SourceID: strings.TrimSpace(event.SourceID), Message: strings.TrimSpace(event.Message),
		TimedOut: DetailBool(event.Details, "timed_out"), Interrupted: DetailBool(event.Details, "interrupted"),
		StdoutTail:     strings.TrimSpace(DetailString(event.Details, "stdout_tail")),
		StderrTail:     strings.TrimSpace(DetailString(event.Details, "stderr_tail")),
		FailureLogPath: strings.TrimSpace(DetailString(event.Details, "failure_log_path")),
	}
	if message := strings.TrimSpace(DetailString(event.Details, "failure_message")); message != "" {
		failure.Message = message
	}
	if sourceID := strings.TrimSpace(DetailString(event.Details, "source_id")); sourceID != "" {
		failure.SourceID = sourceID
	}
	if failure.SourceID == "" {
		failure.SourceID = "sync"
	}
	if exitCode, ok := DetailInt(event.Details, "exit_code"); ok {
		failure.ExitCode = &exitCode
	}
	return failure
}

func DetailString(details map[string]any, key string) string {
	raw, ok := details[key]
	if !ok {
		return ""
	}
	if value, ok := raw.(string); ok {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(fmt.Sprintf("%v", raw))
}

func DetailInt(details map[string]any, key string) (int, bool) {
	raw, ok := details[key]
	if !ok {
		return 0, false
	}
	switch value := raw.(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		return parsed, err == nil
	default:
		return 0, false
	}
}

func DetailFloat(details map[string]any, key string) (float64, bool) {
	raw, ok := details[key]
	if !ok {
		return 0, false
	}
	switch value := raw.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func DetailBool(details map[string]any, key string) bool {
	raw, ok := details[key]
	if !ok {
		return false
	}
	switch value := raw.(type) {
	case bool:
		return value
	case string:
		value = strings.TrimSpace(strings.ToLower(value))
		return value == "1" || value == "true" || value == "yes"
	case int:
		return value != 0
	case int64:
		return value != 0
	case float64:
		return value != 0
	default:
		return false
	}
}
