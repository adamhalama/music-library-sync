package engine

import (
	"time"

	"github.com/jaa/update-downloads/internal/config"
)

type ExecSpec struct {
	Bin            string
	Args           []string
	Dir            string
	Timeout        time.Duration
	DisplayCommand string
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
	AskOnExisting       bool
	AskOnExistingSet    bool
	ScanGaps            bool
	NoPreflight         bool
	AllowPrompt         bool
	PromptOnExisting    func(sourceID string, preflight SoundCloudPreflight) (bool, error)
	PromptOnSpotifyAuth func(sourceID string) (bool, error)
}

type SyncResult struct {
	Total              int
	Attempted          int
	Succeeded          int
	Failed             int
	Skipped            int
	DependencyFailures int
	Interrupted        bool
}

type SoundCloudMode string

const (
	SoundCloudModeBreak    SoundCloudMode = "break"
	SoundCloudModeScanGaps SoundCloudMode = "scan_gaps"
)

type SoundCloudPreflight struct {
	RemoteTotal          int
	KnownCount           int
	ArchiveGapCount      int
	KnownGapCount        int
	FirstExistingIndex   int
	PlannedDownloadCount int
	Mode                 SoundCloudMode
}
