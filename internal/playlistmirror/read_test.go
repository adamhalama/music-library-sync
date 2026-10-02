package playlistmirror

import (
	"context"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/navidrome"
	"github.com/jaa/update-downloads/internal/playlists"
	"github.com/jaa/update-downloads/internal/rekordbox/bridge"
)

type stubNavidromeReader struct {
	playlists []navidrome.Playlist
	details   map[string][]navidrome.Song
	catalog   []navidrome.Song
}

func (stub stubNavidromeReader) Playlists(context.Context) ([]navidrome.Playlist, error) {
	return stub.playlists, nil
}

func (stub stubNavidromeReader) Playlist(_ context.Context, id string) (navidrome.Playlist, []navidrome.Song, error) {
	for _, item := range stub.playlists {
		if item.ID == id {
			return item, stub.details[id], nil
		}
	}
	return navidrome.Playlist{}, nil, nil
}

func (stub stubNavidromeReader) Songs(context.Context) ([]navidrome.Song, error) {
	return stub.catalog, nil
}

func TestReadRekordboxJoinsOrderedContentAndMetadata(t *testing.T) {
	inspect := bridge.InspectResponse{
		Playlists: []bridge.Playlist{{ID: "rb-1", Name: "renamed", Attribute: 0, ContentIDs: []string{"c2", "c1"}}},
		Contents: []bridge.Content{
			{ID: "c1", Artist: "A", Title: "One", Album: "Set", DurationSeconds: 61, FolderPath: "/Music/One.mp3"},
			{ID: "c2", Artist: "B", Title: "Two", FolderPath: "file:///Music/Two.mp3"},
		},
	}
	binding := &playlists.ProviderBinding{PlaylistID: "rb-1", PlaylistName: "old"}
	got, err := ReadRekordbox(inspect, playlists.PlaylistSelector{Playlist: "old"}, binding, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "renamed" || len(got.Tracks) != 2 || got.Tracks[0].ProviderID != "c2" || got.Tracks[1].Duration != "1:01" {
		t.Fatalf("unexpected ordered Rekordbox read: %#v", got)
	}
}

func TestReadRekordboxRejectsMissingContentAndAmbiguousName(t *testing.T) {
	if _, err := ReadRekordbox(bridge.InspectResponse{
		Playlists: []bridge.Playlist{{ID: "rb-1", Name: "favs", ContentIDs: []string{"missing"}}},
	}, playlists.PlaylistSelector{Playlist: "favs"}, nil, false); err == nil || !strings.Contains(err.Error(), "missing content ID") {
		t.Fatalf("missing content error = %v", err)
	}
	if _, err := ReadRekordbox(bridge.InspectResponse{
		Playlists: []bridge.Playlist{{ID: "1", Name: "favs"}, {ID: "2", Name: "favs"}},
	}, playlists.PlaylistSelector{Playlist: "favs"}, nil, false); err == nil || !strings.Contains(err.Error(), "multiple Rekordbox") {
		t.Fatalf("ambiguous name error = %v", err)
	}
}

func TestReadNavidromePreservesOrderOwnershipAndSmartStatus(t *testing.T) {
	stub := stubNavidromeReader{
		playlists: []navidrome.Playlist{{ID: "nd-1", Name: "renamed", Owner: "dj", Smart: true}},
		details: map[string][]navidrome.Song{"nd-1": {
			{ID: "s2", Title: "Two", Path: "/Music/Two.mp3"},
			{ID: "s1", Title: "One", Path: "/Music/One.mp3"},
		}},
	}
	binding := &playlists.ProviderBinding{PlaylistID: "nd-1", PlaylistName: "old"}
	got, err := ReadNavidrome(context.Background(), stub, playlists.PlaylistSelector{Playlist: "old"}, binding, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "renamed" || got.Owner != "dj" || !got.Smart || got.Tracks[0].ProviderID != "s2" {
		t.Fatalf("unexpected Navidrome read: %#v", got)
	}
}

func TestNavidromeResolutionUsesExactNamesAndCanPlanCreation(t *testing.T) {
	stub := stubNavidromeReader{playlists: []navidrome.Playlist{{ID: "upper", Name: "FAVS"}}, details: map[string][]navidrome.Song{}}
	created, err := ReadNavidrome(context.Background(), stub, playlists.PlaylistSelector{Playlist: "favs"}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if !created.Missing || created.Name != "favs" {
		t.Fatalf("case-insensitive playlist must not be adopted: %#v", created)
	}
	stub.playlists = append(stub.playlists,
		navidrome.Playlist{ID: "one", Name: "favs"}, navidrome.Playlist{ID: "two", Name: "favs"})
	if _, err := ReadNavidrome(context.Background(), stub, playlists.PlaylistSelector{Playlist: "favs"}, nil, true); err == nil || !strings.Contains(err.Error(), "multiple Navidrome") {
		t.Fatalf("ambiguous name error = %v", err)
	}
}

func TestReadNavidromeCatalogRequiresRealAbsolutePathsAndKeepsAmbiguity(t *testing.T) {
	stub := stubNavidromeReader{catalog: []navidrome.Song{
		{ID: "s1", Path: "/Music/Café.mp3"},
		{ID: "s2", Path: "/Music/Café.mp3"},
	}}
	index, err := ReadNavidromeCatalog(context.Background(), stub)
	if err != nil {
		t.Fatal(err)
	}
	if len(index["/Music/Café.mp3"]) != 2 {
		t.Fatalf("canonical path ambiguity was collapsed: %#v", index)
	}
	stub.catalog = []navidrome.Song{{ID: "relative", Path: "Music/one.mp3"}}
	if _, err := ReadNavidromeCatalog(context.Background(), stub); err == nil || !strings.Contains(err.Error(), "DefaultReportRealPath") {
		t.Fatalf("relative path error = %v", err)
	}
	stub.catalog = []navidrome.Song{{ID: "same", Path: "/Music/one.mp3"}, {ID: "same", Path: "/Music/two.mp3"}}
	if _, err := ReadNavidromeCatalog(context.Background(), stub); err == nil || !strings.Contains(err.Error(), "duplicate song ID") {
		t.Fatalf("duplicate ID error = %v", err)
	}
}

func TestIndexRekordboxCatalogKeepsPathAmbiguityAndRejectsDuplicateIDs(t *testing.T) {
	index, err := IndexRekordboxCatalog([]bridge.Content{
		{ID: "c1", FolderPath: "/Music/Café.mp3"},
		{ID: "c2", FolderPath: "/Music/Café.mp3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(index["/Music/Café.mp3"]) != 2 {
		t.Fatalf("canonical path ambiguity was collapsed: %#v", index)
	}
	if _, err := IndexRekordboxCatalog([]bridge.Content{{ID: "c1", FolderPath: "/one"}, {ID: "c1", FolderPath: "/two"}}); err == nil {
		t.Fatal("expected duplicate content ID refusal")
	}
}

func TestIndexRekordboxCatalogSkipsNonFileRowsButValidatesTheirIDs(t *testing.T) {
	contents := []bridge.Content{
		{ID: "local", FolderPath: "file:///Music/one.mp3"},
		{ID: "streaming"},
		{ID: "relative", FolderPath: "Music/two.mp3"},
		{ID: "service", FolderPath: "beatport://track/123"},
	}
	index, err := IndexRekordboxCatalog(contents)
	if err != nil {
		t.Fatal(err)
	}
	if len(index) != 1 || len(index["/Music/one.mp3"]) != 1 || index["/Music/one.mp3"][0].ID != "local" {
		t.Fatalf("non-file rows polluted the catalog: %#v", index)
	}
	for _, bad := range [][]bridge.Content{
		{{ID: ""}},
		{{ID: "streaming"}, {ID: "streaming", FolderPath: "/Music/one.mp3"}},
	} {
		if _, err := IndexRekordboxCatalog(bad); err == nil {
			t.Fatalf("invalid IDs accepted on non-file rows: %#v", bad)
		}
	}
}
