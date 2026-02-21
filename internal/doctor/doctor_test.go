package doctor

import (
	"context"
	"fmt"
	"testing"

	"github.com/jaa/update-downloads/internal/config"
)

func spotifyConfig() config.Config {
	return config.Config{
		Version: 1,
		Defaults: config.Defaults{
			StateDir:              "/tmp/state",
			ArchiveFile:           "archive.txt",
			Threads:               1,
			ContinueOnError:       true,
			CommandTimeoutSeconds: 900,
		},
		Sources: []config.Source{
			{
				ID:        "spotify-a",
				Type:      config.SourceTypeSpotify,
				Enabled:   true,
				TargetDir: "/tmp/music",
				URL:       "https://open.spotify.com/playlist/a",
				StateFile: "spotify-a.sync.spotdl",
				Adapter:   config.AdapterSpec{Kind: "spotdl"},
			},
		},
	}
}

func TestDoctorMissingBinary(t *testing.T) {
	checker := &Checker{
		LookPath:      func(name string) (string, error) { return "", fmt.Errorf("not found") },
		ReadVersion:   func(ctx context.Context, binary string) (string, error) { return "spotdl 4.5.0", nil },
		Getenv:        func(key string) string { return "" },
		CheckWritable: func(path string) error { return nil },
	}

	report := checker.Check(context.Background(), spotifyConfig())
	if !report.HasErrors() {
		t.Fatalf("expected doctor errors for missing binary")
	}
}

func TestDoctorBadVersion(t *testing.T) {
	checker := &Checker{
		LookPath:      func(name string) (string, error) { return "/usr/bin/" + name, nil },
		ReadVersion:   func(ctx context.Context, binary string) (string, error) { return "spotdl 3.9.0", nil },
		Getenv:        func(key string) string { return "" },
		CheckWritable: func(path string) error { return nil },
	}

	report := checker.Check(context.Background(), spotifyConfig())
	if !report.HasErrors() {
		t.Fatalf("expected version incompatibility error")
	}
}

func TestDoctorMissingSoundCloudEnv(t *testing.T) {
	cfg := config.Config{
		Version: 1,
		Defaults: config.Defaults{
			StateDir:              "/tmp/state",
			ArchiveFile:           "archive.txt",
			Threads:               1,
			ContinueOnError:       true,
			CommandTimeoutSeconds: 900,
		},
		Sources: []config.Source{
			{
				ID:        "sc-a",
				Type:      config.SourceTypeSoundCloud,
				Enabled:   true,
				TargetDir: "/tmp/music",
				URL:       "https://soundcloud.com/user",
				Adapter:   config.AdapterSpec{Kind: "scdl"},
			},
		},
	}

	checker := &Checker{
		LookPath:      func(name string) (string, error) { return "/usr/bin/" + name, nil },
		ReadVersion:   func(ctx context.Context, binary string) (string, error) { return "scdl 3.0.0", nil },
		Getenv:        func(key string) string { return "" },
		CheckWritable: func(path string) error { return nil },
	}

	report := checker.Check(context.Background(), cfg)
	if !report.HasErrors() {
		t.Fatalf("expected auth error when SCDL_CLIENT_ID is missing")
	}
}

func TestDoctorUnwritableDirectory(t *testing.T) {
	checker := &Checker{
		LookPath:      func(name string) (string, error) { return "/usr/bin/" + name, nil },
		ReadVersion:   func(ctx context.Context, binary string) (string, error) { return "spotdl 4.5.0", nil },
		Getenv:        func(key string) string { return "" },
		CheckWritable: func(path string) error { return fmt.Errorf("permission denied") },
	}

	report := checker.Check(context.Background(), spotifyConfig())
	if !report.HasErrors() {
		t.Fatalf("expected filesystem error for unwritable path")
	}
}
