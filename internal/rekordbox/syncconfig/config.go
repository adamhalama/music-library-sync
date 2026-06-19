package syncconfig

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jaa/update-downloads/internal/config"
	"gopkg.in/yaml.v3"
)

const (
	Version              = 1
	EnvConfigPath        = "UDL_REKORDBOX_CONFIG"
	ProjectConfigName    = "udl.rekordbox.yaml"
	DefaultMissingTracks = "fail"
)

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

type LoadOptions struct {
	ExplicitPath string
	WorkingDir   string
	Env          map[string]string
	BaseConfig   config.Config
}

type Config struct {
	Version  int
	Defaults Defaults
	Sync     Sync
	Warnings []string
}

type Defaults struct {
	DBDir           string
	PythonBin       string
	PythonPath      string
	BackupDir       string
	Mode            string
	CreateFolders   bool
	CreatePlaylists bool
}

type Sync struct {
	Folders []FolderMapping
	Jobs    []PlaylistJob
}

type FolderMapping struct {
	ID                string
	MusicFolder       string
	MusicFolderID     string
	RekordboxFolder   string
	RekordboxFolderID string
	PlaylistNameMap   map[string]string
	IncludePlaylists  []string
	ExcludePlaylists  []string
	OnMissingTracks   string
	CreateFolders     *bool
	CreatePlaylists   *bool
}

type PlaylistJob struct {
	ID                  string
	MusicPlaylist       string
	MusicPlaylistID     string
	RekordboxPlaylist   string
	RekordboxPlaylistID string
	Mode                string
	CreatePlaylist      *bool
}

type fileConfig struct {
	Version  *int         `yaml:"version"`
	Defaults fileDefaults `yaml:"defaults"`
	Sync     fileSync     `yaml:"sync"`
}

type fileDefaults struct {
	DBDir           *string `yaml:"db_dir"`
	PythonBin       *string `yaml:"python_bin"`
	PythonPath      *string `yaml:"python_path"`
	BackupDir       *string `yaml:"backup_dir"`
	Mode            *string `yaml:"mode"`
	CreateFolders   *bool   `yaml:"create_folders"`
	CreatePlaylists *bool   `yaml:"create_playlists"`
}

type fileSync struct {
	Folders *[]fileFolderMapping `yaml:"folders"`
	Jobs    *[]filePlaylistJob   `yaml:"jobs"`
}

type fileFolderMapping struct {
	ID                string            `yaml:"id"`
	MusicFolder       string            `yaml:"music_folder"`
	MusicFolderID     string            `yaml:"music_folder_id"`
	RekordboxFolder   string            `yaml:"rekordbox_folder"`
	RekordboxFolderID string            `yaml:"rekordbox_folder_id"`
	PlaylistNameMap   map[string]string `yaml:"playlist_name_map"`
	IncludePlaylists  []string          `yaml:"include_playlists"`
	ExcludePlaylists  []string          `yaml:"exclude_playlists"`
	OnMissingTracks   string            `yaml:"on_missing_tracks"`
	CreateFolders     *bool             `yaml:"create_folders"`
	CreatePlaylists   *bool             `yaml:"create_playlists"`
}

type filePlaylistJob struct {
	ID                  string `yaml:"id"`
	MusicPlaylist       string `yaml:"music_playlist"`
	MusicPlaylistID     string `yaml:"music_playlist_id"`
	RekordboxPlaylist   string `yaml:"rekordbox_playlist"`
	RekordboxPlaylistID string `yaml:"rekordbox_playlist_id"`
	Mode                string `yaml:"mode"`
	CreatePlaylist      *bool  `yaml:"create_playlist"`
}

func Load(opts LoadOptions) (Config, error) {
	cfg := defaultConfig(opts.BaseConfig)
	cwd := strings.TrimSpace(opts.WorkingDir)
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return Config{}, fmt.Errorf("resolve working directory: %w", err)
		}
		cwd = wd
	}
	env := opts.Env
	if env == nil {
		env = osEnvMap()
	}

	explicit := firstNonEmpty(strings.TrimSpace(opts.ExplicitPath), strings.TrimSpace(env[EnvConfigPath]))
	if explicit != "" {
		if err := mergeFile(&cfg, explicit, true); err != nil {
			return Config{}, err
		}
	} else {
		userPath, err := UserConfigPath()
		if err != nil {
			return Config{}, err
		}
		if err := mergeFile(&cfg, userPath, false); err != nil {
			return Config{}, err
		}
		if err := mergeFile(&cfg, ProjectConfigPath(cwd), false); err != nil {
			return Config{}, err
		}
	}
	applyLegacyInline(&cfg, opts.BaseConfig)
	normalize(&cfg)
	if err := Validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func UserConfigPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); strings.TrimSpace(xdg) != "" {
		return filepath.Join(xdg, "udl", "rekordbox.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "udl", "rekordbox.yaml"), nil
}

func ProjectConfigPath(cwd string) string {
	return filepath.Join(cwd, ProjectConfigName)
}

func Validate(cfg Config) error {
	problems := []string{}
	if cfg.Version != Version {
		problems = append(problems, "version must be 1")
	}
	if cfg.Defaults.Mode != "" && cfg.Defaults.Mode != "mirror" {
		problems = append(problems, fmt.Sprintf("defaults.mode %q is unsupported", cfg.Defaults.Mode))
	}
	seen := map[string]struct{}{}
	for _, mapping := range cfg.Sync.Folders {
		if strings.TrimSpace(mapping.ID) == "" {
			problems = append(problems, "sync.folders[].id must not be empty")
		} else {
			if !idPattern.MatchString(mapping.ID) {
				problems = append(problems, fmt.Sprintf("folder mapping %q has invalid id format", mapping.ID))
			}
			if _, exists := seen[mapping.ID]; exists {
				problems = append(problems, fmt.Sprintf("duplicate Rekordbox sync mapping id %q", mapping.ID))
			}
			seen[mapping.ID] = struct{}{}
		}
		if strings.TrimSpace(mapping.MusicFolder) == "" && strings.TrimSpace(mapping.MusicFolderID) == "" {
			problems = append(problems, fmt.Sprintf("folder mapping %q must set music_folder or music_folder_id", mapping.ID))
		}
		if strings.TrimSpace(mapping.RekordboxFolder) == "" && strings.TrimSpace(mapping.RekordboxFolderID) == "" {
			problems = append(problems, fmt.Sprintf("folder mapping %q must set rekordbox_folder or rekordbox_folder_id", mapping.ID))
		}
		if mapping.OnMissingTracks != "" && mapping.OnMissingTracks != DefaultMissingTracks {
			problems = append(problems, fmt.Sprintf("folder mapping %q has unsupported on_missing_tracks %q", mapping.ID, mapping.OnMissingTracks))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid Rekordbox sync config: %s", strings.Join(problems, "; "))
	}
	return nil
}

func (c Config) FolderMapping(id string) (FolderMapping, bool) {
	if strings.TrimSpace(id) == "" {
		if len(c.Sync.Folders) == 1 {
			return c.Sync.Folders[0], true
		}
		return FolderMapping{}, false
	}
	for _, mapping := range c.Sync.Folders {
		if mapping.ID == id {
			return mapping, true
		}
	}
	return FolderMapping{}, false
}

func (c Config) HasFolderMappings() bool {
	return len(c.Sync.Folders) > 0
}

func mergeFile(cfg *Config, path string, required bool) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !required {
			return nil
		}
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("Rekordbox config file does not exist: %s", path)
		}
		return fmt.Errorf("read Rekordbox config file %s: %w", path, err)
	}
	var root yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(&root); err != nil {
		return fmt.Errorf("parse Rekordbox config file %s: %w", path, err)
	}
	var fc fileConfig
	if err := root.Decode(&fc); err != nil {
		return fmt.Errorf("parse Rekordbox config file %s: %w", path, err)
	}
	applyFileConfig(cfg, fc)
	return nil
}

func applyFileConfig(cfg *Config, fc fileConfig) {
	if fc.Version != nil {
		cfg.Version = *fc.Version
	}
	if fc.Defaults.DBDir != nil {
		cfg.Defaults.DBDir = strings.TrimSpace(*fc.Defaults.DBDir)
	}
	if fc.Defaults.PythonBin != nil {
		cfg.Defaults.PythonBin = strings.TrimSpace(*fc.Defaults.PythonBin)
	}
	if fc.Defaults.PythonPath != nil {
		cfg.Defaults.PythonPath = strings.TrimSpace(*fc.Defaults.PythonPath)
	}
	if fc.Defaults.BackupDir != nil {
		cfg.Defaults.BackupDir = strings.TrimSpace(*fc.Defaults.BackupDir)
	}
	if fc.Defaults.Mode != nil {
		cfg.Defaults.Mode = strings.TrimSpace(*fc.Defaults.Mode)
	}
	if fc.Defaults.CreateFolders != nil {
		cfg.Defaults.CreateFolders = *fc.Defaults.CreateFolders
	}
	if fc.Defaults.CreatePlaylists != nil {
		cfg.Defaults.CreatePlaylists = *fc.Defaults.CreatePlaylists
	}
	if fc.Sync.Folders != nil {
		cfg.Sync.Folders = make([]FolderMapping, 0, len(*fc.Sync.Folders))
		for _, fm := range *fc.Sync.Folders {
			cfg.Sync.Folders = append(cfg.Sync.Folders, FolderMapping{
				ID:                strings.TrimSpace(fm.ID),
				MusicFolder:       strings.TrimSpace(fm.MusicFolder),
				MusicFolderID:     strings.TrimSpace(fm.MusicFolderID),
				RekordboxFolder:   strings.TrimSpace(fm.RekordboxFolder),
				RekordboxFolderID: strings.TrimSpace(fm.RekordboxFolderID),
				PlaylistNameMap:   copyStringMap(fm.PlaylistNameMap),
				IncludePlaylists:  trimStringSlice(fm.IncludePlaylists),
				ExcludePlaylists:  trimStringSlice(fm.ExcludePlaylists),
				OnMissingTracks:   strings.TrimSpace(fm.OnMissingTracks),
				CreateFolders:     copyBoolPtr(fm.CreateFolders),
				CreatePlaylists:   copyBoolPtr(fm.CreatePlaylists),
			})
		}
	}
	if fc.Sync.Jobs != nil {
		cfg.Sync.Jobs = make([]PlaylistJob, 0, len(*fc.Sync.Jobs))
		for _, fj := range *fc.Sync.Jobs {
			cfg.Sync.Jobs = append(cfg.Sync.Jobs, PlaylistJob{
				ID:                  strings.TrimSpace(fj.ID),
				MusicPlaylist:       strings.TrimSpace(fj.MusicPlaylist),
				MusicPlaylistID:     strings.TrimSpace(fj.MusicPlaylistID),
				RekordboxPlaylist:   strings.TrimSpace(fj.RekordboxPlaylist),
				RekordboxPlaylistID: strings.TrimSpace(fj.RekordboxPlaylistID),
				Mode:                strings.TrimSpace(fj.Mode),
				CreatePlaylist:      copyBoolPtr(fj.CreatePlaylist),
			})
		}
	}
}

func defaultConfig(base config.Config) Config {
	cfg := Config{
		Version: Version,
		Defaults: Defaults{
			DBDir:           "~/Library/Pioneer/rekordbox",
			BackupDir:       "/Users/jaa/Music/rb-library-export",
			Mode:            "mirror",
			CreateFolders:   true,
			CreatePlaylists: true,
		},
	}
	if rb := base.Rekordbox; rb != nil {
		if strings.TrimSpace(rb.DBDir) != "" {
			cfg.Defaults.DBDir = rb.DBDir
		}
		if strings.TrimSpace(rb.PythonBin) != "" {
			cfg.Defaults.PythonBin = rb.PythonBin
		}
		if strings.TrimSpace(rb.PythonPath) != "" {
			cfg.Defaults.PythonPath = rb.PythonPath
		}
		if strings.TrimSpace(rb.BackupDir) != "" {
			cfg.Defaults.BackupDir = rb.BackupDir
		}
	}
	return cfg
}

func applyLegacyInline(cfg *Config, base config.Config) {
	if len(cfg.Sync.Folders) > 0 || len(cfg.Sync.Jobs) > 0 || base.Rekordbox == nil {
		return
	}
	for _, job := range base.Rekordbox.PlaylistSync.Jobs {
		cfg.Sync.Jobs = append(cfg.Sync.Jobs, PlaylistJob{
			ID:                  job.ID,
			MusicPlaylist:       job.MusicPlaylist,
			MusicPlaylistID:     job.MusicPlaylistID,
			RekordboxPlaylist:   job.RekordboxPlaylist,
			RekordboxPlaylistID: job.RekordboxPlaylistID,
			Mode:                job.Mode,
			CreatePlaylist:      copyBoolPtr(job.CreatePlaylist),
		})
	}
	if len(cfg.Sync.Jobs) > 0 {
		cfg.Warnings = append(cfg.Warnings, "legacy inline rekordbox.playlist_sync.jobs is supported; move mappings to rekordbox.yaml")
	}
}

func normalize(cfg *Config) {
	if cfg.Defaults.Mode == "" {
		cfg.Defaults.Mode = "mirror"
	}
	for i := range cfg.Sync.Folders {
		if cfg.Sync.Folders[i].OnMissingTracks == "" {
			cfg.Sync.Folders[i].OnMissingTracks = DefaultMissingTracks
		}
	}
}

func osEnvMap() map[string]string {
	result := map[string]string{}
	for _, pair := range os.Environ() {
		pieces := strings.SplitN(pair, "=", 2)
		if len(pieces) == 2 {
			result[pieces[0]] = pieces[1]
		}
	}
	return result
}

func trimStringSlice(values []string) []string {
	result := []string{}
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	result := map[string]string{}
	for key, value := range input {
		result[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return result
}

func copyBoolPtr(in *bool) *bool {
	if in == nil {
		return nil
	}
	value := *in
	return &value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
