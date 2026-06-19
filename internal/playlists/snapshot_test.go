package playlists

import (
	"os"
	"testing"
	"time"
)

func TestWriteAndLoadSnapshot(t *testing.T) {
	stateDir := t.TempDir()
	input := Snapshot{
		PlaylistID: "favorites", Name: "Favorites", Provider: ProviderAppleMusic,
		ProviderPlaylist: "Favourites", RefreshedAt: time.Unix(10, 0).UTC(),
		Tracks: []Track{{Index: 1, ProviderID: "one", Artist: "Artist", Title: "Track", Path: "/Music/Track.m4a"}},
	}
	path, err := WriteSnapshot(stateDir, input)
	if err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat snapshot: %v", err)
	}
	got, err := LoadSnapshot(stateDir, "favorites")
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if got.ChecksumSHA256 == "" || len(got.Tracks) != 1 {
		t.Fatalf("unexpected snapshot: %#v", got)
	}
}

func TestCompareSnapshots(t *testing.T) {
	previous := Snapshot{Tracks: []Track{{ProviderID: "one"}, {ProviderID: "two"}}}
	next := Snapshot{Tracks: []Track{{ProviderID: "two"}, {ProviderID: "three"}}}
	got := CompareSnapshots(previous, next)
	if got.Added != 1 || got.Removed != 1 || got.Kept != 1 {
		t.Fatalf("unexpected changes: %#v", got)
	}
}
