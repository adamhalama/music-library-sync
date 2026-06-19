package syncconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/config"
)

func TestLoadMergesProjectFeatureConfig(t *testing.T) {
	tmp := t.TempDir()
	payload := `
version: 1
defaults:
  db_dir: /rb
  backup_dir: /backups
sync:
  folders:
    - id: phone
      music_folder: Phone
      rekordbox_folder: Phone RB
      playlist_name_map:
        Favourites: fav_imports
`
	if err := os.WriteFile(filepath.Join(tmp, ProjectConfigName), []byte(payload), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(LoadOptions{WorkingDir: tmp, BaseConfig: config.DefaultConfig(), Env: map[string]string{}})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	mapping, ok := cfg.FolderMapping("phone")
	if !ok {
		t.Fatalf("expected phone mapping")
	}
	if cfg.Defaults.DBDir != "/rb" || mapping.PlaylistNameMap["Favourites"] != "fav_imports" {
		t.Fatalf("unexpected config: %+v mapping=%+v", cfg, mapping)
	}
}

func TestResolveWritePathPrefersExistingProjectConfig(t *testing.T) {
	tmp := t.TempDir()
	project := filepath.Join(tmp, ProjectConfigName)
	if err := os.WriteFile(project, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	resolved, err := ResolveWritePath(WritePathOptions{WorkingDir: tmp, Env: map[string]string{}})
	if err != nil {
		t.Fatalf("ResolveWritePath: %v", err)
	}
	if resolved.Path != project || resolved.Kind != "project" || !resolved.Exists {
		t.Fatalf("unexpected resolution: %+v", resolved)
	}
}

func TestSaveWritesCanonicalFeatureConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rekordbox.yaml")
	create := true
	cfg := Config{
		Version: Version,
		Defaults: Defaults{
			DBDir:           "/rb",
			BackupDir:       "/backups",
			Mode:            "mirror",
			CreateFolders:   true,
			CreatePlaylists: true,
		},
		Sync: Sync{Folders: []FolderMapping{{
			ID:              "phone",
			MusicFolder:     "Phone",
			MusicFolderID:   "music-folder-id",
			RekordboxFolder: "Phone RB",
			PlaylistNameMap: map[string]string{"Favourites": "fav_imports"},
			CreateFolders:   &create,
		}}},
	}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	text := string(payload)
	for _, want := range []string{
		"version: 1",
		"db_dir: /rb",
		"id: phone",
		"music_folder_id: music-folder-id",
		"Favourites: fav_imports",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in saved config:\n%s", want, text)
		}
	}
	loaded, err := Load(LoadOptions{ExplicitPath: path, BaseConfig: config.DefaultConfig(), Env: map[string]string{}})
	if err != nil {
		t.Fatalf("Load saved config: %v", err)
	}
	if len(loaded.Sync.Folders) != 1 || loaded.Sync.Folders[0].ID != "phone" {
		t.Fatalf("unexpected loaded config: %+v", loaded)
	}
}

func TestLoadTranslatesLegacyInlineJobs(t *testing.T) {
	base := config.DefaultConfig()
	base.Rekordbox = &config.RekordboxConfig{
		PlaylistSync: config.RekordboxPlaylistSyncConfig{Jobs: []config.RekordboxPlaylistSyncJob{{
			ID:                "apple-favourites",
			MusicPlaylist:     "Favourites",
			RekordboxPlaylist: "fav_imports",
		}}},
	}
	cfg, err := Load(LoadOptions{WorkingDir: t.TempDir(), BaseConfig: base, Env: map[string]string{}})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Sync.Jobs) != 1 || cfg.Sync.Jobs[0].ID != "apple-favourites" {
		t.Fatalf("expected legacy job translation, got %+v", cfg.Sync.Jobs)
	}
	if len(cfg.Warnings) == 0 {
		t.Fatalf("expected legacy warning")
	}
}
