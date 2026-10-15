package progress

import (
	"encoding/json"
	"testing"
)

func TestTrackEventJSONFieldNames(t *testing.T) {
	got, err := json.Marshal(TrackEvent{
		SourceID: "s", AdapterKind: "a", TrackID: "id", TrackName: "name",
		Index: 1, Total: 2, Percent: 50, Reason: "reason", Kind: TrackProgress,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"source_id":"s","adapter_kind":"a","track_id":"id","track_name":"name","index":1,"total":2,"percent":50,"reason":"reason","kind":"track_progress"}`
	if string(got) != want {
		t.Fatalf("unexpected JSON\n got: %s\nwant: %s", got, want)
	}
}
