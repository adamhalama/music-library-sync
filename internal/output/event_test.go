package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestJSONEmitterSerializesEvent(t *testing.T) {
	buf := &bytes.Buffer{}
	emitter := NewJSONEmitter(buf)

	event := Event{
		Timestamp: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Level:     LevelInfo,
		Event:     EventSyncStarted,
		Message:   "sync started",
		Details: map[string]any{
			"total": 1,
		},
	}

	if err := emitter.Emit(event); err != nil {
		t.Fatalf("emit: %v", err)
	}

	line := strings.TrimSpace(buf.String())
	var decoded map[string]any
	if err := json.Unmarshal([]byte(line), &decoded); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	if decoded["event"] != string(EventSyncStarted) {
		t.Fatalf("unexpected event name: %v", decoded["event"])
	}
	if decoded["message"] != "sync started" {
		t.Fatalf("unexpected message: %v", decoded["message"])
	}
}
