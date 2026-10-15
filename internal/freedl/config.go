package freedl

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaa/update-downloads/internal/config"
	"gopkg.in/yaml.v3"
)

const (
	DefaultPlanLimit     = 50
	DefaultTargetFormat  = "auto"
	DefaultDownloadOrder = "oldest_first"
	DefaultMinMatchScore = 72
	DefaultAmbiguityGap  = 8
)

type Config struct {
	Version  int      `yaml:"version" json:"version"`
	Defaults Defaults `yaml:"defaults" json:"defaults"`
	Jobs     []Job    `yaml:"jobs" json:"jobs"`
}

type Defaults struct {
	PlanLimit         int    `yaml:"plan_limit" json:"plan_limit"`
	DownloadOrder     string `yaml:"download_order" json:"download_order"`
	TargetFormat      string `yaml:"target_format" json:"target_format"`
	MinMatchScore     int    `yaml:"min_match_score" json:"min_match_score"`
	AmbiguityGap      int    `yaml:"ambiguity_gap" json:"ambiguity_gap"`
	ReplaceLimit      int    `yaml:"replace_limit" json:"replace_limit"`
	CommandTimeoutSec int    `yaml:"command_timeout_seconds" json:"command_timeout_seconds"`
}

type Job struct {
	ID              string `yaml:"id" json:"id"`
	Enabled         bool   `yaml:"enabled" json:"enabled"`
	SourceURL       string `yaml:"source_url" json:"source_url"`
	LibraryDir      string `yaml:"library_dir" json:"library_dir"`
	BufferDir       string `yaml:"buffer_dir" json:"buffer_dir"`
	BackupDir       string `yaml:"backup_dir" json:"backup_dir"`
	LogDir          string `yaml:"log_dir" json:"log_dir"`
	StateFile       string `yaml:"state_file" json:"state_file"`
	PlanLimit       int    `yaml:"plan_limit" json:"plan_limit"`
	DownloadOrder   string `yaml:"download_order" json:"download_order"`
	TargetFormat    string `yaml:"target_format" json:"target_format"`
	MinMatchScore   int    `yaml:"min_match_score" json:"min_match_score"`
	AmbiguityGap    int    `yaml:"ambiguity_gap" json:"ambiguity_gap"`
	ReplaceLimit    int    `yaml:"replace_limit" json:"replace_limit"`
	ApplyPromotions bool   `yaml:"apply_promotions" json:"apply_promotions"`
}

type LoadOptions struct {
	ExplicitPath string
	WorkingDir   string
	Env          map[string]string
	MainConfig   config.Config
}

type fileConfig struct {
	Version  *int         `yaml:"version"`
	Defaults fileDefaults `yaml:"defaults"`
	Jobs     *[]fileJob   `yaml:"jobs"`
}

type fileDefaults struct {
	PlanLimit         *int   `yaml:"plan_limit"`
	DownloadOrder     string `yaml:"download_order"`
	TargetFormat      string `yaml:"target_format"`
	MinMatchScore     *int   `yaml:"min_match_score"`
	AmbiguityGap      *int   `yaml:"ambiguity_gap"`
	ReplaceLimit      *int   `yaml:"replace_limit"`
	CommandTimeoutSec *int   `yaml:"command_timeout_seconds"`
}

type fileJob struct {
	ID              string `yaml:"id"`
	Enabled         *bool  `yaml:"enabled"`
	SourceURL       string `yaml:"source_url"`
	LibraryDir      string `yaml:"library_dir"`
	BufferDir       string `yaml:"buffer_dir"`
	BackupDir       string `yaml:"backup_dir"`
	LogDir          string `yaml:"log_dir"`
	StateFile       string `yaml:"state_file"`
	PlanLimit       *int   `yaml:"plan_limit"`
	DownloadOrder   string `yaml:"download_order"`
	TargetFormat    string `yaml:"target_format"`
	MinMatchScore   *int   `yaml:"min_match_score"`
	AmbiguityGap    *int   `yaml:"ambiguity_gap"`
	ReplaceLimit    *int   `yaml:"replace_limit"`
	ApplyPromotions *bool  `yaml:"apply_promotions"`
}

func DefaultConfig(main config.Config) Config {
	stateDir := firstNonEmpty(strings.TrimSpace(main.Defaults.StateDir), config.DefaultStateDir())
	base := Config{
		Version: 1,
		Defaults: Defaults{
			PlanLimit:         DefaultPlanLimit,
			DownloadOrder:     DefaultDownloadOrder,
			TargetFormat:      DefaultTargetFormat,
			MinMatchScore:     DefaultMinMatchScore,
			AmbiguityGap:      DefaultAmbiguityGap,
			ReplaceLimit:      0,
			CommandTimeoutSec: firstPositive(main.Defaults.CommandTimeoutSeconds, 900),
		},
	}
	if stateDir != "" {
		baseDir := filepath.Join(stateDir, "freedl")
		base.Jobs = []Job{{
			ID:            "soundcloud-free-dl",
			Enabled:       false,
			SourceURL:     "https://soundcloud.com/your-user/likes",
			LibraryDir:    "~/Music/downloaded/sc-likes",
			BufferDir:     filepath.Join(baseDir, "buffer"),
			BackupDir:     filepath.Join(baseDir, "backups"),
			LogDir:        filepath.Join(baseDir, "logs"),
			StateFile:     "soundcloud-free-dl.sync.scdl",
			PlanLimit:     base.Defaults.PlanLimit,
			DownloadOrder: base.Defaults.DownloadOrder,
			TargetFormat:  base.Defaults.TargetFormat,
			MinMatchScore: base.Defaults.MinMatchScore,
			AmbiguityGap:  base.Defaults.AmbiguityGap,
		}}
	}
	return base
}

func Load(opts LoadOptions) (Config, error) {
	main := opts.MainConfig
	cfg := DefaultConfig(main)
	cfg.Jobs = nil
	env := opts.Env
	if env == nil {
		env = osEnvMap()
	}
	if explicit := strings.TrimSpace(firstNonEmpty(opts.ExplicitPath, env["UDL_FREEDL_CONFIG"])); explicit != "" {
		if err := mergeFile(&cfg, explicit, true); err != nil {
			return Config{}, err
		}
		normalize(&cfg, main)
		return cfg, nil
	}
	userPath, err := UserConfigPath()
	if err != nil {
		return Config{}, err
	}
	if err := mergeFile(&cfg, userPath, false); err != nil {
		return Config{}, err
	}
	cwd := opts.WorkingDir
	if strings.TrimSpace(cwd) == "" {
		cwd, _ = os.Getwd()
	}
	if strings.TrimSpace(cwd) != "" {
		if err := mergeFile(&cfg, ProjectConfigPath(cwd), false); err != nil {
			return Config{}, err
		}
	}
	if len(cfg.Jobs) == 0 {
		cfg.Jobs = legacyJobs(main)
	}
	normalize(&cfg, main)
	return cfg, nil
}

func UserConfigPath() (string, error) {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "udl", "freedl.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "udl", "freedl.yaml"), nil
}

func ProjectConfigPath(cwd string) string {
	return filepath.Join(cwd, "udl.freedl.yaml")
}

func Validate(cfg Config) error {
	problems := []string{}
	if cfg.Version != 1 {
		problems = append(problems, "freedl.version must be 1")
	}
	seen := map[string]struct{}{}
	for _, job := range cfg.Jobs {
		if strings.TrimSpace(job.ID) == "" {
			problems = append(problems, "freedl.jobs[].id must not be empty")
		}
		if _, ok := seen[job.ID]; ok {
			problems = append(problems, fmt.Sprintf("duplicate freedl job id %q", job.ID))
		}
		seen[job.ID] = struct{}{}
		if !job.Enabled {
			continue
		}
		for label, value := range map[string]string{
			"source_url":  job.SourceURL,
			"library_dir": job.LibraryDir,
			"buffer_dir":  job.BufferDir,
			"backup_dir":  job.BackupDir,
			"log_dir":     job.LogDir,
			"state_file":  job.StateFile,
		} {
			if strings.TrimSpace(value) == "" {
				problems = append(problems, fmt.Sprintf("freedl job %q %s must be set", job.ID, label))
			}
		}
		if job.PlanLimit < 0 {
			problems = append(problems, fmt.Sprintf("freedl job %q plan_limit must be >= 0", job.ID))
		}
		if !validDownloadOrder(job.DownloadOrder) {
			problems = append(problems, fmt.Sprintf("freedl job %q download_order must be newest_first or oldest_first", job.ID))
		}
		if !validTargetFormat(job.TargetFormat) {
			problems = append(problems, fmt.Sprintf("freedl job %q target_format must be auto, wav, mp3-320, or aac-256", job.ID))
		}
		if job.MinMatchScore < 0 || job.MinMatchScore > 100 {
			problems = append(problems, fmt.Sprintf("freedl job %q min_match_score must be between 0 and 100", job.ID))
		}
		if job.AmbiguityGap < 0 {
			problems = append(problems, fmt.Sprintf("freedl job %q ambiguity_gap must be >= 0", job.ID))
		}
		if job.ReplaceLimit < 0 {
			problems = append(problems, fmt.Sprintf("freedl job %q replace_limit must be >= 0", job.ID))
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func EnabledJobs(cfg Config) []Job {
	jobs := []Job{}
	for _, job := range cfg.Jobs {
		if job.Enabled {
			jobs = append(jobs, job)
		}
	}
	return jobs
}

func mergeFile(cfg *Config, path string, required bool) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && !required {
			return nil
		}
		return err
	}
	var root yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(&root); err != nil {
		return fmt.Errorf("parse freedl config %s: %w", path, err)
	}
	var fc fileConfig
	if err := root.Decode(&fc); err != nil {
		return fmt.Errorf("parse freedl config %s: %w", path, err)
	}
	applyFileConfig(cfg, fc)
	return nil
}

func applyFileConfig(cfg *Config, fc fileConfig) {
	if fc.Version != nil {
		cfg.Version = *fc.Version
	}
	if fc.Defaults.PlanLimit != nil {
		cfg.Defaults.PlanLimit = *fc.Defaults.PlanLimit
	}
	if strings.TrimSpace(fc.Defaults.DownloadOrder) != "" {
		cfg.Defaults.DownloadOrder = strings.TrimSpace(fc.Defaults.DownloadOrder)
	}
	if strings.TrimSpace(fc.Defaults.TargetFormat) != "" {
		cfg.Defaults.TargetFormat = strings.TrimSpace(fc.Defaults.TargetFormat)
	}
	if fc.Defaults.MinMatchScore != nil {
		cfg.Defaults.MinMatchScore = *fc.Defaults.MinMatchScore
	}
	if fc.Defaults.AmbiguityGap != nil {
		cfg.Defaults.AmbiguityGap = *fc.Defaults.AmbiguityGap
	}
	if fc.Defaults.ReplaceLimit != nil {
		cfg.Defaults.ReplaceLimit = *fc.Defaults.ReplaceLimit
	}
	if fc.Defaults.CommandTimeoutSec != nil {
		cfg.Defaults.CommandTimeoutSec = *fc.Defaults.CommandTimeoutSec
	}
	if fc.Jobs != nil {
		cfg.Jobs = make([]Job, 0, len(*fc.Jobs))
		for _, fj := range *fc.Jobs {
			enabled := true
			if fj.Enabled != nil {
				enabled = *fj.Enabled
			}
			applyPromotions := false
			if fj.ApplyPromotions != nil {
				applyPromotions = *fj.ApplyPromotions
			}
			cfg.Jobs = append(cfg.Jobs, Job{
				ID:              strings.TrimSpace(fj.ID),
				Enabled:         enabled,
				SourceURL:       strings.TrimSpace(fj.SourceURL),
				LibraryDir:      strings.TrimSpace(fj.LibraryDir),
				BufferDir:       strings.TrimSpace(fj.BufferDir),
				BackupDir:       strings.TrimSpace(fj.BackupDir),
				LogDir:          strings.TrimSpace(fj.LogDir),
				StateFile:       strings.TrimSpace(fj.StateFile),
				PlanLimit:       intOrDefault(fj.PlanLimit, cfg.Defaults.PlanLimit),
				DownloadOrder:   firstNonEmpty(fj.DownloadOrder, cfg.Defaults.DownloadOrder),
				TargetFormat:    firstNonEmpty(fj.TargetFormat, cfg.Defaults.TargetFormat),
				MinMatchScore:   intOrDefault(fj.MinMatchScore, cfg.Defaults.MinMatchScore),
				AmbiguityGap:    intOrDefault(fj.AmbiguityGap, cfg.Defaults.AmbiguityGap),
				ReplaceLimit:    intOrDefault(fj.ReplaceLimit, cfg.Defaults.ReplaceLimit),
				ApplyPromotions: applyPromotions,
			})
		}
	}
}

func normalize(cfg *Config, main config.Config) {
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Defaults.PlanLimit == 0 {
		cfg.Defaults.PlanLimit = DefaultPlanLimit
	}
	if cfg.Defaults.DownloadOrder == "" {
		cfg.Defaults.DownloadOrder = DefaultDownloadOrder
	}
	if cfg.Defaults.TargetFormat == "" {
		cfg.Defaults.TargetFormat = DefaultTargetFormat
	}
	if cfg.Defaults.MinMatchScore == 0 {
		cfg.Defaults.MinMatchScore = DefaultMinMatchScore
	}
	if cfg.Defaults.AmbiguityGap == 0 {
		cfg.Defaults.AmbiguityGap = DefaultAmbiguityGap
	}
	stateDir := firstNonEmpty(strings.TrimSpace(main.Defaults.StateDir), config.DefaultStateDir())
	for i := range cfg.Jobs {
		job := &cfg.Jobs[i]
		if job.PlanLimit == 0 {
			job.PlanLimit = cfg.Defaults.PlanLimit
		}
		job.DownloadOrder = firstNonEmpty(job.DownloadOrder, cfg.Defaults.DownloadOrder)
		job.TargetFormat = firstNonEmpty(job.TargetFormat, cfg.Defaults.TargetFormat)
		if job.MinMatchScore == 0 {
			job.MinMatchScore = cfg.Defaults.MinMatchScore
		}
		if job.AmbiguityGap == 0 {
			job.AmbiguityGap = cfg.Defaults.AmbiguityGap
		}
		base := filepath.Join(stateDir, "freedl", job.ID)
		if strings.TrimSpace(job.BufferDir) == "" {
			job.BufferDir = filepath.Join(base, "buffer")
		}
		if strings.TrimSpace(job.BackupDir) == "" {
			job.BackupDir = filepath.Join(base, "backups")
		}
		if strings.TrimSpace(job.LogDir) == "" {
			job.LogDir = filepath.Join(base, "logs")
		}
		if strings.TrimSpace(job.StateFile) == "" && strings.TrimSpace(job.ID) != "" {
			job.StateFile = job.ID + ".sync.scdl"
		}
	}
}

func legacyJobs(main config.Config) []Job {
	jobs := []Job{}
	for _, source := range main.Sources {
		if source.Type != config.SourceTypeSoundCloud || source.Adapter.Kind != "scdl-freedl" {
			continue
		}
		stateDir := firstNonEmpty(strings.TrimSpace(main.Defaults.StateDir), config.DefaultStateDir())
		base := filepath.Join(stateDir, "freedl", source.ID)
		jobs = append(jobs, Job{
			ID:            source.ID,
			Enabled:       source.Enabled,
			SourceURL:     source.URL,
			LibraryDir:    source.TargetDir,
			BufferDir:     filepath.Join(base, "buffer"),
			BackupDir:     filepath.Join(base, "backups"),
			LogDir:        filepath.Join(base, "logs"),
			StateFile:     source.StateFile,
			PlanLimit:     DefaultPlanLimit,
			DownloadOrder: DefaultDownloadOrder,
			TargetFormat:  DefaultTargetFormat,
			MinMatchScore: DefaultMinMatchScore,
			AmbiguityGap:  DefaultAmbiguityGap,
		})
	}
	return jobs
}

func JobByID(cfg Config, id string) (Job, bool) {
	for _, job := range cfg.Jobs {
		if job.ID == id {
			return job, true
		}
	}
	return Job{}, false
}

func osEnvMap() map[string]string {
	out := map[string]string{}
	for _, pair := range os.Environ() {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			out[parts[0]] = parts[1]
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func intOrDefault(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func validDownloadOrder(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "newest_first", "oldest_first":
		return true
	default:
		return false
	}
}

func validTargetFormat(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case DefaultTargetFormat, TargetWAV, TargetMP3320, TargetAAC256:
		return true
	default:
		return false
	}
}
