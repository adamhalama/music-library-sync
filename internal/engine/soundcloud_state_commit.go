package engine

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaa/update-downloads/internal/fileops"
)

type soundCloudArchiveLine struct {
	Raw string
	ID  string
}

func mergeCommittedSoundCloudSyncStateFile(originalPath string, tempPath string) error {
	originalState, err := parseSoundCloudSyncState(originalPath)
	if err != nil {
		return fmt.Errorf("parse original sync state: %w", err)
	}
	tempState, err := parseSoundCloudSyncState(tempPath)
	if err != nil {
		return fmt.Errorf("parse temporary sync state: %w", err)
	}

	mergedLines := mergeSoundCloudSyncStateLines(originalState, tempState)
	if err := writeSoundCloudLinesAtomically(originalPath, ".udl-sync-commit-*.scdl", mergedLines); err != nil {
		return err
	}
	return cleanupTempFile(tempPath)
}

func mergeCommittedSoundCloudArchiveFile(originalPath string, tempPath string) error {
	originalLines, err := readSoundCloudArchiveLines(originalPath)
	if err != nil {
		return fmt.Errorf("parse original archive: %w", err)
	}
	tempLines, err := readSoundCloudArchiveLines(tempPath)
	if err != nil {
		return fmt.Errorf("parse temporary archive: %w", err)
	}

	mergedLines := mergeSoundCloudArchiveLines(originalLines, tempLines)
	if err := writeSoundCloudLinesAtomically(originalPath, ".udl-archive-commit-*.txt", mergedLines); err != nil {
		return err
	}
	return cleanupTempFile(tempPath)
}

func mergeSoundCloudSyncStateLines(original soundCloudSyncState, temp soundCloudSyncState) []string {
	lines := make([]string, 0, len(original.Entries)+len(temp.Entries))
	originalIDs := map[string]struct{}{}

	for _, entry := range original.Entries {
		if entry.ID == "" {
			lines = append(lines, entry.RawLine)
			continue
		}
		originalIDs[entry.ID] = struct{}{}
		if replacement, ok := temp.ByID[entry.ID]; ok {
			lines = append(lines, replacement.RawLine)
			continue
		}
		lines = append(lines, entry.RawLine)
	}

	appendedIDs := map[string]struct{}{}
	for _, entry := range temp.Entries {
		if entry.ID == "" {
			continue
		}
		if _, seen := originalIDs[entry.ID]; seen {
			continue
		}
		if _, seen := appendedIDs[entry.ID]; seen {
			continue
		}
		lines = append(lines, entry.RawLine)
		appendedIDs[entry.ID] = struct{}{}
	}

	return lines
}

func mergeSoundCloudArchiveLines(original []soundCloudArchiveLine, temp []soundCloudArchiveLine) []string {
	lines := make([]string, 0, len(original)+len(temp))
	originalIDs := map[string]struct{}{}
	tempByID := map[string]string{}

	for _, line := range temp {
		if line.ID == "" {
			continue
		}
		tempByID[line.ID] = line.Raw
	}

	for _, line := range original {
		if line.ID == "" {
			lines = append(lines, line.Raw)
			continue
		}
		originalIDs[line.ID] = struct{}{}
		if replacement, ok := tempByID[line.ID]; ok {
			lines = append(lines, replacement)
			continue
		}
		lines = append(lines, line.Raw)
	}

	appendedIDs := map[string]struct{}{}
	for _, line := range temp {
		if line.ID == "" {
			continue
		}
		if _, seen := originalIDs[line.ID]; seen {
			continue
		}
		if _, seen := appendedIDs[line.ID]; seen {
			continue
		}
		lines = append(lines, line.Raw)
		appendedIDs[line.ID] = struct{}{}
	}

	return lines
}

func readSoundCloudArchiveLines(path string) ([]soundCloudArchiveLine, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []soundCloudArchiveLine{}, nil
		}
		return nil, err
	}
	defer file.Close()

	lines := []soundCloudArchiveLine{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		raw := scanner.Text()
		lines = append(lines, soundCloudArchiveLine{
			Raw: raw,
			ID:  parseSoundCloudArchiveLineID(raw),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func parseSoundCloudArchiveLineID(raw string) string {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) < 2 || fields[0] != "soundcloud" {
		return ""
	}
	return strings.TrimSpace(fields[1])
}

func writeSoundCloudLinesAtomically(targetPath string, pattern string, lines []string) error {
	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}

	tempFile, err := os.CreateTemp(targetDir, pattern)
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	success := false
	defer func() {
		_ = tempFile.Close()
		if !success {
			_ = cleanupTempFile(tempPath)
		}
	}()

	for _, line := range lines {
		if _, err := tempFile.WriteString(line + "\n"); err != nil {
			return err
		}
	}
	if err := tempFile.Sync(); err != nil {
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tempPath, 0o644); err != nil {
		return err
	}
	if err := fileops.ReplaceFileSafely(tempPath, targetPath); err != nil {
		return err
	}
	success = true
	return nil
}
