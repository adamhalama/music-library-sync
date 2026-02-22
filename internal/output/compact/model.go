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
	ID           string
	Lifecycle    SourceLifecycle
	PlannedTotal int
	ItemTotal    int
	ItemIndex    int
	Completed    int
}

type TrackProgress struct {
	Name            string
	Lifecycle       TrackLifecycle
	ProgressPercent float64
}

type GlobalProgress struct {
	Total     int
	Completed int
}

type ProgressModel struct {
	Source SourceProgress
	Track  TrackProgress
	Global GlobalProgress
}
