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
				StateFile: "groove.sync.spotdl",
				Adapter:   AdapterSpec{Kind: "spotdl"},
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
				Adapter:   AdapterSpec{Kind: "spotdl"},
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
