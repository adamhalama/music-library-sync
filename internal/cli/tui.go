package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jaa/update-downloads/internal/adapters/deemix"
	"github.com/jaa/update-downloads/internal/adapters/scdl"
	"github.com/jaa/update-downloads/internal/adapters/scdlfreedl"
	"github.com/jaa/update-downloads/internal/adapters/spotdl"
	workflows "github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/doctor"
	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/output"
	compactstate "github.com/jaa/update-downloads/internal/output/compact"
	"github.com/spf13/cobra"
)

func newTUICommand(app *AppContext) *cobra.Command {
	debugMessages := false
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch the full-screen TUI shell",
		RunE: func(cmd *cobra.Command, args []string) (runErr error) {
			defer func() {
				if recovered := recover(); recovered != nil {
					_, _ = fmt.Fprint(app.IO.Out, "\x1b[?25h")
					runErr = fmt.Errorf("tui panic recovered: %v", recovered)
				}
			}()
			model := newTUIRootModel(app, debugMessages)
			program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithInput(app.IO.In), tea.WithOutput(app.IO.Out))
			if _, err := program.Run(); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&debugMessages, "debug-messages", false, "Show Bubble Tea message tracing footer")
	return cmd
}

type tuiScreen int

const (
	tuiScreenMenu tuiScreen = iota
	tuiScreenInteractiveSync
	tuiScreenSync
	tuiScreenDoctor
	tuiScreenValidate
	tuiScreenInit
)

const tuiDefaultPlanLimit = 10
const tuiMinPlanLimit = 1

type tuiSyncWorkflowMode string

const (
	tuiSyncWorkflowInteractive tuiSyncWorkflowMode = "interactive"
	tuiSyncWorkflowStandard    tuiSyncWorkflowMode = "standard"
)

type tuiRootModel struct {
	app           *AppContext
	width         int
	height        int
	debugMessages bool
	lastMsgType   string

	screen     tuiScreen
	menuCursor int
	menuItems  []string

	syncModel     tuiSyncModel
	doctorModel   tuiDoctorModel
	validateModel tuiValidateModel
	initModel     tuiInitModel
}

func newTUIRootModel(app *AppContext, debugMessages bool) tuiRootModel {
	return tuiRootModel{
		app:           app,
		debugMessages: debugMessages,
		menuItems:     []string{"interactive sync", "sync", "doctor", "validate", "init", "quit"},
		screen:        tuiScreenMenu,
	}
}

func (m tuiRootModel) Init() tea.Cmd {
	return nil
}

func (m tuiRootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.debugMessages {
		m.lastMsgType = fmt.Sprintf("%T", msg)
	}
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = typed.Width
		m.height = typed.Height
		switch m.screen {
		case tuiScreenInteractiveSync, tuiScreenSync:
			m.syncModel.width = typed.Width
			m.syncModel.height = typed.Height
			next, cmd := m.syncModel.Update(msg)
			m.syncModel = next
			return m, cmd
		case tuiScreenDoctor:
			next, cmd := m.doctorModel.Update(msg)
			m.doctorModel = next
			return m, cmd
		case tuiScreenValidate:
			next, cmd := m.validateModel.Update(msg)
			m.validateModel = next
			return m, cmd
		case tuiScreenInit:
			next, cmd := m.initModel.Update(msg)
			m.initModel = next
			return m, cmd
		default:
			return m, nil
		}
	case tea.KeyMsg:
		if m.screen == tuiScreenMenu {
			switch typed.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "up", "k":
				if m.menuCursor > 0 {
					m.menuCursor--
				}
				return m, nil
			case "down", "j":
				if m.menuCursor < len(m.menuItems)-1 {
					m.menuCursor++
				}
				return m, nil
			case "enter":
				switch m.menuItems[m.menuCursor] {
				case "interactive sync":
					m.screen = tuiScreenInteractiveSync
					m.syncModel = newTUISyncModel(m.app, tuiSyncWorkflowInteractive)
					return m, m.syncModel.Init()
				case "sync":
					m.screen = tuiScreenSync
					m.syncModel = newTUISyncModel(m.app, tuiSyncWorkflowStandard)
					return m, m.syncModel.Init()
				case "doctor":
					m.screen = tuiScreenDoctor
					m.doctorModel = newTUIDoctorModel(m.app)
					return m, m.doctorModel.Init()
				case "validate":
					m.screen = tuiScreenValidate
					m.validateModel = newTUIValidateModel(m.app)
					return m, m.validateModel.Init()
				case "init":
					m.screen = tuiScreenInit
					m.initModel = newTUIInitModel(m.app)
					return m, m.initModel.Init()
				default:
					return m, tea.Quit
				}
			}
		}
		if typed.String() == "esc" && m.canReturnToMenuOnEsc() {
			m.screen = tuiScreenMenu
			return m, nil
		}
	}

	switch m.screen {
	case tuiScreenInteractiveSync, tuiScreenSync:
		m.syncModel.width = m.width
		m.syncModel.height = m.height
		next, cmd := m.syncModel.Update(msg)
		m.syncModel = next
		return m, cmd
	case tuiScreenDoctor:
		next, cmd := m.doctorModel.Update(msg)
		m.doctorModel = next
		return m, cmd
	case tuiScreenValidate:
		next, cmd := m.validateModel.Update(msg)
		m.validateModel = next
		return m, cmd
	case tuiScreenInit:
		next, cmd := m.initModel.Update(msg)
		m.initModel = next
		return m, cmd
	default:
		return m, nil
	}
}

func (m tuiRootModel) canReturnToMenuOnEsc() bool {
	switch m.screen {
	case tuiScreenInteractiveSync, tuiScreenSync:
		// Keep esc available for in-flow sync actions (for example plan cancel).
		return !m.syncModel.running &&
			!m.syncModel.hasActivePlanPrompt() &&
			!m.syncModel.hasActiveInteractionPrompt() &&
			!m.syncModel.hasActivePlanLimitInput() &&
			!m.syncModel.hasActiveTimeoutInput()
	case tuiScreenInit:
		return !m.initModel.running && !m.initModel.hasActivePrompt()
	case tuiScreenMenu:
		return false
	default:
		return true
	}
}

func (m tuiRootModel) View() string {
	layout := newTUIShellLayout(m.width, m.height)
	return renderTUIShell(m.shellState(layout), layout)
}

type tuiSyncModel struct {
	app                  *AppContext
	mode                 tuiSyncWorkflowMode
	width                int
	height               int
	cfg                  config.Config
	cfgLoaded            bool
	cfgErr               error
	sources              []config.Source
	selected             map[string]bool
	cursor               int
	dryRun               bool
	timeoutOverride      time.Duration
	timeoutInputActive   bool
	timeoutInput         string
	timeoutInputErr      string
	askOnExisting        bool
	askOnExistingSet     bool
	scanGaps             bool
	noPreflight          bool
	planLimit            int
	running              bool
	cancelRequested      bool
	runCancel            context.CancelFunc
	done                 bool
	result               engine.SyncResult
	runErr               error
	validationErr        string
	events               []string
	progress             *output.StructuredProgressTracker
	lastFailure          *tuiSyncFailureState
	eventCh              chan tea.Msg
	interactiveSelection *tuiInteractiveSelectionState
	planPrompt           *tuiPlanPromptState
	interactionPrompt    *tuiInteractionPromptState
	planLimitInputActive bool
	planLimitInput       string
	planLimitInputErr    string
	runStartedAt         time.Time
	runFinishedAt        time.Time
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
	tuiPlanFilterAll        tuiStatusFilter = "all"
	tuiPlanFilterSelected   tuiStatusFilter = "selected"
	tuiPlanFilterMissingNew tuiStatusFilter = "missing_new"
	tuiPlanFilterKnownGap   tuiStatusFilter = "known_gap"
	tuiPlanFilterDownloaded tuiStatusFilter = "downloaded"
)

type tuiTrackRowState struct {
	SourceID          string
	SourceLabel       string
	RemoteID          string
	Title             string
	Index             int
	Toggleable        bool
	Selected          bool
	Status            engine.PlanRowStatus
	StatusLabel       string
	FailureDetail     string
	SelectedByDefault bool
}

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

func newTUISyncModel(app *AppContext, mode tuiSyncWorkflowMode) tuiSyncModel {
	dryRun := false
	if app != nil {
		dryRun = app.Opts.DryRun
	}
	var interactiveSelection *tuiInteractiveSelectionState
	if mode == tuiSyncWorkflowInteractive {
		interactiveSelection = newEmptyTUIInteractiveSelectionState()
	}
	return tuiSyncModel{
		app:                  app,
		mode:                 mode,
		selected:             map[string]bool{},
		events:               []string{},
		dryRun:               dryRun,
		planLimit:            tuiDefaultPlanLimit,
		progress:             output.NewStructuredProgressTracker(nil),
		interactiveSelection: interactiveSelection,
	}
}

func (m tuiSyncModel) Init() tea.Cmd {
	return func() tea.Msg {
		cfg, err := loadConfig(m.app)
		if err == nil {
			err = config.Validate(cfg)
		}
		return tuiConfigLoadedMsg{cfg: cfg, err: err}
	}
}

func (m tuiSyncModel) Update(msg tea.Msg) (tuiSyncModel, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = typed.Width
		m.height = typed.Height
		return m, nil
	case tuiConfigLoadedMsg:
		m.cfgLoaded = true
		m.cfgErr = typed.err
		m.cfg = typed.cfg
		if typed.err == nil {
			for _, source := range typed.cfg.Sources {
				if source.Enabled {
					m.sources = append(m.sources, source)
					m.selected[source.ID] = true
				}
			}
		}
		return m, nil
	case tuiPlanSelectRequestMsg:
		state := newTUIInteractiveSelectionState(typed)
		m.interactiveSelection = state
		m.planPrompt = &tuiPlanPromptState{
			tuiInteractiveSelectionState: state,
			reply:                        typed.Reply,
		}
		return m, nil
	case tuiPromptRequestMsg:
		m.interactionPrompt = &tuiInteractionPromptState{
			kind:       typed.Kind,
			sourceID:   typed.SourceID,
			prompt:     typed.Prompt,
			defaultYes: typed.DefaultYes,
			maskInput:  typed.MaskInput,
			reply:      typed.Reply,
		}
		return m, nil
	case tea.KeyMsg:
		if m.running && (typed.String() == "x" || typed.String() == "ctrl+c") {
			m = m.cancelActiveRun()
			return m, m.waitRunMsgCmd()
		}
		if m.isInteractiveSyncWorkflow() && typed.String() == "p" && !m.timeoutInputActive && !m.planLimitInputActive && m.interactionPrompt == nil {
			if state := m.currentInteractiveSelection(); state != nil {
				state.toggleActivity(m.currentShellLayout())
			}
			return m, nil
		}
		if m.planLimitInputActive {
			switch typed.String() {
			case "enter":
				limit, err := parsePlanLimitInput(m.planLimitInput)
				if err != nil {
					m.planLimitInputErr = err.Error()
					return m, nil
				}
				m.planLimit = limit
				m.planLimitInputActive = false
				m.planLimitInput = ""
				m.planLimitInputErr = ""
				return m, nil
			case "esc":
				m.planLimitInputActive = false
				m.planLimitInput = ""
				m.planLimitInputErr = ""
				return m, nil
			case "backspace", "ctrl+h":
				if len(m.planLimitInput) > 0 {
					m.planLimitInput = m.planLimitInput[:len(m.planLimitInput)-1]
				}
				m.planLimitInputErr = ""
				return m, nil
			default:
				if len(typed.Runes) == 1 && typed.Runes[0] >= '0' && typed.Runes[0] <= '9' {
					m.planLimitInput += string(typed.Runes[0])
					m.planLimitInputErr = ""
				}
				return m, nil
			}
		}
		if m.timeoutInputActive {
			switch typed.String() {
			case "enter":
				timeout, err := parseTimeoutInput(m.timeoutInput)
				if err != nil {
					m.timeoutInputErr = err.Error()
					return m, nil
				}
				m.timeoutOverride = timeout
				m.timeoutInputActive = false
				m.timeoutInput = ""
				m.timeoutInputErr = ""
				return m, nil
			case "esc":
				m.timeoutInputActive = false
				m.timeoutInput = ""
				m.timeoutInputErr = ""
				return m, nil
			case "backspace", "ctrl+h":
				if len(m.timeoutInput) > 0 {
					m.timeoutInput = m.timeoutInput[:len(m.timeoutInput)-1]
				}
				m.timeoutInputErr = ""
				return m, nil
			default:
				if len(typed.Runes) > 0 {
					m.timeoutInput += string(typed.Runes)
					m.timeoutInputErr = ""
				}
				return m, nil
			}
		}
		if m.planPrompt != nil {
			if typed.String() == "l" {
				m.timeoutInputActive = false
				m.timeoutInput = ""
				m.timeoutInputErr = ""
				m.planLimitInputActive = true
				m.planLimitInput = ""
				m.planLimitInputErr = ""
				return m, nil
			}
			if typed.String() == "tab" {
				m.planPrompt.focusFilters = !m.planPrompt.focusFilters
				if !m.planPrompt.focusFilters {
					m.planPrompt.ensureCursorVisible()
				}
				return m, nil
			}
			if m.planPrompt.focusFilters {
				switch typed.String() {
				case "up", "k":
					if m.planPrompt.filterCursor > 0 {
						m.planPrompt.filterCursor--
					}
					return m, nil
				case "down", "j":
					if m.planPrompt.filterCursor < len(m.planPrompt.filters())-1 {
						m.planPrompt.filterCursor++
					}
					return m, nil
				case " ", "enter":
					m.planPrompt.filter = m.planPrompt.filters()[m.planPrompt.filterCursor]
					m.planPrompt.focusFilters = false
					m.planPrompt.ensureCursorVisible()
					return m, nil
				case "ctrl+c", "q", "esc":
					m.interactiveSelection = m.planPrompt.tuiInteractiveSelectionState
					m.planPrompt.reply <- tuiPlanSelectResult{Canceled: true}
					m.planPrompt = nil
					return m, m.waitRunMsgCmd()
				default:
					return m, nil
				}
			}
			switch typed.String() {
			case "up", "k":
				m.planPrompt.moveCursor(-1)
				return m, nil
			case "down", "j":
				m.planPrompt.moveCursor(1)
				return m, nil
			case " ":
				row, ok := m.planPrompt.currentRow()
				if !ok {
					return m, nil
				}
				if !row.Toggleable {
					return m, nil
				}
				m.planPrompt.setSelected(row.Index, !row.Selected)
				return m, nil
			case "a":
				for _, row := range m.planPrompt.filteredRows() {
					if row.Toggleable {
						m.planPrompt.setSelected(row.Index, true)
					}
				}
				return m, nil
			case "n":
				for _, row := range m.planPrompt.filteredRows() {
					if row.Toggleable {
						m.planPrompt.setSelected(row.Index, false)
					}
				}
				return m, nil
			case "enter":
				m.interactiveSelection = m.planPrompt.tuiInteractiveSelectionState
				m.planPrompt.reply <- tuiPlanSelectResult{SelectedIndices: m.planPrompt.selectedIndices()}
				m.planPrompt = nil
				return m, m.waitRunMsgCmd()
			case "ctrl+c", "q", "esc":
				m.interactiveSelection = m.planPrompt.tuiInteractiveSelectionState
				m.planPrompt.reply <- tuiPlanSelectResult{Canceled: true}
				m.planPrompt = nil
				return m, m.waitRunMsgCmd()
			default:
				return m, nil
			}
		}
		if m.interactionPrompt != nil {
			switch m.interactionPrompt.kind {
			case tuiPromptKindConfirm:
				switch typed.String() {
				case "y":
					m.interactionPrompt.reply <- tuiPromptResult{Confirmed: true}
					m.interactionPrompt = nil
					return m, m.waitRunMsgCmd()
				case "n":
					m.interactionPrompt.reply <- tuiPromptResult{Confirmed: false}
					m.interactionPrompt = nil
					return m, m.waitRunMsgCmd()
				case "enter":
					m.interactionPrompt.reply <- tuiPromptResult{Confirmed: m.interactionPrompt.defaultYes}
					m.interactionPrompt = nil
					return m, m.waitRunMsgCmd()
				case "esc", "q":
					m.interactionPrompt.reply <- tuiPromptResult{Canceled: true, Err: engine.ErrInterrupted}
					m.interactionPrompt = nil
					return m, m.waitRunMsgCmd()
				default:
					return m, nil
				}
			case tuiPromptKindInput:
				switch typed.String() {
				case "enter":
					m.interactionPrompt.reply <- tuiPromptResult{Input: strings.TrimSpace(m.interactionPrompt.input)}
					m.interactionPrompt = nil
					return m, m.waitRunMsgCmd()
				case "esc", "q":
					m.interactionPrompt.reply <- tuiPromptResult{Canceled: true, Err: engine.ErrInterrupted}
					m.interactionPrompt = nil
					return m, m.waitRunMsgCmd()
				case "backspace", "ctrl+h":
					if len(m.interactionPrompt.input) > 0 {
						m.interactionPrompt.input = m.interactionPrompt.input[:len(m.interactionPrompt.input)-1]
					}
					return m, nil
				default:
					if len(typed.Runes) > 0 {
						m.interactionPrompt.input += string(typed.Runes)
					}
					return m, nil
				}
			default:
				return m, nil
			}
		}
		if !m.cfgLoaded || m.cfgErr != nil {
			return m, nil
		}
		if m.running {
			return m, nil
		}
		switch typed.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "down", "j":
			if m.cursor < len(m.sources)-1 {
				m.cursor++
			}
			return m, nil
		case " ":
			if len(m.sources) > 0 {
				sourceID := m.sources[m.cursor].ID
				m.selected[sourceID] = !m.selected[sourceID]
			}
			return m, nil
		case "d":
			m.dryRun = !m.dryRun
			m.validationErr = ""
			return m, nil
		case "a":
			if m.isInteractiveSyncWorkflow() {
				return m, nil
			}
			if m.askOnExistingSet {
				m.askOnExisting = false
				m.askOnExistingSet = false
			} else {
				m.askOnExisting = true
				m.askOnExistingSet = true
			}
			m.validationErr = ""
			return m, nil
		case "g":
			if m.isInteractiveSyncWorkflow() {
				return m, nil
			}
			m.scanGaps = !m.scanGaps
			m.validationErr = ""
			return m, nil
		case "f":
			if m.isInteractiveSyncWorkflow() {
				return m, nil
			}
			m.noPreflight = !m.noPreflight
			m.validationErr = ""
			return m, nil
		case "t":
			m.planLimitInputActive = false
			m.planLimitInput = ""
			m.planLimitInputErr = ""
			m.timeoutInputActive = true
			if m.timeoutOverride > 0 {
				m.timeoutInput = m.timeoutOverride.String()
			} else {
				m.timeoutInput = ""
			}
			m.timeoutInputErr = ""
			return m, nil
		case "l":
			if !m.isInteractiveSyncWorkflow() {
				return m, nil
			}
			m.timeoutInputActive = false
			m.timeoutInput = ""
			m.timeoutInputErr = ""
			m.planLimitInputActive = true
			m.planLimitInput = ""
			m.planLimitInputErr = ""
			return m, nil
		case "]":
			if !m.isInteractiveSyncWorkflow() {
				return m, nil
			}
			if m.planLimit == 0 {
				m.planLimit = tuiDefaultPlanLimit
			} else {
				m.planLimit++
			}
			m.validationErr = ""
			return m, nil
		case "[":
			if !m.isInteractiveSyncWorkflow() {
				return m, nil
			}
			if m.planLimit == 0 {
				m.planLimit = tuiDefaultPlanLimit
			} else if m.planLimit > tuiMinPlanLimit {
				m.planLimit--
			}
			m.validationErr = ""
			return m, nil
		case "u":
			if !m.isInteractiveSyncWorkflow() {
				return m, nil
			}
			if m.planLimit == 0 {
				m.planLimit = tuiDefaultPlanLimit
			} else {
				m.planLimit = 0
			}
			m.validationErr = ""
			return m, nil
		case "enter":
			if errMsg := validateTUISyncOptions(m); errMsg != "" {
				m.validationErr = errMsg
				return m, nil
			}
			m.running = true
			m.cancelRequested = false
			m.done = false
			m.runErr = nil
			m.validationErr = ""
			m.events = []string{}
			m.lastFailure = nil
			m.runStartedAt = time.Now()
			m.runFinishedAt = time.Time{}
			if m.progress == nil {
				m.progress = output.NewStructuredProgressTracker(nil)
			} else {
				m.progress.Reset()
			}
			m.eventCh = make(chan tea.Msg, 256)
			runCtx, cancel := context.WithCancel(context.Background())
			m.runCancel = cancel
			start := m.startRunCmd(runCtx)
			return m, tea.Batch(start, m.waitRunMsgCmd())
		}
	case tuiSyncEventMsg:
		if m.progress == nil {
			m.progress = output.NewStructuredProgressTracker(nil)
		}
		m.progress.ObserveEvent(typed.Event)
		if failure := tuiFailureStateFromEvent(typed.Event); failure != nil {
			m.lastFailure = failure
		}
		outcomes := m.progress.DrainTrackOutcomes()
		for _, outcome := range outcomes {
			m.events = append(m.events, output.FormatCompactTrackOutcome(outcome, output.CompactTrackStatusNames))
		}
		historyLine, historyOK := tuiSyncHistoryLine(typed.Event)
		if historyOK {
			m.events = append(m.events, historyLine)
		}
		m.appendInteractiveActivity(typed.Event, outcomes, historyLine, historyOK)
		if m.eventCh != nil {
			return m, m.waitRunMsgCmd()
		}
		return m, nil
	case tuiSyncDoneMsg:
		m.running = false
		m.cancelRequested = false
		m.runCancel = nil
		m.planPrompt = nil
		m.interactionPrompt = nil
		m.done = true
		m.result = typed.Result
		m.runErr = typed.Err
		m.runFinishedAt = time.Now()
		return m, nil
	}
	return m, nil
}

func (m tuiSyncModel) waitRunMsgCmd() tea.Cmd {
	ch := m.eventCh
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func (m tuiSyncModel) startRunCmd(runCtx context.Context) tea.Cmd {
	cfg := m.cfg
	selectedIDs := make([]string, 0, len(m.sources))
	for _, source := range m.sources {
		if m.selected[source.ID] {
			selectedIDs = append(selectedIDs, source.ID)
		}
	}
	ch := m.eventCh
	return func() tea.Msg {
		emitter := &tuiChannelEmitter{ch: ch}
		useCase := workflows.SyncUseCase{
			Registry: map[string]engine.Adapter{
				"deemix":      deemix.New(),
				"spotdl":      spotdl.New(),
				"scdl":        scdl.New(),
				"scdl-freedl": scdlfreedl.New(),
			},
			Runner:  engine.NewSubprocessRunner(m.app.IO.In, io.Discard, io.Discard),
			Emitter: emitter,
		}
		req := m.buildSyncRequest(selectedIDs)
		sourceByID := map[string]config.Source{}
		for _, source := range m.sources {
			sourceByID[source.ID] = source
		}
		interaction := &tuiSyncInteraction{
			ch:         ch,
			defaults:   cfg.Defaults,
			sourceByID: sourceByID,
			planLimit:  req.PlanLimit,
			dryRun:     req.DryRun,
		}
		result, err := useCase.Run(runCtx, cfg, req, interaction)
		ch <- tuiSyncDoneMsg{Result: result, Err: err}
		close(ch)
		return nil
	}
}

func (m tuiSyncModel) View() string {
	return m.bodyView(true)
}

func (m tuiSyncModel) hasActivePlanPrompt() bool {
	return m.planPrompt != nil
}

func (m tuiSyncModel) hasActiveInteractionPrompt() bool {
	return m.interactionPrompt != nil
}

func (m tuiSyncModel) hasActivePlanLimitInput() bool {
	return m.planLimitInputActive
}

func (m tuiSyncModel) hasActiveTimeoutInput() bool {
	return m.timeoutInputActive
}

func formatPlanLimit(limit int) string {
	if limit == 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%d", limit)
}

func (m tuiSyncModel) isInteractiveSyncWorkflow() bool {
	return m.mode == tuiSyncWorkflowInteractive
}

func (m tuiSyncModel) workflowTitle() string {
	if m.isInteractiveSyncWorkflow() {
		return "Interactive Sync Workflow"
	}
	return "Sync Workflow"
}

func (m tuiSyncModel) currentInteractiveSelection() *tuiInteractiveSelectionState {
	if m.planPrompt != nil {
		return m.planPrompt.tuiInteractiveSelectionState
	}
	return m.interactiveSelection
}

func (m tuiSyncModel) currentShellLayout() tuiShellLayout {
	return newTUIShellLayout(m.width, m.height)
}

func (m *tuiSyncModel) appendInteractiveActivity(event output.Event, outcomes []output.StructuredTrackOutcome, historyLine string, historyOK bool) {
	if m == nil || !m.isInteractiveSyncWorkflow() {
		return
	}
	state := m.currentInteractiveSelection()
	if state == nil {
		return
	}
	for _, outcome := range outcomes {
		level := output.LevelInfo
		switch outcome.Kind {
		case output.StructuredTrackOutcomeSkip:
			level = output.LevelWarn
		case output.StructuredTrackOutcomeFail:
			level = output.LevelError
		}
		state.appendActivity(tuiActivityEntry{
			Timestamp: event.Timestamp,
			Level:     level,
			Message:   output.FormatCompactTrackOutcome(outcome, output.CompactTrackStatusNames),
			SourceID:  event.SourceID,
		})
	}
	if historyOK {
		state.appendActivity(tuiActivityEntry{
			Timestamp: event.Timestamp,
			Level:     event.Level,
			Message:   historyLine,
			SourceID:  event.SourceID,
		})
	}
}

func (m tuiSyncModel) elapsedLabel() string {
	if m.runStartedAt.IsZero() {
		return "0:00"
	}
	end := m.runFinishedAt
	if end.IsZero() {
		end = time.Now()
	}
	if end.Before(m.runStartedAt) {
		end = m.runStartedAt
	}
	elapsed := end.Sub(m.runStartedAt).Round(time.Second)
	if elapsed < 0 {
		elapsed = 0
	}
	totalSeconds := int(elapsed / time.Second)
	minutes := totalSeconds / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}

func (m tuiSyncModel) buildSyncRequest(selectedIDs []string) workflows.SyncRequest {
	req := workflows.SyncRequest{
		SourceIDs:       selectedIDs,
		DryRun:          m.dryRun,
		TimeoutOverride: m.timeoutOverride,
		AllowPrompt:     m.app != nil && !m.app.Opts.NoInput,
		TrackStatus:     engine.TrackStatusNames,
	}
	if m.isInteractiveSyncWorkflow() {
		req.Plan = true
		req.PlanLimit = m.planLimit
		return req
	}
	req.AskOnExisting = m.askOnExisting
	req.AskOnExistingSet = m.askOnExistingSet
	req.ScanGaps = m.scanGaps
	req.NoPreflight = m.noPreflight
	return req
}

func formatTimeoutOverride(timeout time.Duration) string {
	if timeout <= 0 {
		return "default"
	}
	return timeout.String()
}

func formatAskOnExisting(set bool) string {
	if set {
		return "on"
	}
	return "inherit"
}

func parsePlanLimitInput(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, fmt.Errorf("enter a number (0 for unlimited)")
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("invalid number")
	}
	if parsed < 0 {
		return 0, fmt.Errorf("plan limit must be >= 0")
	}
	return parsed, nil
}

func parseTimeoutInput(raw string) (time.Duration, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	parsed, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, fmt.Errorf("invalid duration")
	}
	if parsed < 0 {
		return 0, fmt.Errorf("timeout must be >= 0")
	}
	return parsed, nil
}

func (m tuiSyncModel) progressLines() []string {
	if m.progress == nil {
		return nil
	}
	snapshot := m.progress.Snapshot()
	total := m.progress.EffectiveTotal()
	if total <= 0 && !snapshot.StructuredTrackEvents {
		return nil
	}

	lines := []string{}
	if strings.TrimSpace(snapshot.Track.Name) != "" && snapshot.Track.Lifecycle != compactstate.TrackLifecycleIdle {
		lines = append(lines, compactstate.RenderTrackLine(
			snapshot.Track.Name,
			tuiStructuredTrackStage(snapshot.Track.Lifecycle),
			false,
			false,
			snapshot.Track.ProgressKnown,
			snapshot.Track.ProgressPercent,
			snapshot.Track.Lifecycle == compactstate.TrackLifecycleSkipped,
			m.progress.CurrentIndex(),
			total,
		))
		lines = append(lines, compactstate.RenderGlobalLine(
			m.progress.GlobalProgressPercent(),
			32,
			m.progress.CurrentIndex(),
			total,
		))
		return lines
	}

	if total <= 0 {
		return nil
	}
	done := snapshot.Progress.Global.Completed
	lines = append(lines, compactstate.RenderIdleTrackLine(done, total))
	lines = append(lines, compactstate.RenderGlobalLine(
		m.progress.GlobalProgressPercent(),
		32,
		done,
		total,
	))
	return lines
}

func tuiStructuredTrackStage(lifecycle compactstate.TrackLifecycle) string {
	switch lifecycle {
	case compactstate.TrackLifecyclePreparing:
		return "preparing"
	case compactstate.TrackLifecycleFinalizing:
		return "finalizing"
	case compactstate.TrackLifecycleDone:
		return "downloaded"
	case compactstate.TrackLifecycleSkipped:
		return "skipped"
	case compactstate.TrackLifecycleFailed:
		return "failed"
	default:
		return "downloading"
	}
}

func tuiSyncHistoryLine(event output.Event) (string, bool) {
	switch event.Event {
	case output.EventTrackStarted, output.EventTrackProgress, output.EventTrackDone, output.EventTrackSkip, output.EventTrackFail:
		return "", false
	case output.EventSyncStarted, output.EventSourceStarted, output.EventSourcePreflight:
		return "", false
	default:
		if strings.TrimSpace(event.Message) == "" {
			return "", false
		}
		return event.Message, true
	}
}

func (m tuiSyncModel) lastFailureLines() []string {
	if m.lastFailure == nil {
		return nil
	}
	failure := m.lastFailure
	headline := strings.TrimSpace(failure.Message)
	prefix := fmt.Sprintf("[%s]", failure.SourceID)
	if headline == "" {
		headline = prefix
	} else if !strings.HasPrefix(headline, prefix) {
		headline = prefix + " " + headline
	}
	lines := []string{headline}
	statusParts := []string{}
	if failure.ExitCode != nil {
		statusParts = append(statusParts, fmt.Sprintf("exit_code=%d", *failure.ExitCode))
	}
	if failure.TimedOut {
		statusParts = append(statusParts, "timed_out=true")
	}
	if failure.Interrupted {
		statusParts = append(statusParts, "interrupted=true")
	}
	if len(statusParts) > 0 {
		lines = append(lines, strings.Join(statusParts, "  "))
	}
	if tail := strings.TrimSpace(failure.StdoutTail); tail != "" {
		lines = append(lines, "stdout_tail:")
		lines = append(lines, tuiIndentedTailLines(tail)...)
	}
	if tail := strings.TrimSpace(failure.StderrTail); tail != "" {
		lines = append(lines, "stderr_tail:")
		lines = append(lines, tuiIndentedTailLines(tail)...)
	}
	if path := strings.TrimSpace(failure.FailureLogPath); path != "" {
		lines = append(lines, "log: "+path)
	}
	return lines
}

func tuiIndentedTailLines(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimRight(part, "\r")
		if strings.TrimSpace(part) == "" {
			continue
		}
		lines = append(lines, "  "+part)
	}
	return lines
}

func tuiFailureStateFromEvent(event output.Event) *tuiSyncFailureState {
	if event.Event != output.EventSourceFailed || event.Level != output.LevelError {
		return nil
	}
	failure := &tuiSyncFailureState{
		SourceID:       strings.TrimSpace(event.SourceID),
		Message:        strings.TrimSpace(event.Message),
		TimedOut:       tuiDetailBool(event.Details, "timed_out"),
		Interrupted:    tuiDetailBool(event.Details, "interrupted"),
		StdoutTail:     strings.TrimSpace(tuiDetailString(event.Details, "stdout_tail")),
		StderrTail:     strings.TrimSpace(tuiDetailString(event.Details, "stderr_tail")),
		FailureLogPath: strings.TrimSpace(tuiDetailString(event.Details, "failure_log_path")),
	}
	if message := strings.TrimSpace(tuiDetailString(event.Details, "failure_message")); message != "" {
		failure.Message = message
	}
	if sourceID := strings.TrimSpace(tuiDetailString(event.Details, "source_id")); sourceID != "" {
		failure.SourceID = sourceID
	}
	if failure.SourceID == "" {
		failure.SourceID = "sync"
	}
	if exitCode, ok := tuiDetailInt(event.Details, "exit_code"); ok {
		failure.ExitCode = &exitCode
	}
	return failure
}

func tuiDetailString(details map[string]any, key string) string {
	if details == nil {
		return ""
	}
	raw, ok := details[key]
	if !ok {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	}
}

func tuiDetailInt(details map[string]any, key string) (int, bool) {
	if details == nil {
		return 0, false
	}
	raw, ok := details[key]
	if !ok {
		return 0, false
	}
	switch value := raw.(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func tuiDetailBool(details map[string]any, key string) bool {
	if details == nil {
		return false
	}
	raw, ok := details[key]
	if !ok {
		return false
	}
	switch value := raw.(type) {
	case bool:
		return value
	case string:
		switch strings.TrimSpace(strings.ToLower(value)) {
		case "1", "true", "yes":
			return true
		default:
			return false
		}
	case int:
		return value != 0
	case int64:
		return value != 0
	case float64:
		return value != 0
	default:
		return false
	}
}

func validateTUISyncOptions(m tuiSyncModel) string {
	if m.isInteractiveSyncWorkflow() && m.scanGaps {
		return "plan mode cannot be combined with scan-gaps"
	}
	if m.isInteractiveSyncWorkflow() && m.askOnExistingSet {
		return "plan mode cannot be combined with ask-on-existing"
	}
	if m.isInteractiveSyncWorkflow() && m.noPreflight {
		return "plan mode cannot be combined with no-preflight"
	}
	return ""
}

func (m tuiSyncModel) cancelActiveRun() tuiSyncModel {
	if !m.running {
		return m
	}
	m.cancelRequested = true
	if m.planPrompt != nil {
		select {
		case m.planPrompt.reply <- tuiPlanSelectResult{Canceled: true}:
		default:
		}
		m.planPrompt = nil
	}
	if m.interactionPrompt != nil {
		select {
		case m.interactionPrompt.reply <- tuiPromptResult{Canceled: true, Err: engine.ErrInterrupted}:
		default:
		}
		m.interactionPrompt = nil
	}
	if m.runCancel != nil {
		m.runCancel()
	}
	return m
}

func (m tuiSyncModel) interactionPromptView() string {
	state := m.interactionPrompt
	if state == nil {
		return ""
	}
	switch state.kind {
	case tuiPromptKindConfirm:
		defaultLabel := "no"
		if state.defaultYes {
			defaultLabel = "yes"
		}
		return strings.Join([]string{
			m.workflowTitle(),
			fmt.Sprintf("[%s] confirm", state.sourceID),
			state.prompt,
			fmt.Sprintf("y: yes  n: no  enter: default (%s)  esc/q: cancel run", defaultLabel),
		}, "\n")
	case tuiPromptKindInput:
		displayInput := state.input
		if state.maskInput {
			displayInput = strings.Repeat("*", len(state.input))
		}
		return strings.Join([]string{
			m.workflowTitle(),
			fmt.Sprintf("[%s] input", state.sourceID),
			state.prompt,
			fmt.Sprintf("value=%q", displayInput),
			"type to edit  backspace: delete  enter: submit  esc/q: cancel run",
		}, "\n")
	default:
		return m.workflowTitle() + "\nUnknown prompt state"
	}
}

func (m tuiSyncModel) planPromptView() string {
	state := m.planPrompt
	if state == nil {
		return ""
	}
	limitLabel := "unlimited"
	if state.details.PlanLimit > 0 {
		limitLabel = fmt.Sprintf("%d", state.details.PlanLimit)
	}
	modeLabel := "run"
	if state.details.DryRun {
		modeLabel = "dry-run"
	}
	lines := []string{
		m.workflowTitle(),
		fmt.Sprintf("udl sync --plan  source=%s  mode=%s  plan-limit=%s", state.sourceID, modeLabel, limitLabel),
		fmt.Sprintf("type=%s  adapter=%s", state.details.SourceType, state.details.Adapter),
		fmt.Sprintf("target_dir=%s", state.details.TargetDir),
		fmt.Sprintf("state_file=%s", state.details.StateFile),
		fmt.Sprintf("url=%s", state.details.URL),
		"up/down or j/k: move   space: toggle   a: select all   n: clear all   enter: confirm   q/esc: cancel",
		"",
	}
	if len(state.rows) == 0 {
		lines = append(lines, "No tracks found in selected preflight window.")
		return strings.Join(lines, "\n")
	}
	start, end := planSelectorWindow(len(state.rows), state.cursor, 0)
	for i := start; i < end; i++ {
		row := state.rows[i]
		cursor := " "
		if i == state.cursor {
			cursor = ">"
		}
		marker := state.selectionMarker(row)
		title := strings.TrimSpace(row.Title)
		if title == "" {
			title = "(untitled)"
		}
		lines = append(lines, fmt.Sprintf("%s %s %3d  %-16s  %s (%s)", cursor, marker, row.Index, string(row.Status), title, row.RemoteID))
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("Selected: %d/%d toggleable tracks", state.selectedCount(), state.toggleableCount()))
	return strings.Join(lines, "\n")
}

func newEmptyTUIInteractiveSelectionState() *tuiInteractiveSelectionState {
	return &tuiInteractiveSelectionState{
		selected: map[int]bool{},
		filter:   tuiPlanFilterAll,
	}
}

func newTUIInteractiveSelectionState(req tuiPlanSelectRequestMsg) *tuiInteractiveSelectionState {
	selected := map[int]bool{}
	rows := make([]tuiTrackRowState, 0, len(req.Rows))
	for _, row := range req.Rows {
		if row.Toggleable && row.SelectedByDefault {
			selected[row.Index] = true
		}
		rows = append(rows, tuiTrackRowState{
			SourceID:          req.SourceID,
			SourceLabel:       req.Details.SourceID,
			RemoteID:          row.RemoteID,
			Title:             row.Title,
			Index:             row.Index,
			Toggleable:        row.Toggleable,
			Selected:          row.Toggleable && row.SelectedByDefault,
			Status:            row.Status,
			StatusLabel:       tuiPlanRowStatusLabel(row.Status),
			SelectedByDefault: row.SelectedByDefault,
		})
	}
	state := &tuiInteractiveSelectionState{
		sourceID:     req.SourceID,
		rows:         rows,
		details:      req.Details,
		selected:     selected,
		filter:       tuiPlanFilterAll,
		filterCursor: 0,
	}
	state.syncSelectedRows()
	return state
}

func newTUIPlanPromptState(req tuiPlanSelectRequestMsg) *tuiPlanPromptState {
	return &tuiPlanPromptState{
		tuiInteractiveSelectionState: newTUIInteractiveSelectionState(req),
		reply:                        req.Reply,
	}
}

func (s *tuiInteractiveSelectionState) selectedIndices() []int {
	if s == nil {
		return nil
	}
	out := make([]int, 0, len(s.selected))
	for _, row := range s.rows {
		if !row.Toggleable {
			continue
		}
		if row.Selected {
			out = append(out, row.Index)
		}
	}
	sort.Ints(out)
	return out
}

func (s *tuiInteractiveSelectionState) filters() []tuiPlanPromptFilter {
	return []tuiPlanPromptFilter{
		tuiPlanFilterAll,
		tuiPlanFilterSelected,
		tuiPlanFilterMissingNew,
		tuiPlanFilterKnownGap,
		tuiPlanFilterDownloaded,
	}
}

func (s *tuiInteractiveSelectionState) filteredRows() []tuiTrackRowState {
	if s == nil {
		return nil
	}
	rows := make([]tuiTrackRowState, 0, len(s.rows))
	for _, row := range s.rows {
		if s.matchesFilter(row) {
			rows = append(rows, row)
		}
	}
	return rows
}

func (s *tuiInteractiveSelectionState) matchesFilter(row tuiTrackRowState) bool {
	switch s.filter {
	case tuiPlanFilterSelected:
		return row.Toggleable && row.Selected
	case tuiPlanFilterMissingNew:
		return row.Status == engine.PlanRowMissingNew
	case tuiPlanFilterKnownGap:
		return row.Status == engine.PlanRowMissingKnownGap
	case tuiPlanFilterDownloaded:
		return row.Status == engine.PlanRowAlreadyDownloaded
	default:
		return true
	}
}

func (s *tuiInteractiveSelectionState) visibleRowIndices() []int {
	if s == nil {
		return nil
	}
	indices := make([]int, 0, len(s.rows))
	for idx, row := range s.rows {
		if s.matchesFilter(row) {
			indices = append(indices, idx)
		}
	}
	return indices
}

func (s *tuiInteractiveSelectionState) ensureCursorVisible() {
	if s == nil {
		return
	}
	visible := s.visibleRowIndices()
	if len(visible) == 0 {
		s.cursor = 0
		return
	}
	for _, idx := range visible {
		if idx == s.cursor {
			return
		}
	}
	s.cursor = visible[0]
}

func (s *tuiInteractiveSelectionState) moveCursor(delta int) {
	if s == nil {
		return
	}
	visible := s.visibleRowIndices()
	if len(visible) == 0 {
		s.cursor = 0
		return
	}
	current := 0
	for i, idx := range visible {
		if idx == s.cursor {
			current = i
			break
		}
	}
	current += delta
	if current < 0 {
		current = 0
	}
	if current >= len(visible) {
		current = len(visible) - 1
	}
	s.cursor = visible[current]
}

func (s *tuiInteractiveSelectionState) currentRow() (tuiTrackRowState, bool) {
	if s == nil {
		return tuiTrackRowState{}, false
	}
	visible := s.visibleRowIndices()
	for _, idx := range visible {
		if idx == s.cursor {
			return s.rows[idx], true
		}
	}
	return tuiTrackRowState{}, false
}

func (s *tuiInteractiveSelectionState) filterDisplayLabel(filter tuiPlanPromptFilter) string {
	switch filter {
	case tuiPlanFilterSelected:
		return "Selected"
	case tuiPlanFilterMissingNew:
		return "New"
	case tuiPlanFilterKnownGap:
		return "Known Gap"
	case tuiPlanFilterDownloaded:
		return "Downloaded"
	default:
		return "All"
	}
}

func (s *tuiInteractiveSelectionState) filterLabel() string {
	if s == nil {
		return "all"
	}
	return strings.ToLower(s.filterDisplayLabel(s.filter))
}

func (s *tuiInteractiveSelectionState) focusLabel() string {
	if s == nil {
		return "tracks"
	}
	if s.focusFilters {
		return "filters"
	}
	return "tracks"
}

func (s *tuiInteractiveSelectionState) filterCount(filter tuiPlanPromptFilter) int {
	if s == nil {
		return 0
	}
	original := s.filter
	s.filter = filter
	rows := s.filteredRows()
	s.filter = original
	return len(rows)
}

func (s *tuiInteractiveSelectionState) selectedCount() int {
	if s == nil {
		return 0
	}
	count := 0
	for _, row := range s.rows {
		if row.Toggleable && row.Selected {
			count++
		}
	}
	return count
}

func (s *tuiInteractiveSelectionState) toggleableCount() int {
	if s == nil {
		return 0
	}
	count := 0
	for _, row := range s.rows {
		if row.Toggleable {
			count++
		}
	}
	return count
}

func (s *tuiInteractiveSelectionState) skippedCount() int {
	if s == nil {
		return 0
	}
	count := 0
	for _, row := range s.rows {
		if row.Status == engine.PlanRowAlreadyDownloaded {
			count++
		}
	}
	return count
}

func (s *tuiInteractiveSelectionState) setSelected(index int, selected bool) {
	if s == nil {
		return
	}
	if s.selected == nil {
		s.selected = map[int]bool{}
	}
	s.selected[index] = selected
	s.syncSelectedRows()
}

func (s *tuiInteractiveSelectionState) syncSelectedRows() {
	if s == nil {
		return
	}
	for idx := range s.rows {
		row := &s.rows[idx]
		row.Selected = row.Toggleable && s.selected[row.Index]
	}
}

func (s *tuiInteractiveSelectionState) selectionMarker(row tuiTrackRowState) string {
	if !row.Toggleable {
		return "[-]"
	}
	if row.Selected {
		return "[x]"
	}
	return "[ ]"
}

func (s *tuiInteractiveSelectionState) activityCollapsedFor(layout tuiShellLayout) bool {
	if s == nil {
		return layout.Compact
	}
	if s.activityCollapseConfigured {
		return s.activityCollapsed
	}
	return layout.Compact
}

func (s *tuiInteractiveSelectionState) toggleActivity(layout tuiShellLayout) {
	if s == nil {
		return
	}
	if s.activityCollapseConfigured {
		s.activityCollapsed = !s.activityCollapsed
		return
	}
	s.activityCollapsed = !layout.Compact
	s.activityCollapseConfigured = true
}

func (s *tuiInteractiveSelectionState) appendActivity(entry tuiActivityEntry) {
	if s == nil || strings.TrimSpace(entry.Message) == "" {
		return
	}
	s.activity = append(s.activity, entry)
	const maxEntries = 18
	if len(s.activity) > maxEntries {
		s.activity = append([]tuiActivityEntry(nil), s.activity[len(s.activity)-maxEntries:]...)
	}
}

func tuiPlanRowStatusLabel(status engine.PlanRowStatus) string {
	switch status {
	case engine.PlanRowMissingNew:
		return "new"
	case engine.PlanRowMissingKnownGap:
		return "known gap"
	case engine.PlanRowAlreadyDownloaded:
		return "have it"
	default:
		return strings.ReplaceAll(string(status), "_", " ")
	}
}

type tuiChannelEmitter struct {
	ch chan tea.Msg
}

func (e *tuiChannelEmitter) Emit(event output.Event) error {
	if e == nil || e.ch == nil {
		return nil
	}
	e.ch <- tuiSyncEventMsg{Event: event}
	return nil
}

type tuiSyncInteraction struct {
	ch         chan tea.Msg
	defaults   config.Defaults
	sourceByID map[string]config.Source
	planLimit  int
	dryRun     bool
}

func (i *tuiSyncInteraction) Confirm(prompt string, defaultYes bool) (bool, error) {
	if i == nil || i.ch == nil {
		return defaultYes, nil
	}
	reply := make(chan tuiPromptResult, 1)
	i.ch <- tuiPromptRequestMsg{
		Kind:       tuiPromptKindConfirm,
		SourceID:   sourceIDFromPrompt(prompt),
		Prompt:     prompt,
		DefaultYes: defaultYes,
		Reply:      reply,
	}
	result := <-reply
	if result.Err != nil {
		return false, result.Err
	}
	if result.Canceled {
		return false, engine.ErrInterrupted
	}
	return result.Confirmed, nil
}

func (i *tuiSyncInteraction) Input(prompt string) (string, error) {
	if i == nil || i.ch == nil {
		return "", nil
	}
	reply := make(chan tuiPromptResult, 1)
	i.ch <- tuiPromptRequestMsg{
		Kind:      tuiPromptKindInput,
		SourceID:  sourceIDFromPrompt(prompt),
		Prompt:    prompt,
		MaskInput: shouldMaskPromptInput(prompt),
		Reply:     reply,
	}
	result := <-reply
	if result.Err != nil {
		return "", result.Err
	}
	if result.Canceled {
		return "", engine.ErrInterrupted
	}
	return result.Input, nil
}

func (i *tuiSyncInteraction) SelectRows(sourceID string, rows []engine.PlanRow) ([]int, bool, error) {
	if i == nil || i.ch == nil {
		selected := make([]int, 0, len(rows))
		for _, row := range rows {
			if row.Toggleable && row.SelectedByDefault {
				selected = append(selected, row.Index)
			}
		}
		sort.Ints(selected)
		return selected, false, nil
	}
	source, ok := i.sourceByID[sourceID]
	if !ok {
		source.ID = sourceID
	}
	reply := make(chan tuiPlanSelectResult, 1)
	i.ch <- tuiPlanSelectRequestMsg{
		SourceID: sourceID,
		Rows:     append([]engine.PlanRow{}, rows...),
		Details:  buildPlanSourceDetails(source, i.defaults, i.planLimit, i.dryRun),
		Reply:    reply,
	}
	result := <-reply
	return result.SelectedIndices, result.Canceled, result.Err
}

func sourceIDFromPrompt(prompt string) string {
	trimmed := strings.TrimSpace(prompt)
	if strings.HasPrefix(trimmed, "[") {
		if end := strings.Index(trimmed, "]"); end > 1 {
			return strings.TrimSpace(trimmed[1:end])
		}
	}
	return "sync"
}

func shouldMaskPromptInput(prompt string) bool {
	lower := strings.ToLower(prompt)
	return strings.Contains(lower, "arl") ||
		strings.Contains(lower, "token") ||
		strings.Contains(lower, "secret") ||
		strings.Contains(lower, "password")
}

type tuiInitInteraction struct {
	ch chan tea.Msg
}

func (i *tuiInitInteraction) Confirm(prompt string, defaultYes bool) (bool, error) {
	if i == nil || i.ch == nil {
		return defaultYes, nil
	}
	reply := make(chan tuiPromptResult, 1)
	i.ch <- tuiPromptRequestMsg{
		Kind:       tuiPromptKindConfirm,
		SourceID:   "init",
		Prompt:     prompt,
		DefaultYes: defaultYes,
		Reply:      reply,
	}
	result := <-reply
	if result.Err != nil {
		return false, result.Err
	}
	if result.Canceled {
		return false, nil
	}
	return result.Confirmed, nil
}

func (i *tuiInitInteraction) Input(prompt string) (string, error) {
	if i == nil || i.ch == nil {
		return "", nil
	}
	reply := make(chan tuiPromptResult, 1)
	i.ch <- tuiPromptRequestMsg{
		Kind:      tuiPromptKindInput,
		SourceID:  "init",
		Prompt:    prompt,
		MaskInput: shouldMaskPromptInput(prompt),
		Reply:     reply,
	}
	result := <-reply
	if result.Err != nil {
		return "", result.Err
	}
	if result.Canceled {
		return "", nil
	}
	return result.Input, nil
}

func (i *tuiInitInteraction) SelectRows(sourceID string, rows []engine.PlanRow) ([]int, bool, error) {
	selected := make([]int, 0, len(rows))
	for _, row := range rows {
		if row.Toggleable && row.SelectedByDefault {
			selected = append(selected, row.Index)
		}
	}
	sort.Ints(selected)
	return selected, false, nil
}

type tuiDoctorModel struct {
	app    *AppContext
	lines  []string
	done   bool
	runErr error
}

type tuiDoctorDoneMsg struct {
	Lines []string
	Err   error
}

func newTUIDoctorModel(app *AppContext) tuiDoctorModel {
	return tuiDoctorModel{app: app}
}

func (m tuiDoctorModel) Init() tea.Cmd {
	return func() tea.Msg {
		cfg, err := loadConfig(m.app)
		if err != nil {
			return tuiDoctorDoneMsg{Err: err}
		}
		if len(cfg.Sources) > 0 {
			if err := config.Validate(cfg); err != nil {
				return tuiDoctorDoneMsg{Err: err}
			}
		}
		report := workflows.DoctorUseCase{Checker: doctor.NewChecker()}.Run(context.Background(), cfg)
		checks := append([]doctor.Check{}, report.Checks...)
		sort.SliceStable(checks, func(i, j int) bool {
			return checks[i].Name < checks[j].Name
		})
		lines := make([]string, 0, len(checks)+1)
		for _, check := range checks {
			lines = append(lines, fmt.Sprintf("[%s] %s: %s", check.Severity, check.Name, check.Message))
		}
		if report.HasErrors() {
			lines = append(lines, fmt.Sprintf("doctor found %d error(s)", report.ErrorCount()))
		}
		return tuiDoctorDoneMsg{Lines: lines}
	}
}

func (m tuiDoctorModel) Update(msg tea.Msg) (tuiDoctorModel, tea.Cmd) {
	switch typed := msg.(type) {
	case tuiDoctorDoneMsg:
		m.done = true
		m.lines = typed.Lines
		m.runErr = typed.Err
	}
	return m, nil
}

func (m tuiDoctorModel) View() string {
	lines := []string{}
	if !m.done {
		lines = append(lines, "Running checks...")
		return strings.Join(lines, "\n")
	}
	if m.runErr != nil {
		lines = append(lines, "Doctor failed: "+m.runErr.Error())
		return strings.Join(lines, "\n")
	}
	lines = append(lines, m.lines...)
	return strings.Join(lines, "\n")
}

type tuiValidateModel struct {
	app    *AppContext
	done   bool
	result string
}

func newTUIValidateModel(app *AppContext) tuiValidateModel {
	return tuiValidateModel{app: app}
}

type tuiValidateDoneMsg struct {
	Result string
}

func (m tuiValidateModel) Init() tea.Cmd {
	return func() tea.Msg {
		cfg, err := loadConfig(m.app)
		if err != nil {
			return tuiValidateDoneMsg{Result: "Validate failed: " + err.Error()}
		}
		if err := (workflows.ValidateUseCase{}).Run(cfg); err != nil {
			return tuiValidateDoneMsg{Result: "Validate failed: " + err.Error()}
		}
		return tuiValidateDoneMsg{Result: "Config is valid."}
	}
}

func (m tuiValidateModel) Update(msg tea.Msg) (tuiValidateModel, tea.Cmd) {
	switch typed := msg.(type) {
	case tuiValidateDoneMsg:
		m.done = true
		m.result = typed.Result
	}
	return m, nil
}

func (m tuiValidateModel) View() string {
	lines := []string{}
	if !m.done {
		lines = append(lines, "Validating...")
		return strings.Join(lines, "\n")
	}
	lines = append(lines, m.result)
	return strings.Join(lines, "\n")
}

type tuiInitModel struct {
	app     *AppContext
	running bool
	done    bool
	result  string
	eventCh chan tea.Msg
	prompt  *tuiInteractionPromptState
}

func newTUIInitModel(app *AppContext) tuiInitModel {
	return tuiInitModel{app: app}
}

type tuiInitDoneMsg struct {
	Result string
}

func (m tuiInitModel) Init() tea.Cmd {
	return func() tea.Msg {
		return tuiInitStartMsg{}
	}
}

type tuiInitStartMsg struct{}

func (m tuiInitModel) waitRunMsgCmd() tea.Cmd {
	ch := m.eventCh
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func (m tuiInitModel) startRunCmd() tea.Cmd {
	ch := m.eventCh
	return func() tea.Msg {
		interaction := &tuiInitInteraction{ch: ch}
		res, err := (workflows.InitUseCase{}).Run(workflows.InitRequest{
			ConfigPath: m.app.Opts.ConfigPath,
			Force:      false,
			NoInput:    m.app.Opts.NoInput,
			IsTTY:      true,
		}, interaction)
		if err != nil {
			ch <- tuiInitDoneMsg{Result: "Init failed: " + err.Error()}
			close(ch)
			return nil
		}
		if res.Canceled {
			ch <- tuiInitDoneMsg{Result: "Initialization canceled."}
			close(ch)
			return nil
		}
		ch <- tuiInitDoneMsg{Result: fmt.Sprintf("Wrote config: %s\nEnsured state dir: %s", res.ConfigPath, res.StateDir)}
		close(ch)
		return nil
	}
}

func (m tuiInitModel) Update(msg tea.Msg) (tuiInitModel, tea.Cmd) {
	switch typed := msg.(type) {
	case tuiInitStartMsg:
		m.running = true
		m.done = false
		m.result = ""
		m.eventCh = make(chan tea.Msg, 32)
		return m, tea.Batch(m.startRunCmd(), m.waitRunMsgCmd())
	case tuiPromptRequestMsg:
		m.prompt = &tuiInteractionPromptState{
			kind:       typed.Kind,
			sourceID:   typed.SourceID,
			prompt:     typed.Prompt,
			defaultYes: typed.DefaultYes,
			maskInput:  typed.MaskInput,
			reply:      typed.Reply,
		}
		return m, nil
	case tea.KeyMsg:
		if m.prompt != nil {
			switch m.prompt.kind {
			case tuiPromptKindConfirm:
				switch typed.String() {
				case "y":
					m.prompt.reply <- tuiPromptResult{Confirmed: true}
					m.prompt = nil
					return m, m.waitRunMsgCmd()
				case "n":
					m.prompt.reply <- tuiPromptResult{Confirmed: false}
					m.prompt = nil
					return m, m.waitRunMsgCmd()
				case "enter":
					m.prompt.reply <- tuiPromptResult{Confirmed: m.prompt.defaultYes}
					m.prompt = nil
					return m, m.waitRunMsgCmd()
				case "esc", "q":
					m.prompt.reply <- tuiPromptResult{Canceled: true}
					m.prompt = nil
					return m, m.waitRunMsgCmd()
				default:
					return m, nil
				}
			case tuiPromptKindInput:
				switch typed.String() {
				case "enter":
					m.prompt.reply <- tuiPromptResult{Input: strings.TrimSpace(m.prompt.input)}
					m.prompt = nil
					return m, m.waitRunMsgCmd()
				case "esc", "q":
					m.prompt.reply <- tuiPromptResult{Canceled: true}
					m.prompt = nil
					return m, m.waitRunMsgCmd()
				case "backspace", "ctrl+h":
					if len(m.prompt.input) > 0 {
						m.prompt.input = m.prompt.input[:len(m.prompt.input)-1]
					}
					return m, nil
				default:
					if len(typed.Runes) > 0 {
						m.prompt.input += string(typed.Runes)
					}
					return m, nil
				}
			default:
				return m, nil
			}
		}
		return m, nil
	case tuiInitDoneMsg:
		m.running = false
		m.done = true
		m.result = typed.Result
	}
	return m, nil
}

func (m tuiInitModel) View() string {
	lines := []string{}
	if m.running && !m.done {
		lines = append(lines, "Running init...")
		return strings.Join(lines, "\n")
	}
	if !m.done {
		lines = append(lines, "Preparing init...")
		return strings.Join(lines, "\n")
	}
	lines = append(lines, m.result)
	return strings.Join(lines, "\n")
}

func (m tuiInitModel) promptView() string {
	state := m.prompt
	if state == nil {
		return ""
	}
	switch state.kind {
	case tuiPromptKindConfirm:
		defaultLabel := "no"
		if state.defaultYes {
			defaultLabel = "yes"
		}
		return strings.Join([]string{
			"Init Workflow",
			state.prompt,
			fmt.Sprintf("y: yes  n: no  enter: default (%s)  esc/q: cancel", defaultLabel),
		}, "\n")
	case tuiPromptKindInput:
		displayInput := state.input
		if state.maskInput {
			displayInput = strings.Repeat("*", len(state.input))
		}
		return strings.Join([]string{
			"Init Workflow",
			state.prompt,
			fmt.Sprintf("value=%q", displayInput),
			"type to edit  backspace: delete  enter: submit  esc/q: cancel",
		}, "\n")
	default:
		return "Init Workflow\nUnknown prompt state"
	}
}

func (m tuiInitModel) hasActivePrompt() bool {
	return m.prompt != nil
}
