package runstate

import (
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/output"
)

func TestTrackerMatchesRowsInStablePriorityOrder(t *testing.T) {
	rows := []PlanTrackRow{
		{SourceID: "source-a", RemoteID: "id-1", Title: "First", Index: 1, Toggleable: true, PlanStatus: engine.PlanRowMissingNew, PlanClass: TrackPlanClassNew},
		{SourceID: "source-a", RemoteID: "id-2", Title: "Second", Index: 2, Toggleable: true, PlanStatus: engine.PlanRowMissingNew, PlanClass: TrackPlanClassNew},
		{SourceID: "source-a", RemoteID: "id-3", Title: "Premiere: Third [LABEL]", Index: 3, Toggleable: true, PlanStatus: engine.PlanRowMissingNew, PlanClass: TrackPlanClassNew},
	}
	manifest, err := engine.BuildExecutionManifest("source-a", []engine.PlanRow{
		{Index: 1, RemoteID: "id-1", Title: "First", Toggleable: true},
		{Index: 2, RemoteID: "id-2", Title: "Second", Toggleable: true},
		{Index: 3, RemoteID: "id-3", Title: "Premiere: Third [LABEL]", Toggleable: true},
	}, []int{1, 2, 3}, engine.DownloadOrderOldestFirst)
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker()
	tracker.ConfirmSelection("source-a", rows, manifest, func(int) bool { return true })
	tracker.ObserveEvent(output.Event{
		Event: output.EventTrackDone, SourceID: "source-a",
		Details: map[string]any{"track_id": "id-1", "track_name": "Second", "index": 1},
	}, nil, "", false)
	tracker.ObserveEvent(output.Event{
		Event: output.EventTrackSkip, SourceID: "source-a",
		Details: map[string]any{"track_name": "Premiere： Third [LABEL]", "index": 1, "reason": "duplicate"},
	}, nil, "", false)
	tracker.ObserveEvent(output.Event{
		Event: output.EventTrackProgress, SourceID: "source-a",
		Details: map[string]any{"index": 2, "percent": 25.0},
	}, nil, "", false)

	snapshot := tracker.SourceSnapshot("source-a")
	if snapshot.Rows[0].RuntimeStatus != TrackStatusDownloaded {
		t.Fatalf("track ID must win over conflicting name and execution slot: %+v", snapshot.Rows[0])
	}
	if snapshot.Rows[2].RuntimeStatus != TrackStatusSkipped {
		t.Fatalf("normalized name must win over conflicting execution slot: %+v", snapshot.Rows[2])
	}
	if snapshot.Rows[1].RuntimeStatus != TrackStatusDownloading || snapshot.Rows[1].ProgressPercent != 25 {
		t.Fatalf("execution slot fallback did not resolve the second row: %+v", snapshot.Rows[1])
	}
}

func TestTrackerSnapshotsAreIndependentAndActivityIsBounded(t *testing.T) {
	tracker := NewTracker()
	for i := 0; i < 24; i++ {
		tracker.ObserveEvent(output.Event{
			Event: output.EventSourcePreflight, SourceID: "source-a",
			Timestamp: time.Unix(int64(i), 0),
		}, nil, "history", true)
	}

	first := tracker.SourceSnapshot("source-a")
	if len(first.Activity) != 18 {
		t.Fatalf("expected 18 bounded activity entries, got %d", len(first.Activity))
	}
	first.Activity[0].Message = "mutated"
	second := tracker.SourceSnapshot("source-a")
	if second.Activity[0].Message != "history" {
		t.Fatalf("snapshot mutation leaked into tracker state: %+v", second.Activity[0])
	}
}

func TestFailureStateFromEventConvertsTransportDetailTypes(t *testing.T) {
	failure := FailureStateFromEvent(output.Event{
		Event: output.EventSourceFailed, Level: output.LevelError,
		Details: map[string]any{
			"source_id": "source-a", "failure_message": "failed",
			"exit_code": "7", "timed_out": "true", "interrupted": float64(1),
		},
	})
	if failure == nil || failure.SourceID != "source-a" || failure.Message != "failed" {
		t.Fatalf("unexpected failure: %+v", failure)
	}
	if failure.ExitCode == nil || *failure.ExitCode != 7 || !failure.TimedOut || !failure.Interrupted {
		t.Fatalf("detail conversion failed: %+v", failure)
	}
}

func TestTrackerAggregateCountsAndElapsedLabel(t *testing.T) {
	rows := []PlanTrackRow{
		{SourceID: "s", RemoteID: "1", Title: "one", Index: 1, Toggleable: true, PlanStatus: engine.PlanRowMissingNew, PlanClass: TrackPlanClassNew},
		{SourceID: "s", RemoteID: "2", Title: "two", Index: 2, Toggleable: true, PlanStatus: engine.PlanRowMissingNew, PlanClass: TrackPlanClassNew},
	}
	manifest, err := engine.BuildExecutionManifest("s", []engine.PlanRow{
		{Index: 1, RemoteID: "1", Title: "one", Toggleable: true},
		{Index: 2, RemoteID: "2", Title: "two", Toggleable: true},
	}, []int{1, 2}, engine.DownloadOrderNewestFirst)
	if err != nil {
		t.Fatal(err)
	}
	tracker := NewTracker()
	tracker.ConfirmSelection("s", rows, manifest, func(int) bool { return true })
	tracker.ObserveEvent(output.Event{Event: output.EventTrackDone, SourceID: "s", Details: map[string]any{"track_id": "1"}}, nil, "", false)
	tracker.ObserveEvent(output.Event{Event: output.EventTrackProgress, SourceID: "s", Details: map[string]any{"track_id": "2", "percent": 50}}, nil, "", false)
	selected, completed, skipped, failed, progress := tracker.AggregateCounts(false)
	if selected != 2 || completed != 1 || skipped != 0 || failed != 0 || progress != 75 {
		t.Fatalf("unexpected aggregate: selected=%d completed=%d skipped=%d failed=%d progress=%v", selected, completed, skipped, failed, progress)
	}

	start := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	tracker.MarkRuntimeStarted(start)
	if got := tracker.ElapsedLabel(start.Add(5 * time.Second)); got != "0:05" {
		t.Fatalf("unexpected running elapsed label: %q", got)
	}
	tracker.MarkRunFinished(start.Add(67 * time.Second))
	if got := tracker.ElapsedLabel(start.Add(5 * time.Minute)); got != "1:07" {
		t.Fatalf("unexpected finished elapsed label: %q", got)
	}
}
