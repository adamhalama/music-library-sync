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

func TestDefaultConfigUsesPortableBackupDir(t *testing.T) {
	cfg := defaultConfig(config.DefaultConfig())
	if cfg.Defaults.BackupDir != "~/Music/rb-library-export" {
		t.Fatalf("expected portable backup default, got %q", cfg.Defaults.BackupDir)
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

func TestValidateRejectsInvalidStandaloneConfig(t *testing.T) {
	valid := defaultConfig(config.DefaultConfig())
	valid.Sync.Folders = []FolderMapping{{
		ID:              "phone",
		MusicFolder:     "Phone",
		RekordboxFolder: "Phone RB",
		OnMissingTracks: DefaultMissingTracks,
	}}
	valid.Sync.Jobs = []PlaylistJob{{
		ID:                "favorites",
		RekordboxPlaylist: "fav_imports",
		Mode:              "mirror",
	}}

	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{
			name: "relative database path",
			mutate: func(cfg *Config) {
				cfg.Defaults.DBDir = "relative/rekordbox"
			},
			want: "defaults.db_dir must resolve to an absolute path",
		},
		{
			name: "empty backup path",
			mutate: func(cfg *Config) {
				cfg.Defaults.BackupDir = ""
			},
			want: "defaults.backup_dir must not be empty",
		},
		{
			name: "relative python path",
			mutate: func(cfg *Config) {
				cfg.Defaults.PythonPath = "relative/site-packages"
			},
			want: "defaults.python_path must resolve to an absolute path",
		},
		{
			name: "empty folder source selector",
			mutate: func(cfg *Config) {
				cfg.Sync.Folders[0].MusicFolder = ""
			},
			want: `folder mapping "phone" must set music_folder or music_folder_id`,
		},
		{
			name: "empty folder target selector",
			mutate: func(cfg *Config) {
				cfg.Sync.Folders[0].RekordboxFolder = ""
			},
			want: `folder mapping "phone" must set rekordbox_folder or rekordbox_folder_id`,
		},
		{
			name: "invalid name mapping",
			mutate: func(cfg *Config) {
				cfg.Sync.Folders[0].PlaylistNameMap = map[string]string{"Favourites": ""}
			},
			want: "playlist_name_map entries must have non-empty source and target names",
		},
		{
			name: "duplicate folder id",
			mutate: func(cfg *Config) {
				cfg.Sync.Folders = append(cfg.Sync.Folders, cfg.Sync.Folders[0])
			},
			want: `duplicate Rekordbox sync mapping id "phone"`,
		},
		{
			name: "empty playlist target",
			mutate: func(cfg *Config) {
				cfg.Sync.Jobs[0].RekordboxPlaylist = ""
			},
			want: `playlist job "favorites" must set rekordbox_playlist or rekordbox_playlist_id`,
		},
		{
			name: "duplicate playlist job id",
			mutate: func(cfg *Config) {
				cfg.Sync.Jobs = append(cfg.Sync.Jobs, cfg.Sync.Jobs[0])
			},
			want: `duplicate Rekordbox playlist job id "favorites"`,
		},
		{
			name: "invalid playlist job id",
			mutate: func(cfg *Config) {
				cfg.Sync.Jobs[0].ID = "not valid"
			},
			want: `playlist job "not valid" has invalid id format`,
		},
		{
			name: "unsupported playlist job mode",
			mutate: func(cfg *Config) {
				cfg.Sync.Jobs[0].Mode = "append"
			},
			want: `playlist job "favorites" has unsupported mode "append"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Clone(valid)
			tt.mutate(&cfg)
			err := Validate(cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}
