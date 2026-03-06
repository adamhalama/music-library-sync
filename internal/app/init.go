package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/jaa/update-downloads/internal/config"
)

type InitRequest struct {
	ConfigPath string
	Force      bool
	NoInput    bool
	IsTTY      bool
}

type InitResult struct {
	ConfigPath string
	StateDir   string
	Canceled   bool
}

type InitUseCase struct{}

func (InitUseCase) Run(req InitRequest, interaction Interaction) (InitResult, error) {
	if interaction == nil {
		interaction = NoopInteraction{}
	}

	path := strings.TrimSpace(req.ConfigPath)
	if path == "" {
		userPath, err := config.UserConfigPath()
		if err != nil {
			return InitResult{}, err
		}
		path = userPath
	}

	if err := config.EnsureConfigDir(path); err != nil {
		return InitResult{}, err
	}

	if _, err := os.Stat(path); err == nil && !req.Force {
		if req.NoInput || !req.IsTTY {
			return InitResult{}, fmt.Errorf("config already exists at %s (rerun with --force)", path)
		}
		confirmed, confirmErr := interaction.Confirm(fmt.Sprintf("Config already exists at %s. Overwrite?", path), false)
		if confirmErr != nil {
			return InitResult{}, confirmErr
		}
		if !confirmed {
			return InitResult{Canceled: true}, nil
		}
	}

	if err := os.WriteFile(path, []byte(config.DefaultTemplate()), 0o644); err != nil {
		return InitResult{}, fmt.Errorf("write config file: %w", err)
	}

	stateDir, err := config.ExpandPath(config.DefaultConfig().Defaults.StateDir)
	if err != nil {
		return InitResult{}, fmt.Errorf("resolve state directory: %w", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return InitResult{}, fmt.Errorf("create state directory %s: %w", stateDir, err)
	}

	return InitResult{
		ConfigPath: path,
		StateDir:   stateDir,
	}, nil
}
