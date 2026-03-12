package output

import (
	"testing"

	compactstate "github.com/jaa/update-downloads/internal/output/compact"
)

func TestStructuredProgressTrackerTracksProgressAndOutcomes(t *testing.T) {
	tracker := NewStructuredProgressTracker(nil)

	tracker.ObserveEvent(Event{
		Event:    EventSourcePreflight,
		SourceID: "sc-source",
		Details: map[string]any{
			"planned_download_count": 2,
		},
	})
	tracker.ObserveEvent(Event{
		Event:    EventTrackStarted,
		SourceID: "sc-source",
		Details: map[string]any{
			"track_name": "Track One",
			"index":      1,
			"total":      9,
		},
	})
	tracker.ObserveEvent(Event{
		Event:    EventTrackProgress,
		SourceID: "sc-source",
		Details: map[string]any{
			"track_name": "Track One",
			"index":      1,
			"total":      9,
			"percent":    50.0,
		},
	})

	snapshot := tracker.Snapshot()
	if snapshot.Track.Name != "Track One" {
		t.Fatalf("expected active track name, got %+v", snapshot.Track)
	}
	if snapshot.Track.Lifecycle != compactstate.TrackLifecycleDownloading {
		t.Fatalf("expected downloading lifecycle, got %+v", snapshot.Track)
	}
	if snapshot.Track.ProgressPercent != 50 {
		t.Fatalf("expected 50%% progress, got %+v", snapshot.Track)
	}
	if tracker.EffectiveTotal() != 2 {
		t.Fatalf("expected planned total 2 to win, got %d", tracker.EffectiveTotal())
	}
	if got := tracker.GlobalProgressPercent(); got != 25 {
		t.Fatalf("expected 25%% global progress, got %.1f", got)
	}

	tracker.ObserveEvent(Event{
		Event:    EventTrackDone,
		SourceID: "sc-source",
		Details: map[string]any{
			"track_name": "Track One",
			"index":      1,
			"total":      9,
		},
	})

	outcomes := tracker.DrainTrackOutcomes()
	if len(outcomes) != 1 {
		t.Fatalf("expected one outcome, got %+v", outcomes)
	}
	if outcomes[0].Kind != StructuredTrackOutcomeDone || outcomes[0].Name != "Track One" {
		t.Fatalf("unexpected outcome %+v", outcomes[0])
	}
	if outcomes[0].Completed != 1 || outcomes[0].Total != 2 {
		t.Fatalf("unexpected outcome counts %+v", outcomes[0])
	}

	snapshot = tracker.Snapshot()
	if snapshot.Track.Lifecycle != compactstate.TrackLifecycleIdle {
		t.Fatalf("expected idle track after completion, got %+v", snapshot.Track)
	}
	if snapshot.Progress.Global.Completed != 1 {
		t.Fatalf("expected one completed track, got %+v", snapshot.Progress.Global)
	}
}

func TestStructuredProgressTrackerEmitsSkipAndFailOutcomes(t *testing.T) {
	tracker := NewStructuredProgressTracker(nil)
	tracker.ObserveEvent(Event{
		Event:    EventSourcePreflight,
		SourceID: "sc-source",
		Details: map[string]any{
			"planned_download_count": 2,
		},
	})
	tracker.ObserveEvent(Event{
		Event:    EventTrackSkip,
		SourceID: "sc-source",
		Details: map[string]any{
			"track_name": "Track One",
			"index":      1,
			"total":      2,
			"reason":     "already-present",
		},
	})
	tracker.ObserveEvent(Event{
		Event:    EventTrackFail,
		SourceID: "sc-source",
		Details: map[string]any{
			"track_name": "Track Two",
			"index":      2,
			"total":      2,
			"reason":     "download-error",
		},
	})

	outcomes := tracker.DrainTrackOutcomes()
	if len(outcomes) != 2 {
		t.Fatalf("expected two outcomes, got %+v", outcomes)
	}
	if outcomes[0].Kind != StructuredTrackOutcomeSkip || outcomes[0].Reason != "already-present" {
		t.Fatalf("unexpected skip outcome %+v", outcomes[0])
	}
	if outcomes[1].Kind != StructuredTrackOutcomeFail || outcomes[1].Reason != "download-error" {
		t.Fatalf("unexpected fail outcome %+v", outcomes[1])
	}
	if got := FormatCompactTrackOutcome(outcomes[0], CompactTrackStatusNames); got != "[skip] Track One (already-present)" {
		t.Fatalf("unexpected formatted skip line %q", got)
	}
	if got := FormatCompactTrackOutcome(outcomes[1], CompactTrackStatusCount); got != "[fail] track 2/2 (download-error)" {
		t.Fatalf("unexpected formatted fail line %q", got)
	}
}
