package playlists

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
	ConfigVersion      = 1
	ProviderAppleMusic = "apple_music"
	EnvConfigPath      = "UDL_PLAYLISTS_CONFIG"
	ProjectConfigName  = "udl.playlists.yaml"
)

var definitionIDPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

type Config struct {
	Version   int          `yaml:"version" json:"version"`
	Playlists []Definition `yaml:"playlists" json:"playlists"`
}

type Definition struct {
	ID                     string `yaml:"id" json:"id"`
	Name                   string `yaml:"name" json:"name"`
	Provider               string `yaml:"provider" json:"provider"`
	ProviderPlaylist       string `yaml:"provider_playlist,omitempty" json:"provider_playlist,omitempty"`
	ProviderPlaylistID     string `yaml:"provider_playlist_id,omitempty" json:"provider_playlist_id,omitempty"`
	DefaultFreeDLJob       string `yaml:"default_freedl_job,omitempty" json:"default_freedl_job,omitempty"`
	DefaultRekordboxTarget string `yaml:"default_rekordbox_target,omitempty" json:"default_rekordbox_target,omitempty"`
}

type LoadOptions struct {
	ExplicitPath string
	WorkingDir   string
	Env          map[string]string
}

func Load(opts LoadOptions) (Config, error) {
	cfg := Config{Version: ConfigVersion}
	env := opts.Env
	if env == nil {
		env = osEnvMap()
	}
	if explicit := firstNonEmpty(strings.TrimSpace(opts.ExplicitPath), strings.TrimSpace(env[EnvConfigPath])); explicit != "" {
		if err := mergeFile(&cfg, explicit, true); err != nil {
			return Config{}, err
		}
		normalizeConfig(&cfg)
		return cfg, Validate(cfg)
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
		wd, err = os.Getwd()
		if err != nil {
			return Config{}, fmt.Errorf("resolve working directory: %w", err)
		}
	}
	if err := mergeFile(&cfg, ProjectConfigPath(wd), false); err != nil {
		return Config{}, err
	}
	normalizeConfig(&cfg)
	return cfg, Validate(cfg)
}

func Validate(cfg Config) error {
	problems := []string{}
	if cfg.Version != ConfigVersion {
		problems = append(problems, fmt.Sprintf("playlists.version must be %d", ConfigVersion))
	}
	seen := map[string]struct{}{}
	for _, playlist := range cfg.Playlists {
		if playlist.ID == "" {
			problems = append(problems, "playlists[].id must not be empty")
		} else if !definitionIDPattern.MatchString(playlist.ID) {
			problems = append(problems, fmt.Sprintf("playlist %q has invalid id format", playlist.ID))
		} else if _, exists := seen[playlist.ID]; exists {
			problems = append(problems, fmt.Sprintf("duplicate playlist id %q", playlist.ID))
		}
		seen[playlist.ID] = struct{}{}
		if playlist.Name == "" {
			problems = append(problems, fmt.Sprintf("playlist %q name must not be empty", playlist.ID))
		}
		if playlist.Provider != ProviderAppleMusic {
			problems = append(problems, fmt.Sprintf("playlist %q provider %q is unsupported", playlist.ID, playlist.Provider))
		}
		if playlist.ProviderPlaylist == "" && playlist.ProviderPlaylistID == "" {
			problems = append(problems, fmt.Sprintf("playlist %q must set provider_playlist or provider_playlist_id", playlist.ID))
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func (c Config) Definition(id string) (Definition, bool) {
	for _, definition := range c.Playlists {
		if definition.ID == id {
			return definition, true
		}
	}
	return Definition{}, false
}

func UserConfigPath() (string, error) {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "udl", "playlists.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "udl", "playlists.yaml"), nil
}

func ProjectConfigPath(cwd string) string {
	return filepath.Join(cwd, ProjectConfigName)
}

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

func Save(path string, cfg Config) error {
	normalizeConfig(&cfg)
	if err := Validate(cfg); err != nil {
		return err
	}
	payload, err := Marshal(cfg)
	if err != nil {
		return err
	}
	return atomicWrite(path, payload, 0o644)
}

func Marshal(cfg Config) ([]byte, error) {
	normalizeConfig(&cfg)
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(cfg); err != nil {
		return nil, fmt.Errorf("encode playlists config: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("encode playlists config: %w", err)
	}
	return buf.Bytes(), nil
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
			return fmt.Errorf("playlists config file does not exist: %s", expanded)
		}
		return fmt.Errorf("read playlists config %s: %w", expanded, err)
	}
	var file Config
	if err := yaml.Unmarshal(payload, &file); err != nil {
		return fmt.Errorf("parse playlists config %s: %w", expanded, err)
	}
	if file.Version != 0 {
		cfg.Version = file.Version
	}
	if file.Playlists != nil {
		cfg.Playlists = append([]Definition(nil), file.Playlists...)
	}
	return nil
}

func normalizeConfig(cfg *Config) {
	if cfg.Version == 0 {
		cfg.Version = ConfigVersion
	}
	for idx := range cfg.Playlists {
		item := &cfg.Playlists[idx]
		item.ID = strings.TrimSpace(item.ID)
		item.Name = strings.TrimSpace(item.Name)
		item.Provider = strings.ToLower(strings.TrimSpace(item.Provider))
		item.ProviderPlaylist = strings.TrimSpace(item.ProviderPlaylist)
		item.ProviderPlaylistID = strings.TrimSpace(item.ProviderPlaylistID)
		item.DefaultFreeDLJob = strings.TrimSpace(item.DefaultFreeDLJob)
		item.DefaultRekordboxTarget = strings.TrimSpace(item.DefaultRekordboxTarget)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
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
