package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaa/update-downloads/internal/fileops"
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

func DefaultStateDir() string {
	return defaultStateDir()
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

// ResolveTargetDir gives downloads and imports the same source folder path.
func ResolveTargetDir(raw string) (string, error) {
	path, err := ExpandPath(raw)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", fmt.Errorf("target_dir must be set")
	}
	return fileops.ResolveSymlinks(path)
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

func ResolveArchiveFile(defaultStateDir string, archiveFile string, sourceID string) (string, error) {
	candidate := strings.TrimSpace(archiveFile)
	if candidate == "" {
		candidate = "archive.txt"
	}

	expandedArchiveFile, err := ExpandPath(candidate)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(expandedArchiveFile) {
		return filepath.Clean(expandedArchiveFile), nil
	}

	expandedStateDir, err := ExpandPath(defaultStateDir)
	if err != nil {
		return "", err
	}

	// Keep nested relative archive paths as provided.
	if strings.ContainsRune(expandedArchiveFile, filepath.Separator) {
		return filepath.Clean(filepath.Join(expandedStateDir, expandedArchiveFile)), nil
	}

	// For simple filenames, namespace per-source to avoid cross-source collisions.
	if strings.TrimSpace(sourceID) != "" {
		return filepath.Clean(filepath.Join(expandedStateDir, sourceID+"."+expandedArchiveFile)), nil
	}

	return filepath.Clean(filepath.Join(expandedStateDir, expandedArchiveFile)), nil
}
