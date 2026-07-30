package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/freedl"
)

type OnboardingReason string

const (
	OnboardingReasonFirstRun      OnboardingReason = "first_run"
	OnboardingReasonNoSources     OnboardingReason = "no_sources"
	OnboardingReasonInvalidConfig OnboardingReason = "invalid_config"
)

type OnboardingState struct {
	Reason             OnboardingReason `json:"reason"`
	AutoStarted        bool             `json:"auto_started"`
	ConfigPath         string           `json:"config_path"`
	ConfigContextLabel string           `json:"config_context_label"`
	DetailLines        []string         `json:"detail_lines"`
	Defaults           config.Defaults  `json:"defaults"`
}

type OnboardingOptions struct {
	ConfigPath       string
	FreeDLConfigPath string
	WorkingDir       string
	Env              map[string]string
}

func DetectOnboardingState(opts OnboardingOptions) (OnboardingState, bool) {
	workingDir := strings.TrimSpace(opts.WorkingDir)
	if workingDir == "" {
		if wd, err := os.Getwd(); err == nil {
			workingDir = wd
		}
	}
	startup := OnboardingState{
		Reason:             OnboardingReasonFirstRun,
		ConfigContextLabel: configContextLabel(opts.ConfigPath, workingDir),
		Defaults:           config.DefaultConfig().Defaults,
	}
	if path, err := resolveInitConfigPath(opts.ConfigPath); err == nil {
		startup.ConfigPath = path
	}

	cfg, loadErr := config.Load(config.LoadOptions{
		ExplicitPath: strings.TrimSpace(opts.ConfigPath),
		WorkingDir:   workingDir,
		Env:          opts.Env,
	})
	if loadErr != nil {
		startup.Reason, startup.AutoStarted = OnboardingReasonInvalidConfig, true
		startup.DetailLines = splitDetailLines(loadErr.Error())
		if strings.TrimSpace(startup.ConfigPath) == "" {
			startup.ConfigPath = startup.ConfigContextLabel
		}
		return startup, true
	}
	startup.Defaults = cfg.Defaults
	if strings.TrimSpace(startup.ConfigPath) == "" {
		startup.ConfigPath = startup.ConfigContextLabel
	}
	if err := config.Validate(cfg); err != nil {
		if len(cfg.Sources) == 0 && hasAlternateWorkflow(opts, cfg, workingDir) {
			return startup, false
		}
		if len(cfg.Sources) == 0 {
			startup.Reason, startup.AutoStarted = OnboardingReasonNoSources, true
			startup.DetailLines = []string{"No sources are configured yet. The guided setup will create your first one."}
			return startup, true
		}
		startup.Reason, startup.AutoStarted = OnboardingReasonInvalidConfig, true
		startup.DetailLines = splitDetailLines(err.Error())
		return startup, true
	}
	if len(cfg.Sources) == 0 {
		if hasAlternateWorkflow(opts, cfg, workingDir) {
			return startup, false
		}
		startup.Reason, startup.AutoStarted = OnboardingReasonNoSources, true
		startup.DetailLines = []string{"No sources are configured yet. The guided setup will create your first one."}
		return startup, true
	}
	if _, err := os.Stat(startup.ConfigPath); err != nil && strings.TrimSpace(opts.ConfigPath) != "" {
		startup.DetailLines = []string{"The explicit config path does not exist yet, but your runtime config is still resolved elsewhere."}
	}
	return startup, false
}

func hasAlternateWorkflow(opts OnboardingOptions, main config.Config, workingDir string) bool {
	freeDL, err := freedl.Load(freedl.LoadOptions{
		ExplicitPath: opts.FreeDLConfigPath, WorkingDir: workingDir, Env: opts.Env, MainConfig: main,
	})
	if err == nil && len(freedl.EnabledJobs(freeDL)) > 0 {
		return true
	}
	return main.Rekordbox != nil && config.ValidateRekordbox(main) == nil
}

func resolveInitConfigPath(explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return config.ExpandPath(explicit)
	}
	return config.UserConfigPath()
}

func configContextLabel(explicit, workingDir string) string {
	if strings.TrimSpace(explicit) != "" {
		if expanded, err := config.ExpandPath(explicit); err == nil && expanded != "" {
			return expanded
		}
		return strings.TrimSpace(explicit)
	}
	if workingDir == "" {
		return "user config + project udl.yaml"
	}
	userPath, err := config.UserConfigPath()
	if err != nil {
		return config.ProjectConfigPath(workingDir)
	}
	return fmt.Sprintf("%s + %s", userPath, config.ProjectConfigPath(workingDir))
}

func splitDetailLines(text string) []string {
	lines := []string{}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
