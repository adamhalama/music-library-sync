package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaa/update-downloads/internal/fileops"
	"gopkg.in/yaml.v3"
)

var (
	createTempFile     = os.CreateTemp
	writeTempFile      = os.WriteFile
	removeTempFile     = os.Remove
	replaceTempFile    = fileops.ReplaceFileSafely
	createEditorDirAll = os.MkdirAll
)

func LoadSingleFile(path string) (Config, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return Config{}, fmt.Errorf("config file path must be set")
	}
	payload, err := os.ReadFile(trimmed)
	if err != nil {
		return Config{}, fmt.Errorf("read config file %s: %w", trimmed, err)
	}
	return ParseSingleFile(trimmed, payload)
}

func ParseSingleFile(path string, payload []byte) (Config, error) {
	cfg := DefaultConfig()
	fc, err := parseFileConfig(payload, path)
	if err != nil {
		return Config{}, err
	}
	applyFileConfig(&cfg, fc)
	normalize(&cfg)
	return cfg, nil
}

func MarshalCanonical(cfg Config) ([]byte, error) {
	normalized := cfg
	normalize(&normalized)
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(normalized); err != nil {
		_ = encoder.Close()
		return nil, fmt.Errorf("encode config yaml: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("finalize config yaml: %w", err)
	}
	return buf.Bytes(), nil
}

func SaveSingleFile(path string, cfg Config) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("config file path must be set")
	}
	if info, err := os.Stat(trimmed); err == nil {
		if info.IsDir() {
			return "", fmt.Errorf("config file path is a directory: %s", trimmed)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect config file path %s: %w", trimmed, err)
	}
	payload, err := MarshalCanonical(cfg)
	if err != nil {
		return "", err
	}
	if err := EnsureConfigDir(trimmed); err != nil {
		return "", err
	}

	dir := filepath.Dir(trimmed)
	tempFile, err := createTempFile(dir, ".udl-config-*")
	if err != nil {
		return "", fmt.Errorf("create temp config file: %w", err)
	}
	tempPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		_ = removeTempFile(tempPath)
		return "", fmt.Errorf("close temp config file: %w", err)
	}
	defer func() {
		_ = removeTempFile(tempPath)
	}()

	if err := writeTempFile(tempPath, payload, 0o644); err != nil {
		return "", fmt.Errorf("write temp config file: %w", err)
	}
	if err := replaceTempFile(tempPath, trimmed); err != nil {
		return "", fmt.Errorf("replace config file: %w", err)
	}

	stateDir, err := ExpandPath(cfg.Defaults.StateDir)
	if err != nil {
		return "", fmt.Errorf("resolve state directory: %w", err)
	}
	if strings.TrimSpace(stateDir) == "" {
		return "", fmt.Errorf("resolve state directory: defaults.state_dir must be set")
	}
	if err := createEditorDirAll(stateDir, 0o755); err != nil {
		return "", fmt.Errorf("create state directory %s: %w", stateDir, err)
	}
	return stateDir, nil
}
