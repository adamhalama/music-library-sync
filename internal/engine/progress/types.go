package progress

type TrackEventKind string

const (
	TrackStarted  TrackEventKind = "track_started"
	TrackProgress TrackEventKind = "track_progress"
	TrackDone     TrackEventKind = "track_done"
	TrackSkip     TrackEventKind = "track_skip"
	TrackFail     TrackEventKind = "track_fail"
)

type TrackEvent struct {
	SourceID    string         `json:"source_id"`
	AdapterKind string         `json:"adapter_kind"`
	TrackID     string         `json:"track_id"`
	TrackName   string         `json:"track_name"`
	Index       int            `json:"index"`
	Total       int            `json:"total"`
	Percent     float64        `json:"percent"`
	Reason      string         `json:"reason"`
	Kind        TrackEventKind `json:"kind"`
}
