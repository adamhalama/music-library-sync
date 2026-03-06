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
	SourceID    string
	AdapterKind string
	TrackID     string
	TrackName   string
	Index       int
	Total       int
	Percent     float64
	Reason      string
	Kind        TrackEventKind
}
