package playlists

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadProjectConfigOverridesUserConfig(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	userPath := filepath.Join(home, "udl", "playlists.yaml")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte("version: 1\nplaylists:\n  - id: user\n    name: User\n    provider: apple_music\n    provider_playlist: User\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ProjectConfigPath(project), []byte("version: 1\nplaylists:\n  - id: favorites\n    name: Favorites\n    provider: apple_music\n    provider_playlist: Favourites\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(LoadOptions{WorkingDir: project, Env: map[string]string{}})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Playlists) != 1 || cfg.Playlists[0].ID != "favorites" {
		t.Fatalf("unexpected playlists: %#v", cfg.Playlists)
	}
}

func TestValidateRejectsDuplicateIDs(t *testing.T) {
	err := Validate(Config{Version: ConfigVersion, Playlists: []Definition{
		{ID: "favorites", Name: "Favorites", Provider: ProviderAppleMusic, ProviderPlaylist: "Favourites"},
		{ID: "favorites", Name: "Other", Provider: ProviderAppleMusic, ProviderPlaylist: "Other"},
	}})
	if err == nil {
		t.Fatal("expected duplicate ID validation error")
	}
}

func TestSyncJobsLoadNormalizeAndRoundTripWithoutChangingVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ProjectConfigName)
	payload := `version: 1
playlists:
  - id: favorites
    name: Favorites
    provider: apple_music
    provider_playlist: Favourites
sync_jobs:
  - id: " favs-august "
    rekordbox:
      playlist: " favs_august "
    navidrome:
      playlist: " favs_august "
      playlist_id: " nd-1 "
`
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(LoadOptions{ExplicitPath: path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Version != ConfigVersion || len(cfg.SyncJobs) != 1 {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	job := cfg.SyncJobs[0]
	if job.ID != "favs-august" || job.Rekordbox.Playlist != "favs_august" || job.Navidrome.PlaylistID != "nd-1" {
		t.Fatalf("sync job was not normalized: %#v", job)
	}
	encoded, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "sync_jobs:") || !strings.Contains(string(encoded), "playlist_id: nd-1") {
		t.Fatalf("sync job did not round-trip:\n%s", encoded)
	}
}

func TestPairedFavsAugustFixture(t *testing.T) {
	cfg, err := Load(LoadOptions{ExplicitPath: filepath.Join("testdata", "paired_favs_august.yaml")})
	if err != nil {
		t.Fatalf("Load fixture: %v", err)
	}
	job, ok := cfg.SyncJob("favs-august")
	if !ok || job.Rekordbox.PlaylistID != "rb-favs-august" || job.Navidrome.PlaylistID != "nd-favs-august" {
		t.Fatalf("unexpected fixture job: %#v, found=%v", job, ok)
	}
}

func TestExistingConfigDoesNotEmitSyncJobs(t *testing.T) {
	cfg := Config{Version: ConfigVersion, Playlists: []Definition{{
		ID: "favorites", Name: "Favorites", Provider: ProviderAppleMusic, ProviderPlaylist: "Favourites",
	}}}
	payload, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "sync_jobs") {
		t.Fatalf("legacy config gained a sync_jobs field:\n%s", payload)
	}
	jsonPayload, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(jsonPayload), "sync_jobs") {
		t.Fatalf("legacy config JSON gained a sync_jobs field: %s", jsonPayload)
	}
}

func TestValidateSyncJobsReportsFieldPathsAndCaseCollisions(t *testing.T) {
	err := Validate(Config{Version: ConfigVersion, SyncJobs: []SyncJob{
		{ID: "FAVS", Rekordbox: PlaylistSelector{Playlist: "rb"}, Navidrome: PlaylistSelector{Playlist: "nd"}},
		{ID: "favs", Rekordbox: PlaylistSelector{}, Navidrome: PlaylistSelector{}},
		{ID: "unsafe/id", Rekordbox: PlaylistSelector{Playlist: "rb"}, Navidrome: PlaylistSelector{Playlist: "nd"}},
	}})
	if err == nil {
		t.Fatal("expected validation failure")
	}
	for _, want := range []string{
		"sync_jobs[1].id", "collides", "sync_jobs[1].rekordbox.playlist",
		"sync_jobs[1].navidrome.playlist", "sync_jobs[2].id has invalid format",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

func TestPairStateRoundTripAndConfigInvalidation(t *testing.T) {
	dir := t.TempDir()
	job := SyncJob{
		ID:        "favs-august",
		Rekordbox: PlaylistSelector{Playlist: "favs_august"},
		Navidrome: PlaylistSelector{Playlist: "favs_august"},
	}
	fingerprint, err := SyncJobFingerprint(job)
	if err != nil {
		t.Fatal(err)
	}
	state := PairState{
		JobID: job.ID, ConfigFingerprint: fingerprint,
		Rekordbox:     ProviderBinding{PlaylistID: "rb-1", PlaylistName: "renamed in rekordbox"},
		Navidrome:     ProviderBinding{PlaylistID: "nd-1", PlaylistName: "renamed in navidrome"},
		LastDirection: DirectionRekordboxToNavidrome, LastVerifiedAt: time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC),
		FinalPathChecksum: "paths-sha256",
	}
	path, err := WritePairState(dir, state)
	if err != nil {
		t.Fatalf("WritePairState: %v", err)
	}
	if want := filepath.Join(dir, "playlist-sync", "jobs", "favs-august.json"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state permissions = %o, want 600", info.Mode().Perm())
	}
	loaded, err := LoadPairState(dir, job.ID)
	if err != nil {
		t.Fatalf("LoadPairState: %v", err)
	}
	if trusted, ok, err := PairStateForJob(loaded, job); err != nil || !ok || trusted.Navidrome.PlaylistID != "nd-1" {
		t.Fatalf("expected trusted renamed binding, got ok=%v state=%#v err=%v", ok, trusted, err)
	}
	changed := job
	changed.Navidrome.Playlist = "different"
	if _, ok, err := PairStateForJob(loaded, changed); err != nil || ok {
		t.Fatalf("changed selector must invalidate binding, ok=%v err=%v", ok, err)
	}
}

func TestPairStateMissingAndCorruptAreDistinct(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadPairState(dir, "missing"); !os.IsNotExist(err) {
		t.Fatalf("missing state error = %v, want os.ErrNotExist", err)
	}
	path, err := PairStatePath(dir, "corrupt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPairState(dir, "corrupt"); err == nil || !strings.Contains(err.Error(), "parse playlist sync state") {
		t.Fatalf("corrupt state error = %v", err)
	}
}

func TestPairStateRejectsUnsafePathAndBadChecksum(t *testing.T) {
	if _, err := PairStatePath(t.TempDir(), "../escape"); err == nil {
		t.Fatal("expected unsafe ID error")
	}
	job := SyncJob{ID: "job", Rekordbox: PlaylistSelector{Playlist: "rb"}, Navidrome: PlaylistSelector{Playlist: "nd"}}
	fingerprint, err := SyncJobFingerprint(job)
	if err != nil {
		t.Fatal(err)
	}
	state := PairState{
		Version: PairStateVersion, JobID: "job", ConfigFingerprint: fingerprint,
		Rekordbox:     ProviderBinding{PlaylistID: "rb-1", PlaylistName: "rb"},
		Navidrome:     ProviderBinding{PlaylistID: "nd-1", PlaylistName: "nd"},
		LastDirection: DirectionNavidromeToRekordbox, LastVerifiedAt: time.Now().UTC(),
		FinalPathChecksum: "paths", ChecksumSHA256: "tampered",
	}
	if err := ValidatePairState(state); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("bad checksum error = %v", err)
	}
}

func TestFailedPairStateReplacementPreservesPreviousState(t *testing.T) {
	dir := t.TempDir()
	job := SyncJob{ID: "job", Rekordbox: PlaylistSelector{Playlist: "rb"}, Navidrome: PlaylistSelector{Playlist: "nd"}}
	fingerprint, err := SyncJobFingerprint(job)
	if err != nil {
		t.Fatal(err)
	}
	valid := PairState{
		JobID: "job", ConfigFingerprint: fingerprint,
		Rekordbox:      ProviderBinding{PlaylistID: "rb-1", PlaylistName: "rb"},
		Navidrome:      ProviderBinding{PlaylistID: "nd-1", PlaylistName: "nd"},
		LastDirection:  DirectionRekordboxToNavidrome,
		LastVerifiedAt: time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC), FinalPathChecksum: "first",
	}
	if _, err := WritePairState(dir, valid); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.ConfigFingerprint = ""
	invalid.FinalPathChecksum = "replacement"
	if _, err := WritePairState(dir, invalid); err == nil {
		t.Fatal("expected invalid replacement to fail")
	}
	loaded, err := LoadPairState(dir, "job")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.FinalPathChecksum != "first" {
		t.Fatalf("failed replacement changed prior state: %#v", loaded)
	}
}

func TestLoadPairStateRejectsTamperedChecksum(t *testing.T) {
	dir := t.TempDir()
	job := SyncJob{ID: "job", Rekordbox: PlaylistSelector{Playlist: "rb"}, Navidrome: PlaylistSelector{Playlist: "nd"}}
	fingerprint, err := SyncJobFingerprint(job)
	if err != nil {
		t.Fatal(err)
	}
	state := PairState{
		JobID: "job", ConfigFingerprint: fingerprint,
		Rekordbox:     ProviderBinding{PlaylistID: "rb-1", PlaylistName: "rb"},
		Navidrome:     ProviderBinding{PlaylistID: "nd-1", PlaylistName: "nd"},
		LastDirection: DirectionRekordboxToNavidrome, LastVerifiedAt: time.Now().UTC(), FinalPathChecksum: "paths",
	}
	path, err := WritePairState(dir, state)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	payload = []byte(strings.Replace(string(payload), `"final_path_checksum": "paths"`, `"final_path_checksum": "tampered"`, 1))
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPairState(dir, "job"); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("tampered state error = %v", err)
	}
}
