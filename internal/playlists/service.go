package playlists

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/rekordbox/music"
)

type ProviderPlaylist struct {
	Name       string `json:"name"`
	ID         string `json:"id,omitempty"`
	Smart      bool   `json:"smart,omitempty"`
	TrackCount int    `json:"track_count"`
}

type Provider interface {
	List(ctx context.Context) ([]ProviderPlaylist, error)
	Read(ctx context.Context, definition Definition) (ProviderPlaylist, []Track, error)
}

type AppleMusicProvider struct {
	Reader music.Reader
}

func (p AppleMusicProvider) List(ctx context.Context) ([]ProviderPlaylist, error) {
	items, err := p.Reader.ListPlaylists(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ProviderPlaylist, 0, len(items))
	for _, item := range items {
		if item.Folder {
			continue
		}
		out = append(out, ProviderPlaylist{
			Name: item.Name, ID: item.PersistentID, Smart: item.Smart, TrackCount: item.TrackCount,
		})
	}
	return out, nil
}

func (p AppleMusicProvider) Read(ctx context.Context, definition Definition) (ProviderPlaylist, []Track, error) {
	playlist, tracks, err := p.Reader.ReadPlaylist(ctx, music.PlaylistSelector{
		Name: definition.ProviderPlaylist, PersistentID: definition.ProviderPlaylistID,
	})
	if err != nil {
		return ProviderPlaylist{}, nil, err
	}
	out := make([]Track, 0, len(tracks))
	for _, track := range tracks {
		path := strings.TrimSpace(track.Path)
		missing := false
		if path == "" {
			missing = true
		} else if _, statErr := os.Stat(path); statErr != nil {
			missing = true
		}
		out = append(out, Track{
			Index: track.Index, ProviderID: track.PersistentID, DatabaseID: track.DatabaseID,
			Artist: track.Artist, Title: track.Title, Album: track.Album,
			Duration: track.Duration, Path: path, MissingLocal: missing,
		})
	}
	return ProviderPlaylist{
		Name: playlist.Name, ID: playlist.PersistentID, Smart: playlist.Smart, TrackCount: playlist.TrackCount,
	}, out, nil
}

type Service struct {
	// Provider, when set, overrides provider selection entirely. Tests use it;
	// production code lets the definition's provider decide.
	Provider Provider
	// ProviderFactory overrides how a named provider is constructed.
	ProviderFactory func(provider string) (Provider, error)
	Now             func() time.Time
}

type RefreshResult struct {
	Snapshot Snapshot `json:"snapshot"`
	Changes  Changes  `json:"changes"`
	Path     string   `json:"path"`
}

func (s Service) ListProviderPlaylists(ctx context.Context, provider string) ([]ProviderPlaylist, error) {
	selected, err := s.providerFor(provider)
	if err != nil {
		return nil, err
	}
	return selected.List(ctx)
}

func (s Service) Refresh(ctx context.Context, main config.Config, definition Definition) (RefreshResult, error) {
	if err := Validate(Config{Version: ConfigVersion, Playlists: []Definition{definition}}); err != nil {
		return RefreshResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return RefreshResult{}, err
	}
	selected, err := s.providerFor(definition.Provider)
	if err != nil {
		return RefreshResult{}, err
	}
	providerPlaylist, tracks, err := selected.Read(ctx, definition)
	if err != nil {
		return RefreshResult{}, fmt.Errorf("refresh playlist %q: %w", definition.Name, err)
	}
	if err := ctx.Err(); err != nil {
		return RefreshResult{}, err
	}
	next := Snapshot{
		Version: SnapshotVersion, PlaylistID: definition.ID, Name: definition.Name,
		Provider: definition.Provider, ProviderPlaylist: providerPlaylist.Name,
		ProviderPlaylistID: providerPlaylist.ID, RefreshedAt: s.now(), Tracks: tracks,
	}
	previous, previousErr := LoadSnapshot(main.Defaults.StateDir, definition.ID)
	changes := Changes{Added: len(next.Tracks)}
	if previousErr == nil {
		changes = CompareSnapshots(previous, next)
	}
	path, err := WriteSnapshot(main.Defaults.StateDir, next)
	if err != nil {
		return RefreshResult{}, err
	}
	saved, err := LoadSnapshot(main.Defaults.StateDir, definition.ID)
	if err != nil {
		return RefreshResult{}, err
	}
	return RefreshResult{Snapshot: saved, Changes: changes, Path: path}, nil
}

func (s Service) providerFor(provider string) (Provider, error) {
	if s.Provider != nil {
		return s.Provider, nil
	}
	if s.ProviderFactory != nil {
		return s.ProviderFactory(provider)
	}
	switch provider {
	case ProviderAppleMusic:
		return AppleMusicProvider{}, nil
	case ProviderNavidrome:
		return NewNavidromeProvider()
	default:
		return nil, fmt.Errorf("playlist provider %q is unsupported", provider)
	}
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
