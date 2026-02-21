package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompactLogWriterPersistsOnlyTrackResults(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewCompactLogWriterWithOptions(buf, CompactLogOptions{Interactive: false})

	payload := strings.Join([]string{
		"[download] Downloading item 1 of 2",
		"[download] Destination: /tmp/ninnidslvx - FUJI.m4a",
		"[info] Writing video thumbnail t500x500 to: /tmp/ninnidslvx - FUJI.jpg",
		"[download]   3.8% of ~  19.32KiB at    3.84KiB/s ETA Unknown (frag 0/26)",
		"[download]   5.2% of ~  19.32KiB at    3.84KiB/s ETA Unknown (frag 1/26)",
		"[download] 100% of    5.00MiB in 00:00:02 at 1.98MiB/s",
		"[download] Downloading item 2 of 2",
		"[download] /tmp/Aether.m4a has already been downloaded",
		"[download] 100% of    5.00MiB in 00:00:01 at 1.98MiB/s",
	}, "\n") + "\n"

	if _, err := writer.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[done] ninnidslvx - FUJI (thumb)") {
		t.Fatalf("expected downloaded track line, got: %s", out)
	}
	if !strings.Contains(out, "[skip] Aether (already-present)") {
		t.Fatalf("expected already-present track line, got: %s", out)
	}
	if strings.Contains(out, "frag 0/26") || strings.Contains(out, "Downloading item") || strings.Contains(out, "Destination") {
		t.Fatalf("expected noisy lines to be suppressed, got: %s", out)
	}
}

func TestCompactLogWriterFlushesWarningLineWithoutTrailingNewline(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewCompactLogWriterWithOptions(buf, CompactLogOptions{Interactive: false})

	if _, err := writer.Write([]byte("Warning: network is slow")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if !strings.Contains(buf.String(), "Warning: network is slow") {
		t.Fatalf("expected buffered line to flush, got: %s", buf.String())
	}
}
