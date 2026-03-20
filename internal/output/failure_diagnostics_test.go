package output

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type captureEmitter struct {
	events []Event
}

func (e *captureEmitter) Emit(event Event) error {
	e.events = append(e.events, event)
	return nil
}

func TestBoundDiagnosticTailTruncatesToFixedSize(t *testing.T) {
	raw := strings.Repeat("x", diagnosticTailMaxChars+32)
	got := boundDiagnosticTail(raw)
	if len(got) != diagnosticTailMaxChars {
		t.Fatalf("expected tail length %d, got %d", diagnosticTailMaxChars, len(got))
	}
	if !strings.HasSuffix(raw, got) {
		t.Fatalf("expected bounded tail to keep the end of the payload")
	}
}

func TestFailureDiagnosticsEmitterNormalizesAndPersistsSourceFailures(t *testing.T) {
	stateDir := t.TempDir()
	next := &captureEmitter{}
	emitter := NewFailureDiagnosticsEmitter(stateDir, next)

	stdoutTail := strings.Repeat("o", diagnosticTailMaxChars+24)
	stderrTail := strings.Repeat("e", diagnosticTailMaxChars+24)
	event := Event{
		Timestamp: time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC),
		Level:     LevelError,
		Event:     EventSourceFailed,
		SourceID:  "spotify-source",
		Message:   "[spotify-source] command failed with exit code 1",
		Details: map[string]any{
			"adapter_kind": "spotdl",
			"command":      "spotdl sync https://open.spotify.com/playlist/test",
			"exit_code":    1,
			"timed_out":    true,
			"interrupted":  false,
			"stdout_tail":  stdoutTail,
			"stderr_tail":  stderrTail,
		},
	}

	if err := emitter.Emit(event); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if len(next.events) != 1 {
		t.Fatalf("expected one emitted event, got %d", len(next.events))
	}

	got := next.events[0]
	if got.Details["failure_message"] != event.Message {
		t.Fatalf("expected failure_message detail, got %#v", got.Details["failure_message"])
	}
	if got.Details["source_id"] != "spotify-source" {
		t.Fatalf("expected source_id detail, got %#v", got.Details["source_id"])
	}
	gotStdout := eventDetailString(got.Details, "stdout_tail")
	gotStderr := eventDetailString(got.Details, "stderr_tail")
	if len(gotStdout) != diagnosticTailMaxChars || len(gotStderr) != diagnosticTailMaxChars {
		t.Fatalf("expected bounded tails, got stdout=%d stderr=%d", len(gotStdout), len(gotStderr))
	}

	logPath := eventDetailString(got.Details, "failure_log_path")
	if logPath != filepath.Join(stateDir, syncFailureLogName) {
		t.Fatalf("unexpected failure log path %q", logPath)
	}

	payload, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read failure log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(payload)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected one jsonl record, got %d", len(lines))
	}

	var record syncFailureLogRecord
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("unmarshal failure record: %v", err)
	}
	if record.SourceID != "spotify-source" || record.AdapterKind != "spotdl" {
		t.Fatalf("unexpected record metadata: %+v", record)
	}
	if record.ExitCode == nil || *record.ExitCode != 1 {
		t.Fatalf("expected exit code 1, got %+v", record.ExitCode)
	}
	if !record.TimedOut || record.Interrupted {
		t.Fatalf("unexpected timeout/interrupted flags: %+v", record)
	}
	if len(record.StdoutTail) != diagnosticTailMaxChars || len(record.StderrTail) != diagnosticTailMaxChars {
		t.Fatalf("expected bounded tails in persisted record, got stdout=%d stderr=%d", len(record.StdoutTail), len(record.StderrTail))
	}
}

func TestAppendSyncFailureLogRecordPrunesToMaxEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), syncFailureLogName)
	for i := 0; i < syncFailureLogMaxEvents+7; i++ {
		record := syncFailureLogRecord{
			Timestamp: time.Date(2026, 3, 19, 12, 0, 0, i, time.UTC).Format(time.RFC3339Nano),
			SourceID:  "source",
			Message:   "failure-" + strconv.Itoa(i),
		}
		if err := appendSyncFailureLogRecord(path, record); err != nil {
			t.Fatalf("append record %d: %v", i, err)
		}
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read failure log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(payload)), "\n")
	if len(lines) != syncFailureLogMaxEvents {
		t.Fatalf("expected %d retained records, got %d", syncFailureLogMaxEvents, len(lines))
	}
	if strings.Contains(lines[0], `"message":"failure-0"`) {
		t.Fatalf("expected oldest records to be pruned, got first line %s", lines[0])
	}
	if !strings.Contains(lines[len(lines)-1], `"message":"failure-206"`) {
		t.Fatalf("expected newest record to be retained, got %s", lines[len(lines)-1])
	}
}
