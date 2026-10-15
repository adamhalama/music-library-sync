package navidrome

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newDateAddedFixture builds a temporary Navidrome-shaped database with the two
// columns the reconcile touches, using the real sqlite3 the reconcile itself
// shells out to.
func newDateAddedFixture(t *testing.T) (Resolved, string) {
	t.Helper()
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 is required for the Date Added reconcile tests")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := DefaultConfig()
	cfg.Paths.DataDir = filepath.Join(home, "data")
	cfg.Paths.BackupDir = filepath.Join(home, "backups")
	cfg.Paths.CacheDir = filepath.Join(home, "cache")
	cfg.Paths.LogFile = filepath.Join(home, "data", "navidrome.log")
	cfg.Paths.ConfigFile = filepath.Join(home, "data", "navidrome.toml")
	normalize(&cfg)
	resolved, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := os.MkdirAll(resolved.DataDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	database := DatabasePath(resolved)
	schema := `CREATE TABLE media_file (path text, created_at datetime, birth_time datetime);
INSERT INTO media_file VALUES
 ('/m/new.mp3','2026-08-04 22:00:00+02:00','2026-08-03 10:00:00+02:00'),
 ('/m/old.mp3','2026-08-04 22:00:00+02:00','2014-02-18 09:00:00+01:00'),
 ('/m/same.mp3','2026-08-04 22:00:00+02:00','2026-08-04 22:00:00+02:00');`
	if out, err := exec.Command("sqlite3", database, schema).CombinedOutput(); err != nil {
		t.Fatalf("seed database: %v: %s", err, out)
	}
	return resolved, database
}

func stubBackup(t *testing.T, resolved Resolved) BackupFunc {
	t.Helper()
	return func(context.Context) (Backup, error) {
		path := filepath.Join(resolved.BackupDir, "navidrome_backup_test.db")
		if err := os.MkdirAll(resolved.BackupDir, 0o755); err != nil {
			return Backup{}, err
		}
		if err := os.WriteFile(path, []byte("database"), 0o600); err != nil {
			return Backup{}, err
		}
		return Backup{Path: path, SizeBytes: 8}, nil
	}
}

func TestReconcileDateAddedRewritesOnlyMismatchedRows(t *testing.T) {
	resolved, database := newDateAddedFixture(t)
	result, err := ReconcileDateAdded(context.Background(), nil, resolved, stubBackup(t, resolved))
	if err != nil {
		t.Fatalf("ReconcileDateAdded: %v", err)
	}
	if result.Candidates != 2 || result.Updated != 2 {
		t.Fatalf("result = %+v, want 2 candidates and 2 updates (the already-correct row is untouched)", result)
	}
	out, err := exec.Command("sqlite3", database,
		"SELECT count(*) FROM media_file WHERE created_at <> birth_time;").CombinedOutput()
	if err != nil {
		t.Fatalf("verify: %v: %s", err, out)
	}
	if strings.TrimSpace(string(out)) != "0" {
		t.Fatalf("rows still mismatched: %s", out)
	}
}

func TestReconcileDateAddedIsIdempotent(t *testing.T) {
	resolved, _ := newDateAddedFixture(t)
	if _, err := ReconcileDateAdded(context.Background(), nil, resolved, stubBackup(t, resolved)); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	result, err := ReconcileDateAdded(context.Background(), nil, resolved, stubBackup(t, resolved))
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if result.Candidates != 0 || result.Updated != 0 {
		t.Fatalf("a second run must change nothing: %+v", result)
	}
	if result.BackupPath != "" {
		t.Fatalf("a no-op run must not take a backup: %+v", result)
	}
}

func TestReconcileDateAddedRefusesWithoutADatabase(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := DefaultConfig()
	cfg.Paths.DataDir = filepath.Join(home, "data")
	normalize(&cfg)
	resolved, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	_, err = ReconcileDateAdded(context.Background(), nil, resolved, nil)
	if err == nil || !strings.Contains(err.Error(), "missing or empty") {
		t.Fatalf("error = %v, want a missing-database refusal", err)
	}
}

func TestReconcileDateAddedRefusesWhenTheBackupFails(t *testing.T) {
	resolved, database := newDateAddedFixture(t)
	failing := func(context.Context) (Backup, error) {
		return Backup{}, os.ErrPermission
	}
	if _, err := ReconcileDateAdded(context.Background(), nil, resolved, failing); err == nil {
		t.Fatalf("a failed backup must stop the database write")
	}
	// The database must be exactly as it was.
	out, _ := exec.Command("sqlite3", database,
		"SELECT count(*) FROM media_file WHERE created_at <> birth_time;").CombinedOutput()
	if strings.TrimSpace(string(out)) != "2" {
		t.Fatalf("the database was modified behind a failed backup: %s", out)
	}
}
