package cli

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/output"
)

const tuiDefaultPlanLimit = 10
const tuiMinPlanLimit = 1

type tuiSyncWorkflowMode string

const (
	tuiSyncWorkflowInteractive tuiSyncWorkflowMode = "interactive"
	tuiSyncWorkflowStandard    tuiSyncWorkflowMode = "standard"
)

type tuiSyncModel struct {
	app                   *AppContext
	mode                  tuiSyncWorkflowMode
	width                 int
	height                int
	cfg                   config.Config
	cfgLoaded             bool
	cfgErr                error
	sources               []config.Source
	selected              map[string]bool
	cursor                int
	dryRun                bool
	timeoutOverride       time.Duration
	timeoutInputActive    bool
	timeoutInput          string
	timeoutInputErr       string
	askOnExisting         bool
	askOnExistingSet      bool
	scanGaps              bool
	noPreflight           bool
	planLimit             int
	running               bool
	cancelRequested       bool
	runCancel             context.CancelFunc
	done                  bool
	result                engine.SyncResult
	runErr                error
	validationErr         string
	events                []string
	progress              *output.StructuredProgressTracker
	lastFailure           *tuiSyncFailureState
	eventCh               chan tea.Msg
	standardSummaries     map[string]*tuiStandardSyncSourceSummary
	standardActiveSource  string
	standardLastSource    string
	standardActivityState tuiStandardSyncActivityState
	interactivePhase      tuiInteractiveSyncPhase
	sourceLifecycle       map[string]tuiInteractiveSourceLifecycle
	interactiveSelections map[string]*tuiInteractiveSelectionState
	interactiveDisplayID  string
	planPrompt            *tuiPlanPromptState
	interactionPrompt     *tuiInteractionPromptState
	planLimitInputActive  bool
	planLimitInput        string
	planLimitInputErr     string
	runStartedAt          time.Time
	runFinishedAt         time.Time
}

type tuiSyncFailureState struct {
	SourceID       string
	Message        string
	ExitCode       *int
	TimedOut       bool
	Interrupted    bool
	StdoutTail     string
	StderrTail     string
	FailureLogPath string
}

type tuiStandardSyncSourceLifecycle string

const (
	tuiStandardSyncSourceIdle      tuiStandardSyncSourceLifecycle = "idle"
	tuiStandardSyncSourcePreflight tuiStandardSyncSourceLifecycle = "preflight"
	tuiStandardSyncSourceRunning   tuiStandardSyncSourceLifecycle = "running"
	tuiStandardSyncSourceFinished  tuiStandardSyncSourceLifecycle = "finished"
	tuiStandardSyncSourceFailed    tuiStandardSyncSourceLifecycle = "failed"
)

type tuiStandardSyncSourceSummary struct {
	SourceID    string
	Lifecycle   tuiStandardSyncSourceLifecycle
	Planned     int
	Done        int
	Skipped     int
	Failed      int
	LastTrack   string
	LastOutcome string
}

type tuiStandardSyncActivityState struct {
	Collapsed          bool
	CollapseConfigured bool
}

type tuiConfigLoadedMsg struct {
	cfg config.Config
	err error
}

type tuiSyncEventMsg struct {
	Event output.Event
}

type tuiSyncDoneMsg struct {
	Result engine.SyncResult
	Err    error
}

type tuiPlanSelectRequestMsg struct {
	SourceID string
	Rows     []engine.PlanRow
	Details  planSourceDetails
	Reply    chan tuiPlanSelectResult
}

type tuiPlanSelectResult struct {
	SelectedIndices []int
	Canceled        bool
	Err             error
}

type tuiPromptKind string

const (
	tuiPromptKindConfirm tuiPromptKind = "confirm"
	tuiPromptKindInput   tuiPromptKind = "input"
)

type tuiInteractiveSyncPhase string

const (
	tuiInteractivePhaseIdle      tuiInteractiveSyncPhase = "idle"
	tuiInteractivePhasePreflight tuiInteractiveSyncPhase = "preflight"
	tuiInteractivePhaseReview    tuiInteractiveSyncPhase = "review"
	tuiInteractivePhaseSyncing   tuiInteractiveSyncPhase = "syncing"
	tuiInteractivePhaseDone      tuiInteractiveSyncPhase = "done"
)

type tuiInteractiveSourceLifecycle string

const (
	tuiSourceLifecycleIdle      tuiInteractiveSourceLifecycle = "idle"
	tuiSourceLifecyclePreflight tuiInteractiveSourceLifecycle = "preflight"
	tuiSourceLifecycleRunning   tuiInteractiveSourceLifecycle = "running"
	tuiSourceLifecycleFinished  tuiInteractiveSourceLifecycle = "finished"
	tuiSourceLifecycleFailed    tuiInteractiveSourceLifecycle = "failed"
)

type tuiPromptRequestMsg struct {
	Kind       tuiPromptKind
	SourceID   string
	Prompt     string
	DefaultYes bool
	MaskInput  bool
	Reply      chan tuiPromptResult
}

type tuiPromptResult struct {
	Confirmed bool
	Input     string
	Canceled  bool
	Err       error
}

type tuiInteractionPromptState struct {
	kind       tuiPromptKind
	sourceID   string
	prompt     string
	defaultYes bool
	maskInput  bool
	input      string
	reply      chan tuiPromptResult
}

type tuiStatusFilter string

const (
	tuiTrackFilterAll         tuiStatusFilter = "all"
	tuiTrackFilterWillSync    tuiStatusFilter = "will_sync"
	tuiTrackFilterMissingNew  tuiStatusFilter = "missing_new"
	tuiTrackFilterKnownGap    tuiStatusFilter = "known_gap"
	tuiTrackFilterAlreadyHave tuiStatusFilter = "already_have"
	tuiTrackFilterInRun       tuiStatusFilter = "in_run"
	tuiTrackFilterRemaining   tuiStatusFilter = "remaining"
	tuiTrackFilterDownloaded  tuiStatusFilter = "downloaded"
	tuiTrackFilterSkipped     tuiStatusFilter = "skipped"
	tuiTrackFilterFailed      tuiStatusFilter = "failed"
)

type tuiTrackPlanClass string

const (
	tuiTrackPlanClassNew         tuiTrackPlanClass = "new"
	tuiTrackPlanClassKnownGap    tuiTrackPlanClass = "known_gap"
	tuiTrackPlanClassAlreadyHave tuiTrackPlanClass = "already_have"
)

type tuiTrackRunScope string

const (
	tuiTrackRunScopeIncluded tuiTrackRunScope = "included"
	tuiTrackRunScopeExcluded tuiTrackRunScope = "excluded"
	tuiTrackRunScopeLocked   tuiTrackRunScope = "locked"
)

type tuiTrackRowState struct {
	SourceID          string
	SourceLabel       string
	RemoteID          string
	Title             string
	Index             int
	Toggleable        bool
	Selected          bool
	PlanStatus        engine.PlanRowStatus
	PlanClass         tuiTrackPlanClass
	RunScope          tuiTrackRunScope
	RuntimeStatus     tuiTrackRuntimeStatus
	StatusLabel       string
	FailureDetail     string
	SelectedByDefault bool
	ProgressKnown     bool
	ProgressPercent   float64
}

type tuiTrackRuntimeStatus string

const (
	tuiTrackStatusIdle        tuiTrackRuntimeStatus = "idle"
	tuiTrackStatusQueued      tuiTrackRuntimeStatus = "queued"
	tuiTrackStatusDownloading tuiTrackRuntimeStatus = "downloading"
	tuiTrackStatusDownloaded  tuiTrackRuntimeStatus = "downloaded"
	tuiTrackStatusSkipped     tuiTrackRuntimeStatus = "skipped"
	tuiTrackStatusFailed      tuiTrackRuntimeStatus = "failed"
)

type tuiActivityEntry struct {
	Timestamp time.Time
	Level     output.Level
	Message   string
	SourceID  string
}

type tuiInteractiveSelectionState struct {
	sourceID                   string
	rows                       []tuiTrackRowState
	details                    planSourceDetails
	confirmed                  bool
	cursor                     int
	selected                   map[int]bool
	filter                     tuiStatusFilter
	filterCursor               int
	focusFilters               bool
	activity                   []tuiActivityEntry
	activityCollapsed          bool
	activityCollapseConfigured bool
}

type tuiPlanPromptState struct {
	*tuiInteractiveSelectionState
	reply chan tuiPlanSelectResult
}

type tuiPlanPromptFilter = tuiStatusFilter
