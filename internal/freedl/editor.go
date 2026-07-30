package freedl

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/fileops"
	"gopkg.in/yaml.v3"
)

var (
	createFreeDLTempFile  = os.CreateTemp
	writeFreeDLTempFile   = os.WriteFile
	removeFreeDLTempFile  = os.Remove
	replaceFreeDLTempFile = fileops.ReplaceFileSafely
)

func LoadSingleFile(path string, main config.Config) (Config, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return Config{}, fmt.Errorf("freedl config file path must be set")
	}
	cfg := DefaultConfig(main)
	cfg.Jobs = nil
	if err := mergeFile(&cfg, trimmed, true); err != nil {
		return Config{}, err
	}
	normalize(&cfg, main)
	return cfg, nil
}

func MarshalCanonical(cfg Config, main config.Config) ([]byte, error) {
	normalized := cfg
	normalize(&normalized, main)
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(normalized); err != nil {
		_ = encoder.Close()
		return nil, fmt.Errorf("encode freedl config yaml: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("finalize freedl config yaml: %w", err)
	}
	return buf.Bytes(), nil
}

func SaveSingleFile(path string, cfg Config, main config.Config) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return fmt.Errorf("freedl config file path must be set")
	}
	if info, err := os.Stat(trimmed); err == nil {
		if info.IsDir() {
			return fmt.Errorf("freedl config file path is a directory: %s", trimmed)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect freedl config file path %s: %w", trimmed, err)
	}
	payload, err := MarshalCanonical(cfg, main)
	if err != nil {
		return err
	}
	if err := config.EnsureConfigDir(trimmed); err != nil {
		return err
	}
	dir := filepath.Dir(trimmed)
	tempFile, err := createFreeDLTempFile(dir, ".udl-freedl-config-*")
	if err != nil {
		return fmt.Errorf("create temp freedl config file: %w", err)
	}
	tempPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		_ = removeFreeDLTempFile(tempPath)
		return fmt.Errorf("close temp freedl config file: %w", err)
	}
	defer func() {
		_ = removeFreeDLTempFile(tempPath)
	}()
	if err := writeFreeDLTempFile(tempPath, payload, 0o644); err != nil {
		return fmt.Errorf("write temp freedl config file: %w", err)
	}
	if err := replaceFreeDLTempFile(tempPath, trimmed); err != nil {
		return fmt.Errorf("replace freedl config file: %w", err)
	}
	return nil
}
