package navidrome

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigMatchesPlanDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Version != ConfigVersion {
		t.Fatalf("version = %d, want %d", cfg.Version, ConfigVersion)
	}
	if cfg.Enabled {
		t.Fatalf("navidrome must be opt-in, not enabled by default")
	}
	if cfg.Server.Port != DefaultPort {
		t.Fatalf("port = %d, want %d", cfg.Server.Port, DefaultPort)
	}
	if cfg.Paths.MusicDir != "~/Music/downloaded" {
		t.Fatalf("music dir = %q", cfg.Paths.MusicDir)
	}
	if cfg.Paths.PlaylistsPath != DefaultPlaylistsPath {
		t.Fatalf("playlists path = %q", cfg.Paths.PlaylistsPath)
	}
	if cfg.Backup.Count != DefaultBackupCount {
		t.Fatalf("backup count = %d", cfg.Backup.Count)
	}
}

func TestConfigModelHasNoPasswordField(t *testing.T) {
	payload, err := Marshal(DefaultConfig())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	lower := strings.ToLower(string(payload))
	for _, forbidden := range []string{"password", "secret", "token", "arl"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("rendered config must not mention %q:\n%s", forbidden, payload)
		}
	}
}

func TestLoadMergesExplicitFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "navidrome.yaml")
	body := "" +
		"version: 1\n" +
		"enabled: true\n" +
		"server:\n" +
		"  username: jaa\n" +
		"  port: 4600\n" +
		"paths:\n" +
		"  music_dir: " + dir + "/music\n" +
		"playlists:\n" +
		"  hard_bounce_genres: [\"Hard Bounce\", \"hard bounce\", \" Bounce \"]\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(LoadOptions{ExplicitPath: path, Env: map[string]string{}})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Enabled {
		t.Fatalf("enabled was not merged")
	}
	if cfg.Server.Port != 4600 {
		t.Fatalf("port = %d, want 4600", cfg.Server.Port)
	}
	if cfg.Server.Username != "jaa" {
		t.Fatalf("username = %q", cfg.Server.Username)
	}
	if got, want := cfg.Paths.MusicDir, dir+"/music"; got != want {
		t.Fatalf("music dir = %q, want %q", got, want)
	}
	// Unset values keep the managed defaults.
	if cfg.Backup.Count != DefaultBackupCount {
		t.Fatalf("backup count = %d, want default", cfg.Backup.Count)
	}
	// "Hard Bounce" and "hard bounce" are distinct: Navidrome's genre match is
	// case-sensitive, so both spellings need their own allowlist entry.
	want := []string{"Bounce", "Hard Bounce", "hard bounce"}
	if len(cfg.Playlists.HardBounceGenres) != len(want) {
		t.Fatalf("genres = %v, want %v", cfg.Playlists.HardBounceGenres, want)
	}
	for i, value := range want {
		if cfg.Playlists.HardBounceGenres[i] != value {
			t.Fatalf("genres = %v, want %v", cfg.Playlists.HardBounceGenres, want)
		}
	}
}

func TestLoadMissingExplicitFileFails(t *testing.T) {
	_, err := Load(LoadOptions{ExplicitPath: filepath.Join(t.TempDir(), "nope.yaml"), Env: map[string]string{}})
	if err == nil {
		t.Fatalf("expected an error for a missing explicit config")
	}
}

func TestResolveExpandsPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := DefaultConfig()
	resolved, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !filepath.IsAbs(resolved.MusicDir) || !strings.HasPrefix(resolved.MusicDir, home) {
		t.Fatalf("music dir = %q, want under %q", resolved.MusicDir, home)
	}
	if resolved.PlaylistsDir != filepath.Join(resolved.MusicDir, DefaultPlaylistsPath) {
		t.Fatalf("playlists dir = %q", resolved.PlaylistsDir)
	}
	if filepath.Base(resolved.LaunchAgent) != LaunchAgentLabel+".plist" {
		t.Fatalf("launch agent = %q", resolved.LaunchAgent)
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cases := map[string]func(*Config){
		"navidrome.server.port": func(c *Config) { c.Server.Port = 0 },
		"http://":               func(c *Config) { c.Server.URL = "localhost:4533" },
		"relative to the music directory": func(c *Config) {
			c.Paths.PlaylistsPath = "/tmp/playlists"
		},
		"must not live inside music_dir": func(c *Config) {
			c.Paths.DataDir = "~/Music/downloaded/nd"
			c.Paths.CacheDir = "~/Music/downloaded/nd/cache"
			c.Paths.LogFile = "~/Music/downloaded/nd/log"
			c.Paths.ConfigFile = "~/Music/downloaded/nd/navidrome.toml"
			c.Paths.BackupDir = "~/Music/downloaded/nd/backups"
		},
		"navidrome.version": func(c *Config) { c.Version = 7 },
	}
	for fragment, mutate := range cases {
		cfg := DefaultConfig()
		mutate(&cfg)
		err := Validate(cfg)
		if err == nil {
			t.Fatalf("expected a validation error containing %q", fragment)
		}
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("error %q does not contain %q", err, fragment)
		}
	}
}

func TestValidateAcceptsDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := Validate(DefaultConfig()); err != nil {
		t.Fatalf("Validate(defaults): %v", err)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, "navidrome.yaml")
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Server.Username = "jaa"
	cfg.Playlists.HardBounceGenres = []string{"Hard Bounce"}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat saved config: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
	loaded, err := Load(LoadOptions{ExplicitPath: path, Env: map[string]string{}})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Server.Username != "jaa" || !loaded.Enabled {
		t.Fatalf("round trip lost values: %+v", loaded)
	}
	if len(loaded.Playlists.HardBounceGenres) != 1 {
		t.Fatalf("genres = %v", loaded.Playlists.HardBounceGenres)
	}
}

func TestNormalizeGenresDeduplicatesExactlyAndSorts(t *testing.T) {
	// Case variants are kept as separate entries: Navidrome matches genre
	// case-sensitively, so folding them would drop whichever spelling lost.
	got := NormalizeGenres([]string{" Techno ", "hard bounce", "HARD BOUNCE", "", "Bounce", "Techno"})
	want := []string{"Bounce", "HARD BOUNCE", "hard bounce", "Techno"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if NormalizeGenres(nil) != nil {
		t.Fatalf("empty input must stay nil")
	}
}

func TestValidateRejectsAHiddenPlaylistsPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := DefaultConfig()
	cfg.Paths.PlaylistsPath = ".udl/playlists"
	err := Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "hidden") {
		t.Fatalf("error = %v, want a hidden-path refusal", err)
	}
	// Verified against Navidrome 0.63.2: the scanner walks past hidden folders
	// without a word, so a dot-directory silently imports nothing.
	if !strings.Contains(err.Error(), "never import") {
		t.Fatalf("the refusal must say what would go wrong: %v", err)
	}
}

func TestDefaultPlaylistsPathIsNotHidden(t *testing.T) {
	if hiddenSegment(DefaultPlaylistsPath) != "" {
		t.Fatalf("the default playlists path %q is hidden and would never be scanned", DefaultPlaylistsPath)
	}
}
