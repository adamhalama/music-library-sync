package playlists

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/navidrome"
)

type stubNavidromeClient struct {
	playlists []navidrome.Playlist
	songs     map[string][]navidrome.Song
	starred   []navidrome.Song
	err       error
	calls     int
}

func (s *stubNavidromeClient) Starred(context.Context) ([]navidrome.Song, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.starred, nil
}

func (s *stubNavidromeClient) Playlists(context.Context) ([]navidrome.Playlist, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.playlists, nil
}

func (s *stubNavidromeClient) Playlist(_ context.Context, id string) (navidrome.Playlist, []navidrome.Song, error) {
	s.calls++
	if s.err != nil {
		return navidrome.Playlist{}, nil, s.err
	}
	for _, item := range s.playlists {
		if item.ID == id {
			return item, s.songs[id], nil
		}
	}
	return navidrome.Playlist{}, nil, errors.New("playlist not found")
}

func (s *stubNavidromeClient) PlaylistByName(_ context.Context, name string) (navidrome.Playlist, bool, error) {
	s.calls++
	if s.err != nil {
		return navidrome.Playlist{}, false, s.err
	}
	for _, item := range s.playlists {
		if item.Name == name {
			return item, true, nil
		}
	}
	return navidrome.Playlist{}, false, nil
}

func TestNavidromeProviderIsASupportedProvider(t *testing.T) {
	if !SupportedProvider(ProviderNavidrome) {
		t.Fatalf("navidrome must be a supported provider")
	}
	cfg := Config{Version: ConfigVersion, Playlists: []Definition{{
		ID: "navidrome-all", Name: "All Music", Provider: ProviderNavidrome, ProviderPlaylist: "All Music",
	}}}
	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestExistingAppleMusicDefinitionsStillValidate(t *testing.T) {
	cfg := Config{Version: ConfigVersion, Playlists: []Definition{{
		ID: "favorites", Name: "Favorites", Provider: ProviderAppleMusic, ProviderPlaylist: "Favorites",
	}}}
	if err := Validate(cfg); err != nil {
		t.Fatalf("adding navidrome must not invalidate apple_music definitions: %v", err)
	}
}

func TestNavidromeProviderReadPreservesOrder(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "one.mp3")
	if err := os.WriteFile(present, []byte("audio"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	client := &stubNavidromeClient{
		playlists: []navidrome.Playlist{{ID: "pl-1", Name: "All Music", TrackCount: 3}},
		songs: map[string][]navidrome.Song{"pl-1": {
			{ID: "c", Title: "Third", Path: filepath.Join(dir, "three.mp3")},
			{ID: "a", Title: "First", Path: present},
			{ID: "b", Title: "Second"},
		}},
	}
	provider := NavidromeProvider{Client: client}
	playlist, tracks, err := provider.Read(context.Background(), Definition{
		ID: "navidrome-all", Name: "All Music", Provider: ProviderNavidrome, ProviderPlaylist: "All Music",
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if playlist.ID != "pl-1" {
		t.Fatalf("playlist = %+v", playlist)
	}
	wantIDs := []string{"c", "a", "b"}
	for i, want := range wantIDs {
		if tracks[i].ProviderID != want {
			t.Fatalf("order = %+v, want %v", tracks, wantIDs)
		}
		if tracks[i].Index != i+1 {
			t.Fatalf("index = %d, want %d", tracks[i].Index, i+1)
		}
	}
	if tracks[1].MissingLocal {
		t.Fatalf("an existing local file must not be flagged missing: %+v", tracks[1])
	}
	if !tracks[0].MissingLocal || !tracks[2].MissingLocal {
		t.Fatalf("absent and path-less tracks must be flagged missing: %+v", tracks)
	}
}

func TestNavidromeProviderReportsMissingPlaylist(t *testing.T) {
	provider := NavidromeProvider{Client: &stubNavidromeClient{}}
	_, _, err := provider.Read(context.Background(), Definition{
		ID: "navidrome-all", Name: "All Music", Provider: ProviderNavidrome, ProviderPlaylist: "All Music",
	})
	if err == nil {
		t.Fatalf("expected a not-found error")
	}
}

func TestNavidromeSnapshotRoundTripsThroughTheExistingShape(t *testing.T) {
	stateDir := t.TempDir()
	client := &stubNavidromeClient{
		playlists: []navidrome.Playlist{{ID: "pl-1", Name: "All Music", TrackCount: 1}},
		songs:     map[string][]navidrome.Song{"pl-1": {{ID: "a", Title: "First", Path: "/music/a.mp3"}}},
	}
	service := Service{
		Provider: NavidromeProvider{Client: client},
		Now:      func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}
	definition := Definition{
		ID: "navidrome-all", Name: "All Music", Provider: ProviderNavidrome, ProviderPlaylist: "All Music",
	}
	main := config.Config{}
	main.Defaults.StateDir = stateDir

	result, err := service.Refresh(context.Background(), main, definition)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if result.Snapshot.Provider != ProviderNavidrome {
		t.Fatalf("provider = %q", result.Snapshot.Provider)
	}
	// The snapshot must validate through the unchanged checksum path.
	loaded, err := LoadSnapshot(stateDir, definition.ID)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if len(loaded.Tracks) != 1 || loaded.Tracks[0].ProviderID != "a" {
		t.Fatalf("snapshot = %+v", loaded)
	}
}

func TestFailedRefreshPreservesThePreviousSnapshot(t *testing.T) {
	stateDir := t.TempDir()
	client := &stubNavidromeClient{
		playlists: []navidrome.Playlist{{ID: "pl-1", Name: "All Music", TrackCount: 1}},
		songs:     map[string][]navidrome.Song{"pl-1": {{ID: "a", Title: "First", Path: "/music/a.mp3"}}},
	}
	service := Service{Provider: NavidromeProvider{Client: client}}
	definition := Definition{
		ID: "navidrome-all", Name: "All Music", Provider: ProviderNavidrome, ProviderPlaylist: "All Music",
	}
	main := config.Config{}
	main.Defaults.StateDir = stateDir

	if _, err := service.Refresh(context.Background(), main, definition); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	client.err = errors.New("server unreachable")
	if _, err := service.Refresh(context.Background(), main, definition); err == nil {
		t.Fatalf("expected the refresh to fail")
	}
	loaded, err := LoadSnapshot(stateDir, definition.ID)
	if err != nil {
		t.Fatalf("the previous snapshot must survive a failed refresh: %v", err)
	}
	if len(loaded.Tracks) != 1 {
		t.Fatalf("snapshot = %+v", loaded)
	}
}

func TestCanceledRefreshDoesNotWrite(t *testing.T) {
	stateDir := t.TempDir()
	client := &stubNavidromeClient{playlists: []navidrome.Playlist{{ID: "pl-1", Name: "All Music"}}}
	service := Service{Provider: NavidromeProvider{Client: client}}
	main := config.Config{}
	main.Defaults.StateDir = stateDir

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.Refresh(ctx, main, Definition{
		ID: "navidrome-all", Name: "All Music", Provider: ProviderNavidrome, ProviderPlaylist: "All Music",
	})
	if err == nil {
		t.Fatalf("expected a cancellation error")
	}
	if client.calls != 0 {
		t.Fatalf("a canceled refresh must not call the server")
	}
	if _, err := LoadSnapshot(stateDir, "navidrome-all"); err == nil {
		t.Fatalf("a canceled refresh must not write a snapshot")
	}
}

func starredDefinition() Definition {
	return Definition{
		ID:                 navidrome.SmartPlaylistFavorites,
		Name:               navidrome.SmartPlaylistFavoritesName,
		Provider:           ProviderNavidrome,
		ProviderPlaylist:   navidrome.SmartPlaylistFavoritesName,
		ProviderPlaylistID: NavidromeStarredPlaylistID,
	}
}

func TestStarredReadSortsByPathAndFlagsMissingFiles(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "b-present.mp3")
	if err := os.WriteFile(present, []byte("audio"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	client := &stubNavidromeClient{starred: []navidrome.Song{
		{ID: "c", Title: "Third", Path: filepath.Join(dir, "c-absent.mp3")},
		{ID: "a", Title: "First", Path: filepath.Join(dir, "a-absent.mp3")},
		{ID: "b", Title: "Second", Path: present},
	}}
	provider := NavidromeProvider{Client: client}

	playlist, tracks, err := provider.Read(context.Background(), starredDefinition())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// The sentinel must not be resolved as a server playlist ID.
	if playlist.ID != NavidromeStarredPlaylistID || playlist.Name != navidrome.SmartPlaylistFavoritesName {
		t.Fatalf("playlist = %+v", playlist)
	}
	if playlist.TrackCount != 3 {
		t.Fatalf("track count = %d, want 3", playlist.TrackCount)
	}
	wantIDs := []string{"a", "b", "c"}
	for i, want := range wantIDs {
		if tracks[i].ProviderID != want {
			t.Fatalf("order = %+v, want %v", tracks, wantIDs)
		}
		if tracks[i].Index != i+1 {
			t.Fatalf("index = %d, want %d", tracks[i].Index, i+1)
		}
	}
	if tracks[1].MissingLocal {
		t.Fatalf("an existing local file must not be flagged missing: %+v", tracks[1])
	}
	if !tracks[0].MissingLocal || !tracks[2].MissingLocal {
		t.Fatalf("absent files must be flagged missing: %+v", tracks)
	}
}

// getStarred2 defines no ordering, so the same set delivered in a different
// order must still produce the same snapshot. Otherwise every refresh churns the
// checksum and Rekordbox sees adds and removes that never happened.
func TestStarredReadIsOrderIndependent(t *testing.T) {
	songs := []navidrome.Song{
		{ID: "a", Title: "First", Path: "/music/a.mp3"},
		{ID: "b", Title: "Second", Path: "/music/b.mp3"},
		{ID: "c", Title: "Third", Path: "/music/c.mp3"},
		{ID: "d", Title: "Path-less"},
		{ID: "e", Title: "Also path-less"},
	}
	shuffled := []navidrome.Song{songs[3], songs[2], songs[0], songs[4], songs[1]}

	read := func(input []navidrome.Song) []Track {
		provider := NavidromeProvider{Client: &stubNavidromeClient{starred: input}}
		_, tracks, err := provider.Read(context.Background(), starredDefinition())
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		return tracks
	}
	first, second := read(songs), read(shuffled)
	if len(first) != len(second) {
		t.Fatalf("lengths differ: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("track %d differs: %+v vs %+v", i, first[i], second[i])
		}
	}
}

// Two refreshes over unchanged server data must report nothing changed, and —
// with the clock held still, since RefreshedAt is a checksum input — produce a
// byte-identical snapshot.
func TestRepeatedStarredRefreshesProduceNoChurn(t *testing.T) {
	stateDir := t.TempDir()
	client := &stubNavidromeClient{starred: []navidrome.Song{
		{ID: "b", Title: "Second", Path: "/music/b.mp3"},
		{ID: "a", Title: "First", Path: "/music/a.mp3"},
	}}
	service := Service{
		Provider: NavidromeProvider{Client: client},
		Now:      func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}
	main := config.Config{}
	main.Defaults.StateDir = stateDir
	definition := starredDefinition()

	first, err := service.Refresh(context.Background(), main, definition)
	if err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	// The server hands the same set back in a different order.
	client.starred = []navidrome.Song{client.starred[1], client.starred[0]}
	second, err := service.Refresh(context.Background(), main, definition)
	if err != nil {
		t.Fatalf("second Refresh: %v", err)
	}
	if second.Changes.Added != 0 || second.Changes.Removed != 0 {
		t.Fatalf("changes = %+v, want no adds or removes", second.Changes)
	}
	if second.Changes.Kept != 2 {
		t.Fatalf("kept = %d, want 2", second.Changes.Kept)
	}
	if first.Snapshot.ChecksumSHA256 != second.Snapshot.ChecksumSHA256 {
		t.Fatalf("checksum churned: %q vs %q",
			first.Snapshot.ChecksumSHA256, second.Snapshot.ChecksumSHA256)
	}
}

func TestStarredReadPropagatesServerFailure(t *testing.T) {
	provider := NavidromeProvider{Client: &stubNavidromeClient{err: errors.New("server unreachable")}}
	if _, _, err := provider.Read(context.Background(), starredDefinition()); err == nil {
		t.Fatalf("expected the server error to propagate")
	}
}

// PLAN.md decision 3 — the two favourite sets stay separate all the way to
// Rekordbox, so the managed definition must carry its own target.
func TestManagedFavoritesDefinitionUsesTheSentinelAndItsOwnRekordboxTarget(t *testing.T) {
	var favorites Definition
	for _, definition := range NavidromeDefinitions() {
		if definition.ID == navidrome.SmartPlaylistFavorites {
			favorites = definition
		}
	}
	if favorites.ProviderPlaylistID != NavidromeStarredPlaylistID {
		t.Fatalf("provider playlist id = %q", favorites.ProviderPlaylistID)
	}
	if favorites.DefaultRekordboxTarget != "nav_fav_imports" {
		t.Fatalf("rekordbox target = %q, want nav_fav_imports", favorites.DefaultRekordboxTarget)
	}
	if favorites.ProviderPlaylist != navidrome.SmartPlaylistFavoritesName {
		t.Fatalf("the display name must stay populated: %+v", favorites)
	}
	if err := Validate(Config{Version: ConfigVersion, Playlists: NavidromeDefinitions()}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// The Apple Music favourites flow is finished and is not being revisited; the
// Navidrome branch must not reach it.
func TestAppleFavoritesDefinitionIsUntouchedByTheNavidromeMerge(t *testing.T) {
	apple := Definition{
		ID: "favorites", Name: "Favorites", Provider: ProviderAppleMusic,
		ProviderPlaylist: "Favorites", DefaultRekordboxTarget: "fav_imports",
	}
	merged, _ := EnsureNavidromeDefinitions(Config{Version: ConfigVersion, Playlists: []Definition{apple}})
	got, ok := merged.Definition("favorites")
	if !ok {
		t.Fatalf("the Apple Music favorites definition was dropped")
	}
	if got != apple {
		t.Fatalf("definition = %+v, want %+v", got, apple)
	}
	navFavorites, ok := merged.Definition(navidrome.SmartPlaylistFavorites)
	if !ok {
		t.Fatalf("the navidrome favourites definition was not added")
	}
	if navFavorites.DefaultRekordboxTarget == got.DefaultRekordboxTarget {
		t.Fatalf("the two favourite sets must not share a Rekordbox target")
	}
}

func TestEnsureNavidromeDefinitionsPreservesExistingEntries(t *testing.T) {
	cfg := Config{Version: ConfigVersion, Playlists: []Definition{{
		ID: "favorites", Name: "Favorites", Provider: ProviderAppleMusic, ProviderPlaylist: "Favorites",
	}}}
	merged, added := EnsureNavidromeDefinitions(cfg)
	if len(added) != 3 {
		t.Fatalf("added = %v", added)
	}
	if _, ok := merged.Definition("favorites"); !ok {
		t.Fatalf("the Apple Music favorites definition was dropped")
	}
	if err := Validate(merged); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	// A second call must be a no-op.
	_, addedAgain := EnsureNavidromeDefinitions(merged)
	if len(addedAgain) != 0 {
		t.Fatalf("addedAgain = %v", addedAgain)
	}
}

func TestUnsupportedProviderIsRejectedByTheService(t *testing.T) {
	_, err := (Service{}).ListProviderPlaylists(context.Background(), "spotify")
	if err == nil {
		t.Fatalf("expected an unsupported-provider error")
	}
}

// A managed definition nobody wrote down cannot be refreshed. Setup registers
// them, and doing so twice must not duplicate anything or disturb what is
// already there.
func TestWriteNavidromeDefinitionsIsAppendOnlyAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "playlists.yaml")
	existing := Config{Version: ConfigVersion, Playlists: []Definition{{
		ID: "favorites", Name: "Favorites", Provider: ProviderAppleMusic,
		ProviderPlaylist: "Favourites", DefaultRekordboxTarget: "fav_imports",
	}}}
	if err := Save(path, existing); err != nil {
		t.Fatalf("Save: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	added, err := WriteNavidromeDefinitions(path, dir)
	if err != nil {
		t.Fatalf("WriteNavidromeDefinitions: %v", err)
	}
	if len(added) != 3 {
		t.Fatalf("added = %v, want the three managed definitions", added)
	}
	merged, err := Load(LoadOptions{ExplicitPath: path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	apple, ok := merged.Definition("favorites")
	if !ok || apple != existing.Playlists[0] {
		t.Fatalf("the Apple Music definition was altered: %+v", apple)
	}
	starred, ok := merged.Definition(navidrome.SmartPlaylistFavorites)
	if !ok || starred.ProviderPlaylistID != NavidromeStarredPlaylistID {
		t.Fatalf("navidrome favourites definition = %+v", starred)
	}

	// A second call adds nothing and leaves the file alone.
	againBefore, _ := os.ReadFile(path)
	added, err = WriteNavidromeDefinitions(path, dir)
	if err != nil {
		t.Fatalf("second WriteNavidromeDefinitions: %v", err)
	}
	if len(added) != 0 {
		t.Fatalf("second call added = %v", added)
	}
	againAfter, _ := os.ReadFile(path)
	if string(againBefore) != string(againAfter) {
		t.Fatalf("an idempotent call rewrote the file")
	}
	if string(before) == string(againAfter) {
		t.Fatalf("the first call did not write anything")
	}
}
