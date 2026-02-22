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

type tailBuffer struct {
	buf []byte
	max int
}

func newTailBuffer(max int) *tailBuffer {
	if max <= 0 {
		max = 64 * 1024
	}
	return &tailBuffer{
		buf: make([]byte, 0, max),
		max: max,
	}
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(p) >= t.max {
		t.buf = append(t.buf[:0], p[len(p)-t.max:]...)
		return len(p), nil
	}
	overflow := len(t.buf) + len(p) - t.max
	if overflow > 0 {
		t.buf = append(t.buf[:0], t.buf[overflow:]...)
	}
	t.buf = append(t.buf, p...)
	return len(p), nil
}

func (t *tailBuffer) String() string {
	return string(t.buf)
}

type flushWriter interface {
	Flush() error
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

	stdoutTail := newTailBuffer(64 * 1024)
	stderrTail := newTailBuffer(64 * 1024)

	if r.Stdout != nil {
		cmd.Stdout = io.MultiWriter(r.Stdout, stdoutTail)
	} else {
		cmd.Stdout = stdoutTail
	}
	if r.Stderr != nil {
		cmd.Stderr = io.MultiWriter(r.Stderr, stderrTail)
	} else {
		cmd.Stderr = stderrTail
	}

	err := cmd.Run()
	flushWriterIfSupported(r.Stdout)
	flushWriterIfSupported(r.Stderr)
	result := ExecResult{
		Duration:   time.Since(start),
		StdoutTail: stdoutTail.String(),
		StderrTail: stderrTail.String(),
		Err:        err,
	}
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

func flushWriterIfSupported(w io.Writer) {
	if f, ok := w.(flushWriter); ok {
		_ = f.Flush()
	}
}
