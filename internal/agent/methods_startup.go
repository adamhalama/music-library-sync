package agent

import (
	"github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
)

type onboardingStateResult struct {
	Needed bool                `json:"needed"`
	State  app.OnboardingState `json:"state"`
}

type startupAttentionResult struct {
	Status    string                `json:"status"`
	Attention *app.StartupAttention `json:"attention,omitempty"`
}

type sourceCapability struct {
	SourceID              string               `json:"source_id"`
	SourceType            config.SourceType    `json:"source_type"`
	Adapter               string               `json:"adapter"`
	SupportsPlan          bool                 `json:"supports_plan"`
	SupportsPlanWindow    bool                 `json:"supports_plan_window"`
	SupportsDownloadOrder bool                 `json:"supports_download_order"`
	DefaultPlanWindow     engine.PlanWindow    `json:"default_plan_window"`
	DefaultDownloadOrder  engine.DownloadOrder `json:"default_download_order"`
}

func (s *Server) onboardingState() (any, *RPCError) {
	state, needed := app.DetectOnboardingState(app.OnboardingOptions{
		ConfigPath:       s.ConfigPath,
		FreeDLConfigPath: s.FreeDLConfigPath,
		WorkingDir:       s.WorkingDir,
	})
	return onboardingStateResult{Needed: needed, State: state}, nil
}

func (s *Server) startupAttention() (any, *RPCError) {
	cfg, err := s.loadValidatedConfig()
	if err != nil {
		return nil, err
	}
	inspectors := app.DefaultCredentialInspectors()
	if s.StartupInspectors != nil {
		inspectors = *s.StartupInspectors
	}
	attention := app.DetectStartupAttention(cfg, inspectors)
	status := "ready"
	if attention != nil {
		status = string(attention.Severity)
	}
	return startupAttentionResult{Status: status, Attention: attention}, nil
}

func (s *Server) sourceCapabilities() (any, *RPCError) {
	cfg, err := s.loadValidatedConfig()
	if err != nil {
		return nil, err
	}
	capabilities := make([]sourceCapability, 0, len(cfg.Sources))
	for _, source := range cfg.Sources {
		capabilities = append(capabilities, sourceCapability{
			SourceID:              source.ID,
			SourceType:            source.Type,
			Adapter:               source.Adapter.Kind,
			SupportsPlan:          engine.SupportsPlan(source),
			SupportsPlanWindow:    engine.SupportsPlanWindow(source),
			SupportsDownloadOrder: engine.SupportsDownloadOrder(source),
			DefaultPlanWindow:     engine.DefaultPlanWindowForSource(source),
			DefaultDownloadOrder:  engine.DefaultDownloadOrder,
		})
	}
	return map[string]any{"sources": capabilities}, nil
}

func (s *Server) loadValidatedConfig() (config.Config, *RPCError) {
	cfg, err := config.Load(config.LoadOptions{ExplicitPath: s.ConfigPath, WorkingDir: s.WorkingDir})
	if err != nil {
		return config.Config{}, configRPCError(err)
	}
	if err := config.Validate(cfg); err != nil {
		return config.Config{}, configRPCError(err)
	}
	return cfg, nil
}
