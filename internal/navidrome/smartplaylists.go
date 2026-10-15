package navidrome

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Managed smart playlist identifiers. These are also the UDL standalone
// playlist definition IDs, so a snapshot and its `.nsp` always agree.
const (
	SmartPlaylistAll        = "navidrome-all"
	SmartPlaylistHardBounce = "navidrome-hard-bounce"
	SmartPlaylistFavorites  = "navidrome-favorites"
)

// Managed smart playlist display names.
const (
	SmartPlaylistAllName        = "All Music"
	SmartPlaylistHardBounceName = "HARD BOUNCE"
	SmartPlaylistFavoritesName  = "Favourites (Navidrome)"
)

// ManagedSort is the deterministic ordering for every managed playlist:
// newest first, then relative path so files sharing a timestamp never
// reshuffle.
//
// IMPORTANT — this is *not* APFS creation time, despite PLAN.md decision 6.
// Verified against Navidrome 0.63.2: `dateadded` resolves to
// `media_file.created_at`, which is when the scanner first saw the file, not
// its birth time. Navidrome does store the real birth time in
// `media_file.birth_time`, but no smart-playlist criteria field exposes it —
// `birthtime`, `birth_time`, `created_at` and friends are all silently
// ignored rather than rejected, which is exactly why ManagedSortFields exists.
// See the deviation recorded in IMPLEMENTATION.md.
const ManagedSort = "-dateadded,filepath"

// ManagedSortFields are the criteria fields Navidrome actually honours as sort
// keys, confirmed empirically: sorting ascending and descending by each of
// these produces different orders, while an unrecognised field produces the
// same default order both ways.
//
// An unrecognised sort field is not an error in Navidrome — the playlist just
// comes back in arbitrary order. A silent mis-ordering of the whole phone
// library is worse than a refusal, so UDL validates its own sort string.
var ManagedSortFields = map[string]struct{}{
	"dateadded":    {},
	"datemodified": {},
	"filepath":     {},
	"title":        {},
	"album":        {},
	"artist":       {},
	"year":         {},
	"duration":     {},
}

// ValidateSort rejects a sort string containing a field Navidrome would
// silently ignore.
func ValidateSort(sort string) error {
	unknown := []string{}
	for _, field := range strings.Split(sort, ",") {
		name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(field), "-"))
		if name == "" {
			continue
		}
		if _, ok := ManagedSortFields[strings.ToLower(name)]; !ok {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf(
			"navidrome would silently ignore sort field(s) %s and return the playlist in arbitrary order",
			strings.Join(unknown, ", "))
	}
	return nil
}

// smartPlaylistComment carries the ownership marker into Navidrome's own
// playlist comment, so a managed playlist is identifiable from the server too.
const smartPlaylistComment = "Managed by UDL (" + OwnershipMarker + "). Edits are replaced on the next refresh."

// SmartPlaylist is the `.nsp` document Navidrome imports.
type SmartPlaylist struct {
	Name    string           `json:"name"`
	Comment string           `json:"comment"`
	All     []map[string]any `json:"all,omitempty"`
	Any     []map[string]any `json:"any,omitempty"`
	Sort    string           `json:"sort"`
	Order   string           `json:"order,omitempty"`
	// Limit is deliberately absent when zero: PLAN.md requires no arbitrary
	// item limit, and Navidrome treats a missing limit as unbounded.
	Limit int `json:"limit,omitempty"`
}

// GeneratedPlaylist records one written `.nsp`.
type GeneratedPlaylist struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Written bool   `json:"written"`
}

// AllMusicPlaylist matches every track. `filepath` is always present, so a
// contains-empty predicate is the least surprising "everything" rule.
func AllMusicPlaylist() SmartPlaylist {
	return SmartPlaylist{
		Name:    SmartPlaylistAllName,
		Comment: smartPlaylistComment,
		All:     []map[string]any{{"contains": map[string]any{"filepath": ""}}},
		Sort:    ManagedSort,
	}
}

// FavoritesPlaylist matches every track starred by the shared account.
func FavoritesPlaylist() SmartPlaylist {
	return SmartPlaylist{
		Name:    SmartPlaylistFavoritesName,
		Comment: smartPlaylistComment,
		All:     []map[string]any{{"is": map[string]any{"loved": true}}},
		Sort:    ManagedSort,
	}
}

// HardBouncePlaylist matches any genre in the approved explicit allowlist. An
// empty allowlist is refused rather than silently producing a rule that either
// matches nothing or, worse, everything.
func HardBouncePlaylist(genres []string) (SmartPlaylist, error) {
	normalized := NormalizeGenres(genres)
	if len(normalized) == 0 {
		return SmartPlaylist{}, fmt.Errorf(
			"the HARD BOUNCE genre allowlist is empty; derive and approve it before writing the playlist")
	}
	clauses := make([]map[string]any, 0, len(normalized))
	for _, genre := range normalized {
		clauses = append(clauses, map[string]any{"is": map[string]any{"genre": genre}})
	}
	return SmartPlaylist{
		Name:    SmartPlaylistHardBounceName,
		Comment: smartPlaylistComment,
		Any:     clauses,
		Sort:    ManagedSort,
	}, nil
}

// MarshalSmartPlaylist renders the canonical `.nsp` bytes.
func MarshalSmartPlaylist(playlist SmartPlaylist) ([]byte, error) {
	if err := ValidateSort(playlist.Sort); err != nil {
		return nil, fmt.Errorf("smart playlist %q: %w", playlist.Name, err)
	}
	payload, err := json.MarshalIndent(playlist, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode smart playlist %q: %w", playlist.Name, err)
	}
	return append(payload, '\n'), nil
}

// SmartPlaylistPath is where a managed `.nsp` lives.
func SmartPlaylistPath(resolved Resolved, name string) string {
	return filepath.Join(resolved.PlaylistsDir, name+".nsp")
}

// WriteSmartPlaylists atomically writes All Music, Favourites, and — when an
// approved allowlist exists — HARD BOUNCE. A missing allowlist skips only that
// playlist and is reported, never guessed at.
func WriteSmartPlaylists(cfg Config, resolved Resolved) ([]GeneratedPlaylist, []string, error) {
	if strings.TrimSpace(resolved.PlaylistsDir) == "" {
		return nil, nil, fmt.Errorf("the managed playlists directory is not resolved")
	}
	if err := os.MkdirAll(resolved.PlaylistsDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create playlists directory %s: %w", resolved.PlaylistsDir, err)
	}

	warnings := []string{}
	planned := []struct {
		id       string
		playlist SmartPlaylist
	}{
		{SmartPlaylistAll, AllMusicPlaylist()},
		{SmartPlaylistFavorites, FavoritesPlaylist()},
	}

	hardBounce, err := HardBouncePlaylist(cfg.Playlists.HardBounceGenres)
	if err != nil {
		warnings = append(warnings, err.Error())
	} else {
		planned = append(planned, struct {
			id       string
			playlist SmartPlaylist
		}{SmartPlaylistHardBounce, hardBounce})
	}

	out := []GeneratedPlaylist{}
	for _, item := range planned {
		payload, marshalErr := MarshalSmartPlaylist(item.playlist)
		if marshalErr != nil {
			return out, warnings, marshalErr
		}
		path := SmartPlaylistPath(resolved, item.playlist.Name)
		existing, readErr := os.ReadFile(path)
		if readErr == nil && string(existing) == string(payload) {
			out = append(out, GeneratedPlaylist{ID: item.id, Name: item.playlist.Name, Path: path})
			continue
		}
		if readErr == nil && !IsUDLOwned(string(existing)) {
			warnings = append(warnings, fmt.Sprintf(
				"%s already exists and is not UDL-managed; UDL will not overwrite it", path))
			out = append(out, GeneratedPlaylist{ID: item.id, Name: item.playlist.Name, Path: path})
			continue
		}
		if err := AtomicWrite(path, payload, 0o644); err != nil {
			return out, warnings, err
		}
		out = append(out, GeneratedPlaylist{ID: item.id, Name: item.playlist.Name, Path: path, Written: true})
	}
	return out, warnings, nil
}

// GenreDerivation previews the Hard Bounce allowlist derived from Apple Music.
// Nothing is persisted until the user approves it.
type GenreDerivation struct {
	SourcePlaylist   string   `json:"source_playlist"`
	SourceTrackCount int      `json:"source_track_count"`
	MatchedCount     int      `json:"matched_count"`
	Genres           []string `json:"genres"`
	UnmatchedPaths   []string `json:"unmatched_paths"`
	GenrelessPaths   []string `json:"genreless_paths"`
	OutsideLibrary   []string `json:"outside_library"`
	CurrentAllowlist []string `json:"current_allowlist"`
	AllowlistChanged bool     `json:"allowlist_changed"`
}

// SourceTrack is one Apple Music track feeding the derivation.
type SourceTrack struct {
	Title string
	Path  string
}

// DeriveHardBounceGenres matches the Apple Music source playlist to the
// Navidrome catalog by real path and collects the distinct embedded genres the
// server parsed. Unmatched and genre-less tracks are reported so an incomplete
// scan can never quietly narrow — or broaden — the rule.
func DeriveHardBounceGenres(ctx context.Context, cfg Config, resolved Resolved, sourceName string, source []SourceTrack, catalog []Song) (GenreDerivation, error) {
	if err := ctx.Err(); err != nil {
		return GenreDerivation{}, err
	}
	byPath := map[string]Song{}
	for _, song := range catalog {
		if song.Path == "" {
			continue
		}
		byPath[song.Path] = song
	}

	derivation := GenreDerivation{
		SourcePlaylist:   sourceName,
		SourceTrackCount: len(source),
		Genres:           []string{},
		UnmatchedPaths:   []string{},
		GenrelessPaths:   []string{},
		OutsideLibrary:   []string{},
		CurrentAllowlist: NormalizeGenres(cfg.Playlists.HardBounceGenres),
	}

	collected := []string{}
	for _, track := range source {
		path := NormalizePath(track.Path)
		if path == "" {
			derivation.UnmatchedPaths = append(derivation.UnmatchedPaths, track.Title)
			continue
		}
		if resolved.MusicDir != "" && !withinDir(resolved.MusicDir, path) {
			derivation.OutsideLibrary = append(derivation.OutsideLibrary, path)
			continue
		}
		song, ok := byPath[path]
		if !ok {
			derivation.UnmatchedPaths = append(derivation.UnmatchedPaths, path)
			continue
		}
		derivation.MatchedCount++
		if len(song.Genres) == 0 {
			derivation.GenrelessPaths = append(derivation.GenrelessPaths, path)
			continue
		}
		collected = append(collected, song.Genres...)
	}

	derivation.Genres = NormalizeGenres(collected)
	if derivation.Genres == nil {
		derivation.Genres = []string{}
	}
	derivation.AllowlistChanged = !sameStrings(derivation.CurrentAllowlist, derivation.Genres)
	if derivation.CurrentAllowlist == nil {
		derivation.CurrentAllowlist = []string{}
	}
	return derivation, nil
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// VerifyPlaylistOwnership checks that Navidrome assigned the imported smart
// playlists to the shared account. A playlist owned by someone else would make
// `loved` and personalized results refer to a different user's data.
func VerifyPlaylistOwnership(playlists []Playlist, username string) []string {
	problems := []string{}
	want := strings.TrimSpace(username)
	if want == "" {
		return problems
	}
	managed := map[string]struct{}{
		SmartPlaylistAllName:        {},
		SmartPlaylistHardBounceName: {},
		SmartPlaylistFavoritesName:  {},
	}
	for _, playlist := range playlists {
		if _, ok := managed[playlist.Name]; !ok {
			continue
		}
		if playlist.Owner != "" && !strings.EqualFold(playlist.Owner, want) {
			problems = append(problems, fmt.Sprintf(
				"playlist %q is owned by %q, not the shared account %q; personalized results would refer to another user",
				playlist.Name, playlist.Owner, want))
		}
	}
	return problems
}

// PlaylistRefreshResult is the outcome of regenerating the managed `.nsp`
// files and letting Navidrome re-import them.
type PlaylistRefreshResult struct {
	Generated []GeneratedPlaylist `json:"generated"`
	Imported  []Playlist          `json:"imported"`
	Scanned   bool                `json:"scanned"`
	Warnings  []string            `json:"warnings"`
}

// playlistRefreshClient is the API surface a refresh needs.
type playlistRefreshClient interface {
	StartScan(ctx context.Context, full bool) error
	Playlists(ctx context.Context) ([]Playlist, error)
	Playlist(ctx context.Context, id string) (Playlist, []Song, error)
}

// RefreshManagedPlaylists rewrites the managed `.nsp` files, asks Navidrome to
// rescan so it re-imports them, and then verifies the shared account owns the
// results. A nil client still regenerates the files: the server may simply not
// be reachable yet, and that must not lose the generated definitions.
func RefreshManagedPlaylists(ctx context.Context, cfg Config, resolved Resolved, client playlistRefreshClient) (PlaylistRefreshResult, error) {
	generated, warnings, err := WriteSmartPlaylists(cfg, resolved)
	result := PlaylistRefreshResult{Generated: generated, Imported: []Playlist{}, Warnings: warnings}
	if err != nil {
		return result, err
	}
	if client == nil {
		result.Warnings = append(result.Warnings,
			"the Navidrome API is not configured, so the files were written but not imported yet")
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := client.StartScan(ctx, false); err != nil {
		return result, fmt.Errorf("request a Navidrome scan: %w", err)
	}
	result.Scanned = true

	items, err := client.Playlists(ctx)
	if err != nil {
		return result, err
	}
	managed := map[string]struct{}{
		SmartPlaylistAllName:        {},
		SmartPlaylistHardBounceName: {},
		SmartPlaylistFavoritesName:  {},
	}
	for _, item := range items {
		if _, ok := managed[item.Name]; !ok {
			continue
		}
		// `getPlaylists` reports songCount 0 for a smart playlist Navidrome has
		// not evaluated yet, and only reading the playlist forces it. Doing that
		// here means the refresh never leaves a full playlist displayed as empty.
		if _, songs, readErr := client.Playlist(ctx, item.ID); readErr == nil {
			item.TrackCount = len(songs)
		}
		result.Imported = append(result.Imported, item)
	}
	result.Warnings = append(result.Warnings, VerifyPlaylistOwnership(items, cfg.Server.Username)...)
	if len(result.Imported) < len(generated) {
		result.Warnings = append(result.Warnings,
			"Navidrome has not imported every managed playlist yet; the scan may still be running")
	}
	return result, nil
}
