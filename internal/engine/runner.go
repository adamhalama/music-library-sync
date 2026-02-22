package engine

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ExecRunner interface {
	Run(ctx context.Context, spec ExecSpec) ExecResult
}

type SubprocessRunner struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

const spotifyRateLimitAbortThresholdSeconds = 300

var spotifyRateLimitPattern = regexp.MustCompile(`(?i)rate/request limit\. retry will occur after:\s*([0-9]+)\s*s`)

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

func NewSubprocessRunner(stdin io.Reader, stdout, stderr io.Writer) *SubprocessRunner {
	return &SubprocessRunner{Stdin: stdin, Stdout: stdout, Stderr: stderr}
}

func (r *SubprocessRunner) Run(ctx context.Context, spec ExecSpec) ExecResult {
	start := time.Now()
	if spec.Bin == "" {
		return ExecResult{ExitCode: 1, Duration: time.Since(start), Err: errors.New("missing binary")}
	}

	baseCtx := ctx
	baseCancel := func() {}
	if spec.Timeout > 0 {
		baseCtx, baseCancel = context.WithTimeout(ctx, spec.Timeout)
	}
	defer baseCancel()

	runCtx, cancel := context.WithCancel(baseCtx)
	defer cancel()

	cmd := exec.CommandContext(runCtx, spec.Bin, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Stdin = r.Stdin

	stdoutTail := newTailBuffer(64 * 1024)
	stderrTail := newTailBuffer(64 * 1024)

	rateLimitAbort := false
	var rateLimitAbortOnce sync.Once
	abortForRateLimit := func(line string) {
		retryAfter, ok := parseSpotifyRateLimitRetryAfterSeconds(line)
		if !ok || retryAfter < spotifyRateLimitAbortThresholdSeconds {
			return
		}
		rateLimitAbortOnce.Do(func() {
			rateLimitAbort = true
			cancel()
		})
	}

	stdoutSink := io.Writer(stdoutTail)
	if r.Stdout != nil {
		stdoutSink = io.MultiWriter(r.Stdout, stdoutTail)
	}
	stderrSink := io.Writer(stderrTail)
	if r.Stderr != nil {
		stderrSink = io.MultiWriter(r.Stderr, stderrTail)
	}
	stdoutSink = newLineObserverWriter(stdoutSink, abortForRateLimit)
	stderrSink = newLineObserverWriter(stderrSink, abortForRateLimit)
	cmd.Stdout = stdoutSink
	cmd.Stderr = stderrSink

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
		if rateLimitAbort {
			result.ExitCode = 1
			return result
		}
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

type lineObserverWriter struct {
	dst      io.Writer
	onLine   func(line string)
	buf      []byte
	lineLock sync.Mutex
}

func newLineObserverWriter(dst io.Writer, onLine func(line string)) *lineObserverWriter {
	return &lineObserverWriter{
		dst:    dst,
		onLine: onLine,
		buf:    make([]byte, 0, 256),
	}
}

func (w *lineObserverWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	n, err := w.dst.Write(p)
	if n > len(p) {
		n = len(p)
	}
	if n > 0 {
		w.consumeLines(p[:n])
	}
	return n, err
}

func (w *lineObserverWriter) consumeLines(p []byte) {
	if w.onLine == nil {
		return
	}
	w.lineLock.Lock()
	defer w.lineLock.Unlock()

	for _, b := range p {
		switch b {
		case '\n', '\r':
			line := strings.TrimSpace(string(w.buf))
			w.buf = w.buf[:0]
			if line != "" {
				w.onLine(line)
			}
		default:
			w.buf = append(w.buf, b)
		}
	}
}

func parseSpotifyRateLimitRetryAfterSeconds(line string) (int, bool) {
	match := spotifyRateLimitPattern.FindStringSubmatch(strings.TrimSpace(line))
	if len(match) != 2 {
		return 0, false
	}
	seconds, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, false
	}
	return seconds, true
}
