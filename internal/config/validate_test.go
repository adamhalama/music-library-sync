package config

import "testing"

func TestValidateSuccess(t *testing.T) {
	cfg := Config{
		Version: 1,
		Defaults: Defaults{
			StateDir:              "/tmp/udl-state",
			ArchiveFile:           "archive.txt",
			Threads:               1,
			ContinueOnError:       true,
			CommandTimeoutSeconds: 900,
		},
		Sources: []Source{
			{
				ID:        "spotify-groove",
				Type:      SourceTypeSpotify,
				Enabled:   true,
				TargetDir: "/tmp/music",
				URL:       "https://open.spotify.com/playlist/abc",
				StateFile: "groove.sync.spotify",
				Adapter:   AdapterSpec{Kind: "deemix"},
				Sync: SyncPolicy{
					BreakOnExisting: testBoolPtr(true),
					AskOnExisting:   testBoolPtr(false),
				},
			},
			{
				ID:        "soundcloud-likes",
				Type:      SourceTypeSoundCloud,
				Enabled:   true,
				TargetDir: "/tmp/music-sc",
				URL:       "https://soundcloud.com/user",
				StateFile: "soundcloud-likes.sync.scdl",
				Sync: SyncPolicy{
					BreakOnExisting: testBoolPtr(true),
					AskOnExisting:   testBoolPtr(false),
					LocalIndexCache: testBoolPtr(true),
				},
				Adapter: AdapterSpec{Kind: "scdl-freedl"},
			},
		},
	}

	if err := Validate(cfg); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
}

func TestValidateFailure(t *testing.T) {
	cfg := Config{
		Version: 2,
		Defaults: Defaults{
			StateDir:              "relative/state",
			ArchiveFile:           "",
			Threads:               0,
			ContinueOnError:       true,
			CommandTimeoutSeconds: 0,
		},
		Sources: []Source{
			{
				ID:        "bad id",
				Type:      "unsupported",
				Enabled:   true,
				TargetDir: "relative/target",
				URL:       "notaurl",
				Adapter:   AdapterSpec{},
			},
			{
				ID:        "bad id",
				Type:      SourceTypeSpotify,
				Enabled:   true,
				TargetDir: "/tmp/music",
				URL:       "https://open.spotify.com/playlist/abc",
				StateFile: "",
				Adapter:   AdapterSpec{},
			},
			{
				ID:        "spotify-with-sync",
				Type:      SourceTypeSpotify,
				Enabled:   true,
				TargetDir: "/tmp/music-2",
				URL:       "https://open.spotify.com/playlist/xyz",
				StateFile: "x.sync.spotify",
				Sync: SyncPolicy{
					BreakOnExisting: testBoolPtr(true),
					LocalIndexCache: testBoolPtr(true),
				},
				Adapter: AdapterSpec{Kind: "spotdl"},
			},
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	validationErr, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if len(validationErr.Problems) < 5 {
		t.Fatalf("expected multiple problems, got %v", validationErr.Problems)
	}
}

// Validate covers the rekordbox block too, so a malformed one is reported at
// load instead of surfacing only when a rekordbox command runs.
func TestValidateRejectsInvalidRekordboxBlock(t *testing.T) {
	cfg := validRekordboxConfigForTest()
	cfg.Rekordbox.DBDir = "relative/rekordbox"
	cfg.Rekordbox.PlaylistSync = RekordboxPlaylistSyncConfig{Jobs: []RekordboxPlaylistSyncJob{
		{ID: "favourites"},
		{ID: "favourites"},
		{ID: "other", Mode: "two-way"},
	}}

	err := Validate(cfg)
	if err == nil {
		t.Fatalf("expected validation error for invalid rekordbox block")
	}
	validationErr, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	wants := []string{
		"rekordbox.db_dir must resolve to an absolute path",
		`duplicate rekordbox playlist sync job id "favourites"`,
		`rekordbox playlist sync job "other" has unsupported mode "two-way"`,
	}
	for _, want := range wants {
		found := false
		for _, problem := range validationErr.Problems {
			if problem == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected problem %q, got %v", want, validationErr.Problems)
		}
	}
}

func TestValidateAcceptsValidRekordboxBlock(t *testing.T) {
	cfg := validRekordboxConfigForTest()
	if err := Validate(cfg); err != nil {
		t.Fatalf("expected valid config with rekordbox block, got %v", err)
	}
}

// ValidateRekordbox stays usable without any download sources, so rekordbox
// commands work on a config that only configures rekordbox.
func TestValidateRekordboxIgnoresMissingSources(t *testing.T) {
	cfg := validRekordboxConfigForTest()
	cfg.Sources = nil
	if err := ValidateRekordbox(cfg); err != nil {
		t.Fatalf("expected rekordbox-only validation to pass, got %v", err)
	}
	if err := Validate(cfg); err == nil {
		t.Fatalf("expected full validation to still require a source")
	}
}

func validRekordboxConfigForTest() Config {
	return Config{
		Version: 1,
		Defaults: Defaults{
			StateDir:              "/tmp/udl-state",
			ArchiveFile:           "archive.txt",
			Threads:               1,
			CommandTimeoutSeconds: 900,
		},
		Sources: []Source{{
			ID:        "soundcloud-likes",
			Type:      SourceTypeSoundCloud,
			Enabled:   true,
			TargetDir: "/tmp/music-sc",
			URL:       "https://soundcloud.com/user",
			StateFile: "soundcloud-likes.sync.scdl",
			Adapter:   AdapterSpec{Kind: "scdl"},
		}},
		Rekordbox: &RekordboxConfig{
			DBDir:     "/Users/test/Library/Pioneer/rekordbox",
			BackupDir: "/Users/test/Music/rb-library-export",
		},
	}
}

func testBoolPtr(v bool) *bool {
	return &v
}
