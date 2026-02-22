package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type LoadOptions struct {
	ExplicitPath string
	WorkingDir   string
	Env          map[string]string
}

type fileConfig struct {
	Version  *int          `yaml:"version"`
	Defaults fileDefaults  `yaml:"defaults"`
	Sources  *[]fileSource `yaml:"sources"`
}

type fileDefaults struct {
	StateDir              *string `yaml:"state_dir"`
	ArchiveFile           *string `yaml:"archive_file"`
	Threads               *int    `yaml:"threads"`
	ContinueOnError       *bool   `yaml:"continue_on_error"`
	CommandTimeoutSeconds *int    `yaml:"command_timeout_seconds"`
}

type fileSource struct {
	ID        string          `yaml:"id"`
	Type      SourceType      `yaml:"type"`
	Enabled   *bool           `yaml:"enabled"`
	TargetDir string          `yaml:"target_dir"`
	URL       string          `yaml:"url"`
	StateFile string          `yaml:"state_file"`
	Sync      fileSyncPolicy  `yaml:"sync"`
	Adapter   fileAdapterSpec `yaml:"adapter"`
}

type fileSyncPolicy struct {
	BreakOnExisting *bool `yaml:"break_on_existing"`
	AskOnExisting   *bool `yaml:"ask_on_existing"`
}

type fileAdapterSpec struct {
	Kind       string   `yaml:"kind"`
	ExtraArgs  []string `yaml:"extra_args"`
	MinVersion string   `yaml:"min_version"`
}

func Load(opts LoadOptions) (Config, error) {
	cfg := DefaultConfig()

	cwd := opts.WorkingDir
	if strings.TrimSpace(cwd) == "" {
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

	if explicit := strings.TrimSpace(opts.ExplicitPath); explicit != "" {
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

	if err := applyEnvOverrides(&cfg, env); err != nil {
		return Config{}, err
	}

	normalize(&cfg)
	return cfg, nil
}

func mergeFile(cfg *Config, path string, required bool) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !required {
			return nil
		}
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("config file does not exist: %s", path)
		}
		return fmt.Errorf("read config file %s: %w", path, err)
	}

	var fc fileConfig
	if err := yaml.Unmarshal(payload, &fc); err != nil {
		return fmt.Errorf("parse config file %s: %w", path, err)
	}

	if fc.Version != nil {
		cfg.Version = *fc.Version
	}
	if fc.Defaults.StateDir != nil {
		cfg.Defaults.StateDir = strings.TrimSpace(*fc.Defaults.StateDir)
	}
	if fc.Defaults.ArchiveFile != nil {
		cfg.Defaults.ArchiveFile = strings.TrimSpace(*fc.Defaults.ArchiveFile)
	}
	if fc.Defaults.Threads != nil {
		cfg.Defaults.Threads = *fc.Defaults.Threads
	}
	if fc.Defaults.ContinueOnError != nil {
		cfg.Defaults.ContinueOnError = *fc.Defaults.ContinueOnError
	}
	if fc.Defaults.CommandTimeoutSeconds != nil {
		cfg.Defaults.CommandTimeoutSeconds = *fc.Defaults.CommandTimeoutSeconds
	}

	if fc.Sources != nil {
		cfg.Sources = make([]Source, 0, len(*fc.Sources))
		for _, fs := range *fc.Sources {
			enabled := true
			if fs.Enabled != nil {
				enabled = *fs.Enabled
			}

			source := Source{
				ID:        strings.TrimSpace(fs.ID),
				Type:      fs.Type,
				Enabled:   enabled,
				TargetDir: strings.TrimSpace(fs.TargetDir),
				URL:       strings.TrimSpace(fs.URL),
				StateFile: strings.TrimSpace(fs.StateFile),
				Sync: SyncPolicy{
					BreakOnExisting: copyBoolPtr(fs.Sync.BreakOnExisting),
					AskOnExisting:   copyBoolPtr(fs.Sync.AskOnExisting),
				},
				Adapter: AdapterSpec{
					Kind:       strings.TrimSpace(fs.Adapter.Kind),
					ExtraArgs:  append([]string{}, fs.Adapter.ExtraArgs...),
					MinVersion: strings.TrimSpace(fs.Adapter.MinVersion),
				},
			}
			cfg.Sources = append(cfg.Sources, source)
		}
	}

	return nil
}

func applyEnvOverrides(cfg *Config, env map[string]string) error {
	if value := strings.TrimSpace(env["UDL_STATE_DIR"]); value != "" {
		cfg.Defaults.StateDir = value
	}
	if value := strings.TrimSpace(env["UDL_ARCHIVE_FILE"]); value != "" {
		cfg.Defaults.ArchiveFile = value
	}
	if value := strings.TrimSpace(env["UDL_THREADS"]); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid UDL_THREADS value %q: %w", value, err)
		}
		cfg.Defaults.Threads = parsed
	}
	if value := strings.TrimSpace(env["UDL_CONTINUE_ON_ERROR"]); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid UDL_CONTINUE_ON_ERROR value %q: %w", value, err)
		}
		cfg.Defaults.ContinueOnError = parsed
	}
	if value := strings.TrimSpace(env["UDL_COMMAND_TIMEOUT_SECONDS"]); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid UDL_COMMAND_TIMEOUT_SECONDS value %q: %w", value, err)
		}
		cfg.Defaults.CommandTimeoutSeconds = parsed
	}
	return nil
}

func normalize(cfg *Config) {
	for i := range cfg.Sources {
		if strings.TrimSpace(cfg.Sources[i].Adapter.Kind) == "" {
			cfg.Sources[i].Adapter.Kind = defaultAdapterKind(cfg.Sources[i].Type)
		}
		if cfg.Sources[i].Type == SourceTypeSpotify && strings.TrimSpace(cfg.Sources[i].StateFile) == "" && cfg.Sources[i].ID != "" {
			cfg.Sources[i].StateFile = cfg.Sources[i].ID + ".sync.spotify"
		}
		if cfg.Sources[i].Type == SourceTypeSoundCloud && strings.TrimSpace(cfg.Sources[i].StateFile) == "" && cfg.Sources[i].ID != "" {
			cfg.Sources[i].StateFile = cfg.Sources[i].ID + ".sync.scdl"
		}
		if cfg.Sources[i].Type == SourceTypeSoundCloud {
			if cfg.Sources[i].Sync.BreakOnExisting == nil {
				cfg.Sources[i].Sync.BreakOnExisting = boolPtr(true)
			}
			if cfg.Sources[i].Sync.AskOnExisting == nil {
				cfg.Sources[i].Sync.AskOnExisting = boolPtr(false)
			}
		}
	}
}

func defaultAdapterKind(sourceType SourceType) string {
	switch sourceType {
	case SourceTypeSoundCloud:
		return "scdl"
	default:
		return ""
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

func EnsureConfigDir(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config directory %s: %w", dir, err)
	}
	return nil
}

func copyBoolPtr(in *bool) *bool {
	if in == nil {
		return nil
	}
	value := *in
	return &value
}

func boolPtr(v bool) *bool {
	return &v
}
