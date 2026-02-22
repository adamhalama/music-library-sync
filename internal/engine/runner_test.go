package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSubprocessRunnerPassesStdinToCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test is POSIX-specific")
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	runner := NewSubprocessRunner(strings.NewReader("hello-from-stdin\n"), &stdout, &stderr)
	result := runner.Run(context.Background(), ExecSpec{
		Bin:     "sh",
		Args:    []string{"-c", "read line; echo \"$line\""},
		Dir:     ".",
		Timeout: 0,
	})

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", result.ExitCode, stderr.String())
	}
	got := strings.TrimSpace(stdout.String())
	if got != "hello-from-stdin" {
		t.Fatalf("expected stdin passthrough output, got %q", got)
	}
}

func TestParseSpotifyRateLimitRetryAfterSeconds(t *testing.T) {
	seconds, ok := parseSpotifyRateLimitRetryAfterSeconds("Your application has reached a rate/request limit. Retry will occur after: 71747 s")
	if !ok {
		t.Fatalf("expected parser to match rate-limit line")
	}
	if seconds != 71747 {
		t.Fatalf("expected parsed seconds to be 71747, got %d", seconds)
	}
}

func TestSubprocessRunnerAbortsSpotDLOnLargeRateLimitRetryWindow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test is POSIX-specific")
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	runner := NewSubprocessRunner(strings.NewReader(""), &stdout, &stderr)
	tmp := t.TempDir()
	scriptPath := filepath.Join(tmp, "spotdl")
	script := "#!/bin/sh\n" +
		"echo 'Your application has reached a rate/request limit. Retry will occur after: 600 s' >&2\n" +
		"sleep 1\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	result := runner.Run(context.Background(), ExecSpec{
		Bin:     scriptPath,
		Args:    nil,
		Dir:     ".",
		Timeout: 0,
	})

	if result.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code when rate-limit guard aborts process")
	}
	if result.Interrupted {
		t.Fatalf("expected rate-limit abort to not be marked as interrupted")
	}
	if !strings.Contains(result.StderrTail, "rate/request limit") {
		t.Fatalf("expected stderr tail to include rate-limit marker, got %q", result.StderrTail)
	}
}
