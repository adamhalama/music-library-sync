package navidrome

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/auth"
	"github.com/jaa/update-downloads/internal/rekordbox/music"
)

// ManagerOptions selects the configuration a Manager operates on.
type ManagerOptions struct {
	ConfigPath string
	WorkingDir string
	Env        map[string]string
	// Password overrides Keychain lookup. Only tests and the env override set
	// it; it is never read from configuration.
	Password string
	// SkipCredentials builds a manager for read-only operations that need no
	// server access, so a missing password is not an error.
	SkipCredentials bool
}

// Manager composes config, dependency, service, and API access into the
// operations the CLI, the agent, and the native app all need.
type Manager struct {
	Config     Config
	Resolved   Resolved
	ConfigPath string

	Deps   DependencyChecker
	Svc    Service
	Music  music.Reader
	Now    func() time.Time
	Run    CommandRunner
	API    *Client
	APIErr error
}

// NewManager loads configuration and resolves credentials.
func NewManager(opts ManagerOptions) (*Manager, error) {
	cfg, err := Load(LoadOptions{
		ExplicitPath: opts.ConfigPath,
		WorkingDir:   opts.WorkingDir,
		Env:          opts.Env,
	})
	if err != nil {
		return nil, err
	}
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	resolved, err := Resolve(cfg)
	if err != nil {
		return nil, err
	}
	manager := &Manager{Config: cfg, Resolved: resolved, ConfigPath: strings.TrimSpace(opts.ConfigPath)}

	if opts.SkipCredentials {
		manager.APIErr = errors.New("credentials were not requested")
		return manager, nil
	}
	password := opts.Password
	if password == "" {
		password, err = auth.ResolveNavidromePassword()
		if err != nil {
			manager.APIErr = err
			return manager, nil
		}
	}
	if strings.TrimSpace(cfg.Server.Username) == "" {
		manager.APIErr = errors.New("no Navidrome username is configured")
		return manager, nil
	}
	manager.API = NewClient(cfg.Server.URL, Credentials{Username: cfg.Server.Username, Password: password})
	return manager, nil
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

// Client returns the API client or an actionable reason it is unavailable.
func (m *Manager) Client() (*Client, error) {
	if m.API != nil {
		return m.API, nil
	}
	if m.APIErr != nil {
		return nil, fmt.Errorf("the Navidrome API is not available: %w", m.APIErr)
	}
	return nil, errors.New("the Navidrome API is not configured")
}

// Status is the aggregate read-only view the CLI, agent, and native workspace
// all render. Producing it never mutates anything.
type Status struct {
	Enabled          bool             `json:"enabled"`
	ConfigPath       string           `json:"config_path,omitempty"`
	MusicDir         string           `json:"music_dir"`
	DataDir          string           `json:"data_dir"`
	PlaylistsDir     string           `json:"playlists_dir"`
	Username         string           `json:"username,omitempty"`
	PasswordStored   bool             `json:"password_stored"`
	Dependency       DependencyStatus `json:"dependency"`
	Service          ServiceStatus    `json:"service"`
	Reachable        bool             `json:"reachable"`
	Server           ServerInfo       `json:"server"`
	LibraryTracks    int              `json:"library_tracks"`
	Scanning         bool             `json:"scanning"`
	ManagedPlaylists []Playlist       `json:"managed_playlists"`
	BackupCount      int              `json:"backup_count"`
	LatestBackup     *Backup          `json:"latest_backup,omitempty"`
	LogPath          string           `json:"log_path,omitempty"`
	Problems         []string         `json:"problems"`
}

// Status inspects dependency, service, and — when credentials exist — server
// state. Every failure becomes a problem string rather than an error, because
// a status view must always render.
func (m *Manager) Status(ctx context.Context) Status {
	status := Status{
		Enabled:      m.Config.Enabled,
		ConfigPath:   m.ConfigPath,
		MusicDir:     m.Resolved.MusicDir,
		DataDir:      m.Resolved.DataDir,
		PlaylistsDir: m.Resolved.PlaylistsDir,
		Username:     m.Config.Server.Username,
		LogPath:      m.Resolved.LogFile,
		Problems:     []string{},
	}
	status.PasswordStored = auth.HasNavidromePassword()
	status.Dependency = m.Deps.Status(ctx)
	status.Problems = append(status.Problems, status.Dependency.Problems...)
	status.Service = m.Svc.Status(ctx, m.Config, m.Resolved)
	status.Problems = append(status.Problems, status.Service.Problems...)
	status.Problems = append(status.Problems, m.staleManagedFiles(ctx)...)

	if backups, err := Backups(m.Resolved); err == nil {
		status.BackupCount = len(backups)
		if len(backups) > 0 {
			latest := backups[0]
			status.LatestBackup = &latest
		}
	} else {
		status.Problems = append(status.Problems, err.Error())
	}

	client, err := m.Client()
	if err != nil {
		status.ManagedPlaylists = []Playlist{}
		if !status.PasswordStored {
			status.Problems = append(status.Problems,
				"no Navidrome password is saved; add it to macOS Keychain to let UDL read the library")
		} else {
			status.Problems = append(status.Problems, err.Error())
		}
		return status
	}

	info, err := client.Ping(ctx)
	status.Server = info
	if err != nil {
		status.ManagedPlaylists = []Playlist{}
		status.Problems = append(status.Problems, err.Error())
		return status
	}
	status.Reachable = true

	if scanning, count, err := client.ScanStatus(ctx); err == nil {
		status.Scanning = scanning
		status.LibraryTracks = int(count)
	} else {
		status.Problems = append(status.Problems, err.Error())
	}

	status.ManagedPlaylists = []Playlist{}
	playlists, err := client.Playlists(ctx)
	if err != nil {
		status.Problems = append(status.Problems, err.Error())
		return status
	}
	managed := map[string]struct{}{
		SmartPlaylistAllName:        {},
		SmartPlaylistHardBounceName: {},
		SmartPlaylistFavoritesName:  {},
	}
	for _, playlist := range playlists {
		if _, ok := managed[playlist.Name]; ok {
			status.ManagedPlaylists = append(status.ManagedPlaylists, playlist)
		}
	}
	status.Problems = append(status.Problems, VerifyPlaylistOwnership(playlists, m.Config.Server.Username)...)
	return status
}

// SetupPlan builds a checksummed setup plan.
func (m *Manager) SetupPlan(ctx context.Context) (SetupPlan, error) {
	return m.planner().Plan(ctx, m.Config, m.ConfigPath)
}

// ApplySetup revalidates and performs a setup plan.
func (m *Manager) ApplySetup(ctx context.Context, plan SetupPlan) (ApplyResult, error) {
	return m.planner().Apply(ctx, m.Config, plan)
}

func (m *Manager) planner() Planner {
	return Planner{Deps: m.Deps, Service: m.Svc, Now: m.Now}
}

// staleManagedFiles reports managed files whose on-disk content no longer
// matches what UDL would write. Without this a config fix shipped in a new
// build stays invisible: the running service keeps the old file and status
// reports everything as healthy. That is exactly how a missing FFmpegPath
// survived unnoticed until playback failed on the phone.
func (m *Manager) staleManagedFiles(ctx context.Context) []string {
	plan, err := m.planner().Plan(ctx, m.Config, m.ConfigPath)
	if err != nil {
		return nil
	}
	var stale []string
	for _, file := range plan.Files {
		if file.Action == FileActionReplace {
			stale = append(stale, fmt.Sprintf(
				"%s at %s is out of date; run `udl navidrome setup apply` to update it",
				file.Label, file.Path))
		}
	}
	return stale
}

// RefreshPlaylists regenerates the managed `.nsp` files and re-imports them.
func (m *Manager) RefreshPlaylists(ctx context.Context) (PlaylistRefreshResult, error) {
	client, err := m.Client()
	if err != nil {
		return RefreshManagedPlaylists(ctx, m.Config, m.Resolved, nil)
	}
	return RefreshManagedPlaylists(ctx, m.Config, m.Resolved, client)
}

// DeriveGenres previews the Hard Bounce allowlist from the configured Apple
// Music source playlist. It reads Apple Music only when called.
func (m *Manager) DeriveGenres(ctx context.Context) (GenreDerivation, error) {
	client, err := m.Client()
	if err != nil {
		return GenreDerivation{}, err
	}
	catalog, err := client.Songs(ctx)
	if err != nil {
		return GenreDerivation{}, err
	}
	sourceName := m.Config.Playlists.AppleHardBouncePlaylist
	_, tracks, err := m.Music.ReadPlaylist(ctx, music.PlaylistSelector{Name: sourceName})
	if err != nil {
		return GenreDerivation{}, fmt.Errorf("read Apple Music playlist %q: %w", sourceName, err)
	}
	source := make([]SourceTrack, 0, len(tracks))
	for _, track := range tracks {
		source = append(source, SourceTrack{Title: track.Title, Path: track.Path})
	}
	return DeriveHardBounceGenres(ctx, m.Config, m.Resolved, sourceName, source, catalog)
}

// SaveGenres persists an approved allowlist and rewrites the playlists.
func (m *Manager) SaveGenres(ctx context.Context, genres []string) (PlaylistRefreshResult, error) {
	normalized := NormalizeGenres(genres)
	if len(normalized) == 0 {
		return PlaylistRefreshResult{}, fmt.Errorf("refusing to save an empty HARD BOUNCE genre allowlist")
	}
	m.Config.Playlists.HardBounceGenres = normalized
	path, err := ResolveWritePath(m.ConfigPath, "")
	if err != nil {
		return PlaylistRefreshResult{}, err
	}
	if err := Save(path, m.Config); err != nil {
		return PlaylistRefreshResult{}, err
	}
	m.ConfigPath = path
	return m.RefreshPlaylists(ctx)
}

// AppleFavorites reads the favorited local tracks from Apple Music.
func (m *Manager) AppleFavorites(ctx context.Context) ([]AppleFavorite, error) {
	tracks, err := m.Music.ListFavoriteTracks(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AppleFavorite, 0, len(tracks))
	for _, track := range tracks {
		out = append(out, AppleFavorite{
			PersistentID: track.PersistentID,
			Title:        track.Title,
			Artist:       track.Artist,
			Album:        track.Album,
			Path:         track.Path,
		})
	}
	return out, nil
}

// StarredFavorites reads what is starred on the server right now — the return
// path for a like made on the phone. It is read-only and never touches Apple
// Music, so it works with the server as the only live dependency.
//
// The order matches the snapshot builder's, so a listing and a refreshed
// snapshot can be compared line for line.
func (m *Manager) StarredFavorites(ctx context.Context) ([]Song, error) {
	client, err := m.Client()
	if err != nil {
		return nil, err
	}
	songs, err := client.Starred(ctx)
	if err != nil {
		return nil, err
	}
	SortStarredSongs(songs)
	return songs, nil
}

// FavoritePlan builds the migration plan from live Apple Music and Navidrome
// state.
func (m *Manager) FavoritePlan(ctx context.Context) (FavoritePlan, error) {
	client, err := m.Client()
	if err != nil {
		return FavoritePlan{}, err
	}
	favorites, err := m.AppleFavorites(ctx)
	if err != nil {
		return FavoritePlan{}, err
	}
	catalog, err := client.Songs(ctx)
	if err != nil {
		return FavoritePlan{}, err
	}
	return BuildFavoritePlan(ctx, m.Config, m.Resolved, favorites, catalog, m.now())
}

// ApplyFavorites revalidates and performs a favorite migration plan.
func (m *Manager) ApplyFavorites(ctx context.Context, plan FavoritePlan) (FavoriteApplyResult, error) {
	client, err := m.Client()
	if err != nil {
		return FavoriteApplyResult{}, err
	}
	favorites, err := m.AppleFavorites(ctx)
	if err != nil {
		return FavoriteApplyResult{}, err
	}
	backup := func(backupCtx context.Context) (Backup, error) {
		result, err := CreateBackup(backupCtx, m.Run, m.Deps.BinaryPath(), m.Resolved)
		return result.Backup, err
	}
	return ApplyFavoritePlan(ctx, m.Config, m.Resolved, plan, favorites, client, backup)
}

// ReconcileDateAdded makes Date Added mean the file's real creation time.
func (m *Manager) ReconcileDateAdded(ctx context.Context) (ReconcileResult, error) {
	return ReconcileDateAdded(ctx, m.Run, m.Resolved, func(backupCtx context.Context) (Backup, error) {
		result, err := CreateBackup(backupCtx, m.Run, m.Deps.BinaryPath(), m.Resolved)
		return result.Backup, err
	})
}

// CreateBackup makes an explicit database backup.
func (m *Manager) CreateBackup(ctx context.Context) (BackupResult, error) {
	return CreateBackup(ctx, m.Run, m.Deps.BinaryPath(), m.Resolved)
}

// RequestScan asks Navidrome to rescan. Callers that must not fail on an
// unavailable server should use RequestScanBestEffort instead.
func (m *Manager) RequestScan(ctx context.Context, full bool) error {
	client, err := m.Client()
	if err != nil {
		return err
	}
	return client.StartScan(ctx, full)
}

// RequestScanBestEffort asks for a scan and returns a human-readable warning
// instead of an error. A download run must never fail because the optional
// phone server is unavailable.
func RequestScanBestEffort(ctx context.Context, opts ManagerOptions) (bool, string) {
	// The enabled check comes first and deliberately skips credentials: an
	// unconfigured install must not touch the Keychain on every sync.
	probeOpts := opts
	probeOpts.SkipCredentials = true
	probe, err := NewManager(probeOpts)
	if err != nil {
		return false, fmt.Sprintf("Navidrome scan skipped: %v", err)
	}
	if !probe.Config.Enabled {
		return false, ""
	}
	manager, err := NewManager(opts)
	if err != nil {
		return false, fmt.Sprintf("Navidrome scan skipped: %v", err)
	}
	if err := manager.RequestScan(ctx, false); err != nil {
		return false, fmt.Sprintf("Navidrome scan request failed: %v", err)
	}
	return true, ""
}
