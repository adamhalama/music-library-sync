package navidrome

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jaa/update-downloads/internal/config"
	"gopkg.in/yaml.v3"
)

const (
	// ConfigVersion is the schema version of navidrome.yaml.
	ConfigVersion = 1

	// EnvConfigPath overrides discovery with an explicit config file.
	EnvConfigPath = "UDL_NAVIDROME_CONFIG"

	// ProjectConfigName is the per-project config file name.
	ProjectConfigName = "udl.navidrome.yaml"

	// MinimumServerVersion is the oldest Navidrome UDL will manage.
	MinimumServerVersion = "0.63.2"

	// LaunchAgentLabel is the launchd label of the UDL-managed service.
	LaunchAgentLabel = "com.jaa.udl.navidrome"

	// OwnershipMarker appears in every file UDL is allowed to replace. A file
	// that lacks it is treated as someone else's and never overwritten.
	OwnershipMarker = "udl-managed: com.jaa.udl.navidrome"

	// DefaultPort is the Navidrome LAN port.
	DefaultPort = 4533

	// DefaultPlaylistsPath is relative to the music root, per Navidrome's
	// PlaylistsPath semantics.
	//
	// It is deliberately not a dot-directory: Navidrome's scanner skips hidden
	// files and folders, so a `.udl/playlists` path is walked past in silence
	// and no managed playlist is ever imported. Verified against 0.63.2.
	DefaultPlaylistsPath = "udl-playlists"

	// DefaultBackupCount is how many scheduled database backups are retained.
	DefaultBackupCount = 7

	// DefaultBackupSchedule runs a database backup nightly.
	DefaultBackupSchedule = "0 3 * * *"

	// DefaultScanSchedule keeps the library fresh even without a sync run.
	DefaultScanSchedule = "@every 1h"

	// DefaultAppleHardBouncePlaylist is the Apple Music playlist the genre
	// allowlist is derived from.
	DefaultAppleHardBouncePlaylist = "HARD BOUNCE"
)

// Config is the versioned navidrome.yaml feature configuration. It never holds
// the account password: that lives only in the macOS Keychain.
type Config struct {
	Version   int             `yaml:"version" json:"version"`
	Enabled   bool            `yaml:"enabled" json:"enabled"`
	Server    ServerConfig    `yaml:"server" json:"server"`
	Paths     PathsConfig     `yaml:"paths" json:"paths"`
	Scan      ScanConfig      `yaml:"scan" json:"scan"`
	Backup    BackupConfig    `yaml:"backup" json:"backup"`
	Playlists PlaylistsConfig `yaml:"playlists" json:"playlists"`
	Phone     PhoneConfig     `yaml:"phone" json:"phone"`
}

// ServerConfig describes how UDL and Amperfy reach the server.
type ServerConfig struct {
	URL      string `yaml:"url" json:"url"`
	Username string `yaml:"username" json:"username"`
	Port     int    `yaml:"port" json:"port"`
	Address  string `yaml:"address" json:"address"`
}

// PathsConfig holds every managed filesystem location.
type PathsConfig struct {
	MusicDir string `yaml:"music_dir" json:"music_dir"`
	DataDir  string `yaml:"data_dir" json:"data_dir"`
	CacheDir string `yaml:"cache_dir" json:"cache_dir"`
	LogFile  string `yaml:"log_file" json:"log_file"`
	// ConfigFile is the rendered navidrome.toml.
	ConfigFile string `yaml:"config_file" json:"config_file"`
	BackupDir  string `yaml:"backup_dir" json:"backup_dir"`
	// PlaylistsPath is relative to MusicDir, matching Navidrome's own
	// PlaylistsPath semantics.
	PlaylistsPath string `yaml:"playlists_path" json:"playlists_path"`
}

// ScanConfig controls Navidrome's own scan scheduling.
type ScanConfig struct {
	Schedule string `yaml:"schedule" json:"schedule"`
}

// BackupConfig controls scheduled database backups.
type BackupConfig struct {
	Schedule string `yaml:"schedule" json:"schedule"`
	Count    int    `yaml:"count" json:"count"`
}

// PlaylistsConfig holds the derived, explicitly approved smart playlist inputs.
type PlaylistsConfig struct {
	HardBounceGenres        []string `yaml:"hard_bounce_genres" json:"hard_bounce_genres"`
	AppleHardBouncePlaylist string   `yaml:"apple_hard_bounce_playlist" json:"apple_hard_bounce_playlist"`
}

// PhoneConfig records the one setup step that happens on another device.
//
// Nothing on this Mac can observe that Amperfy was added to the phone: the
// Subsonic API exposes no client registry UDL reads, so this is the user's own
// acknowledgement, stored so the checklist can complete and stay complete.
type PhoneConfig struct {
	Connected bool `yaml:"connected" json:"connected"`
}

// LoadOptions selects which configuration files participate in discovery.
type LoadOptions struct {
	ExplicitPath string
	WorkingDir   string
	Env          map[string]string
}

// DefaultConfig returns the managed defaults from PLAN.md.
func DefaultConfig() Config {
	dataDir := defaultDataDir()
	return Config{
		Version: ConfigVersion,
		Enabled: false,
		Server: ServerConfig{
			URL:      fmt.Sprintf("http://localhost:%d", DefaultPort),
			Username: "",
			Port:     DefaultPort,
			Address:  "0.0.0.0",
		},
		Paths: PathsConfig{
			MusicDir:      "~/Music/downloaded",
			DataDir:       dataDir,
			CacheDir:      filepath.Join(dataDir, "cache"),
			LogFile:       filepath.Join(dataDir, "navidrome.log"),
			ConfigFile:    filepath.Join(dataDir, "navidrome.toml"),
			BackupDir:     filepath.Join(dataDir, "backups"),
			PlaylistsPath: DefaultPlaylistsPath,
		},
		Scan:   ScanConfig{Schedule: DefaultScanSchedule},
		Backup: BackupConfig{Schedule: DefaultBackupSchedule, Count: DefaultBackupCount},
		Playlists: PlaylistsConfig{
			HardBounceGenres:        nil,
			AppleHardBouncePlaylist: DefaultAppleHardBouncePlaylist,
		},
	}
}

func defaultDataDir() string {
	return "~/Library/Application Support/UDL/Navidrome"
}

// Load discovers and merges navidrome.yaml. Missing files are not an error;
// the managed defaults stand in.
func Load(opts LoadOptions) (Config, error) {
	cfg := DefaultConfig()
	env := opts.Env
	if env == nil {
		env = osEnvMap()
	}
	if explicit := firstNonEmpty(opts.ExplicitPath, env[EnvConfigPath]); explicit != "" {
		if err := mergeFile(&cfg, explicit, true); err != nil {
			return Config{}, err
		}
		normalize(&cfg)
		return cfg, nil
	}
	userPath, err := UserConfigPath()
	if err != nil {
		return Config{}, err
	}
	if err := mergeFile(&cfg, userPath, false); err != nil {
		return Config{}, err
	}
	wd := strings.TrimSpace(opts.WorkingDir)
	if wd == "" {
		wd, _ = os.Getwd()
	}
	if wd != "" {
		if err := mergeFile(&cfg, ProjectConfigPath(wd), false); err != nil {
			return Config{}, err
		}
	}
	normalize(&cfg)
	return cfg, nil
}

// UserConfigPath is the user-level navidrome.yaml location.
func UserConfigPath() (string, error) {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "udl", "navidrome.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "udl", "navidrome.yaml"), nil
}

// ProjectConfigPath is the project-level navidrome config path.
func ProjectConfigPath(cwd string) string {
	return filepath.Join(cwd, ProjectConfigName)
}

// ResolveWritePath picks where a save should land: an explicit path, an
// existing project file, else the user config.
func ResolveWritePath(explicitPath, workingDir string) (string, error) {
	if explicit := strings.TrimSpace(explicitPath); explicit != "" {
		return config.ExpandPath(explicit)
	}
	wd := strings.TrimSpace(workingDir)
	if wd == "" {
		var err error
		wd, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve working directory: %w", err)
		}
	}
	projectPath := ProjectConfigPath(wd)
	if info, err := os.Stat(projectPath); err == nil && info.Mode().IsRegular() {
		return projectPath, nil
	}
	return UserConfigPath()
}

// Save validates and atomically writes the configuration.
func Save(path string, cfg Config) error {
	normalize(&cfg)
	if err := Validate(cfg); err != nil {
		return err
	}
	payload, err := Marshal(cfg)
	if err != nil {
		return err
	}
	return AtomicWrite(path, payload, 0o600)
}

// Marshal renders the canonical YAML form.
func Marshal(cfg Config) ([]byte, error) {
	normalize(&cfg)
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(cfg); err != nil {
		return nil, fmt.Errorf("encode navidrome config: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("encode navidrome config: %w", err)
	}
	return buf.Bytes(), nil
}

// Validate reports every configuration problem at once.
func Validate(cfg Config) error {
	problems := []string{}
	if cfg.Version != ConfigVersion {
		problems = append(problems, fmt.Sprintf("navidrome.version must be %d", ConfigVersion))
	}
	if runtime.GOOS != "darwin" {
		problems = append(problems, "navidrome integration is supported on macOS only")
	}
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		problems = append(problems, "navidrome.server.port must be between 1 and 65535")
	}
	if strings.TrimSpace(cfg.Server.Address) == "" {
		problems = append(problems, "navidrome.server.address must be set")
	}
	if url := strings.TrimSpace(cfg.Server.URL); url == "" {
		problems = append(problems, "navidrome.server.url must be set")
	} else if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		problems = append(problems, "navidrome.server.url must start with http:// or https://")
	}
	if strings.TrimSpace(cfg.Paths.PlaylistsPath) == "" {
		problems = append(problems, "navidrome.paths.playlists_path must be set")
	} else if filepath.IsAbs(cfg.Paths.PlaylistsPath) {
		problems = append(problems, "navidrome.paths.playlists_path must be relative to the music directory")
	} else if strings.HasPrefix(filepath.Clean(cfg.Paths.PlaylistsPath), "..") {
		problems = append(problems, "navidrome.paths.playlists_path must stay inside the music directory")
	} else if hidden := hiddenSegment(cfg.Paths.PlaylistsPath); hidden != "" {
		problems = append(problems, fmt.Sprintf(
			"navidrome.paths.playlists_path segment %q is hidden; Navidrome skips hidden folders and would never import the managed playlists",
			hidden))
	}
	if cfg.Backup.Count < 0 {
		problems = append(problems, "navidrome.backup.count must be >= 0")
	}

	resolved, resolveErr := Resolve(cfg)
	if resolveErr != nil {
		problems = append(problems, resolveErr.Error())
	} else {
		named := []struct {
			label string
			value string
		}{
			{"music_dir", resolved.MusicDir},
			{"data_dir", resolved.DataDir},
			{"cache_dir", resolved.CacheDir},
			{"log_file", resolved.LogFile},
			{"config_file", resolved.ConfigFile},
			{"backup_dir", resolved.BackupDir},
		}
		for _, item := range named {
			if item.value == "" {
				problems = append(problems, fmt.Sprintf("navidrome.paths.%s must be set", item.label))
				continue
			}
			if !filepath.IsAbs(item.value) {
				problems = append(problems, fmt.Sprintf("navidrome.paths.%s must resolve to an absolute path", item.label))
			}
		}
		// Operational data must never live inside the music library: a scan
		// would index it and a cleanup could touch audio files.
		for _, item := range named[1:] {
			if item.value == "" || resolved.MusicDir == "" {
				continue
			}
			if withinDir(resolved.MusicDir, item.value) {
				problems = append(problems, fmt.Sprintf("navidrome.paths.%s must not live inside music_dir", item.label))
			}
		}
		if resolved.MusicDir != "" && resolved.DataDir != "" && withinDir(resolved.DataDir, resolved.MusicDir) {
			problems = append(problems, "navidrome.paths.music_dir must not live inside data_dir")
		}
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// Resolved holds every managed path after expansion.
type Resolved struct {
	MusicDir      string
	DataDir       string
	CacheDir      string
	LogFile       string
	ConfigFile    string
	BackupDir     string
	PlaylistsDir  string
	PlaylistsPath string
	LaunchAgent   string
}

// Resolve expands every configured path.
func Resolve(cfg Config) (Resolved, error) {
	out := Resolved{PlaylistsPath: filepath.Clean(strings.TrimSpace(cfg.Paths.PlaylistsPath))}
	fields := []struct {
		raw    string
		target *string
	}{
		{cfg.Paths.MusicDir, &out.MusicDir},
		{cfg.Paths.DataDir, &out.DataDir},
		{cfg.Paths.CacheDir, &out.CacheDir},
		{cfg.Paths.LogFile, &out.LogFile},
		{cfg.Paths.ConfigFile, &out.ConfigFile},
		{cfg.Paths.BackupDir, &out.BackupDir},
	}
	for _, field := range fields {
		expanded, err := config.ExpandPath(field.raw)
		if err != nil {
			return Resolved{}, err
		}
		*field.target = expanded
	}
	if out.MusicDir != "" && out.PlaylistsPath != "" && !filepath.IsAbs(out.PlaylistsPath) {
		out.PlaylistsDir = filepath.Join(out.MusicDir, out.PlaylistsPath)
	}
	agent, err := LaunchAgentPath()
	if err != nil {
		return Resolved{}, err
	}
	out.LaunchAgent = agent
	return out, nil
}

// LaunchAgentPath is the UDL-owned LaunchAgent plist location.
func LaunchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", LaunchAgentLabel+".plist"), nil
}

// hiddenSegment returns the first dot-prefixed path segment, or "" when none
// is hidden.
func hiddenSegment(relative string) string {
	for _, segment := range strings.Split(filepath.Clean(relative), string(filepath.Separator)) {
		if strings.HasPrefix(segment, ".") && segment != "." {
			return segment
		}
	}
	return ""
}

func withinDir(dir, candidate string) bool {
	if dir == "" || candidate == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(candidate))
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !strings.HasPrefix(rel, "..")
}

func mergeFile(cfg *Config, path string, required bool) error {
	expanded, err := config.ExpandPath(path)
	if err != nil {
		return err
	}
	payload, err := os.ReadFile(expanded)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !required {
			return nil
		}
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("navidrome config file does not exist: %s", expanded)
		}
		return fmt.Errorf("read navidrome config %s: %w", expanded, err)
	}
	var file fileConfig
	if err := yaml.Unmarshal(payload, &file); err != nil {
		return fmt.Errorf("parse navidrome config %s: %w", expanded, err)
	}
	file.apply(cfg)
	return nil
}

type fileConfig struct {
	Version   *int           `yaml:"version"`
	Enabled   *bool          `yaml:"enabled"`
	Server    *fileServer    `yaml:"server"`
	Paths     *filePaths     `yaml:"paths"`
	Scan      *fileScan      `yaml:"scan"`
	Backup    *fileBackup    `yaml:"backup"`
	Playlists *filePlaylists `yaml:"playlists"`
	Phone     *filePhone     `yaml:"phone"`
}

type fileServer struct {
	URL      *string `yaml:"url"`
	Username *string `yaml:"username"`
	Port     *int    `yaml:"port"`
	Address  *string `yaml:"address"`
}

type filePaths struct {
	MusicDir      *string `yaml:"music_dir"`
	DataDir       *string `yaml:"data_dir"`
	CacheDir      *string `yaml:"cache_dir"`
	LogFile       *string `yaml:"log_file"`
	ConfigFile    *string `yaml:"config_file"`
	BackupDir     *string `yaml:"backup_dir"`
	PlaylistsPath *string `yaml:"playlists_path"`
}

type fileScan struct {
	Schedule *string `yaml:"schedule"`
}

type fileBackup struct {
	Schedule *string `yaml:"schedule"`
	Count    *int    `yaml:"count"`
}

type filePlaylists struct {
	HardBounceGenres        *[]string `yaml:"hard_bounce_genres"`
	AppleHardBouncePlaylist *string   `yaml:"apple_hard_bounce_playlist"`
}

type filePhone struct {
	Connected *bool `yaml:"connected"`
}

func (f fileConfig) apply(cfg *Config) {
	assignInt(f.Version, &cfg.Version)
	assignBool(f.Enabled, &cfg.Enabled)
	if f.Server != nil {
		assignString(f.Server.URL, &cfg.Server.URL)
		assignString(f.Server.Username, &cfg.Server.Username)
		assignInt(f.Server.Port, &cfg.Server.Port)
		assignString(f.Server.Address, &cfg.Server.Address)
	}
	if f.Paths != nil {
		assignString(f.Paths.MusicDir, &cfg.Paths.MusicDir)
		assignString(f.Paths.DataDir, &cfg.Paths.DataDir)
		assignString(f.Paths.CacheDir, &cfg.Paths.CacheDir)
		assignString(f.Paths.LogFile, &cfg.Paths.LogFile)
		assignString(f.Paths.ConfigFile, &cfg.Paths.ConfigFile)
		assignString(f.Paths.BackupDir, &cfg.Paths.BackupDir)
		assignString(f.Paths.PlaylistsPath, &cfg.Paths.PlaylistsPath)
	}
	if f.Scan != nil {
		assignString(f.Scan.Schedule, &cfg.Scan.Schedule)
	}
	if f.Backup != nil {
		assignString(f.Backup.Schedule, &cfg.Backup.Schedule)
		assignInt(f.Backup.Count, &cfg.Backup.Count)
	}
	if f.Playlists != nil {
		if f.Playlists.HardBounceGenres != nil {
			cfg.Playlists.HardBounceGenres = append([]string(nil), *f.Playlists.HardBounceGenres...)
		}
		assignString(f.Playlists.AppleHardBouncePlaylist, &cfg.Playlists.AppleHardBouncePlaylist)
	}
	if f.Phone != nil {
		assignBool(f.Phone.Connected, &cfg.Phone.Connected)
	}
}

func assignString(src *string, dst *string) {
	if src != nil {
		*dst = strings.TrimSpace(*src)
	}
}

func assignInt(src *int, dst *int) {
	if src != nil {
		*dst = *src
	}
}

func assignBool(src *bool, dst *bool) {
	if src != nil {
		*dst = *src
	}
}

func normalize(cfg *Config) {
	defaults := DefaultConfig()
	if cfg.Version == 0 {
		cfg.Version = ConfigVersion
	}
	cfg.Server.URL = strings.TrimRight(firstNonEmpty(cfg.Server.URL, defaults.Server.URL), "/")
	cfg.Server.Username = strings.TrimSpace(cfg.Server.Username)
	if cfg.Server.Port == 0 {
		cfg.Server.Port = defaults.Server.Port
	}
	cfg.Server.Address = firstNonEmpty(cfg.Server.Address, defaults.Server.Address)

	cfg.Paths.MusicDir = firstNonEmpty(cfg.Paths.MusicDir, defaults.Paths.MusicDir)
	cfg.Paths.DataDir = firstNonEmpty(cfg.Paths.DataDir, defaults.Paths.DataDir)
	dataDir := cfg.Paths.DataDir
	cfg.Paths.CacheDir = firstNonEmpty(cfg.Paths.CacheDir, filepath.Join(dataDir, "cache"))
	cfg.Paths.LogFile = firstNonEmpty(cfg.Paths.LogFile, filepath.Join(dataDir, "navidrome.log"))
	cfg.Paths.ConfigFile = firstNonEmpty(cfg.Paths.ConfigFile, filepath.Join(dataDir, "navidrome.toml"))
	cfg.Paths.BackupDir = firstNonEmpty(cfg.Paths.BackupDir, filepath.Join(dataDir, "backups"))
	cfg.Paths.PlaylistsPath = firstNonEmpty(cfg.Paths.PlaylistsPath, defaults.Paths.PlaylistsPath)

	cfg.Scan.Schedule = firstNonEmpty(cfg.Scan.Schedule, defaults.Scan.Schedule)
	cfg.Backup.Schedule = firstNonEmpty(cfg.Backup.Schedule, defaults.Backup.Schedule)
	if cfg.Backup.Count == 0 {
		cfg.Backup.Count = defaults.Backup.Count
	}
	cfg.Playlists.AppleHardBouncePlaylist = firstNonEmpty(
		cfg.Playlists.AppleHardBouncePlaylist, defaults.Playlists.AppleHardBouncePlaylist)
	cfg.Playlists.HardBounceGenres = NormalizeGenres(cfg.Playlists.HardBounceGenres)
}

// NormalizeGenres trims, drops empties, deduplicates **exactly**, and sorts
// case-insensitively for a stable, readable rule.
//
// The dedup is exact and not case-folded on purpose. Navidrome matches
// `is: {genre: …}` case-sensitively — verified against 0.63.2, where a rule for
// "bounce" returned only the lowercase-tagged track and not the "Bounce" one.
// Folding `Bounce` and `bounce` into a single spelling would therefore drop
// every track carrying the spelling that lost, silently: in the real library
// that was 155 tracks against 1.
func NormalizeGenres(values []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sortStrings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func osEnvMap() map[string]string {
	out := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			out[key] = value
		}
	}
	return out
}
