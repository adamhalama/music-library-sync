package fileops

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

var (
	statFile   = os.Stat
	renameFile = os.Rename
	removeFile = os.Remove
)

// ReplaceFileSafely replaces targetPath with tempPath while preserving the
// previous target content as a rollback backup until replacement succeeds.
func ReplaceFileSafely(tempPath string, targetPath string) error {
	temp := strings.TrimSpace(tempPath)
	target := strings.TrimSpace(targetPath)
	if temp == "" {
		return fmt.Errorf("replacement temp path is empty")
	}
	if target == "" {
		return fmt.Errorf("replacement target path is empty")
	}
	if temp == target {
		return fmt.Errorf("replacement temp and target paths must differ")
	}

	tempInfo, err := statFile(temp)
	if err != nil {
		return fmt.Errorf("stat replacement temp %q: %w", temp, err)
	}
	if tempInfo.IsDir() {
		return fmt.Errorf("replacement temp path is a directory: %s", temp)
	}

	backup := target + ".udl.bak"
	if _, err := statFile(backup); err == nil {
		if removeErr := removeFile(backup); removeErr != nil {
			return fmt.Errorf("remove stale replacement backup %q: %w", backup, removeErr)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat replacement backup %q: %w", backup, err)
	}

	hadTarget := false
	if _, err := statFile(target); err == nil {
		hadTarget = true
		if err := renameFile(target, backup); err != nil {
			return fmt.Errorf("move existing target to backup: %w", err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat replacement target %q: %w", target, err)
	}

	if err := renameFile(temp, target); err != nil {
		if hadTarget {
			if rollbackErr := renameFile(backup, target); rollbackErr != nil {
				return fmt.Errorf("replace failed (%v) and rollback failed (%w)", err, rollbackErr)
			}
		}
		return fmt.Errorf("replace target with temp: %w", err)
	}

	if hadTarget {
		if err := removeFile(backup); err != nil {
			return fmt.Errorf("cleanup replacement backup %q: %w", backup, err)
		}
	}
	return nil
}
