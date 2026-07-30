package output

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/output/compact"
)

func TestStructuredProgressTransportUsesSnakeCase(t *testing.T) {
	value := StructuredProgressSnapshot{
		Progress: compact.ProgressModel{
			Source: compact.SourceProgress{
				ID: "source-a", PlannedTotal: 2, ItemTotal: 2, ItemIndex: 1,
			},
			Track:  compact.TrackProgress{Name: "Track", ProgressPercent: 50},
			Global: compact.GlobalProgress{Total: 2, Completed: 1},
		},
		Track: StructuredTrackState{
			Name: "Track", ProgressKnown: true, ProgressPercent: 50,
		},
		StructuredTrackEvents: true,
	}
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, field := range []string{
		`"structured_track_events"`, `"planned_total"`, `"item_total"`,
		`"item_index"`, `"progress_percent"`, `"progress_known"`,
	} {
		if !strings.Contains(text, field) {
			t.Fatalf("missing snake_case field %s in %s", field, text)
		}
	}
	for _, leaked := range []string{`"Progress"`, `"PlannedTotal"`, `"ProgressPercent"`} {
		if strings.Contains(text, leaked) {
			t.Fatalf("exported Go field leaked into protocol as %s: %s", leaked, text)
		}
	}
}
