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

type captureObserver struct {
	events []Event
}

func (o *captureObserver) ObserveEvent(event Event) {
	o.events = append(o.events, event)
}

func TestObservingEmitterForwardsToObserverAndEmitter(t *testing.T) {
	buf := &bytes.Buffer{}
	observer := &captureObserver{}
	emitter := NewObservingEmitter(observer, NewJSONEmitter(buf))

	event := Event{
		Timestamp: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Level:     LevelInfo,
		Event:     EventSourcePreflight,
		SourceID:  "source-a",
		Message:   "preflight complete",
	}
	if err := emitter.Emit(event); err != nil {
		t.Fatalf("emit: %v", err)
	}

	if len(observer.events) != 1 || observer.events[0].Event != EventSourcePreflight {
		t.Fatalf("expected observer to receive forwarded event, got %+v", observer.events)
	}
	if !strings.Contains(buf.String(), "\"event\":\"source_preflight\"") {
		t.Fatalf("expected wrapped emitter output, got %s", buf.String())
	}
}
