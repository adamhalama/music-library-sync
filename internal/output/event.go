package output

import "time"

type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

type EventName string

const (
	EventSyncStarted     EventName = "sync_started"
	EventSourcePreflight EventName = "source_preflight"
	EventSourceStarted   EventName = "source_started"
	EventSourceFinished  EventName = "source_finished"
	EventSourceFailed    EventName = "source_failed"
	EventSyncFinished    EventName = "sync_finished"
	EventTrackStarted    EventName = "track_started"
	EventTrackProgress   EventName = "track_progress"
	EventTrackDone       EventName = "track_done"
	EventTrackSkip       EventName = "track_skip"
	EventTrackFail       EventName = "track_fail"
)

func IsTrackEventName(name EventName) bool {
	switch name {
	case EventTrackStarted, EventTrackProgress, EventTrackDone, EventTrackSkip, EventTrackFail:
		return true
	default:
		return false
	}
}

type Event struct {
	Timestamp time.Time      `json:"timestamp"`
	Level     Level          `json:"level"`
	Event     EventName      `json:"event"`
	SourceID  string         `json:"source_id,omitempty"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
}
