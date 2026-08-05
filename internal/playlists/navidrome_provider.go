package playlists

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jaa/update-downloads/internal/auth"
	"github.com/jaa/update-downloads/internal/navidrome"
)

// NavidromeClient is the subset of the Navidrome API this provider needs.
// Keeping it an interface lets the provider be tested against the package's
// own fake server without a live service.
type NavidromeClient interface {
	Playlists(ctx context.Context) ([]navidrome.Playlist, error)
	Playlist(ctx context.Context, id string) (navidrome.Playlist, []navidrome.Song, error)
	PlaylistByName(ctx context.Context, name string) (navidrome.Playlist, bool, error)
	Starred(ctx context.Context) ([]navidrome.Song, error)
}

// NavidromeStarredPlaylistID is a sentinel provider playlist ID, not a server
// ID. A definition carrying it reads stars straight from `getStarred2` instead
// of resolving a playlist. The `Favourites (Navidrome)` smart playlist stays on
// the server for Amperfy's own browsing, but reading through it would depend on
// the server having imported the `.nsp` file and on ownership verification
// passing — a round trip that is not needed to answer "what is starred".
const NavidromeStarredPlaylistID = "starred"

// NavidromeProvider reads Navidrome playlists into the existing snapshot
// shape, so Free DL and Rekordbox consume them without learning the API.
type NavidromeProvider struct {
	Client NavidromeClient
}

// NewNavidromeProvider builds a provider from the feature config and the
// Keychain-stored password. It never reads the password from configuration.
func NewNavidromeProvider() (NavidromeProvider, error) {
	cfg, err := navidrome.Load(navidrome.LoadOptions{})
	if err != nil {
		return NavidromeProvider{}, err
	}
	if strings.TrimSpace(cfg.Server.Username) == "" {
		return NavidromeProvider{}, fmt.Errorf("no Navidrome username is configured; finish Phone Library setup first")
	}
	password, err := auth.ResolveNavidromePassword()
	if err != nil {
		return NavidromeProvider{}, fmt.Errorf("navidrome password is not available: %w", err)
	}
	return NavidromeProvider{
		Client: navidrome.NewClient(cfg.Server.URL, navidrome.Credentials{
			Username: cfg.Server.Username,
			Password: password,
		}),
	}, nil
}

// List enumerates server playlists.
func (p NavidromeProvider) List(ctx context.Context) ([]ProviderPlaylist, error) {
	if p.Client == nil {
		return nil, fmt.Errorf("navidrome client is not configured")
	}
	items, err := p.Client.Playlists(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ProviderPlaylist, 0, len(items))
	for _, item := range items {
		out = append(out, ProviderPlaylist{
			Name: item.Name, ID: item.ID, Smart: item.Smart, TrackCount: item.TrackCount,
		})
	}
	return out, nil
}

// Read returns ordered playlist membership as snapshot tracks.
func (p NavidromeProvider) Read(ctx context.Context, definition Definition) (ProviderPlaylist, []Track, error) {
	if p.Client == nil {
		return ProviderPlaylist{}, nil, fmt.Errorf("navidrome client is not configured")
	}
	id := strings.TrimSpace(definition.ProviderPlaylistID)
	if id == NavidromeStarredPlaylistID {
		return p.readStarred(ctx)
	}
	if id == "" {
		found, ok, err := p.Client.PlaylistByName(ctx, definition.ProviderPlaylist)
		if err != nil {
			return ProviderPlaylist{}, nil, err
		}
		if !ok {
			return ProviderPlaylist{}, nil, fmt.Errorf(
				"navidrome playlist %q was not found; run a refresh after the server imports the managed .nsp files",
				definition.ProviderPlaylist)
		}
		id = found.ID
	}
	playlist, songs, err := p.Client.Playlist(ctx, id)
	if err != nil {
		return ProviderPlaylist{}, nil, err
	}
	return ProviderPlaylist{
		Name: playlist.Name, ID: playlist.ID, Smart: playlist.Smart, TrackCount: playlist.TrackCount,
	}, songTracks(songs), nil
}

// readStarred answers "what is starred right now" directly. There is no server
// playlist behind it, so the returned ProviderPlaylist is synthetic and carries
// the sentinel as its ID.
func (p NavidromeProvider) readStarred(ctx context.Context) (ProviderPlaylist, []Track, error) {
	songs, err := p.Client.Starred(ctx)
	if err != nil {
		return ProviderPlaylist{}, nil, err
	}
	// One definition of starred order, shared with the CLI and agent surfaces,
	// so no consumer can drift and reintroduce checksum churn.
	sorted := make([]navidrome.Song, len(songs))
	copy(sorted, songs)
	navidrome.SortStarredSongs(sorted)
	tracks := songTracks(sorted)
	return ProviderPlaylist{
		Name:       navidrome.SmartPlaylistFavoritesName,
		ID:         NavidromeStarredPlaylistID,
		Smart:      true,
		TrackCount: len(tracks),
	}, tracks, nil
}

// songTracks converts server songs into snapshot tracks in the order given.
// Both read paths share it so a starred track and a playlist track are
// described identically, including how a missing local file is decided.
func songTracks(songs []navidrome.Song) []Track {
	tracks := make([]Track, 0, len(songs))
	for index, song := range songs {
		path := song.Path
		missing := path == ""
		if !missing {
			if _, statErr := os.Stat(path); statErr != nil {
				missing = true
			}
		}
		tracks = append(tracks, Track{
			Index:        index + 1,
			ProviderID:   song.ID,
			Artist:       song.Artist,
			Title:        song.Title,
			Album:        song.Album,
			Duration:     song.Duration,
			Path:         path,
			MissingLocal: missing,
		})
	}
	return tracks
}

// NavidromeDefinitions are the managed standalone definitions added by setup.
// They never replace the existing Apple Music `favorites` entry.
func NavidromeDefinitions() []Definition {
	return []Definition{
		{
			ID:               navidrome.SmartPlaylistAll,
			Name:             navidrome.SmartPlaylistAllName,
			Provider:         ProviderNavidrome,
			ProviderPlaylist: navidrome.SmartPlaylistAllName,
		},
		{
			ID:               navidrome.SmartPlaylistHardBounce,
			Name:             navidrome.SmartPlaylistHardBounceName,
			Provider:         ProviderNavidrome,
			ProviderPlaylist: navidrome.SmartPlaylistHardBounceName,
		},
		{
			ID:       navidrome.SmartPlaylistFavorites,
			Name:     navidrome.SmartPlaylistFavoritesName,
			Provider: ProviderNavidrome,
			// The name stays populated so Validate passes and the UI reads
			// well, but the sentinel ID is what selects the read path.
			ProviderPlaylist:   navidrome.SmartPlaylistFavoritesName,
			ProviderPlaylistID: NavidromeStarredPlaylistID,
			// Apple and Navidrome favourites stay permanently separate, so
			// they land in different Rekordbox playlists. The Apple
			// `favorites` definition and its `fav_imports` target are not
			// touched, merged, or migrated.
			DefaultRekordboxTarget: "nav_fav_imports",
		},
	}
}

// EnsureNavidromeDefinitions adds any missing managed definition and returns
// the merged config plus which IDs were added. Existing entries — including the
// Apple Music `favorites` definition — are left untouched.
func EnsureNavidromeDefinitions(cfg Config) (Config, []string) {
	added := []string{}
	for _, definition := range NavidromeDefinitions() {
		if _, exists := cfg.Definition(definition.ID); exists {
			continue
		}
		cfg.Playlists = append(cfg.Playlists, definition)
		added = append(added, definition.ID)
	}
	return cfg, added
}

// WriteNavidromeDefinitions merges the managed definitions into the playlists
// config on disk and returns which IDs were added. It is idempotent, so a
// second setup apply reports nothing added rather than duplicating entries.
//
// Setup calls this because a definition nobody wrote down cannot be refreshed:
// without it `udl playlist refresh navidrome-favorites` has nothing to resolve.
func WriteNavidromeDefinitions(explicitPath, workingDir string) ([]string, error) {
	path, err := ResolveWritePath(explicitPath, workingDir)
	if err != nil {
		return nil, err
	}
	// Read the file that will be written, not the merged view. Saving a merged
	// user+project view back into one file would silently copy the other file's
	// entries into it.
	cfg := Config{Version: ConfigVersion}
	if err := mergeFile(&cfg, path, false); err != nil {
		return nil, err
	}
	merged, added := EnsureNavidromeDefinitions(cfg)
	if len(added) == 0 {
		return added, nil
	}
	if err := Save(path, merged); err != nil {
		return nil, err
	}
	return added, nil
}
