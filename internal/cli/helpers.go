package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/jaa/update-downloads/internal/config"
)

func loadConfig(app *AppContext) (config.Config, error) {
	wd, err := os.Getwd()
	if err != nil {
		return config.Config{}, fmt.Errorf("resolve working directory: %w", err)
	}

	cfg, err := config.Load(config.LoadOptions{
		ExplicitPath: strings.TrimSpace(app.Opts.ConfigPath),
		WorkingDir:   wd,
	})
	if err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

func isTTY(file *os.File) bool {
	stat, err := file.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}
