package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var dotenvKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func loadDotEnvFiles(cwd string, environ []string, setenv func(string, string) error) error {
	if strings.TrimSpace(cwd) == "" {
		return nil
	}
	if setenv == nil {
		return fmt.Errorf("setenv is required")
	}

	protected := map[string]struct{}{}
	for _, pair := range environ {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		protected[parts[0]] = struct{}{}
	}

	files := []string{
		filepath.Join(cwd, ".env"),
		filepath.Join(cwd, ".env.local"),
	}
	for _, file := range files {
		if err := applyDotEnvFile(file, protected, setenv); err != nil {
			return err
		}
	}
	return nil
}

func applyDotEnvFile(path string, protected map[string]struct{}, setenv func(string, string) error) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read %s: %w", path, err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(payload)))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		key, value, ok, parseErr := parseDotEnvLine(scanner.Text())
		if parseErr != nil {
			return fmt.Errorf("parse %s:%d: %w", path, lineNo, parseErr)
		}
		if !ok {
			continue
		}
		if _, exists := protected[key]; exists {
			continue
		}
		if err := setenv(key, value); err != nil {
			return fmt.Errorf("set %s from %s: %w", key, path, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", path, err)
	}
	return nil
}

func parseDotEnvLine(raw string) (string, string, bool, error) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false, nil
	}
	if strings.HasPrefix(line, "export ") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	}

	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", "", false, fmt.Errorf("expected KEY=VALUE format")
	}
	key := strings.TrimSpace(parts[0])
	if !dotenvKeyPattern.MatchString(key) {
		return "", "", false, fmt.Errorf("invalid key %q", key)
	}
	value := strings.TrimSpace(parts[1])
	if value == "" {
		return key, "", true, nil
	}

	if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") && len(value) >= 2 {
		decoded, err := strconv.Unquote(value)
		if err != nil {
			return "", "", false, fmt.Errorf("invalid quoted value for %q", key)
		}
		return key, decoded, true, nil
	}
	if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") && len(value) >= 2 {
		return key, value[1 : len(value)-1], true, nil
	}

	return key, value, true, nil
}
