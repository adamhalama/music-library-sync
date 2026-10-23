package navidrome

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func writeLegacyExportHistory(t *testing.T, resolved Resolved) (ServerDBExportState, []byte) {
	t.Helper()
	last := time.Date(2026, 9, 1, 10, 0, 0, 123456789, time.UTC)
	failed := last.Add(7 * 24 * time.Hour)
	state := ServerDBExportState{LastExportAt: &last, LastExportError: "weekly copy failed", LastExportErrorAt: &failed}
	raw, err := json.Marshal(struct {
		ServerDBExportState
		ScanPending bool `json:"scan_pending"`
	}{state, true})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(MirrorStatePath(resolved), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return state, raw
}

func assertExportHistory(t *testing.T, got, want ServerDBExportState) {
	t.Helper()
	if got.LastExportAt == nil || want.LastExportAt == nil || !got.LastExportAt.Equal(*want.LastExportAt) ||
		got.LastExportError != want.LastExportError || got.LastExportErrorAt == nil || want.LastExportErrorAt == nil ||
		!got.LastExportErrorAt.Equal(*want.LastExportErrorAt) {
		t.Fatalf("export history = %+v, want %+v", got, want)
	}
}

// Regression for the round-4 legacy-history probe: migrate the old fields
// before a typed mirror-state rewrite can drop them, and still warn about the
// failed/stale off-server copy. Migration copies history, not the mirror state.
func TestLegacyExportHistoryMigratesBeforeAMirrorRewrite(t *testing.T) {
	for _, entry := range []string{"export load", "mirror load", "mirror write", "status"} {
		t.Run(entry, func(t *testing.T) {
			manager, _ := exportManager(t, nil, 0)
			want, before := writeLegacyExportHistory(t, manager.Resolved)
			manager.Now = func() time.Time { return want.LastExportAt.Add(30 * 24 * time.Hour) }
			switch entry {
			case "export load":
				got, err := LoadServerDBExportState(manager.Resolved)
				if err != nil {
					t.Fatal(err)
				}
				assertExportHistory(t, got, want)
			case "mirror load":
				state, err := LoadMirrorState(manager.Resolved)
				if err != nil || !state.ScanPending {
					t.Fatalf("mirror = %+v, %v", state, err)
				}
			case "mirror write":
				if err := saveMirrorState(manager.Resolved, MirrorState{ScanPending: true}); err != nil {
					t.Fatal(err)
				}
			case "status":
				if problems := strings.Join(manager.serverDBExportProblems(), "\n"); !strings.Contains(problems, want.LastExportError) {
					t.Fatalf("legacy export failure not reported: %s", problems)
				}
			}
			if entry != "mirror write" {
				after, err := os.ReadFile(MirrorStatePath(manager.Resolved))
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("migration changed mirror bytes: %s, %v", after, err)
				}
			}
			// Check the on-disk file, not another potentially migrating load.
			got, err := readServerDBExportState(ServerDBExportStatePath(manager.Resolved))
			if err != nil {
				t.Fatal(err)
			}
			assertExportHistory(t, got, want)
			info, err := os.Stat(ServerDBExportStatePath(manager.Resolved))
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("export state must be private: %v, %v", info, err)
			}
			if err := saveMirrorState(manager.Resolved, MirrorState{ScanPending: true}); err != nil {
				t.Fatal(err)
			}
			after, _ := os.ReadFile(MirrorStatePath(manager.Resolved))
			if bytes.Contains(after, []byte("last_export")) {
				t.Fatalf("mirror must no longer own export history: %s", after)
			}
			got, err = LoadServerDBExportState(manager.Resolved)
			if err != nil {
				t.Fatal(err)
			}
			assertExportHistory(t, got, want)
			if problems := strings.Join(manager.serverDBExportProblems(), "\n"); !strings.Contains(problems, want.LastExportError) {
				t.Fatalf("migrated failure not reported: %s", problems)
			}
			got.LastExportError, got.LastExportErrorAt = "", nil
			if problems := strings.Join(exportProblems(got, manager.Now()), "\n"); !strings.Contains(problems, "30 days ago") {
				t.Fatalf("migrated stale copy not reported: %s", problems)
			}
		})
	}
}

func TestLegacyExportMigrationDoesNotReplayOverNewState(t *testing.T) {
	manager, _ := exportManager(t, nil, 0)
	_, legacy := writeLegacyExportHistory(t, manager.Resolved)
	if _, err := LoadServerDBExportState(manager.Resolved); err != nil {
		t.Fatal(err)
	}
	// An explicit empty record is authoritative too. It must not resurrect
	// an old failed export still present in the unchanged legacy file.
	if err := saveServerDBExportState(manager.Resolved, ServerDBExportState{}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		got, err := LoadServerDBExportState(manager.Resolved)
		if err != nil || got != (ServerDBExportState{}) {
			t.Fatalf("legacy history replayed: %+v, %v", got, err)
		}
	}
	if after, _ := os.ReadFile(MirrorStatePath(manager.Resolved)); !bytes.Equal(after, legacy) {
		t.Fatalf("migration changed legacy file: %s", after)
	}
}

func TestMirrorWriteKeepsLegacyHistoryWhileMigrationIsBusy(t *testing.T) {
	manager, _ := exportManager(t, nil, 0)
	want, before := writeLegacyExportHistory(t, manager.Resolved)
	unlock, err := lockServerDBExport(manager.Resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()
	if _, err := LoadServerDBExportState(manager.Resolved); !errors.Is(err, ErrServerDBExportBusy) {
		t.Fatalf("migration must own the export lock: %v", err)
	}
	if _, err := LoadMirrorState(manager.Resolved); !errors.Is(err, ErrServerDBExportBusy) {
		t.Fatalf("mirror load must attempt migration: %v", err)
	}
	if err := saveMirrorState(manager.Resolved, MirrorState{}); !errors.Is(err, ErrServerDBExportBusy) {
		t.Fatalf("mirror write must refuse before discarding history: %v", err)
	}
	if after, _ := os.ReadFile(MirrorStatePath(manager.Resolved)); !bytes.Equal(before, after) {
		t.Fatalf("mirror write discarded history before migration: %s", after)
	}
	if _, err := os.Stat(ServerDBExportStatePath(manager.Resolved)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("busy migration must not write the export state: %v", err)
	}
	unlock()
	unlock = nil
	if err := saveMirrorState(manager.Resolved, MirrorState{}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadServerDBExportState(manager.Resolved)
	if err != nil {
		t.Fatal(err)
	}
	assertExportHistory(t, got, want)
}

func TestMirrorWriteKeepsLegacyHistoryWhenMigrationCannotPersist(t *testing.T) {
	manager, _ := exportManager(t, nil, 0)
	_, before := writeLegacyExportHistory(t, manager.Resolved)
	if err := os.Mkdir(ServerDBExportStatePath(manager.Resolved), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := saveMirrorState(manager.Resolved, MirrorState{}); err == nil {
		t.Fatal("a failed migration must prevent the mirror rewrite")
	}
	if after, _ := os.ReadFile(MirrorStatePath(manager.Resolved)); !bytes.Equal(before, after) {
		t.Fatalf("failed migration discarded history: %s", after)
	}
}

func TestExportFailureMigratesLastGoodTimeUnderItsExistingLock(t *testing.T) {
	manager, _ := exportManager(t, nil, 255)
	want, _ := writeLegacyExportHistory(t, manager.Resolved)
	result, err := manager.ExportServerDB(context.Background(), ServerDBExportOptions{})
	if err == nil || len(result.Warnings) != 0 {
		t.Fatalf("export should fail but record its outcome: %+v, %v", result, err)
	}
	got, err := LoadServerDBExportState(manager.Resolved)
	if err != nil || got.LastExportAt == nil || !got.LastExportAt.Equal(*want.LastExportAt) ||
		!strings.Contains(got.LastExportError, "not reachable over SSH") || got.LastExportErrorAt == nil ||
		!got.LastExportErrorAt.Equal(manager.Now()) {
		t.Fatalf("export failure must keep migrated last good time: %+v, %v", got, err)
	}
}

func TestNoLegacyHistoryDoesNotCreateExportStateOrCoupleMirrorWrites(t *testing.T) {
	manager, _ := exportManager(t, nil, 0)
	if err := saveMirrorState(manager.Resolved, MirrorState{ScanPending: true}); err != nil {
		t.Fatal(err)
	}
	if state, err := LoadServerDBExportState(manager.Resolved); err != nil || state != (ServerDBExportState{}) {
		t.Fatalf("fresh export state = %+v, %v", state, err)
	}
	if _, err := os.Stat(ServerDBExportStatePath(manager.Resolved)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("no legacy history must not create a state file: %v", err)
	}
	// Without legacy fields, a broken export-state file belongs exclusively
	// to that workflow and must not block otherwise unrelated mirror writes.
	if err := os.WriteFile(ServerDBExportStatePath(manager.Resolved), []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveMirrorState(manager.Resolved, MirrorState{}); err != nil {
		t.Fatalf("unrelated export error blocked the mirror: %v", err)
	}
}
