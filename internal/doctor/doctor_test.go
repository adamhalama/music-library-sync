package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
				StateFile: "sc-a.sync.scdl",
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

func TestDoctorRequiresYTDLPForSoundCloud(t *testing.T) {
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
				StateFile: "sc-a.sync.scdl",
				Adapter:   config.AdapterSpec{Kind: "scdl"},
			},
		},
	}

	checker := &Checker{
		LookPath: func(name string) (string, error) {
			if name == "yt-dlp" {
				return "", fmt.Errorf("not found")
			}
			return "/usr/bin/" + name, nil
		},
		ReadVersion:   func(ctx context.Context, binary string) (string, error) { return "3.0.0", nil },
		Getenv:        func(key string) string { return "set" },
		CheckWritable: func(path string) error { return nil },
	}

	report := checker.Check(context.Background(), cfg)
	if !report.HasErrors() {
		t.Fatalf("expected missing yt-dlp dependency to be reported")
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

func TestDoctorWarnsOnSharedSpotDLCredentials(t *testing.T) {
	checker := &Checker{
		LookPath:      func(name string) (string, error) { return "/usr/bin/" + name, nil },
		ReadVersion:   func(ctx context.Context, binary string) (string, error) { return "spotdl 4.5.0", nil },
		Getenv:        func(key string) string { return "" },
		CheckWritable: func(path string) error { return nil },
		HomeDir:       func() (string, error) { return "/home/tester", nil },
		ReadFile: func(path string) ([]byte, error) {
			if path != "/home/tester/.spotdl/config.json" {
				return nil, fmt.Errorf("unexpected path: %s", path)
			}
			return []byte(`{"client_id":"5f573c9620494bae87890c0f08a60293","client_secret":"212476d9b0f3472eaa762d90b19b0ba8"}`), nil
		},
	}

	report := checker.Check(context.Background(), spotifyConfig())
	if report.HasErrors() {
		t.Fatalf("did not expect doctor errors")
	}
	if !hasWarnContaining(report, "shared default Spotify credentials") {
		t.Fatalf("expected warning for shared spotdl credentials, got %+v", report.Checks)
	}
}

func TestDoctorSkipsSharedCredentialWarningForCustomCredentials(t *testing.T) {
	checker := &Checker{
		LookPath:      func(name string) (string, error) { return "/usr/bin/" + name, nil },
		ReadVersion:   func(ctx context.Context, binary string) (string, error) { return "spotdl 4.5.0", nil },
		Getenv:        func(key string) string { return "" },
		CheckWritable: func(path string) error { return nil },
		HomeDir:       func() (string, error) { return "/home/tester", nil },
		ReadFile: func(path string) ([]byte, error) {
			return []byte(`{"client_id":"custom-client","client_secret":"custom-secret"}`), nil
		},
	}

	report := checker.Check(context.Background(), spotifyConfig())
	if hasWarnContaining(report, "shared default Spotify credentials") {
		t.Fatalf("unexpected shared credential warning, got %+v", report.Checks)
	}
}

func hasWarnContaining(report Report, snippet string) bool {
	for _, check := range report.Checks {
		if check.Severity != SeverityWarn {
			continue
		}
		if strings.Contains(check.Message, snippet) {
			return true
		}
	}
	return false
}

func TestResolveSpotDLBinaryForDoctorPrefersOverride(t *testing.T) {
	t.Setenv("UDL_SPOTDL_BIN", "/custom/spotdl")
	if got := resolveSpotDLBinaryForDoctor(); got != "/custom/spotdl" {
		t.Fatalf("expected override, got %q", got)
	}
}

func TestResolveSpotDLBinaryForDoctorPrefersManagedVenv(t *testing.T) {
	t.Setenv("UDL_SPOTDL_BIN", "")
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	managed := filepath.Join(tmpHome, ".venvs", "udl-spotdl", "bin", "spotdl")
	if err := os.MkdirAll(filepath.Dir(managed), 0o755); err != nil {
		t.Fatalf("mkdir managed dir: %v", err)
	}
	if err := os.WriteFile(managed, []byte("#!/bin/sh\necho test\n"), 0o755); err != nil {
		t.Fatalf("write managed binary: %v", err)
	}

	if got := resolveSpotDLBinaryForDoctor(); got != managed {
		t.Fatalf("expected managed path %q, got %q", managed, got)
	}
}
