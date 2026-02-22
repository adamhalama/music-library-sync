package deemix

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/config"
)

func setupDeemixSource(t *testing.T) (config.Source, config.Defaults) {
	t.Helper()
	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	stateDir := filepath.Join(tmp, "state")
	runtimeDir := filepath.Join(tmp, "runtime")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}

	source := config.Source{
		ID:               "spotify-source",
		Type:             config.SourceTypeSpotify,
		TargetDir:        targetDir,
		URL:              "https://open.spotify.com/track/abc123?si=redacted",
		StateFile:        "spotify-source.sync.spotify",
		DeemixRuntimeDir: runtimeDir,
		Adapter:          config.AdapterSpec{Kind: "deemix"},
	}
	defaults := config.Defaults{
		StateDir:    stateDir,
		ArchiveFile: "archive.txt",
	}
	return source, defaults
}

func TestBuildExecSpec(t *testing.T) {
	source, defaults := setupDeemixSource(t)

	spec, err := New().BuildExecSpec(source, defaults, 2*time.Minute)
	if err != nil {
		t.Fatalf("build exec spec: %v", err)
	}
	if spec.Bin != "deemix" {
		t.Fatalf("expected deemix binary, got %q", spec.Bin)
	}
	if spec.Dir != source.DeemixRuntimeDir {
		t.Fatalf("expected runtime dir %q, got %q", source.DeemixRuntimeDir, spec.Dir)
	}

	joined := strings.Join(spec.Args, " ")
	if !strings.Contains(joined, "https://open.spotify.com/track/abc123?si=redacted") {
		t.Fatalf("expected source url in args, got %v", spec.Args)
	}
	if !strings.Contains(joined, "--path "+source.TargetDir) {
		t.Fatalf("expected --path arg, got %v", spec.Args)
	}
	if !strings.Contains(joined, "--portable") {
		t.Fatalf("expected --portable arg, got %v", spec.Args)
	}
	if strings.Contains(spec.DisplayCommand, "?si=") {
		t.Fatalf("expected sanitized URL in display command, got %q", spec.DisplayCommand)
	}
}

func TestBuildExecSpecRespectsUserPathFlags(t *testing.T) {
	source, defaults := setupDeemixSource(t)
	source.Adapter.ExtraArgs = []string{"--portable", "--path", "/custom/path"}

	spec, err := New().BuildExecSpec(source, defaults, 2*time.Minute)
	if err != nil {
		t.Fatalf("build exec spec: %v", err)
	}
	joined := strings.Join(spec.Args, " ")
	if strings.Count(joined, "--portable") != 1 {
		t.Fatalf("expected single --portable flag, got %v", spec.Args)
	}
	if strings.Count(joined, "--path") != 1 {
		t.Fatalf("expected single --path flag, got %v", spec.Args)
	}
}

func TestBuildExecSpecCreatesRuntimeDirWhenUnset(t *testing.T) {
	source, defaults := setupDeemixSource(t)
	source.DeemixRuntimeDir = ""
	source.DeezerARL = "arl-value"
	source.SpotifyClientID = "client-id"
	source.SpotifyClientSecret = "client-secret"

	spec, err := New().BuildExecSpec(source, defaults, 2*time.Minute)
	if err != nil {
		t.Fatalf("build exec spec: %v", err)
	}
	if strings.TrimSpace(spec.Dir) == "" {
		t.Fatalf("expected auto-created runtime dir")
	}
	defer CleanupRuntimeConfig(spec.Dir)
}

func TestResolveBinaryOverride(t *testing.T) {
	t.Setenv("UDL_DEEMIX_BIN", "/custom/deemix")
	if got := New().Binary(); got != "/custom/deemix" {
		t.Fatalf("expected override binary, got %q", got)
	}
}

func TestValidate(t *testing.T) {
	source, _ := setupDeemixSource(t)
	if err := New().Validate(source); err != nil {
		t.Fatalf("validate: %v", err)
	}

	source.Type = config.SourceTypeSoundCloud
	if err := New().Validate(source); err == nil {
		t.Fatalf("expected type validation error")
	}
}

func TestPrepareRuntimeConfig(t *testing.T) {
	source, _ := setupDeemixSource(t)
	source.DeezerARL = "arl-value"
	source.SpotifyClientID = "client-id"
	source.SpotifyClientSecret = "client-secret"

	runtimeDir, err := PrepareRuntimeConfig(source)
	if err != nil {
		t.Fatalf("prepare runtime config: %v", err)
	}
	defer CleanupRuntimeConfig(runtimeDir)

	arlPath := filepath.Join(runtimeDir, "config", ".arl")
	arlPayload, err := os.ReadFile(arlPath)
	if err != nil {
		t.Fatalf("read .arl: %v", err)
	}
	if string(arlPayload) != "arl-value" {
		t.Fatalf("unexpected .arl payload %q", string(arlPayload))
	}

	spotifyPath := filepath.Join(runtimeDir, "config", "spotify", "config.json")
	spotifyPayload, err := os.ReadFile(spotifyPath)
	if err != nil {
		t.Fatalf("read spotify config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(spotifyPayload, &cfg); err != nil {
		t.Fatalf("decode spotify config: %v", err)
	}
	if cfg["clientId"] != "client-id" || cfg["clientSecret"] != "client-secret" {
		t.Fatalf("unexpected spotify config: %+v", cfg)
	}
}
