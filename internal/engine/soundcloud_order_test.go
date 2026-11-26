package engine

import (
	"context"
	"reflect"
	"testing"

	"github.com/jaa/update-downloads/internal/config"
)

func TestSoundCloudAddedOrderByURLKind(t *testing.T) {
	for _, tc := range []struct {
		name, url string
		args      []string
		oldest    bool
	}{
		{name: "likes", url: "https://soundcloud.com/example/likes"},
		{name: "tracks", url: "https://soundcloud.com/example/tracks"},
		{name: "reposts", url: "https://soundcloud.com/example/reposts"},
		{name: "comments", url: "https://soundcloud.com/example/comments"},
		{name: "user likes", url: "https://soundcloud.com/example", args: []string{"-f"}},
		{name: "user uploads", url: "https://soundcloud.com/example", args: []string{"-t"}},
		{name: "user sets", url: "https://soundcloud.com/example", args: []string{"-p"}},
		{name: "user reposts", url: "https://soundcloud.com/example", args: []string{"-r"}},
		{name: "user all", url: "https://soundcloud.com/example", args: []string{"-a"}},
		{name: "set", url: "https://soundcloud.com/example/sets/sample?secret_token=sample", args: []string{"-f"}, oldest: true},
		{name: "API playlist", url: "https://api.soundcloud.com/playlists/123", oldest: true},
		{name: "web playlist", url: "https://soundcloud.com/example/playlists/sample", oldest: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := config.Source{Type: config.SourceTypeSoundCloud, URL: tc.url, Adapter: config.AdapterSpec{ExtraArgs: tc.args}}
			if got := SoundCloudEnumerationOldestFirst(source); got != tc.oldest {
				t.Fatalf("enumeration oldest=%v", got)
			}
			tracks := []SoundCloudRemoteTrack{{ID: "1"}, {ID: "2"}, {ID: "3"}}
			for _, order := range []DownloadOrder{DownloadOrderOldestFirst, DownloadOrderNewestFirst} {
				got := OrderSoundCloudTracks(source, tracks, order)
				reverse := (order == DownloadOrderOldestFirst) != tc.oldest
				want := []string{"1", "2", "3"}
				if reverse {
					want = []string{"3", "2", "1"}
				}
				ids := []string{got[0].ID, got[1].ID, got[2].ID}
				if !reflect.DeepEqual(ids, want) || tracks[0].ID != "1" {
					t.Fatalf("%s: %v want %v", order, ids, want)
				}
			}
		})
	}
}

func TestSoundCloudSyncAndSelectionUseAddedOrder(t *testing.T) {
	for _, kind := range []string{"likes", "sets/sample"} {
		t.Run(kind, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Defaults.StateDir = t.TempDir()
			source := config.Source{ID: "sample", Type: config.SourceTypeSoundCloud, TargetDir: t.TempDir(), StateFile: "state", URL: "https://soundcloud.com/example/" + kind, Adapter: config.AdapterSpec{Kind: "scdl"}, Sync: config.SyncPolicy{DownloadOrder: "oldest_first"}}
			tracks := []SoundCloudRemoteTrack{{ID: "1", Title: "One"}, {ID: "2", Title: "Two"}, {ID: "3", Title: "Three"}}
			original := enumerateSoundCloudTracksFn
			enumerateSoundCloudTracksFn = func(context.Context, config.Source) ([]soundCloudRemoteTrack, error) { return tracks, nil }
			defer func() { enumerateSoundCloudTracksFn = original }()
			syncer := NewSyncer(nil, nil, &captureEventEmitter{})
			prepared, err := syncer.prepareSoundCloudExecutionPlan(context.Background(), cfg, source, SyncOptions{DryRun: true})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := NewSCDLPlanProvider().BuildWithTracks(context.Background(), cfg, source, SyncOptions{}, tracks)
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := BuildExecutionManifest(source.ID, plan.Rows(), []int{1, 3}, DownloadOrderOldestFirst)
			if err != nil {
				t.Fatal(err)
			}
			execution, err := plan.ApplySelection(manifest, PlanApplyOptions{DryRun: true})
			if err != nil {
				t.Fatal(err)
			}
			want := []int{3, 1}
			if kind == "sets/sample" {
				want = []int{1, 3}
			}
			if !reflect.DeepEqual(execution.SourceForExec.SelectedPlaylistIDs, want) || execution.PlannedSoundCloudTracks[0].ID != tracks[want[0]-1].ID || prepared.PlannedTracks[0].ID != tracks[want[0]-1].ID {
				t.Fatalf("wrong source order: %+v %+v", prepared, execution)
			}
		})
	}
}
