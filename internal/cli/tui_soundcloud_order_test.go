package cli

import (
	"testing"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
)

func TestTUISyncSelectorDefaultsToConfiguredSourceOrder(t *testing.T) {
	for _, tc := range []struct {
		saved string
		want  engine.DownloadOrder
	}{
		{saved: "oldest_first", want: engine.DownloadOrderOldestFirst},
		{saved: "newest_first", want: engine.DownloadOrderNewestFirst},
		{saved: "", want: engine.DownloadOrderOldestFirst},
	} {
		t.Run(tc.saved, func(t *testing.T) {
			source := config.Source{ID: "sample", Type: config.SourceTypeSoundCloud, Enabled: true, Adapter: config.AdapterSpec{Kind: "scdl"}, Sync: config.SyncPolicy{DownloadOrder: tc.saved}}
			model := newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
			model, _ = model.Update(tuiConfigLoadedMsg{cfg: config.Config{Sources: []config.Source{source}}})
			if got := model.downloadOrderForSourceID(source.ID); got != tc.want {
				t.Fatalf("selector order=%q want %q", got, tc.want)
			}
			delete(model.interactiveOrders, source.ID)
			if got := model.downloadOrderForSourceID(source.ID); got != tc.want {
				t.Fatalf("fallback order=%q want %q", got, tc.want)
			}
		})
	}
}

func TestTUISoundCloudSelectorUsesAddedOrderForLikesAndSets(t *testing.T) {
	for _, kind := range []string{"likes", "sets/sample"} {
		t.Run(kind, func(t *testing.T) {
			source := config.Source{ID: "sample", Type: config.SourceTypeSoundCloud, URL: "https://soundcloud.com/example/" + kind, Adapter: config.AdapterSpec{Kind: "scdl"}}
			rows := []engine.PlanRow{{Index: 1, RemoteID: "a", Toggleable: true, SelectedByDefault: true}, {Index: 2, RemoteID: "b", Toggleable: true, SelectedByDefault: true}}
			state := newTUIInteractiveSelectionState(tuiPlanSelectRequestMsg{SourceID: source.ID, Rows: rows, DownloadOrder: engine.DownloadOrderOldestFirst, Details: buildPlanSourceDetails(source, config.Defaults{}, 10, engine.PlanWindowFirst, true)})
			first := 2
			if kind == "sets/sample" {
				first = 1
			}
			if state.manifest.Execution[0].Index != first {
				t.Fatalf("wrong selector order: %+v", state.manifest)
			}
			state.setDownloadOrder(engine.DownloadOrderNewestFirst)
			if state.manifest.Execution[0].Index == first {
				t.Fatalf("toggle did not reverse: %+v", state.manifest)
			}
		})
	}
}
