package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/config"
)

var (
	errBrowserDownloadIdleTimeout = errors.New("browser download idle timeout")
	errBrowserDownloadMaxTimeout  = errors.New("browser download max timeout")
)

var (
	openURLInBrowserFn            = openURLInBrowser
	detectBrowserDownloadedFileFn = detectBrowserDownloadedFile
	browserDownloadsDirFn         = defaultBrowserDownloadsDir
	moveDownloadedMediaToTargetFn = moveDownloadedMediaToTarget
	runtimeGOOS                   = runtime.GOOS
	runBrowserCommandFn           = runBrowserCommand
	browserDownloadPollInterval   = 1 * time.Second
)

func isHypedditPurchaseURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "hypeddit.com" {
		return true
	}
	return strings.HasSuffix(host, ".hypeddit.com")
}

func defaultBrowserDownloadsDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv("UDL_FREEDL_BROWSER_DOWNLOAD_DIR")); override != "" {
		return config.ExpandPath(override)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Downloads"), nil
}

func openURLInBrowser(ctx context.Context, rawURL string) error {
	bin, args, err := browserOpenCommand(rawURL)
	if err != nil {
		return err
	}
	if err := runBrowserCommandFn(ctx, bin, args...); err != nil {
		return fmt.Errorf("browser launch command failed: %w", err)
	}
	return nil
}

func browserOpenCommand(rawURL string) (string, []string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", nil, fmt.Errorf("browser url is empty")
	}

	browserApp := strings.TrimSpace(os.Getenv("UDL_FREEDL_BROWSER_APP"))
	switch runtimeGOOS {
	case "darwin":
		if browserApp != "" {
			return "open", []string{"-a", browserApp, trimmed}, nil
		}
		return "open", []string{trimmed}, nil
	case "linux":
		return "xdg-open", []string{trimmed}, nil
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", trimmed}, nil
	default:
		return "", nil, fmt.Errorf("unsupported platform for browser handoff: %s", runtimeGOOS)
	}
}

func runBrowserCommand(ctx context.Context, bin string, args ...string) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, detail)
	}
	return nil
}

func detectBrowserDownloadedFile(
	ctx context.Context,
	downloadsDir string,
	before map[string]mediaFileSnapshot,
	timeout time.Duration,
	metadata soundCloudFreeDownloadMetadata,
) (string, error) {
	dir := strings.TrimSpace(downloadsDir)
	if dir == "" {
		return "", fmt.Errorf("browser download directory is empty")
	}
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	idleTimeout := resolveBrowserDownloadIdleTimeout(timeout)
	if idleTimeout <= 0 {
		idleTimeout = 1 * time.Minute
	}
	startedAt := time.Now()
	absoluteDeadline := startedAt.Add(timeout)
	lastProgressAt := startedAt
	lastCandidate := ""
	lastCandidateSnapshot := mediaFileSnapshot{}
	lastCandidateSnapshotSet := false
	stableSamples := 0
	inProgressBefore, _ := snapshotBrowserInProgressFiles(dir)

	for {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		now := time.Now()
		if now.After(absoluteDeadline) {
			return "", fmt.Errorf("%w in %s (max_wait=%s)", errBrowserDownloadMaxTimeout, dir, timeout)
		}
		if now.Sub(lastProgressAt) >= idleTimeout {
			return "", fmt.Errorf("%w in %s (idle_for=%s)", errBrowserDownloadIdleTimeout, dir, idleTimeout)
		}

		after, err := snapshotMediaFiles(dir)
		if err == nil {
			candidate := selectBrowserDownloadCandidate(before, after, metadata)
			if candidate != "" {
				abs := filepath.Join(dir, filepath.FromSlash(candidate))
				candidateSnapshot := after[candidate]
				if abs == lastCandidate &&
					lastCandidateSnapshotSet &&
					candidateSnapshot.Size == lastCandidateSnapshot.Size &&
					!candidateSnapshot.ModTime.After(lastCandidateSnapshot.ModTime) {
					stableSamples++
				} else {
					lastCandidate = abs
					stableSamples = 1
					lastProgressAt = now
					lastCandidateSnapshotSet = true
				}
				lastCandidateSnapshot = candidateSnapshot
				if stableSamples >= 2 {
					return abs, nil
				}
			}
		}
		inProgressAfter, snapshotErr := snapshotBrowserInProgressFiles(dir)
		if snapshotErr == nil {
			if hasBrowserInProgressActivity(inProgressBefore, inProgressAfter) {
				lastProgressAt = now
			}
			inProgressBefore = inProgressAfter
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(browserDownloadPollInterval):
		}
	}
}

func resolveBrowserDownloadIdleTimeout(maxWait time.Duration) time.Duration {
	idle := 1 * time.Minute
	if override := strings.TrimSpace(os.Getenv("UDL_FREEDL_BROWSER_IDLE_TIMEOUT")); override != "" {
		if parsed, err := time.ParseDuration(override); err == nil && parsed > 0 {
			idle = parsed
		}
	}
	if maxWait > 0 && idle > maxWait {
		return maxWait
	}
	return idle
}

func snapshotBrowserInProgressFiles(dir string) (map[string]mediaFileSnapshot, error) {
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
		if path == root {
			return nil
		}
		name := strings.ToLower(strings.TrimSpace(d.Name()))
		if d.IsDir() {
			if !isBrowserInProgressName(name) {
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
			snapshots[filepath.ToSlash(rel)] = mediaFileSnapshot{
				Size:    0,
				ModTime: info.ModTime(),
			}
			return filepath.SkipDir
		}
		if !isBrowserInProgressName(name) {
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
		snapshots[filepath.ToSlash(rel)] = mediaFileSnapshot{
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

func isBrowserInProgressName(name string) bool {
	switch {
	case strings.HasSuffix(name, ".crdownload"):
		return true
	case strings.HasSuffix(name, ".download"):
		return true
	case strings.HasSuffix(name, ".part"):
		return true
	case strings.HasSuffix(name, ".partial"):
		return true
	case strings.HasSuffix(name, ".tmp"):
		return true
	case strings.HasSuffix(name, ".opdownload"):
		return true
	case strings.HasSuffix(name, ".aria2"):
		return true
	default:
		return false
	}
}

func hasBrowserInProgressActivity(
	before map[string]mediaFileSnapshot,
	after map[string]mediaFileSnapshot,
) bool {
	if len(before) != len(after) {
		return true
	}
	for rel, current := range after {
		previous, exists := before[rel]
		if !exists {
			return true
		}
		if current.Size != previous.Size {
			return true
		}
		if current.ModTime.After(previous.ModTime) {
			return true
		}
	}
	return false
}

func selectBrowserDownloadCandidate(
	before map[string]mediaFileSnapshot,
	after map[string]mediaFileSnapshot,
	metadata soundCloudFreeDownloadMetadata,
) string {
	type candidate struct {
		Rel     string
		ModTime time.Time
		Score   int
	}

	candidates := make([]candidate, 0)
	expectedTitle := normalizeTrackKey(metadata.Title)
	expectedArtist := normalizeTrackKey(metadata.Artist)
	for rel, current := range after {
		previous, existed := before[rel]
		if existed &&
			current.Size == previous.Size &&
			!current.ModTime.After(previous.ModTime) {
			continue
		}
		stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel)))
		key := normalizeTrackKey(stem)
		score := 0
		if expectedTitle != "" && key != "" && strings.Contains(key, expectedTitle) {
			score += 2
		}
		if expectedArtist != "" && key != "" && strings.Contains(key, expectedArtist) {
			score++
		}
		candidates = append(candidates, candidate{
			Rel:     rel,
			ModTime: current.ModTime,
			Score:   score,
		})
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if !candidates[i].ModTime.Equal(candidates[j].ModTime) {
			return candidates[i].ModTime.After(candidates[j].ModTime)
		}
		return candidates[i].Rel < candidates[j].Rel
	})
	return candidates[0].Rel
}

func moveDownloadedMediaToTarget(sourcePath string, targetDir string) (string, error) {
	src := strings.TrimSpace(sourcePath)
	if src == "" {
		return "", fmt.Errorf("source path is empty")
	}
	destRoot := strings.TrimSpace(targetDir)
	if destRoot == "" {
		return "", fmt.Errorf("target_dir is empty")
	}
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		return "", err
	}
	base := filepath.Base(src)
	dest := filepath.Join(destRoot, base)
	dest = nextAvailablePath(dest)
	if err := os.Rename(src, dest); err == nil {
		return dest, nil
	}

	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = in.Close()
	}()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dest)
		return "", err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dest)
		return "", err
	}
	if err := os.Remove(src); err != nil {
		return "", err
	}
	return dest, nil
}

func nextAvailablePath(path string) string {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return path
	}
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
	}
}
