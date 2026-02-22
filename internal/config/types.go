package config

type SourceType string

const (
	SourceTypeSpotify    SourceType = "spotify"
	SourceTypeSoundCloud SourceType = "soundcloud"
)

type Config struct {
	Version  int      `yaml:"version"`
	Defaults Defaults `yaml:"defaults"`
	Sources  []Source `yaml:"sources"`
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
}

type AdapterSpec struct {
	Kind       string   `yaml:"kind"`
	ExtraArgs  []string `yaml:"extra_args,omitempty"`
	MinVersion string   `yaml:"min_version,omitempty"`
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
