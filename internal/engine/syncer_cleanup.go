package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/output"
)

type mediaFileSnapshot struct {
	Size    int64
	ModTime time.Time
}

func snapshotMediaFiles(dir string) (map[string]mediaFileSnapshot, error) {
	snapshots := map[string]mediaFileSnapshot{}
	root := strings.TrimSpace(dir)
	if root == "" {
		return snapshots, nil
	}
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return snapshots, nil
		}
		return nil, err
	}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if !isMediaExt(ext) {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		snapshots[rel] = mediaFileSnapshot{
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return snapshots, nil
}

func detectUpdatedMediaPath(before map[string]mediaFileSnapshot, after map[string]mediaFileSnapshot) string {
	bestPath := ""
	bestTime := time.Time{}
	for path, current := range after {
		previous, existed := before[path]
		changed := !existed ||
			current.Size != previous.Size ||
			current.ModTime.After(previous.ModTime)
		if !changed {
			continue
		}
		if bestPath == "" ||
			current.ModTime.After(bestTime) ||
			(current.ModTime.Equal(bestTime) && path < bestPath) {
			bestPath = path
			bestTime = current.ModTime
		}
	}
	return bestPath
}

func cleanupTempFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func cleanupRuntimeDir(path string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil
	}
	return os.RemoveAll(trimmed)
}

func cleanupTempStateFiles(stateSwap soundCloudStateSwap) error {
	paths := []string{stateSwap.TempSyncPath, stateSwap.TempArchivePath}
	problems := []string{}
	for _, path := range paths {
		if err := cleanupTempFile(path); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func commitTempStateFiles(stateSwap soundCloudStateSwap) error {
	if strings.TrimSpace(stateSwap.TempSyncPath) != "" {
		if strings.TrimSpace(stateSwap.OriginalSyncPath) == "" {
			if err := cleanupTempFile(stateSwap.TempSyncPath); err != nil {
				return err
			}
		} else if err := mergeCommittedSoundCloudSyncStateFile(stateSwap.OriginalSyncPath, stateSwap.TempSyncPath); err != nil {
			_ = cleanupTempFile(stateSwap.TempSyncPath)
			return err
		}
	}
	if strings.TrimSpace(stateSwap.TempArchivePath) != "" {
		if strings.TrimSpace(stateSwap.OriginalArchivePath) == "" {
			if err := cleanupTempFile(stateSwap.TempArchivePath); err != nil {
				return err
			}
		} else if err := mergeCommittedSoundCloudArchiveFile(stateSwap.OriginalArchivePath, stateSwap.TempArchivePath); err != nil {
			_ = cleanupTempFile(stateSwap.TempArchivePath)
			return err
		}
	}
	return nil
}

func ensureSourcePaths(cfg config.Config, source config.Source) error {
	targetDir, err := config.ExpandPath(source.TargetDir)
	if err != nil {
		return fmt.Errorf("[%s] invalid target_dir: %w", source.ID, err)
	}
	info, err := os.Stat(targetDir)
	if err != nil {
		return fmt.Errorf("[%s] target_dir does not exist: %s", source.ID, targetDir)
	}
	if !info.IsDir() {
		return fmt.Errorf("[%s] target_dir is not a directory: %s", source.ID, targetDir)
	}

	if source.Type == config.SourceTypeSpotify || source.Type == config.SourceTypeSoundCloud {
		stateFile, err := config.ResolveStateFile(cfg.Defaults.StateDir, source.StateFile)
		if err != nil {
			return fmt.Errorf("[%s] invalid state_file: %w", source.ID, err)
		}
		stateDir := filepath.Dir(stateFile)
		stateInfo, stateErr := os.Stat(stateDir)
		if stateErr != nil {
			return fmt.Errorf("[%s] state directory does not exist: %s", source.ID, stateDir)
		}
		if !stateInfo.IsDir() {
			return fmt.Errorf("[%s] state directory is not a directory: %s", source.ID, stateDir)
		}
	}

	return nil
}

func artifactSuffixesForAdapter(adapterKind string) []string {
	suffixes := []string{".part", ".ytdl"}
	if adapterKind == "scdl" {
		suffixes = append(suffixes, ".scdl.lock", ".jpg", ".jpeg", ".png", ".webp")
	}
	return suffixes
}

func snapshotArtifacts(dir string, suffixes []string) (map[string]struct{}, error) {
	seen := map[string]struct{}{}
	if len(suffixes) == 0 {
		return seen, nil
	}

	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return seen, nil
		}
		return nil, err
	}

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		for _, suffix := range suffixes {
			if strings.HasSuffix(name, suffix) {
				seen[path] = struct{}{}
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return seen, nil
}

func cleanupNewArtifacts(dir string, baseline map[string]struct{}, suffixes []string) ([]string, error) {
	current, err := snapshotArtifacts(dir, suffixes)
	if err != nil {
		return nil, err
	}

	removed := make([]string, 0)
	for path := range current {
		if _, existed := baseline[path]; existed {
			continue
		}
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return removed, removeErr
		}
		removed = append(removed, path)
	}
	slices.Sort(removed)
	return removed, nil
}

func (s *Syncer) cleanupArtifactsOnFailure(sourceID string, dir string, preArtifacts map[string]struct{}, suffixes []string) {
	if len(suffixes) == 0 {
		return
	}

	removed, err := cleanupNewArtifacts(dir, preArtifacts, suffixes)
	if err != nil {
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelWarn,
			Event:     output.EventSourceFailed,
			SourceID:  sourceID,
			Message:   fmt.Sprintf("[%s] artifact cleanup failed: %v", sourceID, err),
		})
		return
	}
	if len(removed) == 0 {
		return
	}

	_ = s.Emitter.Emit(output.Event{
		Timestamp: s.Now(),
		Level:     output.LevelInfo,
		Event:     output.EventSourceFailed,
		SourceID:  sourceID,
		Message:   fmt.Sprintf("[%s] cleaned %d partial artifact(s)", sourceID, len(removed)),
		Details: map[string]any{
			"removed_artifacts": removed,
		},
	})
}
