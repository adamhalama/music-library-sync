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
	interactiveSelections map[string]*tuiInteractiveSelectionState
	interactiveOrders     map[string]engine.DownloadOrder
	interactiveWindows    map[string]engine.PlanWindow
	interactiveDisplayID  string
	interactiveTracker    *tuiSyncRunTracker
	planPrompt            *tuiPlanPromptState
	interactionPrompt     *tuiInteractionPromptState
	planLimitInputActive  bool
	planLimitInput        string
	planLimitInputErr     string
	runStartedAt          time.Time
	runFinishedAt         time.Time
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
	SourceID      string
	Rows          []engine.PlanRow
	Details       planSourceDetails
	DownloadOrder engine.DownloadOrder
	PlanWindow    engine.PlanWindow
	Reply         chan tuiPlanSelectResult
}

type tuiPlanSelectResult struct {
	Manifest engine.ExecutionManifest
	Canceled bool
	Rebuild  bool
	Window   engine.PlanWindow
	Err      error
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

type tuiInteractiveDisplayState struct {
	sourceID      string
	details       planSourceDetails
	downloadOrder engine.DownloadOrder
	planWindow    engine.PlanWindow
	rows          []tuiTrackRowState
	activity      []tuiActivityEntry
	lifecycle     tuiInteractiveSourceLifecycle
	confirmed     bool
}

type tuiInteractiveSelectionState struct {
	sourceID                   string
	rows                       []tuiPlanTrackRow
	details                    planSourceDetails
	downloadOrder              engine.DownloadOrder
	planWindow                 engine.PlanWindow
	manifest                   engine.ExecutionManifest
	hasManifest                bool
	cursor                     int
	selected                   map[int]bool
	filter                     tuiStatusFilter
	filterCursor               int
	focusFilters               bool
	activityCollapsed          bool
	activityCollapseConfigured bool
}

type tuiPlanPromptState struct {
	*tuiInteractiveSelectionState
	reply chan tuiPlanSelectResult
}

type tuiPlanPromptFilter = tuiStatusFilter
