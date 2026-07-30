package config

type SourceType string

const (
	SourceTypeSpotify    SourceType = "spotify"
	SourceTypeSoundCloud SourceType = "soundcloud"
)

type Config struct {
	Version   int              `yaml:"version" json:"version"`
	Defaults  Defaults         `yaml:"defaults" json:"defaults"`
	Sources   []Source         `yaml:"sources" json:"sources"`
	Rekordbox *RekordboxConfig `yaml:"rekordbox,omitempty" json:"rekordbox,omitempty"`
}

type Defaults struct {
	StateDir              string `yaml:"state_dir" json:"state_dir"`
	ArchiveFile           string `yaml:"archive_file" json:"archive_file"`
	Threads               int    `yaml:"threads" json:"threads"`
	ContinueOnError       bool   `yaml:"continue_on_error" json:"continue_on_error"`
	CommandTimeoutSeconds int    `yaml:"command_timeout_seconds" json:"command_timeout_seconds"`
}

type Source struct {
	ID                  string      `yaml:"id" json:"id"`
	Type                SourceType  `yaml:"type" json:"type"`
	Enabled             bool        `yaml:"enabled" json:"enabled"`
	TargetDir           string      `yaml:"target_dir" json:"target_dir"`
	URL                 string      `yaml:"url" json:"url"`
	StateFile           string      `yaml:"state_file,omitempty" json:"state_file,omitempty"`
	SelectedPlaylistIDs []int       `yaml:"-" json:"-"`
	DisableSyncMode     bool        `yaml:"-" json:"-"`
	DownloadArchivePath string      `yaml:"-" json:"-"`
	DeezerARL           string      `yaml:"-" json:"-"`
	SpotifyClientID     string      `yaml:"-" json:"-"`
	SpotifyClientSecret string      `yaml:"-" json:"-"`
	DeemixRuntimeDir    string      `yaml:"-" json:"-"`
	Sync                SyncPolicy  `yaml:"sync,omitempty" json:"sync,omitempty"`
	Adapter             AdapterSpec `yaml:"adapter" json:"adapter"`
}

type SyncPolicy struct {
	BreakOnExisting *bool `yaml:"break_on_existing,omitempty" json:"break_on_existing,omitempty"`
	AskOnExisting   *bool `yaml:"ask_on_existing,omitempty" json:"ask_on_existing,omitempty"`
	LocalIndexCache *bool `yaml:"local_index_cache,omitempty" json:"local_index_cache,omitempty"`
}

type AdapterSpec struct {
	Kind       string   `yaml:"kind" json:"kind"`
	ExtraArgs  []string `yaml:"extra_args,omitempty" json:"extra_args,omitempty"`
	MinVersion string   `yaml:"min_version,omitempty" json:"min_version,omitempty"`
}

type RekordboxConfig struct {
	DBDir        string                      `yaml:"db_dir,omitempty" json:"db_dir,omitempty"`
	PythonBin    string                      `yaml:"python_bin,omitempty" json:"python_bin,omitempty"`
	PythonPath   string                      `yaml:"python_path,omitempty" json:"python_path,omitempty"`
	BackupDir    string                      `yaml:"backup_dir,omitempty" json:"backup_dir,omitempty"`
	PlaylistSync RekordboxPlaylistSyncConfig `yaml:"playlist_sync,omitempty" json:"playlist_sync,omitempty"`
}

type RekordboxPlaylistSyncConfig struct {
	Jobs []RekordboxPlaylistSyncJob `yaml:"jobs,omitempty" json:"jobs,omitempty"`
}

type RekordboxPlaylistSyncJob struct {
	ID                  string `yaml:"id" json:"id"`
	MusicPlaylist       string `yaml:"music_playlist,omitempty" json:"music_playlist,omitempty"`
	MusicPlaylistID     string `yaml:"music_playlist_id,omitempty" json:"music_playlist_id,omitempty"`
	RekordboxPlaylist   string `yaml:"rekordbox_playlist,omitempty" json:"rekordbox_playlist,omitempty"`
	RekordboxPlaylistID string `yaml:"rekordbox_playlist_id,omitempty" json:"rekordbox_playlist_id,omitempty"`
	Mode                string `yaml:"mode,omitempty" json:"mode,omitempty"`
	CreatePlaylist      *bool  `yaml:"create_playlist,omitempty" json:"create_playlist,omitempty"`
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
