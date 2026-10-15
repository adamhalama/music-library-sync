package engine

import (
	"io"
	"time"

	"github.com/jaa/update-downloads/internal/config"
)

type ExecSpec struct {
	Bin             string
	Args            []string
	Dir             string
	Timeout         time.Duration
	DisplayCommand  string
	Stdin           io.Reader
	StdoutObservers []func(line string)
	StderrObservers []func(line string)
}

type ExecResult struct {
	ExitCode    int
	Duration    time.Duration
	Interrupted bool
	TimedOut    bool
	StdoutTail  string
	StderrTail  string
	Err         error
}

type Adapter interface {
	Kind() string
	Binary() string
	MinVersion() string
	Validate(source config.Source) error
	BuildExecSpec(source config.Source, defaults config.Defaults, timeout time.Duration) (ExecSpec, error)
	RequiredEnv(source config.Source) []string
}

type SyncOptions struct {
	SourceIDs           []string
	DryRun              bool
	TimeoutOverride     time.Duration
	Plan                bool
	PlanLimit           int
	PlanWindow          PlanWindow
	PlanWindowBySource  map[string]PlanWindow
	AskOnExisting       bool
	AskOnExistingSet    bool
	ScanGaps            bool
	NoPreflight         bool
	AllowPrompt         bool
	SelectPlanRows      func(sourceID string, rows []PlanRow) (PlanSelectionResult, error)
	PromptOnExisting    func(sourceID string, preflight SoundCloudPreflight) (bool, error)
	PromptOnSpotifyAuth func(sourceID string) (bool, error)
	PromptOnDeemixARL   func(sourceID string) (string, error)
	TrackStatus         TrackStatusMode
}

type PlanSelectionResult struct {
	Manifest ExecutionManifest
	Canceled bool
	Rebuild  bool
	Window   PlanWindow
}

type PlanApplyOptions struct {
	DryRun bool
}

type SyncResult struct {
	Total              int  `json:"total"`
	Attempted          int  `json:"attempted"`
	Succeeded          int  `json:"succeeded"`
	Failed             int  `json:"failed"`
	Skipped            int  `json:"skipped"`
	DependencyFailures int  `json:"dependency_failures"`
	Interrupted        bool `json:"interrupted"`
}

type SoundCloudMode string

const (
	SoundCloudModeBreak    SoundCloudMode = "break"
	SoundCloudModeScanGaps SoundCloudMode = "scan_gaps"
)

type SoundCloudPreflight struct {
	RemoteTotal          int            `json:"remote_total"`
	KnownCount           int            `json:"known_count"`
	ArchiveGapCount      int            `json:"archive_gap_count"`
	KnownGapCount        int            `json:"known_gap_count"`
	FirstExistingIndex   int            `json:"first_existing_index"`
	PlannedDownloadCount int            `json:"planned_download_count"`
	Mode                 SoundCloudMode `json:"mode"`
}

type TrackStatusMode string

const (
	TrackStatusNames TrackStatusMode = "names"
	TrackStatusCount TrackStatusMode = "count"
	TrackStatusNone  TrackStatusMode = "none"
)

type PlanRowStatus string

const (
	PlanRowAlreadyDownloaded PlanRowStatus = "already_downloaded"
	PlanRowMissingNew        PlanRowStatus = "missing_new"
	PlanRowMissingKnownGap   PlanRowStatus = "missing_known_gap"
)

type PlanRow struct {
	Index             int           `json:"index"`
	RemoteID          string        `json:"remote_id"`
	RemoteURL         string        `json:"remote_url"`
	Title             string        `json:"title"`
	Status            PlanRowStatus `json:"status"`
	Toggleable        bool          `json:"toggleable"`
	SelectedByDefault bool          `json:"selected_by_default"`
}
