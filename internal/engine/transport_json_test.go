package engine

import (
	"encoding/json"
	"testing"
)

func TestTransportDTOJSONFieldNames(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"plan row", PlanRow{Index: 1, RemoteID: "r", RemoteURL: "u", Title: "t", Status: PlanRowMissingNew, Toggleable: true, SelectedByDefault: true}, `{"index":1,"remote_id":"r","remote_url":"u","title":"t","status":"missing_new","toggleable":true,"selected_by_default":true}`},
		{"sync result", SyncResult{Total: 1, Attempted: 2, Succeeded: 3, Failed: 4, Skipped: 5, DependencyFailures: 6, Interrupted: true}, `{"total":1,"attempted":2,"succeeded":3,"failed":4,"skipped":5,"dependency_failures":6,"interrupted":true}`},
		{"manifest", ExecutionManifest{SourceID: "s", DownloadOrder: DownloadOrderNewestFirst, SelectedIndices: []int{2}, Execution: []ExecutionEntry{{Index: 2, RemoteID: "r", Title: "t", ExecutionSlot: 1}}}, `{"source_id":"s","download_order":"newest_first","selected_indices":[2],"execution":[{"index":2,"remote_id":"r","title":"t","execution_slot":1}]}`},
		{"preflight", SoundCloudPreflight{RemoteTotal: 1, KnownCount: 2, ArchiveGapCount: 3, KnownGapCount: 4, FirstExistingIndex: 5, PlannedDownloadCount: 6, Mode: SoundCloudModeScanGaps}, `{"remote_total":1,"known_count":2,"archive_gap_count":3,"known_gap_count":4,"first_existing_index":5,"planned_download_count":6,"mode":"scan_gaps"}`},
		{"free dl probe", SoundCloudFreeDLProbe{Status: SoundCloudFreeDLAvailable, PurchaseURL: "https://example.test", Host: "example.test"}, `{"status":"available","purchase_url":"https://example.test","host":"example.test"}`},
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
