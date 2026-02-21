package output

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
)

var noisyDownloadProgressPattern = regexp.MustCompile(`^\[download\]\s+[0-9]+(?:\.[0-9]+)?%.*\(frag\s+[0-9]+/[0-9]+\)$`)

type CompactLogWriter struct {
	dst io.Writer
	mu  sync.Mutex
	buf []byte
}

func NewCompactLogWriter(dst io.Writer) *CompactLogWriter {
	return &CompactLogWriter{dst: dst, buf: make([]byte, 0, 256)}
}

func (w *CompactLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, b := range p {
		switch b {
		case '\n', '\r':
			if err := w.flushLineLocked(); err != nil {
				return 0, err
			}
		default:
			w.buf = append(w.buf, b)
		}
	}
	return len(p), nil
}

func (w *CompactLogWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.flushLineLocked()
}

func (w *CompactLogWriter) flushLineLocked() error {
	if len(w.buf) == 0 {
		return nil
	}

	line := strings.TrimSpace(string(w.buf))
	w.buf = w.buf[:0]
	if line == "" || shouldSuppressLine(line) {
		return nil
	}

	_, err := fmt.Fprintln(w.dst, line)
	return err
}

func shouldSuppressLine(line string) bool {
	return noisyDownloadProgressPattern.MatchString(line)
}
