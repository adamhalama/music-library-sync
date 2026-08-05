package navidrome

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Backup describes one Navidrome database backup file.
type Backup struct {
	Path      string    `json:"path"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

// BackupResult is the outcome of an explicit backup request.
type BackupResult struct {
	Backup  Backup `json:"backup"`
	Command string `json:"command"`
	Message string `json:"message"`
}

// Backups lists retained backups newest first. A missing directory means no
// backups yet, which is not an error.
func Backups(resolved Resolved) ([]Backup, error) {
	entries, err := os.ReadDir(resolved.BackupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Backup{}, nil
		}
		return nil, fmt.Errorf("read backup directory %s: %w", resolved.BackupDir, err)
	}
	out := []Backup{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, statErr := entry.Info()
		if statErr != nil {
			continue
		}
		out = append(out, Backup{
			Path:      filepath.Join(resolved.BackupDir, entry.Name()),
			SizeBytes: info.Size(),
			CreatedAt: info.ModTime().UTC(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// LatestBackup returns the newest backup, if any.
func LatestBackup(resolved Resolved) (Backup, bool, error) {
	all, err := Backups(resolved)
	if err != nil {
		return Backup{}, false, err
	}
	if len(all) == 0 {
		return Backup{}, false, nil
	}
	return all[0], true, nil
}

// CreateBackup runs Navidrome's own backup command and verifies that a new
// file actually appeared. Favorite migration depends on this succeeding, so a
// silent no-op must be reported as a failure.
func CreateBackup(ctx context.Context, run CommandRunner, binaryPath string, resolved Resolved) (BackupResult, error) {
	if strings.TrimSpace(binaryPath) == "" {
		return BackupResult{}, fmt.Errorf("navidrome binary was not found; cannot create a database backup")
	}
	before, err := Backups(resolved)
	if err != nil {
		return BackupResult{}, err
	}
	known := map[string]struct{}{}
	for _, item := range before {
		known[item.Path] = struct{}{}
	}

	if run == nil {
		run = DefaultCommandRunner
	}
	// `navidrome backup` alone is a parent command that prints help and exits
	// 0; `create` is the subcommand that actually writes a backup. Verified
	// against 0.63.2 — the "reported success but no new backup" guard below is
	// what caught the missing subcommand.
	args := []string{"backup", "create", "--configfile", resolved.ConfigFile}
	result := BackupResult{Command: binaryPath + " " + strings.Join(args, " ")}
	raw, err := run(ctx, binaryPath, args...)
	if err != nil {
		return result, fmt.Errorf("%s failed: %w: %s", result.Command, err, strings.TrimSpace(string(raw)))
	}

	after, err := Backups(resolved)
	if err != nil {
		return result, err
	}
	for _, item := range after {
		if _, existed := known[item.Path]; !existed {
			result.Backup = item
			result.Message = fmt.Sprintf("created %s", item.Path)
			return result, nil
		}
	}
	return result, fmt.Errorf("%s reported success but no new backup appeared in %s", result.Command, resolved.BackupDir)
}
