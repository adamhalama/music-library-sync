package playlists

import (
	"os"
	"path/filepath"
	"testing"
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
