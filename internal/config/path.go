package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func UserConfigPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); strings.TrimSpace(xdg) != "" {
		return filepath.Join(xdg, "udl", "config.yaml"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "udl", "config.yaml"), nil
}

func ProjectConfigPath(cwd string) string {
	return filepath.Join(cwd, "udl.yaml")
}

func defaultStateDir() string {
	if xdg := os.Getenv("XDG_STATE_HOME"); strings.TrimSpace(xdg) != "" {
		return filepath.Join(xdg, "udl")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "./.udl-state"
	}
	return filepath.Join(home, ".local", "state", "udl")
}

func ExpandPath(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}

	expanded := os.ExpandEnv(strings.TrimSpace(raw))
	if expanded == "~" || strings.HasPrefix(expanded, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		expanded = filepath.Join(home, strings.TrimPrefix(expanded, "~/"))
	}

	return filepath.Clean(expanded), nil
}

func ResolveStateFile(defaultStateDir string, stateFile string) (string, error) {
	expandedStateFile, err := ExpandPath(stateFile)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(expandedStateFile) {
		return expandedStateFile, nil
	}

	expandedStateDir, err := ExpandPath(defaultStateDir)
	if err != nil {
		return "", err
	}

	return filepath.Clean(filepath.Join(expandedStateDir, expandedStateFile)), nil
}
