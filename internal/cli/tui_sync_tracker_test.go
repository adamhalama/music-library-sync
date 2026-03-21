package cli

import (
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/output"
)

func TestTUISyncRunTrackerObserveEventMapsRowsByIndexTrackIDAndTrackName(t *testing.T) {
	tracker := newTUISyncRunTracker()
	state := newTUIInteractiveSelectionState(tuiPlanSelectRequestMsg{
		SourceID: "source-a",
		Rows: []engine.PlanRow{
			{Index: 1, Title: "Index Match", RemoteID: "idx-1", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
			{Index: 2, Title: "ID Match", RemoteID: "track-2", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
			{Index: 3, Title: "Name Match", RemoteID: "track-3", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
		},
	})
	tracker.ConfirmSelection(state)

	tracker.ObserveEvent(output.Event{
		Event:    output.EventTrackDone,
		SourceID: "source-a",
		Details:  map[string]any{"index": 1},
	}, nil, "", false)
	tracker.ObserveEvent(output.Event{
		Event:    output.EventTrackSkip,
		SourceID: "source-a",
		Details:  map[string]any{"track_id": "track-2", "reason": "duplicate"},
	}, nil, "", false)
	tracker.ObserveEvent(output.Event{
		Event:    output.EventTrackFail,
		SourceID: "source-a",
		Details:  map[string]any{"track_name": "Name Match", "reason": "network"},
	}, nil, "", false)

	source := tracker.SourceSnapshot("source-a")
	if source.rows[0].RuntimeStatus != tuiTrackStatusDownloaded {
		t.Fatalf("expected index-matched row to be downloaded, got %q", source.rows[0].RuntimeStatus)
	}
	if source.rows[1].RuntimeStatus != tuiTrackStatusSkipped || source.rows[1].FailureDetail != "duplicate" {
		t.Fatalf("expected track-id-matched row to be skipped with reason, got %+v", source.rows[1])
	}
	if source.rows[2].RuntimeStatus != tuiTrackStatusFailed || source.rows[2].FailureDetail != "network" {
		t.Fatalf("expected track-name-matched row to be failed with reason, got %+v", source.rows[2])
	}
}

func TestTUISyncRunTrackerAggregateCountsAcrossConfirmedSources(t *testing.T) {
	tracker := newTUISyncRunTracker()

	sourceA := newTUIInteractiveSelectionState(tuiPlanSelectRequestMsg{
		SourceID: "source-a",
		Rows: []engine.PlanRow{
			{Index: 1, Title: "A done", RemoteID: "a-1", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
			{Index: 2, Title: "A skipped", RemoteID: "a-2", Status: engine.PlanRowMissingKnownGap, Toggleable: true, SelectedByDefault: true},
		},
	})
	tracker.ConfirmSelection(sourceA)
	tracker.ObserveEvent(output.Event{
		Event:    output.EventTrackDone,
		SourceID: "source-a",
		Details:  map[string]any{"index": 1},
	}, nil, "", false)
	tracker.ObserveEvent(output.Event{
		Event:    output.EventTrackSkip,
		SourceID: "source-a",
		Details:  map[string]any{"index": 2, "reason": "duplicate"},
	}, nil, "", false)

	sourceB := newTUIInteractiveSelectionState(tuiPlanSelectRequestMsg{
		SourceID: "source-b",
		Rows: []engine.PlanRow{
			{Index: 1, Title: "B downloading", RemoteID: "b-1", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
			{Index: 2, Title: "locked", RemoteID: "b-2", Status: engine.PlanRowAlreadyDownloaded},
		},
	})
	tracker.ConfirmSelection(sourceB)
	tracker.ObserveEvent(output.Event{
		Event:    output.EventTrackProgress,
		SourceID: "source-b",
		Details:  map[string]any{"index": 1, "percent": 50.0},
	}, nil, "", false)

	selected, completed, skipped, failed, progress := tracker.AggregateCounts(false)
	if selected != 3 {
		t.Fatalf("expected three selected tracks across confirmed sources, got %d", selected)
	}
	if completed != 1 || skipped != 1 || failed != 0 {
		t.Fatalf("unexpected aggregate counts: completed=%d skipped=%d failed=%d", completed, skipped, failed)
	}
	if progress < 83.0 || progress > 84.0 {
		t.Fatalf("expected aggregate progress near 83%%, got %.2f", progress)
	}
}

func TestTUISyncRunTrackerBoundsActivityAndTracksFailure(t *testing.T) {
	tracker := newTUISyncRunTracker()
	for i := 0; i < 24; i++ {
		tracker.ObserveEvent(output.Event{
			Event:     output.EventSourcePreflight,
			SourceID:  "source-a",
			Timestamp: time.Unix(int64(i), 0),
			Message:   "ignored",
			Details:   map[string]any{"planned_download_count": i + 1},
		}, nil, "history-line", true)
	}
	tracker.ObserveEvent(output.Event{
		Event:    output.EventSourceFailed,
		Level:    output.LevelError,
		SourceID: "source-a",
		Message:  "[source-a] failed",
		Details: map[string]any{
			"failure_message": "command failed",
			"stderr_tail":     "fatal output",
			"exit_code":       1,
		},
	}, nil, "", false)

	source := tracker.SourceSnapshot("source-a")
	if len(source.activity) != 18 {
		t.Fatalf("expected bounded activity length of 18, got %d", len(source.activity))
	}
	failure := tracker.LastFailure()
	if failure == nil || failure.Message != "command failed" || failure.StderrTail != "fatal output" {
		t.Fatalf("expected failure snapshot to be captured, got %+v", failure)
	}
}

func TestTUISyncRunTrackerElapsedLabelUsesStartAndFinish(t *testing.T) {
	tracker := newTUISyncRunTracker()
	tracker.MarkRuntimeStarted(time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC))
	if got := tracker.ElapsedLabel(time.Date(2026, 3, 21, 12, 0, 5, 0, time.UTC)); got != "0:05" {
		t.Fatalf("unexpected running elapsed label: %q", got)
	}
	tracker.MarkRunFinished(time.Date(2026, 3, 21, 12, 1, 7, 0, time.UTC))
	if got := tracker.ElapsedLabel(time.Date(2026, 3, 21, 12, 5, 0, 0, time.UTC)); got != "1:07" {
		t.Fatalf("unexpected finished elapsed label: %q", got)
	}
}
