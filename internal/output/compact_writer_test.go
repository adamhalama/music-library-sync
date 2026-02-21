package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompactLogWriterSuppressesFragmentProgressLines(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewCompactLogWriter(buf)

	payload := strings.Join([]string{
		"[download] Destination: /tmp/file.m4a",
		"[download]   3.8% of ~  19.32KiB at    3.84KiB/s ETA Unknown (frag 0/26)",
		"[download]   5.2% of ~  19.32KiB at    3.84KiB/s ETA Unknown (frag 1/26)",
		"[download] 100% of    5.00MiB in 00:00:02 at 1.98MiB/s",
	}, "\n") + "\n"

	if _, err := writer.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Destination") {
		t.Fatalf("expected destination line to be kept, got: %s", out)
	}
	if !strings.Contains(out, "100% of") {
		t.Fatalf("expected final completion line to be kept, got: %s", out)
	}
	if strings.Contains(out, "frag 0/26") || strings.Contains(out, "frag 1/26") {
		t.Fatalf("expected fragment progress lines to be suppressed, got: %s", out)
	}
}

func TestCompactLogWriterFlushesLineWithoutTrailingNewline(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewCompactLogWriter(buf)

	if _, err := writer.Write([]byte("[info] Downloading video thumbnail t500x500 ...")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if !strings.Contains(buf.String(), "Downloading video thumbnail") {
		t.Fatalf("expected buffered line to flush, got: %s", buf.String())
	}
}
