package navidrome

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRemoteMirrorAcceptance drives a real push, scan, Date Added reconcile,
// playlist refresh, backup and status against a real Navidrome serving a
// mirror written by real rsync through the udl-mirror-gate forced command.
//
// It is opt-in and needs a prepared environment (see the remote-mode design's
// end-to-end harness): UDL_NAVIDROME_REMOTE_E2E=1 plus
//
//	E2E_MUSIC_DIR   source library; must equal the server's library root
//	E2E_RSYNC       rsync 3.x client
//	E2E_SSH         ssh stand-in that runs the scan-capable gate with SSH_ORIGINAL_COMMAND
//	E2E_URL, E2E_USER, E2E_PASSWORD   Subsonic access through the server's scan-filtering proxy
//
// E2E_SSH owns the gate argv: supply its --lock, --scan-job-lock, --nd-url and
// --scan-credentials paths there. The client calls scan-status and scan over
// SSH; E2E_URL is for library/playlist reads, not for starting scans.
//
// Creation times are not readable on Linux, so a file's "birth" is its mtime
// minus a year; the assertions compare against the same function.
func TestRemoteMirrorAcceptance(t *testing.T) {
	if os.Getenv("UDL_NAVIDROME_REMOTE_E2E") != "1" {
		t.Skip("set UDL_NAVIDROME_REMOTE_E2E=1 with a prepared mirror environment")
	}
	env := func(key string) string {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			t.Fatalf("%s must be set", key)
		}
		return value
	}
	music := env("E2E_MUSIC_DIR")
	cfg := DefaultConfig()
	cfg.Mode = ModeRemote
	cfg.Server.URL = env("E2E_URL")
	cfg.Server.Username = env("E2E_USER")
	cfg.Paths.MusicDir = music
	cfg.Paths.DataDir = t.TempDir()
	cfg.Playlists.HardBounceGenres = []string{"HARD BOUNCE"}
	cfg.Remote = &RemoteConfig{SSHTarget: "udl-mirror", RsyncPath: env("E2E_RSYNC"), SSHPath: env("E2E_SSH")}
	normalize(&cfg)
	if problems := validateRemote(cfg); len(problems) != 0 {
		t.Fatalf("remote config problems: %v", problems)
	}
	resolved, err := Resolve(cfg)
	if err != nil {
		t.Fatal(err)
	}
	birth := func(path string) (time.Time, error) {
		info, err := os.Stat(path)
		if err != nil {
			return time.Time{}, err
		}
		return info.ModTime().Add(-365 * 24 * time.Hour).UTC(), nil
	}
	manager := &Manager{
		Config: cfg, Resolved: resolved, BirthTime: birth,
		API: NewClient(cfg.Server.URL, Credentials{Username: cfg.Server.Username, Password: env("E2E_PASSWORD")}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	audio := func() map[string]time.Time {
		manifest, err := BuildBirthTimeManifest(resolved, birth)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]time.Time{}
		for _, entry := range manifest.Entries {
			out[filepath.Join(music, entry.Path)] = time.Unix(0, entry.BirthTimeNS).UTC()
		}
		return out
	}
	assertLibrary := func(step string) {
		t.Helper()
		want := audio()
		songs, err := manager.API.Songs(ctx)
		if err != nil {
			t.Fatalf("%s: list songs: %v", step, err)
		}
		got := map[string]Song{}
		for _, song := range songs {
			got[song.Path] = song
		}
		if len(got) != len(want) {
			t.Fatalf("%s: server has %d tracks, source has %d audio files\nserver: %v", step, len(got), len(want), keys(got))
		}
		for path, created := range want {
			song, ok := got[NormalizePath(path)]
			if !ok {
				t.Fatalf("%s: %s missing on the server (real path not reported?); server has %v", step, path, keys(got))
			}
			if !song.Created.Equal(created.Truncate(time.Second)) && !song.Created.Equal(created) {
				t.Errorf("%s: %s Date Added = %s, want %s", step, path, song.Created, created)
			}
		}
	}

	// 1. Initial push into an empty mirror.
	first, err := manager.SyncMirror(ctx, MirrorOptions{Scan: true, Dates: true})
	if err != nil {
		t.Fatalf("first push: %v (%+v)", err, first)
	}
	t.Logf("first push: changed=%d deleted=%d scanned=%v dates=%+v warnings=%v",
		first.Push.Changed, first.Push.Deleted, first.Scanned, first.Dates, first.Warnings)
	if !first.Scanned || first.Push.Changed == 0 || first.Dates == nil || len(first.Warnings) != 0 {
		t.Fatalf("first push result = %+v", first)
	}
	assertLibrary("after first push")

	// 2. Nothing changed: no scan.
	idle, err := manager.SyncMirror(ctx, MirrorOptions{Scan: true, Dates: true})
	if err != nil || idle.Push.HasChanges() || idle.Scanned {
		t.Fatalf("no-op push = %+v, %v", idle, err)
	}

	// 3. Add, rename, delete on the "Mac", then push again.
	entries := audio()
	var victim, renamed string
	for path := range entries {
		if victim == "" {
			victim = path
		} else if renamed == "" {
			renamed = path
		}
	}
	added := filepath.Join(music, "Added Artist", "New Album", "01 Fresh.mp3")
	if err := os.MkdirAll(filepath.Dir(added), 0o755); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(renamed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(added, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(renamed, renamed+".renamed.mp3"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(victim); err != nil {
		t.Fatal(err)
	}
	second, err := manager.SyncMirror(ctx, MirrorOptions{Scan: true, Dates: true})
	if err != nil {
		t.Fatalf("second push: %v", err)
	}
	t.Logf("second push: changed=%d deleted=%d items=%v dates=%+v", second.Push.Changed, second.Push.Deleted, second.Push.Items, second.Dates)
	if second.Push.Deleted != 2 || second.Push.Changed != 2 || !second.Scanned {
		t.Fatalf("second push = %+v", second)
	}
	assertLibrary("after add/rename/delete")

	// 3b. A move bigger than max_delete is refused before anything changes,
	// then goes through with an explicit allowance.
	album := filepath.Dir(added)
	if err := os.WriteFile(filepath.Join(album, "02 Second.mp3"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.SyncMirror(ctx, MirrorOptions{Scan: true, Dates: true}); err != nil {
		t.Fatalf("push of the second file: %v", err)
	}
	manager.Config.Remote.MaxDelete = 1
	moved := filepath.Join(music, "Moved Away")
	if err := os.Rename(album, moved); err != nil {
		t.Fatal(err)
	}
	refused, err := manager.SyncMirror(ctx, MirrorOptions{Scan: true, Dates: true})
	t.Logf("refused push: planned=%d changed=%d err=%v", refused.Push.PlannedDeletes, refused.Push.Changed, err)
	if err == nil || refused.Push.PlannedDeletes != 2 || refused.Push.Changed != 0 || refused.Scanned {
		t.Fatalf("a move over max_delete must be refused untouched: %+v", refused)
	}
	allowed, err := manager.SyncMirror(ctx, MirrorOptions{Scan: true, Dates: true, AllowDeletes: 2})
	if err != nil || !allowed.Scanned || allowed.Push.Deleted != 2 {
		t.Fatalf("allowed push = %+v, %v", allowed, err)
	}
	manager.Config.Remote.MaxDelete = DefaultRemoteMaxDelete
	assertLibrary("after an allowed move")

	// 4. Managed smart playlists are written locally, pushed, and imported.
	refreshed, err := manager.RefreshPlaylists(ctx)
	if err != nil {
		t.Fatalf("refresh playlists: %v", err)
	}
	t.Logf("refresh: imported=%+v warnings=%v", refreshed.Imported, refreshed.Warnings)
	if len(refreshed.Imported) != len(refreshed.Generated) {
		t.Fatalf("refresh imported %d of %d playlists: %+v", len(refreshed.Imported), len(refreshed.Generated), refreshed)
	}

	// 5. Backups and status through the gate.
	backup, err := manager.CreateBackup(ctx)
	if err != nil || backup.Backup.SizeBytes == 0 {
		t.Fatalf("backup = %+v, %v", backup, err)
	}
	status := manager.Status(ctx)
	t.Logf("status: mode=%s reachable=%v tracks=%d backups=%d problems=%v", status.Mode, status.Reachable, status.LibraryTracks, status.BackupCount, status.Problems)
	if status.Remote == nil || !status.Reachable || status.BackupCount == 0 {
		t.Fatalf("status = %+v", status)
	}
	for _, problem := range status.Problems {
		if strings.Contains(problem, "not rooted") {
			t.Fatalf("status problem: %s", problem)
		}
	}

	// 6. Date Added is already right, so a preview finds nothing to change.
	preview, err := manager.PreviewDateAdded(ctx)
	if err != nil || preview.Candidates != 0 {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
}

func keys(m map[string]Song) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	return out
}
