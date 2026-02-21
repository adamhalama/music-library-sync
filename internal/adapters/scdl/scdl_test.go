package scdl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/config"
)

func TestBuildExecSpecRedactsClientID(t *testing.T) {
	t.Setenv("SCDL_CLIENT_ID", "secret-client-id")
	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	adapter := New()
	spec, err := adapter.BuildExecSpec(config.Source{
		ID:        "soundcloud-a",
		Type:      config.SourceTypeSoundCloud,
		TargetDir: targetDir,
		URL:       "https://soundcloud.com/user",
		Adapter:   config.AdapterSpec{Kind: "scdl"},
	}, config.Defaults{}, 2*time.Minute)
	if err != nil {
		t.Fatalf("build exec spec: %v", err)
	}

	if !strings.Contains(strings.Join(spec.Args, " "), "secret-client-id") {
		t.Fatalf("expected real client id in executable args")
	}
	if strings.Contains(spec.DisplayCommand, "secret-client-id") {
		t.Fatalf("expected redacted display command, got %s", spec.DisplayCommand)
	}
}

func TestBuildExecSpecDoesNotDuplicateForceFlag(t *testing.T) {
	t.Setenv("SCDL_CLIENT_ID", "secret-client-id")
	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	adapter := New()
	spec, err := adapter.BuildExecSpec(config.Source{
		ID:        "soundcloud-b",
		Type:      config.SourceTypeSoundCloud,
		TargetDir: targetDir,
		URL:       "https://soundcloud.com/user",
		Adapter:   config.AdapterSpec{Kind: "scdl", ExtraArgs: []string{"-f"}},
	}, config.Defaults{}, 2*time.Minute)
	if err != nil {
		t.Fatalf("build exec spec: %v", err)
	}

	count := 0
	for _, arg := range spec.Args {
		if arg == "-f" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one -f flag, got %d in %v", count, spec.Args)
	}
}

func TestBuildExecSpecAddsDefaultYTDLPArgs(t *testing.T) {
	t.Setenv("SCDL_CLIENT_ID", "secret-client-id")
	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	adapter := New()
	spec, err := adapter.BuildExecSpec(config.Source{
		ID:        "soundcloud-c",
		Type:      config.SourceTypeSoundCloud,
		TargetDir: targetDir,
		URL:       "https://soundcloud.com/user",
		Adapter:   config.AdapterSpec{Kind: "scdl", ExtraArgs: []string{"-f"}},
	}, config.Defaults{}, 2*time.Minute)
	if err != nil {
		t.Fatalf("build exec spec: %v", err)
	}

	joined := strings.Join(spec.Args, " ")
	if !strings.Contains(joined, "--yt-dlp-args") {
		t.Fatalf("expected --yt-dlp-args in args, got %v", spec.Args)
	}
	if !strings.Contains(joined, "--embed-thumbnail") {
		t.Fatalf("expected default thumbnail embedding args, got %v", spec.Args)
	}
}

func TestBuildExecSpecRespectsCustomYTDLPArgs(t *testing.T) {
	t.Setenv("SCDL_CLIENT_ID", "secret-client-id")
	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	adapter := New()
	spec, err := adapter.BuildExecSpec(config.Source{
		ID:        "soundcloud-d",
		Type:      config.SourceTypeSoundCloud,
		TargetDir: targetDir,
		URL:       "https://soundcloud.com/user",
		Adapter: config.AdapterSpec{
			Kind:      "scdl",
			ExtraArgs: []string{"-f", "--yt-dlp-args", "--embed-thumbnail --embed-metadata --download-archive scdl-archive.txt"},
		},
	}, config.Defaults{}, 2*time.Minute)
	if err != nil {
		t.Fatalf("build exec spec: %v", err)
	}

	count := 0
	for _, arg := range spec.Args {
		if arg == "--yt-dlp-args" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one --yt-dlp-args occurrence, got %d in %v", count, spec.Args)
	}
}
