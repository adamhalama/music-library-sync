package runstate

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/output"
)

func TestSourceSnapshotJSONFieldNames(t *testing.T) {
	got, err := json.Marshal(SourceSnapshot{
		Lifecycle: SourceLifecycleRunning,
		Confirmed: true,
		Rows: []TrackRow{{
			SourceID: "s", SourceLabel: "S", RemoteID: "r", Title: "T", Index: 1,
			ExecutionSlot: 2, Toggleable: true, PlanStatus: engine.PlanRowMissingNew,
			PlanClass: TrackPlanClassNew, Selected: true, RunScope: TrackRunScopeIncluded,
			RuntimeStatus: TrackStatusDownloading, StatusLabel: "downloading 25%",
			ProgressKnown: true, ProgressPercent: 25,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"lifecycle":"running","confirmed":true,"rows":[{"source_id":"s","source_label":"S","remote_id":"r","title":"T","index":1,"execution_slot":2,"toggleable":true,"plan_status":"missing_new","plan_class":"new","selected":true,"run_scope":"included","runtime_status":"downloading","status_label":"downloading 25%","progress_known":true,"progress_percent":25}],"activity":null}`
	if string(got) != want {
		t.Fatalf("unexpected JSON\n got: %s\nwant: %s", got, want)
	}
}

func TestAdditionalRunStateDTOJSONFieldNames(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{
			name:  "plan row",
			value: PlanTrackRow{SourceID: "s", SourceLabel: "S", RemoteID: "r", Title: "T", Index: 1, Toggleable: true, PlanStatus: engine.PlanRowMissingNew, PlanClass: TrackPlanClassNew, SelectedByDefault: true},
			want:  `{"source_id":"s","source_label":"S","remote_id":"r","title":"T","index":1,"toggleable":true,"plan_status":"missing_new","plan_class":"new","selected_by_default":true}`,
		},
		{
			name:  "activity",
			value: ActivityEntry{Timestamp: time.Unix(0, 0).UTC(), Level: output.LevelWarn, Message: "m", SourceID: "s"},
			want:  `{"timestamp":"1970-01-01T00:00:00Z","level":"warn","message":"m","source_id":"s"}`,
		},
		{
			name:  "failure",
			value: FailureState{SourceID: "s", Message: "failed", TimedOut: true, Interrupted: false, StderrTail: "tail"},
			want:  `{"source_id":"s","message":"failed","timed_out":true,"interrupted":false,"stderr_tail":"tail"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("unexpected JSON\n got: %s\nwant: %s", got, tt.want)
			}
		})
	}
}
