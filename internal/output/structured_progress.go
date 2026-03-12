package output

import (
	"fmt"
	"strings"

	compactstate "github.com/jaa/update-downloads/internal/output/compact"
)

type StructuredTrackState struct {
	Name            string
	ProgressKnown   bool
	ProgressPercent float64
	Lifecycle       compactstate.TrackLifecycle
}

type StructuredTrackOutcomeKind string

const (
	StructuredTrackOutcomeDone StructuredTrackOutcomeKind = "done"
	StructuredTrackOutcomeSkip StructuredTrackOutcomeKind = "skip"
	StructuredTrackOutcomeFail StructuredTrackOutcomeKind = "fail"
)

type StructuredTrackOutcome struct {
	Kind      StructuredTrackOutcomeKind
	Name      string
	Reason    string
	Completed int
	Total     int
}

type StructuredProgressSnapshot struct {
	Progress              compactstate.ProgressModel
	Track                 StructuredTrackState
	StructuredTrackEvents bool
}

type StructuredProgressTracker struct {
	progress              *compactstate.StateMachine
	track                 StructuredTrackState
	structuredTrackEvents bool
	pendingOutcomes       []StructuredTrackOutcome
}

func NewStructuredProgressTracker(progress *compactstate.StateMachine) *StructuredProgressTracker {
	if progress == nil {
		progress = compactstate.NewStateMachine()
	}
	tracker := &StructuredProgressTracker{progress: progress}
	tracker.Reset()
	return tracker
}

func (t *StructuredProgressTracker) Reset() {
	if t == nil {
		return
	}
	if t.progress == nil {
		t.progress = compactstate.NewStateMachine()
	}
	t.progress.Reset()
	t.track = StructuredTrackState{Lifecycle: compactstate.TrackLifecycleIdle}
	t.structuredTrackEvents = false
	t.pendingOutcomes = nil
}

func (t *StructuredProgressTracker) ObserveEvent(event Event) {
	if t == nil {
		return
	}
	if t.progress == nil {
		t.progress = compactstate.NewStateMachine()
	}

	switch event.Event {
	case EventSyncStarted:
		t.Reset()
	case EventSourcePreflight:
		planned, ok := eventDetailInt(event.Details, "planned_download_count")
		if !ok {
			return
		}
		t.progress.SetPlanningSource(event.SourceID, planned)
		t.track = StructuredTrackState{Lifecycle: compactstate.TrackLifecycleIdle}
	case EventSourceStarted:
		t.progress.BeginSource(event.SourceID)
	case EventSourceFinished:
		t.progress.FinishSource(event.SourceID, false)
		t.track = StructuredTrackState{Lifecycle: compactstate.TrackLifecycleIdle}
	case EventSourceFailed:
		t.progress.FinishSource(event.SourceID, true)
		t.track = StructuredTrackState{Lifecycle: compactstate.TrackLifecycleIdle}
	case EventTrackStarted:
		t.structuredTrackEvents = true
		index, total := t.updateItemPosition(event)
		name := t.resolveTrackName(event, true)
		if name == "" {
			name = formatTrackCounterLabel(index, total)
		}
		t.track = StructuredTrackState{
			Name:            name,
			ProgressKnown:   true,
			ProgressPercent: 0,
			Lifecycle:       compactstate.TrackLifecycleDownloading,
		}
	case EventTrackProgress:
		t.structuredTrackEvents = true
		t.updateItemPosition(event)
		name := strings.TrimSpace(eventDetailString(event.Details, "track_name"))
		if name != "" {
			t.track.Name = name
		} else if t.track.Name == "" {
			t.track.Name = strings.TrimSpace(eventDetailString(event.Details, "track_id"))
		}
		if percent, ok := eventDetailFloat(event.Details, "percent"); ok {
			t.track.ProgressKnown = true
			t.track.ProgressPercent = compactstate.ClampPercent(percent)
		}
		if t.track.Lifecycle == compactstate.TrackLifecycleIdle {
			t.track.Lifecycle = compactstate.TrackLifecycleDownloading
		}
	case EventTrackDone:
		t.structuredTrackEvents = true
		t.updateItemPosition(event)
		name := t.resolveTrackName(event, true)
		t.progress.CompleteTrack()
		t.pendingOutcomes = append(t.pendingOutcomes, StructuredTrackOutcome{
			Kind:      StructuredTrackOutcomeDone,
			Name:      name,
			Completed: t.progress.Completed(),
			Total:     t.progress.EffectiveTotal(),
		})
		t.track = StructuredTrackState{Lifecycle: compactstate.TrackLifecycleIdle}
	case EventTrackSkip:
		t.structuredTrackEvents = true
		t.updateItemPosition(event)
		name := t.resolveTrackName(event, true)
		t.progress.CompleteTrack()
		t.pendingOutcomes = append(t.pendingOutcomes, StructuredTrackOutcome{
			Kind:      StructuredTrackOutcomeSkip,
			Name:      name,
			Reason:    strings.TrimSpace(eventDetailString(event.Details, "reason")),
			Completed: t.progress.Completed(),
			Total:     t.progress.EffectiveTotal(),
		})
		t.track = StructuredTrackState{Lifecycle: compactstate.TrackLifecycleIdle}
	case EventTrackFail:
		t.structuredTrackEvents = true
		t.updateItemPosition(event)
		name := t.resolveTrackName(event, true)
		t.progress.CompleteTrack()
		t.pendingOutcomes = append(t.pendingOutcomes, StructuredTrackOutcome{
			Kind:      StructuredTrackOutcomeFail,
			Name:      name,
			Reason:    strings.TrimSpace(eventDetailString(event.Details, "reason")),
			Completed: t.progress.Completed(),
			Total:     t.progress.EffectiveTotal(),
		})
		t.track = StructuredTrackState{Lifecycle: compactstate.TrackLifecycleIdle}
	}
}

func (t *StructuredProgressTracker) Snapshot() StructuredProgressSnapshot {
	if t == nil {
		return StructuredProgressSnapshot{}
	}
	return StructuredProgressSnapshot{
		Progress:              t.progress.Snapshot(),
		Track:                 t.track,
		StructuredTrackEvents: t.structuredTrackEvents,
	}
}

func (t *StructuredProgressTracker) DrainTrackOutcomes() []StructuredTrackOutcome {
	if t == nil || len(t.pendingOutcomes) == 0 {
		return nil
	}
	out := append([]StructuredTrackOutcome(nil), t.pendingOutcomes...)
	t.pendingOutcomes = nil
	return out
}

func (t *StructuredProgressTracker) EffectiveTotal() int {
	if t == nil || t.progress == nil {
		return 0
	}
	return t.progress.EffectiveTotal()
}

func (t *StructuredProgressTracker) CurrentIndex() int {
	if t == nil || t.progress == nil {
		return 0
	}
	return t.progress.CurrentIndex()
}

func (t *StructuredProgressTracker) GlobalProgressPercent() float64 {
	if t == nil || t.progress == nil {
		return 0
	}
	total := t.progress.EffectiveTotal()
	if total <= 0 {
		return 0
	}
	partial := 0.0
	if t.progress.Completed() < total && t.track.Lifecycle != compactstate.TrackLifecycleIdle {
		switch {
		case t.track.Lifecycle == compactstate.TrackLifecycleDone || t.track.Lifecycle == compactstate.TrackLifecycleSkipped:
			partial = 1.0
		case t.track.ProgressKnown:
			partial = t.track.ProgressPercent / 100.0
		}
	}
	return t.progress.GlobalProgressPercent(partial)
}

func (t *StructuredProgressTracker) resolveTrackName(event Event, allowCounterFallback bool) string {
	name := strings.TrimSpace(eventDetailString(event.Details, "track_name"))
	if name == "" {
		name = strings.TrimSpace(eventDetailString(event.Details, "track_id"))
	}
	if name == "" {
		name = strings.TrimSpace(t.track.Name)
	}
	if name == "" && allowCounterFallback {
		name = formatTrackCounterLabel(t.progress.CurrentIndex(), t.progress.EffectiveTotal())
	}
	return name
}

func (t *StructuredProgressTracker) updateItemPosition(event Event) (int, int) {
	index, hasIndex := eventDetailInt(event.Details, "index")
	total, hasTotal := eventDetailInt(event.Details, "total")
	if hasIndex || hasTotal {
		if !hasTotal {
			total = -1
		}
		t.progress.SetItemIndex(index, total)
	}
	return t.progress.CurrentIndex(), t.progress.EffectiveTotal()
}

func FormatCompactTrackOutcome(outcome StructuredTrackOutcome, mode string) string {
	mode = strings.TrimSpace(strings.ToLower(mode))
	switch mode {
	case "", CompactTrackStatusNames, CompactTrackStatusCount, CompactTrackStatusNone:
	default:
		mode = CompactTrackStatusNames
	}
	if mode == "" {
		mode = CompactTrackStatusNames
	}

	prefix := "[done]"
	switch outcome.Kind {
	case StructuredTrackOutcomeSkip:
		prefix = "[skip]"
	case StructuredTrackOutcomeFail:
		prefix = "[fail]"
	}

	label := ""
	switch mode {
	case CompactTrackStatusCount:
		label = formatTrackCounterLabel(outcome.Completed, outcome.Total)
	case CompactTrackStatusNone:
		label = ""
	default:
		label = strings.TrimSpace(outcome.Name)
		if label == "" {
			label = formatTrackCounterLabel(outcome.Completed, outcome.Total)
		}
	}

	line := prefix
	if label != "" {
		line = fmt.Sprintf("%s %s", prefix, label)
	}
	if reason := strings.TrimSpace(outcome.Reason); reason != "" {
		line = fmt.Sprintf("%s (%s)", line, reason)
	}
	return line
}

func formatTrackCounterLabel(index int, total int) string {
	if index < 1 {
		index = 1
	}
	if total > 0 {
		if index > total {
			index = total
		}
		return fmt.Sprintf("track %d/%d", index, total)
	}
	return fmt.Sprintf("track %d", index)
}
