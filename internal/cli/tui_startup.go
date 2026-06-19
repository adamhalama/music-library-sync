package cli

import (
	"os"
	"strings"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/freedl"
)

func tuiDetectOnboardingState(app *AppContext) (tuiOnboardingStartupState, bool) {
	startup := tuiOnboardingStartupState{
		Reason:             tuiOnboardingReasonFirstRun,
		ConfigContextLabel: tuiValidateConfigContextLabel(app),
		Defaults:           config.DefaultConfig().Defaults,
	}
	configPath, err := tuiResolveInitConfigPath(app)
	if err == nil {
		startup.ConfigPath = configPath
	}

	cfg, loadErr := loadConfig(app)
	if loadErr != nil {
		startup.Reason = tuiOnboardingReasonInvalidConfig
		startup.AutoStarted = true
		startup.DetailLines = append(startup.DetailLines, tuiSplitDetailLines(loadErr.Error())...)
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
		if len(cfg.Sources) == 0 {
			if tuiHasEnabledFreeDLConfig(app, cfg) {
				return startup, false
			}
			startup.Reason = tuiOnboardingReasonNoSources
			startup.AutoStarted = true
			startup.DetailLines = []string{"No sources are configured yet. The guided setup will create your first one."}
			return startup, true
		}
		startup.Reason = tuiOnboardingReasonInvalidConfig
		startup.AutoStarted = true
		startup.DetailLines = append(startup.DetailLines, tuiSplitDetailLines(err.Error())...)
		return startup, true
	}
	if len(cfg.Sources) == 0 {
		if tuiHasEnabledFreeDLConfig(app, cfg) {
			return startup, false
		}
		startup.Reason = tuiOnboardingReasonNoSources
		startup.AutoStarted = true
		startup.DetailLines = []string{"No sources are configured yet. The guided setup will create your first one."}
		return startup, true
	}
	if _, statErr := os.Stat(startup.ConfigPath); statErr != nil && app != nil && strings.TrimSpace(app.Opts.ConfigPath) != "" {
		startup.DetailLines = []string{"The explicit config path does not exist yet, but your runtime config is still resolved elsewhere."}
	}
	return startup, false
}

func tuiHasEnabledFreeDLConfig(app *AppContext, main config.Config) bool {
	if app == nil {
		return false
	}
	cfg, err := freedl.Load(freedl.LoadOptions{
		ExplicitPath: app.Opts.FreeDLConfigPath,
		MainConfig:   main,
	})
	if err != nil {
		return false
	}
	return len(freedl.EnabledJobs(cfg)) > 0
}
