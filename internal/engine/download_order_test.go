package engine

import (
	"testing"

	"github.com/jaa/update-downloads/internal/config"
)

func TestNormalizeDownloadOrderUsesSharedDefaultAndPreservesExplicitValues(t *testing.T) {
	if DefaultDownloadOrder != DownloadOrderOldestFirst {
		t.Fatalf("sync default = %q, want oldest_first", DefaultDownloadOrder)
	}
	for _, input := range []DownloadOrder{"", "invalid"} {
		if got := NormalizeDownloadOrder(input); got != DefaultDownloadOrder {
			t.Fatalf("NormalizeDownloadOrder(%q) = %q, want %q", input, got, DefaultDownloadOrder)
		}
	}
	if got := NormalizeDownloadOrder(DownloadOrderNewestFirst); got != DownloadOrderNewestFirst {
		t.Fatalf("explicit newest_first normalized to %q", got)
	}
	if got := NormalizeDownloadOrder(DownloadOrderOldestFirst); got != DownloadOrderOldestFirst {
		t.Fatalf("explicit oldest_first normalized to %q", got)
	}
}

func TestExecutionManifestDefaultAndExplicitOrderWithSparseSelection(t *testing.T) {
	rows := []PlanRow{
		{Index: 1, RemoteID: "one", Toggleable: true},
		{Index: 2, RemoteID: "locked", Toggleable: false},
		{Index: 3, RemoteID: "three", Toggleable: true},
		{Index: 4, RemoteID: "omitted", Toggleable: true},
		{Index: 5, RemoteID: "five", Toggleable: true},
	}

	oldest, err := BuildExecutionManifest("source-a", rows, []int{1, 3, 5}, "")
	if err != nil {
		t.Fatal(err)
	}
	assertExecutionIndices(t, oldest, DownloadOrderOldestFirst, []int{5, 3, 1})

	newest, err := BuildExecutionManifest("source-a", rows, []int{1, 3, 5}, DownloadOrderNewestFirst)
	if err != nil {
		t.Fatal(err)
	}
	assertExecutionIndices(t, newest, DownloadOrderNewestFirst, []int{1, 3, 5})

	empty, err := BuildExecutionManifest("source-a", rows, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	assertExecutionIndices(t, empty, DownloadOrderOldestFirst, nil)

	if _, err := BuildExecutionManifest("source-a", rows, []int{2}, ""); err == nil {
		t.Fatal("locked row selection unexpectedly succeeded")
	}
}

func assertExecutionIndices(t *testing.T, manifest ExecutionManifest, order DownloadOrder, want []int) {
	t.Helper()
	if manifest.DownloadOrder != order {
		t.Fatalf("download order = %q, want %q", manifest.DownloadOrder, order)
	}
	if len(manifest.Execution) != len(want) {
		t.Fatalf("execution length = %d, want %d", len(manifest.Execution), len(want))
	}
	for i, index := range want {
		if manifest.Execution[i].Index != index || manifest.Execution[i].ExecutionSlot != i+1 {
			t.Fatalf("execution[%d] = %+v, want index %d slot %d", i, manifest.Execution[i], index, i+1)
		}
	}
}

func TestSupportsPlanCoversEveryRegisteredPlanAdapter(t *testing.T) {
	// Free DL capture runs through plan mode, so a registered plan provider that
	// SupportsPlan rejects makes the source skip silently instead of downloading.
	syncer := NewSyncer(map[string]Adapter{}, nil, nil)
	for _, tc := range []struct {
		sourceType  config.SourceType
		adapterKind string
	}{
		{config.SourceTypeSoundCloud, "scdl"},
		{config.SourceTypeSoundCloud, "scdl-freedl"},
		{config.SourceTypeSpotify, "deemix"},
	} {
		source := config.Source{Type: tc.sourceType, Adapter: config.AdapterSpec{Kind: tc.adapterKind}}
		if syncer.PlanRegistry.ProviderFor(tc.adapterKind) == nil {
			t.Fatalf("expected a registered plan provider for %q", tc.adapterKind)
		}
		if !SupportsPlan(source) {
			t.Errorf("SupportsPlan(%s/%s) = false, want true", tc.sourceType, tc.adapterKind)
		}
	}

	unsupported := config.Source{Type: config.SourceTypeSpotify, Adapter: config.AdapterSpec{Kind: "spotdl"}}
	if SupportsPlan(unsupported) {
		t.Error("SupportsPlan(spotify/spotdl) = true, want false")
	}
}
