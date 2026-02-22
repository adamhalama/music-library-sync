package engine

import (
	"bytes"
	"context"
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
