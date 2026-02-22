package scdl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/config"
)

func setupSCDLTest(t *testing.T) (config.Source, config.Defaults) {
	t.Helper()
	resetRuntimeDetectionForTests()
	t.Cleanup(resetRuntimeDetectionForTests)

	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	stateDir := filepath.Join(tmp, "state")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}

	source := config.Source{
		ID:        "soundcloud-a",
		Type:      config.SourceTypeSoundCloud,
		TargetDir: targetDir,
		URL:       "https://soundcloud.com/user",
		StateFile: "soundcloud-a.sync.scdl",
		Adapter:   config.AdapterSpec{Kind: "scdl"},
		Sync:      config.SyncPolicy{BreakOnExisting: boolPtr(true)},
	}
	defaults := config.Defaults{
		StateDir:    stateDir,
		ArchiveFile: "archive.txt",
	}
	return source, defaults
}

func TestBuildExecSpecRedactsClientID(t *testing.T) {
	t.Setenv("SCDL_CLIENT_ID", "secret-client-id")

	source, defaults := setupSCDLTest(t)
	adapter := New()
	spec, err := adapter.BuildExecSpec(source, defaults, 2*time.Minute)
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

func TestBuildExecSpecIncludesSyncFile(t *testing.T) {
	t.Setenv("SCDL_CLIENT_ID", "secret-client-id")

	source, defaults := setupSCDLTest(t)
	spec, err := New().BuildExecSpec(source, defaults, 2*time.Minute)
	if err != nil {
		t.Fatalf("build exec spec: %v", err)
	}

	syncFilePath, resolveErr := config.ResolveStateFile(defaults.StateDir, source.StateFile)
	if resolveErr != nil {
		t.Fatalf("resolve state file: %v", resolveErr)
	}

	joined := strings.Join(spec.Args, " ")
	if !strings.Contains(joined, "--sync") || !strings.Contains(joined, syncFilePath) {
		t.Fatalf("expected --sync with resolved state file path, got %v", spec.Args)
	}
}

func TestBuildExecSpecDoesNotDuplicateForceFlag(t *testing.T) {
	t.Setenv("SCDL_CLIENT_ID", "secret-client-id")

	source, defaults := setupSCDLTest(t)
	source.Adapter.ExtraArgs = []string{"-f"}
	spec, err := New().BuildExecSpec(source, defaults, 2*time.Minute)
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

	source, defaults := setupSCDLTest(t)
	source.Adapter.ExtraArgs = []string{"-f"}
	spec, err := New().BuildExecSpec(source, defaults, 2*time.Minute)
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
	if !strings.Contains(joined, "--download-archive") {
		t.Fatalf("expected default download archive arg, got %v", spec.Args)
	}
	if !strings.Contains(joined, "--break-on-existing") {
		t.Fatalf("expected break-on-existing default, got %v", spec.Args)
	}
}

func TestBuildExecSpecRespectsCustomYTDLPArgs(t *testing.T) {
	t.Setenv("SCDL_CLIENT_ID", "secret-client-id")

	source, defaults := setupSCDLTest(t)
	source.Adapter.ExtraArgs = []string{
		"-f",
		"--yt-dlp-args",
		"--embed-thumbnail --embed-metadata --download-archive scdl-archive.txt",
	}
	spec, err := New().BuildExecSpec(source, defaults, 2*time.Minute)
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
	joined := strings.Join(spec.Args, " ")
	if !strings.Contains(joined, "--download-archive scdl-archive.txt") {
		t.Fatalf("expected custom ytdlp args to be preserved, got %v", spec.Args)
	}
}

func TestBuildExecSpecAddsArchiveWhenCustomYTDLPArgsMissingIt(t *testing.T) {
	t.Setenv("SCDL_CLIENT_ID", "secret-client-id")

	source, defaults := setupSCDLTest(t)
	source.Adapter.ExtraArgs = []string{
		"-f",
		"--yt-dlp-args",
		"--embed-thumbnail --embed-metadata",
	}

	spec, err := New().BuildExecSpec(source, defaults, 2*time.Minute)
	if err != nil {
		t.Fatalf("build exec spec: %v", err)
	}

	joined := strings.Join(spec.Args, " ")
	if !strings.Contains(joined, "--download-archive") {
		t.Fatalf("expected download archive to be injected for custom ytdlp args, got %v", spec.Args)
	}
}

func TestBuildExecSpecScanModeUsesNoBreakFlag(t *testing.T) {
	t.Setenv("SCDL_CLIENT_ID", "secret-client-id")

	source, defaults := setupSCDLTest(t)
	source.Sync.BreakOnExisting = boolPtr(false)

	spec, err := New().BuildExecSpec(source, defaults, 2*time.Minute)
	if err != nil {
		t.Fatalf("build exec spec: %v", err)
	}

	joined := strings.Join(spec.Args, " ")
	if !strings.Contains(joined, "--no-break-on-existing") {
		t.Fatalf("expected no-break-on-existing in scan mode, got %v", spec.Args)
	}
}

func TestBuildExecSpecPrefersCompatibleBinaryFromPATH(t *testing.T) {
	t.Setenv("SCDL_CLIENT_ID", "secret-client-id")

	source, defaults := setupSCDLTest(t)
	tmp := t.TempDir()
	legacyDir := filepath.Join(tmp, "legacy")
	compatibleDir := filepath.Join(tmp, "compatible")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy: %v", err)
	}
	if err := os.MkdirAll(compatibleDir, 0o755); err != nil {
		t.Fatalf("mkdir compatible: %v", err)
	}
	legacyBin := filepath.Join(legacyDir, "scdl")
	compatibleBin := filepath.Join(compatibleDir, "scdl")
	if err := writeFakeSCDL(legacyBin, false); err != nil {
		t.Fatalf("write legacy scdl: %v", err)
	}
	if err := writeFakeSCDL(compatibleBin, true); err != nil {
		t.Fatalf("write compatible scdl: %v", err)
	}
	t.Setenv("PATH", legacyDir+string(os.PathListSeparator)+compatibleDir)
	resetRuntimeDetectionForTests()

	spec, err := New().BuildExecSpec(source, defaults, 2*time.Minute)
	if err != nil {
		t.Fatalf("build exec spec: %v", err)
	}
	if spec.Bin != compatibleBin {
		t.Fatalf("expected compatible binary %q, got %q", compatibleBin, spec.Bin)
	}
}

func TestBuildExecSpecFailsWhenOnlyLegacyBinaryAvailable(t *testing.T) {
	t.Setenv("SCDL_CLIENT_ID", "secret-client-id")

	source, defaults := setupSCDLTest(t)
	tmp := t.TempDir()
	legacyDir := filepath.Join(tmp, "legacy")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy: %v", err)
	}
	legacyBin := filepath.Join(legacyDir, "scdl")
	if err := writeFakeSCDL(legacyBin, false); err != nil {
		t.Fatalf("write legacy scdl: %v", err)
	}
	t.Setenv("PATH", legacyDir)
	resetRuntimeDetectionForTests()

	_, err := New().BuildExecSpec(source, defaults, 2*time.Minute)
	if err == nil {
		t.Fatalf("expected error for legacy scdl without --yt-dlp-args")
	}
	if !strings.Contains(err.Error(), "does not support --yt-dlp-args") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeFakeSCDL(path string, includeYTDLP bool) error {
	help := "Usage:\\nscdl --version\\n"
	if includeYTDLP {
		help = help + "--yt-dlp-args <argstring>\\n"
	}
	content := "#!/bin/sh\nif [ \"$1\" = \"-h\" ] || [ \"$1\" = \"--help\" ]; then\n  printf \"" + help + "\"\n  exit 0\nfi\nif [ \"$1\" = \"--version\" ]; then\n  echo \"v3.0.1\"\n  exit 0\nfi\nexit 0\n"
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		return err
	}
	return nil
}

func boolPtr(v bool) *bool {
	return &v
}
