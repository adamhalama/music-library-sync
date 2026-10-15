package compact

type SourceLifecycle string

const (
	SourceLifecycleIdle     SourceLifecycle = "idle"
	SourceLifecyclePlanning SourceLifecycle = "planning"
	SourceLifecycleRunning  SourceLifecycle = "running"
	SourceLifecycleFinished SourceLifecycle = "finished"
	SourceLifecycleFailed   SourceLifecycle = "failed"
)

type TrackLifecycle string

const (
	TrackLifecycleIdle        TrackLifecycle = "idle"
	TrackLifecyclePreparing   TrackLifecycle = "preparing"
	TrackLifecycleDownloading TrackLifecycle = "downloading"
	TrackLifecycleFinalizing  TrackLifecycle = "finalizing"
	TrackLifecycleDone        TrackLifecycle = "done"
	TrackLifecycleSkipped     TrackLifecycle = "skipped"
	TrackLifecycleFailed      TrackLifecycle = "failed"
)

type SourceProgress struct {
	ID           string          `json:"id"`
	Lifecycle    SourceLifecycle `json:"lifecycle"`
	PlannedTotal int             `json:"planned_total"`
	ItemTotal    int             `json:"item_total"`
	ItemIndex    int             `json:"item_index"`
	Completed    int             `json:"completed"`
}

type TrackProgress struct {
	Name            string         `json:"name"`
	Lifecycle       TrackLifecycle `json:"lifecycle"`
	ProgressPercent float64        `json:"progress_percent"`
}

type GlobalProgress struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
}

type ProgressModel struct {
	Source SourceProgress `json:"source"`
	Track  TrackProgress  `json:"track"`
	Global GlobalProgress `json:"global"`
}
