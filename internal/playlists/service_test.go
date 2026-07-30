package playlists

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/config"
)

type fakeProvider struct {
	playlist ProviderPlaylist
	tracks   []Track
	err      error
}

func (f fakeProvider) List(context.Context) ([]ProviderPlaylist, error) {
	return []ProviderPlaylist{f.playlist}, f.err
}

func (f fakeProvider) Read(context.Context, Definition) (ProviderPlaylist, []Track, error) {
	return f.playlist, f.tracks, f.err
}

func TestRefreshPreservesSnapshotOnProviderFailure(t *testing.T) {
	stateDir := t.TempDir()
	main := config.DefaultConfig()
	main.Defaults.StateDir = stateDir
	definition := Definition{ID: "favorites", Name: "Favorites", Provider: ProviderAppleMusic, ProviderPlaylist: "Favourites"}
	old := Snapshot{
		PlaylistID: "favorites", Name: "Favorites", Provider: ProviderAppleMusic,
		ProviderPlaylist: "Favourites", RefreshedAt: time.Unix(1, 0).UTC(),
		Tracks: []Track{{ProviderID: "old", Title: "Old"}},
	}
	if _, err := WriteSnapshot(stateDir, old); err != nil {
		t.Fatal(err)
	}

	_, err := (Service{Provider: fakeProvider{err: errors.New("Music unavailable")}}).Refresh(context.Background(), main, definition)
	if err == nil {
		t.Fatal("expected refresh error")
	}
	got, loadErr := LoadSnapshot(stateDir, "favorites")
	if loadErr != nil {
		t.Fatalf("LoadSnapshot: %v", loadErr)
	}
	if len(got.Tracks) != 1 || got.Tracks[0].ProviderID != "old" {
		t.Fatalf("snapshot was replaced after failed refresh: %#v", got)
	}
}

func TestRefreshReportsMembershipChanges(t *testing.T) {
	stateDir := t.TempDir()
	main := config.DefaultConfig()
	main.Defaults.StateDir = stateDir
	definition := Definition{ID: "favorites", Name: "Favorites", Provider: ProviderAppleMusic, ProviderPlaylist: "Favourites"}
	if _, err := WriteSnapshot(stateDir, Snapshot{
		PlaylistID: "favorites", Name: "Favorites", Provider: ProviderAppleMusic,
		ProviderPlaylist: "Favourites", RefreshedAt: time.Unix(1, 0).UTC(),
		Tracks: []Track{{ProviderID: "old", Title: "Old"}},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := (Service{
		Provider: fakeProvider{
			playlist: ProviderPlaylist{Name: "Favourites", ID: "playlist-id"},
			tracks:   []Track{{ProviderID: "new", Title: "New"}},
		},
		Now: func() time.Time { return time.Unix(2, 0).UTC() },
	}).Refresh(context.Background(), main, definition)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if result.Changes.Added != 1 || result.Changes.Removed != 1 {
		t.Fatalf("unexpected changes: %#v", result.Changes)
	}
}
