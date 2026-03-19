package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/config"
)

const (
	syncFailureLogName      = "sync-failures.jsonl"
	syncFailureLogMaxEvents = 200
	diagnosticTailMaxChars  = 4096
)

type FailureDiagnosticsEmitter struct {
	stateDir string
	next     EventEmitter
}

type syncFailureLogRecord struct {
	Timestamp   string `json:"timestamp"`
	SourceID    string `json:"source_id,omitempty"`
	AdapterKind string `json:"adapter_kind,omitempty"`
	Message     string `json:"message"`
	Command     string `json:"command,omitempty"`
	ExitCode    *int   `json:"exit_code,omitempty"`
	TimedOut    bool   `json:"timed_out,omitempty"`
	Interrupted bool   `json:"interrupted,omitempty"`
	StdoutTail  string `json:"stdout_tail,omitempty"`
	StderrTail  string `json:"stderr_tail,omitempty"`
}

func NewFailureDiagnosticsEmitter(stateDir string, next EventEmitter) *FailureDiagnosticsEmitter {
	return &FailureDiagnosticsEmitter{
		stateDir: stateDir,
		next:     next,
	}
}

func (e *FailureDiagnosticsEmitter) Emit(event Event) error {
	normalized := event
	if event.Event == EventSourceFailed && event.Level == LevelError {
		normalized = normalizeFailureDiagnosticsEvent(e.stateDir, event)
	}
	if e.next == nil {
		return nil
	}
	return e.next.Emit(normalized)
}

func normalizeFailureDiagnosticsEvent(stateDir string, event Event) Event {
	normalized := event
	details := cloneEventDetails(event.Details)
	details["failure_message"] = strings.TrimSpace(event.Message)

	if tail := boundDiagnosticTail(eventDetailString(details, "stdout_tail")); tail != "" {
		details["stdout_tail"] = tail
	}
	if tail := boundDiagnosticTail(eventDetailString(details, "stderr_tail")); tail != "" {
		details["stderr_tail"] = tail
	}

	if sourceID := strings.TrimSpace(event.SourceID); sourceID != "" {
		details["source_id"] = sourceID
	}
	if logPath, err := resolveSyncFailureLogPath(stateDir); err == nil {
		details["failure_log_path"] = logPath
		record := buildSyncFailureLogRecord(event, details)
		_ = appendSyncFailureLogRecord(logPath, record)
	}

	normalized.Details = details
	return normalized
}

func resolveSyncFailureLogPath(defaultStateDir string) (string, error) {
	stateDir, err := config.ExpandPath(defaultStateDir)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(stateDir) {
		return "", fmt.Errorf("state_dir must resolve to an absolute path")
	}
	return filepath.Join(stateDir, syncFailureLogName), nil
}

func buildSyncFailureLogRecord(event Event, details map[string]any) syncFailureLogRecord {
	record := syncFailureLogRecord{
		Timestamp:   event.Timestamp.UTC().Format(time.RFC3339Nano),
		SourceID:    strings.TrimSpace(event.SourceID),
		AdapterKind: strings.TrimSpace(eventDetailString(details, "adapter_kind")),
		Message:     strings.TrimSpace(event.Message),
		Command:     strings.TrimSpace(eventDetailString(details, "command")),
		TimedOut:    eventDetailBool(details, "timed_out"),
		Interrupted: eventDetailBool(details, "interrupted"),
		StdoutTail:  boundDiagnosticTail(eventDetailString(details, "stdout_tail")),
		StderrTail:  boundDiagnosticTail(eventDetailString(details, "stderr_tail")),
	}
	if record.Timestamp == "" || record.Timestamp == "0001-01-01T00:00:00Z" {
		record.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if exitCode, ok := eventDetailInt(details, "exit_code"); ok {
		record.ExitCode = &exitCode
	}
	return record
}

func appendSyncFailureLogRecord(path string, record syncFailureLogRecord) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(trimmed), 0o755); err != nil {
		return err
	}

	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}

	var existing [][]byte
	if raw, readErr := os.ReadFile(trimmed); readErr == nil {
		for _, line := range bytes.Split(raw, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			existing = append(existing, append([]byte(nil), line...))
		}
	} else if !os.IsNotExist(readErr) {
		return readErr
	}

	existing = append(existing, payload)
	if len(existing) > syncFailureLogMaxEvents {
		existing = existing[len(existing)-syncFailureLogMaxEvents:]
	}

	var buf bytes.Buffer
	for _, line := range existing {
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return os.WriteFile(trimmed, buf.Bytes(), 0o644)
}

func boundDiagnosticTail(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= diagnosticTailMaxChars {
		return trimmed
	}
	return strings.TrimSpace(trimmed[len(trimmed)-diagnosticTailMaxChars:])
}

func cloneEventDetails(details map[string]any) map[string]any {
	if len(details) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(details))
	for key, value := range details {
		out[key] = value
	}
	return out
}

func eventDetailBool(details map[string]any, key string) bool {
	if details == nil {
		return false
	}
	raw, ok := details[key]
	if !ok {
		return false
	}
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		switch strings.TrimSpace(strings.ToLower(v)) {
		case "1", "true", "yes":
			return true
		default:
			return false
		}
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	default:
		return false
	}
}
