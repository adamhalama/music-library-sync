package navidrome

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func playlistFixture(t *testing.T) (Config, Resolved) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := DefaultConfig()
	cfg.Server.Username = "jaa"
	normalize(&cfg)
	resolved, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return cfg, resolved
}

func TestAllMusicPlaylistHasNoLimitAndManagedSort(t *testing.T) {
	payload, err := MarshalSmartPlaylist(AllMusicPlaylist())
	if err != nil {
		t.Fatalf("MarshalSmartPlaylist: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, hasLimit := decoded["limit"]; hasLimit {
		t.Fatalf("All Music must carry no item limit: %s", payload)
	}
	if decoded["sort"] != ManagedSort {
		t.Fatalf("sort = %v, want %q", decoded["sort"], ManagedSort)
	}
	if decoded["name"] != SmartPlaylistAllName {
		t.Fatalf("name = %v", decoded["name"])
	}
	if !IsUDLOwned(string(payload)) {
		t.Fatalf("a managed .nsp must carry the ownership marker: %s", payload)
	}
}

func TestFavoritesPlaylistMatchesLoved(t *testing.T) {
	playlist := FavoritesPlaylist()
	if len(playlist.All) != 1 {
		t.Fatalf("all = %+v", playlist.All)
	}
	is, ok := playlist.All[0]["is"].(map[string]any)
	if !ok || is["loved"] != true {
		t.Fatalf("favourites rule = %+v", playlist.All[0])
	}
	if playlist.Sort != ManagedSort {
		t.Fatalf("sort = %q", playlist.Sort)
	}
}

func TestHardBouncePlaylistUsesExactAllowlist(t *testing.T) {
	playlist, err := HardBouncePlaylist([]string{"Hard Bounce", "hard bounce", " Bounce ", "", "Hard Bounce"})
	if err != nil {
		t.Fatalf("HardBouncePlaylist: %v", err)
	}
	// Three distinct spellings survive; only the exact duplicate is dropped.
	// Navidrome's genre match is case-sensitive, so each spelling needs a clause.
	if len(playlist.Any) != 3 {
		t.Fatalf("expected three exactly-deduplicated genres, got %+v", playlist.Any)
	}
	if len(playlist.All) != 0 {
		t.Fatalf("an allowlist must be an any-of rule, not all-of: %+v", playlist.All)
	}
	// `is` keeps the rule an exact match; `contains` would silently broaden it.
	for _, clause := range playlist.Any {
		if _, ok := clause["is"]; !ok {
			t.Fatalf("clause %+v must use an exact `is` match", clause)
		}
	}
}

func TestHardBouncePlaylistRefusesEmptyAllowlist(t *testing.T) {
	_, err := HardBouncePlaylist(nil)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("error = %v, want an empty-allowlist refusal", err)
	}
}

func TestSmartPlaylistEscapesAwkwardGenres(t *testing.T) {
	playlist, err := HardBouncePlaylist([]string{`Hard "Bounce"`, "Drum & Bass", "Techno/Hard"})
	if err != nil {
		t.Fatalf("HardBouncePlaylist: %v", err)
	}
	payload, err := MarshalSmartPlaylist(playlist)
	if err != nil {
		t.Fatalf("MarshalSmartPlaylist: %v", err)
	}
	var reread SmartPlaylist
	if err := json.Unmarshal(payload, &reread); err != nil {
		t.Fatalf("the rendered .nsp is not valid JSON: %v\n%s", err, payload)
	}
	if len(reread.Any) != 3 {
		t.Fatalf("round trip lost clauses: %+v", reread.Any)
	}
}

func TestWriteSmartPlaylistsWritesAllThree(t *testing.T) {
	cfg, resolved := playlistFixture(t)
	cfg.Playlists.HardBounceGenres = []string{"Hard Bounce"}

	generated, warnings, err := WriteSmartPlaylists(cfg, resolved)
	if err != nil {
		t.Fatalf("WriteSmartPlaylists: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if len(generated) != 3 {
		t.Fatalf("generated = %+v", generated)
	}
	for _, item := range generated {
		if !item.Written {
			t.Fatalf("%s was not written", item.Name)
		}
		if _, statErr := os.Stat(item.Path); statErr != nil {
			t.Fatalf("stat %s: %v", item.Path, statErr)
		}
		if filepath.Dir(item.Path) != resolved.PlaylistsDir {
			t.Fatalf("%s is outside the managed playlists directory", item.Path)
		}
	}
}

func TestWriteSmartPlaylistsSkipsHardBounceWithoutAllowlist(t *testing.T) {
	cfg, resolved := playlistFixture(t)
	generated, warnings, err := WriteSmartPlaylists(cfg, resolved)
	if err != nil {
		t.Fatalf("WriteSmartPlaylists: %v", err)
	}
	if len(generated) != 2 {
		t.Fatalf("generated = %+v, want All Music and Favourites only", generated)
	}
	if !containsFragment(warnings, "allowlist is empty") {
		t.Fatalf("warnings = %v", warnings)
	}
	if _, err := os.Stat(SmartPlaylistPath(resolved, SmartPlaylistHardBounceName)); !os.IsNotExist(err) {
		t.Fatalf("HARD BOUNCE must not be written without an approved allowlist")
	}
}

func TestWriteSmartPlaylistsIsIdempotent(t *testing.T) {
	cfg, resolved := playlistFixture(t)
	cfg.Playlists.HardBounceGenres = []string{"Hard Bounce"}
	if _, _, err := WriteSmartPlaylists(cfg, resolved); err != nil {
		t.Fatalf("first write: %v", err)
	}
	generated, _, err := WriteSmartPlaylists(cfg, resolved)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	for _, item := range generated {
		if item.Written {
			t.Fatalf("%s was rewritten with identical content", item.Name)
		}
	}
}

func TestWriteSmartPlaylistsRefusesUnownedFile(t *testing.T) {
	cfg, resolved := playlistFixture(t)
	if err := os.MkdirAll(resolved.PlaylistsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := SmartPlaylistPath(resolved, SmartPlaylistAllName)
	foreign := `{"name":"All Music","all":[]}` + "\n"
	if err := os.WriteFile(path, []byte(foreign), 0o644); err != nil {
		t.Fatalf("write foreign playlist: %v", err)
	}
	_, warnings, err := WriteSmartPlaylists(cfg, resolved)
	if err != nil {
		t.Fatalf("WriteSmartPlaylists: %v", err)
	}
	if !containsFragment(warnings, "not UDL-managed") {
		t.Fatalf("warnings = %v", warnings)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != foreign {
		t.Fatalf("the foreign playlist was overwritten")
	}
}

func TestDeriveHardBounceGenres(t *testing.T) {
	cfg, resolved := playlistFixture(t)
	music := resolved.MusicDir

	catalog := []Song{
		{ID: "1", Path: filepath.Join(music, "a.mp3"), Genres: []string{"Hard Bounce"}},
		{ID: "2", Path: filepath.Join(music, "b.mp3"), Genres: []string{"hard bounce", "Bounce"}},
		{ID: "3", Path: filepath.Join(music, "c.mp3")},
	}
	source := []SourceTrack{
		{Title: "A", Path: filepath.Join(music, "a.mp3")},
		{Title: "B", Path: filepath.Join(music, "b.mp3")},
		{Title: "C (no genre)", Path: filepath.Join(music, "c.mp3")},
		{Title: "D (not scanned)", Path: filepath.Join(music, "d.mp3")},
		{Title: "E (elsewhere)", Path: filepath.Join(t.TempDir(), "e.mp3")},
		{Title: "F (no local file)"},
	}

	derivation, err := DeriveHardBounceGenres(context.Background(), cfg, resolved, "HARD BOUNCE", source, catalog)
	if err != nil {
		t.Fatalf("DeriveHardBounceGenres: %v", err)
	}
	if derivation.MatchedCount != 3 {
		t.Fatalf("matched = %d, want 3", derivation.MatchedCount)
	}
	if len(derivation.Genres) != 3 {
		t.Fatalf("genres = %v, want all three distinct spellings", derivation.Genres)
	}
	if len(derivation.GenrelessPaths) != 1 {
		t.Fatalf("genreless = %v", derivation.GenrelessPaths)
	}
	if len(derivation.UnmatchedPaths) != 2 {
		t.Fatalf("unmatched = %v, want the unscanned file and the path-less track", derivation.UnmatchedPaths)
	}
	if len(derivation.OutsideLibrary) != 1 {
		t.Fatalf("outside = %v", derivation.OutsideLibrary)
	}
	if !derivation.AllowlistChanged {
		t.Fatalf("an empty stored allowlist must report a change")
	}
}

func TestDeriveHardBounceGenresReportsNoChange(t *testing.T) {
	cfg, resolved := playlistFixture(t)
	cfg.Playlists.HardBounceGenres = []string{"Hard Bounce"}
	catalog := []Song{{ID: "1", Path: filepath.Join(resolved.MusicDir, "a.mp3"), Genres: []string{"Hard Bounce"}}}
	source := []SourceTrack{{Title: "A", Path: filepath.Join(resolved.MusicDir, "a.mp3")}}

	derivation, err := DeriveHardBounceGenres(context.Background(), cfg, resolved, "HARD BOUNCE", source, catalog)
	if err != nil {
		t.Fatalf("DeriveHardBounceGenres: %v", err)
	}
	if derivation.AllowlistChanged {
		t.Fatalf("an identical derivation must not report a change: %+v", derivation)
	}
}

func TestDeriveHardBounceGenresHonoursCancellation(t *testing.T) {
	cfg, resolved := playlistFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DeriveHardBounceGenres(ctx, cfg, resolved, "HARD BOUNCE", nil, nil); err == nil {
		t.Fatalf("expected a cancellation error")
	}
}

func TestVerifyPlaylistOwnership(t *testing.T) {
	problems := VerifyPlaylistOwnership([]Playlist{
		{Name: SmartPlaylistAllName, Owner: "jaa"},
		{Name: SmartPlaylistFavoritesName, Owner: "someone-else"},
		{Name: "Unrelated", Owner: "someone-else"},
	}, "jaa")
	if len(problems) != 1 {
		t.Fatalf("problems = %v, want only the mis-owned managed playlist", problems)
	}
	if !strings.Contains(problems[0], SmartPlaylistFavoritesName) {
		t.Fatalf("problem = %q", problems[0])
	}
}

func TestRefreshManagedPlaylistsScansAndVerifiesOwnership(t *testing.T) {
	cfg, resolved := playlistFixture(t)
	cfg.Playlists.HardBounceGenres = []string{"Hard Bounce"}

	fake := newFakeServer(t)
	fake.AddPlaylist("pl-1", SmartPlaylistAllName, "jaa")
	fake.AddPlaylist("pl-2", SmartPlaylistHardBounceName, "jaa")
	fake.AddPlaylist("pl-3", SmartPlaylistFavoritesName, "someone-else")

	result, err := RefreshManagedPlaylists(context.Background(), cfg, resolved, fake.Client())
	if err != nil {
		t.Fatalf("RefreshManagedPlaylists: %v", err)
	}
	if !result.Scanned {
		t.Fatalf("a refresh must request a scan so Navidrome re-imports the .nsp files")
	}
	if len(result.Generated) != 3 || len(result.Imported) != 3 {
		t.Fatalf("generated=%d imported=%d", len(result.Generated), len(result.Imported))
	}
	if !containsFragment(result.Warnings, "not the shared account") {
		t.Fatalf("a mis-owned managed playlist must be reported: %v", result.Warnings)
	}
}

func TestRefreshManagedPlaylistsWithoutAClientStillWritesFiles(t *testing.T) {
	cfg, resolved := playlistFixture(t)
	cfg.Playlists.HardBounceGenres = []string{"Hard Bounce"}

	result, err := RefreshManagedPlaylists(context.Background(), cfg, resolved, nil)
	if err != nil {
		t.Fatalf("RefreshManagedPlaylists: %v", err)
	}
	if len(result.Generated) != 3 {
		t.Fatalf("generated = %+v", result.Generated)
	}
	if result.Scanned {
		t.Fatalf("no scan can be requested without a client")
	}
	if !containsFragment(result.Warnings, "not configured") {
		t.Fatalf("warnings = %v", result.Warnings)
	}
}

func TestValidateSortRejectsFieldsNavidromeIgnores(t *testing.T) {
	// Every one of these was probed against Navidrome 0.63.2: ascending and
	// descending returned the identical order, proving the field is ignored.
	for _, field := range []string{"birthtime", "birth_time", "created_at", "recentlyadded", "filemodified"} {
		if err := ValidateSort("-" + field); err == nil {
			t.Fatalf("sort field %q is silently ignored by Navidrome and must be refused", field)
		}
	}
}

func TestValidateSortAcceptsTheManagedSort(t *testing.T) {
	if err := ValidateSort(ManagedSort); err != nil {
		t.Fatalf("ValidateSort(%q): %v", ManagedSort, err)
	}
}

func TestMarshalRefusesAPlaylistNavidromeWouldMisorder(t *testing.T) {
	playlist := AllMusicPlaylist()
	playlist.Sort = "-birthtime,filepath"
	if _, err := MarshalSmartPlaylist(playlist); err == nil {
		t.Fatalf("a playlist with an ignored sort field must not be written")
	}
}
