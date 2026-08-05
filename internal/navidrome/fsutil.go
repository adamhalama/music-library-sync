package navidrome

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// AtomicWrite replaces path with payload through a same-directory temp file so
// a crashed write can never leave a half-written managed file behind.
func AtomicWrite(path string, payload []byte, mode os.FileMode) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("write path must not be empty")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(dir, ".udl-navidrome-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary file for %s: %w", path, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temporary file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary file for %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	cleanup = false
	return nil
}

func sortStrings(values []string) {
	sort.Slice(values, func(i, j int) bool {
		left, right := strings.ToLower(values[i]), strings.ToLower(values[j])
		if left == right {
			return values[i] < values[j]
		}
		return left < right
	})
}
