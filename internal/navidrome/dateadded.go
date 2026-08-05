package navidrome

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DatabaseFileName is Navidrome's SQLite database inside the managed data
// directory.
const DatabaseFileName = "navidrome.db"

// ReconcileResult reports what a Date Added reconcile changed.
type ReconcileResult struct {
	DatabasePath string `json:"database_path"`
	BackupPath   string `json:"backup_path,omitempty"`
	Candidates   int    `json:"candidates"`
	Updated      int    `json:"updated"`
	Message      string `json:"message"`
}

// DatabasePath is the managed Navidrome database.
func DatabasePath(resolved Resolved) string {
	return filepath.Join(resolved.DataDir, DatabaseFileName)
}

// ReconcileDateAdded rewrites `media_file.created_at` from `media_file.birth_time`
// so Date Added means the file's real APFS creation time.
//
// Navidrome records the correct birth time but exposes no smart-playlist
// criteria field for it, and `dateadded` — the only date field it honours as a
// sort key — reads `created_at`, which is when the scanner first saw the file.
// On an initial import of an existing library that stamps everything with one
// timestamp, so both the managed playlists and the client's own "Date Added"
// sort come out in scan order. Verified against 0.63.2: this update fixes both,
// survives quick scans, full scans and re-reads of touched files, and is safe
// to run while the server is live.
//
// It is idempotent, so a run with nothing to do reports zero updates.
func ReconcileDateAdded(ctx context.Context, run CommandRunner, resolved Resolved, backup BackupFunc) (ReconcileResult, error) {
	database := DatabasePath(resolved)
	result := ReconcileResult{DatabasePath: database}

	// Only ever touch the database inside the managed data directory.
	if !withinDir(resolved.DataDir, database) {
		return result, fmt.Errorf("%s is outside the managed data directory", database)
	}
	if info, err := os.Stat(database); err != nil || info.Size() == 0 {
		return result, fmt.Errorf("the Navidrome database at %s is missing or empty; scan the library first", database)
	}
	if run == nil {
		run = DefaultCommandRunner
	}

	const countQuery = `SELECT count(*) FROM media_file WHERE created_at <> birth_time;`
	raw, err := run(ctx, "sqlite3", database, countQuery)
	if err != nil {
		return result, fmt.Errorf("count rows needing a Date Added fix: %w: %s", err, strings.TrimSpace(string(raw)))
	}
	candidates, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return result, fmt.Errorf("parse row count %q: %w", strings.TrimSpace(string(raw)), err)
	}
	result.Candidates = candidates
	if candidates == 0 {
		result.Message = "Date Added already matches file creation time; nothing to do"
		return result, nil
	}

	// A database write earns a backup first, on the same rule as the favorite
	// import.
	if backup != nil {
		created, backupErr := backup(ctx)
		if backupErr != nil {
			return result, fmt.Errorf("create a Navidrome database backup: %w", backupErr)
		}
		result.BackupPath = created.Path
		if info, statErr := os.Stat(created.Path); statErr != nil || info.Size() == 0 {
			return result, fmt.Errorf("the backup at %s is missing or empty; refusing to change the database", created.Path)
		}
	}

	const updateQuery = `UPDATE media_file SET created_at = birth_time WHERE created_at <> birth_time;
SELECT changes();`
	raw, err = run(ctx, "sqlite3", database, updateQuery)
	if err != nil {
		return result, fmt.Errorf("rewrite Date Added from creation time: %w: %s", err, strings.TrimSpace(string(raw)))
	}
	updated, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return result, fmt.Errorf("parse updated row count %q: %w", strings.TrimSpace(string(raw)), err)
	}
	result.Updated = updated
	result.Message = fmt.Sprintf("Date Added now matches file creation time for %d track(s)", updated)
	return result, nil
}
