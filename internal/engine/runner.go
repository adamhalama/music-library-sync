package engine

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"time"
)

type ExecRunner interface {
	Run(ctx context.Context, spec ExecSpec) ExecResult
}

type SubprocessRunner struct {
	Stdout io.Writer
	Stderr io.Writer
}

func NewSubprocessRunner(stdout, stderr io.Writer) *SubprocessRunner {
	return &SubprocessRunner{Stdout: stdout, Stderr: stderr}
}

func (r *SubprocessRunner) Run(ctx context.Context, spec ExecSpec) ExecResult {
	start := time.Now()
	if spec.Bin == "" {
		return ExecResult{ExitCode: 1, Duration: time.Since(start), Err: errors.New("missing binary")}
	}

	runCtx := ctx
	cancel := func() {}
	if spec.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, spec.Timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(runCtx, spec.Bin, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr

	err := cmd.Run()
	result := ExecResult{Duration: time.Since(start), Err: err}
	if err == nil {
		result.ExitCode = 0
		return result
	}

	if runCtx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
	}
	if runCtx.Err() == context.Canceled {
		result.Interrupted = true
		result.ExitCode = 130
		return result
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result
	}

	if errors.Is(err, exec.ErrNotFound) {
		result.ExitCode = 127
		return result
	}

	result.ExitCode = 1
	return result
}
