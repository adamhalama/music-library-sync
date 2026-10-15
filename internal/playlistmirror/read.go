package playlistmirror

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jaa/update-downloads/internal/navidrome"
	"github.com/jaa/update-downloads/internal/pathidentity"
	"github.com/jaa/update-downloads/internal/playlists"
	"github.com/jaa/update-downloads/internal/rekordbox/bridge"
)

type Provider string

const (
	ProviderRekordbox Provider = "rekordbox"
	ProviderNavidrome Provider = "navidrome"
)

type Track struct {
	Index          int    `json:"index"`
	ProviderID     string `json:"provider_id"`
	Artist         string `json:"artist,omitempty"`
	Title          string `json:"title"`
	Album          string `json:"album,omitempty"`
	Duration       string `json:"duration,omitempty"`
	RawPath        string `json:"raw_path"`
	NormalizedPath string `json:"normalized_path"`
}

type Playlist struct {
	Provider Provider `json:"provider"`
	ID       string   `json:"id,omitempty"`
	Name     string   `json:"name"`
	Owner    string   `json:"owner,omitempty"`
	Smart    bool     `json:"smart"`
	Missing  bool     `json:"missing"`
	Tracks   []Track  `json:"tracks"`
}

func ReadRekordbox(inspect bridge.InspectResponse, selector playlists.PlaylistSelector, binding *playlists.ProviderBinding, allowMissing bool) (Playlist, error) {
	selected, missing, err := resolveRekordboxPlaylist(inspect.Playlists, selector, binding, allowMissing)
	if err != nil {
		return Playlist{}, err
	}
	result := Playlist{Provider: ProviderRekordbox, ID: selected.ID, Name: selected.Name, Missing: missing, Tracks: []Track{}}
	if missing {
		return result, nil
	}
	contents := make(map[string]bridge.Content, len(inspect.Contents))
	duplicateIDs := map[string]bool{}
	for _, content := range inspect.Contents {
		if _, exists := contents[content.ID]; exists {
			duplicateIDs[content.ID] = true
		}
		contents[content.ID] = content
	}
	for index, contentID := range selected.ContentIDs {
		content, ok := contents[contentID]
		if !ok {
			return Playlist{}, fmt.Errorf("Rekordbox playlist %q references missing content ID %q", selected.Name, contentID)
		}
		if duplicateIDs[contentID] {
			return Playlist{}, fmt.Errorf("Rekordbox inspection returned duplicate content ID %q", contentID)
		}
		result.Tracks = append(result.Tracks, Track{
			Index: index + 1, ProviderID: content.ID, Artist: content.Artist, Title: content.Title, Album: content.Album,
			Duration: formatDuration(content.DurationSeconds), RawPath: content.FolderPath,
			NormalizedPath: rekordboxRealPath(content.FolderPath),
		})
	}
	return result, nil
}

type NavidromeReader interface {
	Playlists(context.Context) ([]navidrome.Playlist, error)
	Playlist(context.Context, string) (navidrome.Playlist, []navidrome.Song, error)
	Songs(context.Context) ([]navidrome.Song, error)
}

func ReadNavidrome(ctx context.Context, client NavidromeReader, selector playlists.PlaylistSelector, binding *playlists.ProviderBinding, allowMissing bool) (Playlist, error) {
	if client == nil {
		return Playlist{}, errors.New("navidrome client is not configured")
	}
	items, err := client.Playlists(ctx)
	if err != nil {
		return Playlist{}, err
	}
	selected, missing, err := resolveNavidromePlaylist(items, selector, binding, allowMissing)
	if err != nil {
		return Playlist{}, err
	}
	result := Playlist{
		Provider: ProviderNavidrome, ID: selected.ID, Name: selected.Name, Owner: selected.Owner,
		Smart: selected.Smart, Missing: missing, Tracks: []Track{},
	}
	if missing {
		return result, nil
	}
	detail, songs, err := client.Playlist(ctx, selected.ID)
	if err != nil {
		return Playlist{}, err
	}
	result.Name, result.Owner, result.Smart = detail.Name, detail.Owner, detail.Smart
	for index, song := range songs {
		result.Tracks = append(result.Tracks, Track{
			Index: index + 1, ProviderID: song.ID, Artist: song.Artist, Title: song.Title, Album: song.Album,
			Duration: song.Duration, RawPath: song.Path, NormalizedPath: pathidentity.Canonical(song.Path),
		})
	}
	return result, nil
}

// NavidromeCatalog preserves every row for a canonical path. Callers must
// treat a slice longer than one as ambiguous; no row is silently collapsed.
type NavidromeCatalog map[string][]navidrome.Song

type RekordboxCatalog map[string][]bridge.Content

func IndexRekordboxCatalog(contents []bridge.Content) (RekordboxCatalog, error) {
	index := RekordboxCatalog{}
	seenIDs := map[string]struct{}{}
	for _, content := range contents {
		if strings.TrimSpace(content.ID) == "" {
			return nil, errors.New("Rekordbox inspection returned content without an ID")
		}
		if _, exists := seenIDs[content.ID]; exists {
			return nil, fmt.Errorf("Rekordbox inspection returned duplicate content ID %q", content.ID)
		}
		seenIDs[content.ID] = struct{}{}
		path := rekordboxRealPath(content.FolderPath)
		if path == "" {
			// Streaming and other non-file rows cannot match a local library path.
			// Selected source rows are retained by ReadRekordbox and blocked by BuildPlan.
			continue
		}
		index[path] = append(index[path], content)
	}
	return index, nil
}

func rekordboxRealPath(raw string) string {
	path := pathidentity.Canonical(raw)
	if !filepath.IsAbs(path) {
		return ""
	}
	return path
}

func ReadNavidromeCatalog(ctx context.Context, client NavidromeReader) (NavidromeCatalog, error) {
	if client == nil {
		return nil, errors.New("navidrome client is not configured")
	}
	songs, err := client.Songs(ctx)
	if err != nil {
		return nil, err
	}
	index := NavidromeCatalog{}
	seenIDs := map[string]struct{}{}
	for _, song := range songs {
		if strings.TrimSpace(song.ID) == "" {
			return nil, errors.New("Navidrome catalog returned a song without an ID")
		}
		if _, exists := seenIDs[song.ID]; exists {
			return nil, fmt.Errorf("Navidrome catalog returned duplicate song ID %q", song.ID)
		}
		seenIDs[song.ID] = struct{}{}
		path := pathidentity.Canonical(song.Path)
		if path == "" || !filepath.IsAbs(path) {
			return nil, fmt.Errorf("Navidrome song %q does not expose an absolute real path; enable DefaultReportRealPath and rescan", song.ID)
		}
		index[path] = append(index[path], song)
	}
	return index, nil
}

func resolveRekordboxPlaylist(items []bridge.Playlist, selector playlists.PlaylistSelector, binding *playlists.ProviderBinding, allowMissing bool) (bridge.Playlist, bool, error) {
	if id := strings.TrimSpace(selector.PlaylistID); id != "" {
		for _, item := range items {
			if item.ID == id {
				return validateRekordboxPlaylist(item)
			}
		}
		return bridge.Playlist{}, false, fmt.Errorf("Rekordbox playlist ID %q was not found", id)
	}
	if binding != nil && strings.TrimSpace(binding.PlaylistID) != "" {
		for _, item := range items {
			if item.ID == binding.PlaylistID {
				return validateRekordboxPlaylist(item)
			}
		}
	}
	matches := []bridge.Playlist{}
	for _, item := range items {
		if item.Name == strings.TrimSpace(selector.Playlist) {
			matches = append(matches, item)
		}
	}
	if len(matches) > 1 {
		return bridge.Playlist{}, false, fmt.Errorf("multiple Rekordbox playlists are named %q; configure playlist_id", selector.Playlist)
	}
	if len(matches) == 1 {
		return validateRekordboxPlaylist(matches[0])
	}
	if allowMissing {
		return bridge.Playlist{Name: strings.TrimSpace(selector.Playlist)}, true, nil
	}
	return bridge.Playlist{}, false, fmt.Errorf("Rekordbox playlist %q was not found", selector.Playlist)
}

func validateRekordboxPlaylist(item bridge.Playlist) (bridge.Playlist, bool, error) {
	if item.Attribute != 0 {
		return bridge.Playlist{}, false, fmt.Errorf("Rekordbox playlist %q is not a normal playlist", item.Name)
	}
	return item, false, nil
}

func resolveNavidromePlaylist(items []navidrome.Playlist, selector playlists.PlaylistSelector, binding *playlists.ProviderBinding, allowMissing bool) (navidrome.Playlist, bool, error) {
	if id := strings.TrimSpace(selector.PlaylistID); id != "" {
		for _, item := range items {
			if item.ID == id {
				return item, false, nil
			}
		}
		return navidrome.Playlist{}, false, fmt.Errorf("Navidrome playlist ID %q was not found", id)
	}
	if binding != nil && strings.TrimSpace(binding.PlaylistID) != "" {
		for _, item := range items {
			if item.ID == binding.PlaylistID {
				return item, false, nil
			}
		}
	}
	matches := []navidrome.Playlist{}
	for _, item := range items {
		if item.Name == strings.TrimSpace(selector.Playlist) {
			matches = append(matches, item)
		}
	}
	if len(matches) > 1 {
		return navidrome.Playlist{}, false, fmt.Errorf("multiple Navidrome playlists are named %q; configure playlist_id", selector.Playlist)
	}
	if len(matches) == 1 {
		return matches[0], false, nil
	}
	if allowMissing {
		return navidrome.Playlist{Name: strings.TrimSpace(selector.Playlist)}, true, nil
	}
	return navidrome.Playlist{}, false, fmt.Errorf("Navidrome playlist %q was not found", selector.Playlist)
}

func formatDuration(seconds int) string {
	if seconds <= 0 {
		return ""
	}
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}
