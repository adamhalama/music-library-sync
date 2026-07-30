package config

type SourceType string

const (
	SourceTypeSpotify    SourceType = "spotify"
	SourceTypeSoundCloud SourceType = "soundcloud"
)

type Config struct {
	Version   int              `yaml:"version"`
	Defaults  Defaults         `yaml:"defaults"`
	Sources   []Source         `yaml:"sources"`
	Rekordbox *RekordboxConfig `yaml:"rekordbox,omitempty"`
}

type Defaults struct {
	StateDir              string `yaml:"state_dir"`
	ArchiveFile           string `yaml:"archive_file"`
	Threads               int    `yaml:"threads"`
	ContinueOnError       bool   `yaml:"continue_on_error"`
	CommandTimeoutSeconds int    `yaml:"command_timeout_seconds"`
}

type Source struct {
	ID                  string      `yaml:"id"`
	Type                SourceType  `yaml:"type"`
	Enabled             bool        `yaml:"enabled"`
	TargetDir           string      `yaml:"target_dir"`
	URL                 string      `yaml:"url"`
	StateFile           string      `yaml:"state_file,omitempty"`
	SelectedPlaylistIDs []int       `yaml:"-"`
	DisableSyncMode     bool        `yaml:"-"`
	DownloadArchivePath string      `yaml:"-"`
	DeezerARL           string      `yaml:"-"`
	SpotifyClientID     string      `yaml:"-"`
	SpotifyClientSecret string      `yaml:"-"`
	DeemixRuntimeDir    string      `yaml:"-"`
	Sync                SyncPolicy  `yaml:"sync,omitempty"`
	Adapter             AdapterSpec `yaml:"adapter"`
}

type SyncPolicy struct {
	BreakOnExisting *bool `yaml:"break_on_existing,omitempty"`
	AskOnExisting   *bool `yaml:"ask_on_existing,omitempty"`
	LocalIndexCache *bool `yaml:"local_index_cache,omitempty"`
}

type AdapterSpec struct {
	Kind       string   `yaml:"kind"`
	ExtraArgs  []string `yaml:"extra_args,omitempty"`
	MinVersion string   `yaml:"min_version,omitempty"`
}

type RekordboxConfig struct {
	DBDir        string                      `yaml:"db_dir,omitempty"`
	PythonBin    string                      `yaml:"python_bin,omitempty"`
	PythonPath   string                      `yaml:"python_path,omitempty"`
	BackupDir    string                      `yaml:"backup_dir,omitempty"`
	PlaylistSync RekordboxPlaylistSyncConfig `yaml:"playlist_sync,omitempty"`
}

type RekordboxPlaylistSyncConfig struct {
	Jobs []RekordboxPlaylistSyncJob `yaml:"jobs,omitempty"`
}

type RekordboxPlaylistSyncJob struct {
	ID                  string `yaml:"id"`
	MusicPlaylist       string `yaml:"music_playlist,omitempty"`
	MusicPlaylistID     string `yaml:"music_playlist_id,omitempty"`
	RekordboxPlaylist   string `yaml:"rekordbox_playlist,omitempty"`
	RekordboxPlaylistID string `yaml:"rekordbox_playlist_id,omitempty"`
	Mode                string `yaml:"mode,omitempty"`
	CreatePlaylist      *bool  `yaml:"create_playlist,omitempty"`
}

func DefaultConfig() Config {
	return Config{
		Version: 1,
		Defaults: Defaults{
			StateDir:              defaultStateDir(),
			ArchiveFile:           "archive.txt",
			Threads:               1,
			ContinueOnError:       true,
			CommandTimeoutSeconds: 900,
		},
		Sources: []Source{},
	}
}
