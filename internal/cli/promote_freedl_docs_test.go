package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPromoteFreeDLReadmeIncludesCurrentFlagsAndNotes(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("resolve caller path")
	}
	readmePath := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "readme.md"))
	payload, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read readme: %v", err)
	}
	body := string(payload)

	required := []string{
		"`promote-freedl` flags:",
		"`--free-dl-dir <path>`",
		"`--library-dir <path>`",
		"`--write-dir <path>`",
		"`--target-format <auto|wav|mp3-320|aac-256>`",
		"`--apply`",
		"`--overwrite`",
		"`--probe-timeout <duration>`",
		"`--min-match-score <0-100>`",
		"`--ambiguity-gap <n>`",
		"`--aac-bitrate <value>`",
		"`--mp3-bitrate <value>`",
		"`--min-aac-kbps <n>`",
		"`--min-mp3-kbps <n>`",
		"`--min-opus-kbps <n>`",
		"`--replace-limit <n>`",
		"Browser launch/wait/post-processing failures are persisted for manual follow-up in `defaults.state_dir/<source-id>.freedl-stuck.jsonl`.",
		"`VBR`",
	}
	for _, token := range required {
		if !strings.Contains(body, token) {
			t.Fatalf("readme missing token for promote-freedl docs: %q", token)
		}
	}
}
