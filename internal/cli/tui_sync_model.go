package cli

import (
	"context"
	"fmt"
	"io"
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
	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/output"
	compactstate "github.com/jaa/update-downloads/internal/output/compact"
)

func newTUISyncModel(app *AppContext, mode tuiSyncWorkflowMode) tuiSyncModel {
	dryRun := false
	if app != nil {
		dryRun = app.Opts.DryRun
	}
	interactivePhase := tuiInteractivePhaseIdle
	return tuiSyncModel{
		app:                   app,
		mode:                  mode,
		selected:              map[string]bool{},
		events:                []string{},
		dryRun:                dryRun,
		planLimit:             tuiDefaultPlanLimit,
		progress:              output.NewStructuredProgressTracker(nil),
		standardSummaries:     map[string]*tuiStandardSyncSourceSummary{},
		interactivePhase:      interactivePhase,
		interactiveSelections: map[string]*tuiInteractiveSelectionState{},
		interactiveTracker:    newTUISyncRunTracker(),
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
		m.storeInteractiveSelection(state)
		if m.interactiveTracker != nil {
			m.interactiveTracker.UpsertSelectionState(state)
		}
		m.setInteractiveDisplaySource(typed.SourceID)
		if m.isInteractiveSyncWorkflow() {
			m.interactivePhase = tuiInteractivePhaseReview
		}
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
		if !m.isInteractiveSyncWorkflow() && typed.String() == "p" && !m.timeoutInputActive && !m.planLimitInputActive && m.interactionPrompt == nil {
			m.toggleStandardActivity(m.currentShellLayout())
			return m, nil
		}
		if m.isInteractiveSyncWorkflow() && typed.String() == "p" && !m.timeoutInputActive && !m.planLimitInputActive && m.interactionPrompt == nil {
			if state := m.ensureInteractiveSelectionForSource(m.currentInteractiveDisplaySourceID()); state != nil {
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
			filterPhase := tuiInteractivePhaseReview
			m.planPrompt.syncFilterForPhase(filterPhase)
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
					m.planPrompt.ensureCursorVisible(filterPhase)
				}
				return m, nil
			}
			if m.planPrompt.focusFilters {
				filters := m.planPrompt.filtersForPhase(filterPhase)
				switch typed.String() {
				case "up", "k":
					if m.planPrompt.filterCursor > 0 {
						m.planPrompt.filterCursor--
					}
					return m, nil
				case "down", "j":
					if m.planPrompt.filterCursor < len(filters)-1 {
						m.planPrompt.filterCursor++
					}
					return m, nil
				case " ", "enter":
					m.planPrompt.filter = filters[m.planPrompt.filterCursor]
					m.planPrompt.focusFilters = false
					m.planPrompt.ensureCursorVisible(filterPhase)
					return m, nil
				case "ctrl+c", "q", "esc":
					m.planPrompt.reply <- tuiPlanSelectResult{Canceled: true}
					m.planPrompt = nil
					m.syncDisplayedInteractiveSelection()
					return m, m.waitRunMsgCmd()
				default:
					return m, nil
				}
			}
			switch typed.String() {
			case "up", "k":
				m.planPrompt.moveCursor(-1, filterPhase)
				return m, nil
			case "down", "j":
				m.planPrompt.moveCursor(1, filterPhase)
				return m, nil
			case " ":
				row, ok := m.planPrompt.currentRow(filterPhase)
				if !ok {
					return m, nil
				}
				if !row.Toggleable {
					return m, nil
				}
				m.planPrompt.setSelected(row.Index, !row.Selected)
				return m, nil
			case "a":
				for _, row := range m.planPrompt.filteredRowsForPhase(filterPhase) {
					if row.Toggleable {
						m.planPrompt.setSelected(row.Index, true)
					}
				}
				return m, nil
			case "n":
				for _, row := range m.planPrompt.filteredRowsForPhase(filterPhase) {
					if row.Toggleable {
						m.planPrompt.setSelected(row.Index, false)
					}
				}
				return m, nil
			case "enter":
				m.confirmInteractiveSelection(m.planPrompt.sourceID)
				if m.isInteractiveSyncWorkflow() {
					m.interactivePhase = tuiInteractivePhaseSyncing
					if m.interactiveTracker != nil {
						m.interactiveTracker.MarkRuntimeStarted(time.Now())
					}
				}
				m.planPrompt.syncFilterForPhase(tuiInteractivePhaseSyncing)
				m.setInteractiveDisplaySource(m.planPrompt.sourceID)
				m.planPrompt.reply <- tuiPlanSelectResult{SelectedIndices: m.planPrompt.selectedIndices()}
				m.planPrompt = nil
				m.syncDisplayedInteractiveSelection()
				return m, m.waitRunMsgCmd()
			case "ctrl+c", "q", "esc":
				m.planPrompt.reply <- tuiPlanSelectResult{Canceled: true}
				m.planPrompt = nil
				m.syncDisplayedInteractiveSelection()
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
		if m.isInteractiveSyncWorkflow() && m.planPrompt == nil {
			if state := m.currentInteractiveSelection(); state != nil && len(state.rows) > 0 {
				if m.handleInteractiveSelectionBrowseKey(typed.String()) {
					return m, nil
				}
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
			if m.isInteractiveSyncWorkflow() {
				if source, ok := m.focusedInteractiveSource(); ok {
					m.setInteractiveDisplaySource(source.ID)
				}
			}
			return m, nil
		case "down", "j":
			if m.cursor < len(m.sources)-1 {
				m.cursor++
			}
			if m.isInteractiveSyncWorkflow() {
				if source, ok := m.focusedInteractiveSource(); ok {
					m.setInteractiveDisplaySource(source.ID)
				}
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
			if m.isInteractiveSyncWorkflow() {
				m.interactivePhase = tuiInteractivePhasePreflight
			}
			m.cancelRequested = false
			m.done = false
			m.runErr = nil
			m.validationErr = ""
			m.events = []string{}
			m.lastFailure = nil
			if !m.isInteractiveSyncWorkflow() {
				m.resetStandardSyncState()
			}
			m.resetInteractiveSourceLifecycle()
			if m.isInteractiveSyncWorkflow() {
				m.runStartedAt = time.Time{}
			} else {
				m.runStartedAt = time.Now()
			}
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
		m.observeStandardSyncEvent(typed.Event)
		if m.isInteractiveSyncWorkflow() {
			state := m.ensureInteractiveSelectionForEventSource(typed.Event.SourceID)
			if m.interactiveTracker != nil {
				m.interactiveTracker.UpsertSelectionState(state)
			}
			switch typed.Event.Event {
			case output.EventSourcePreflight, output.EventSourceStarted, output.EventSourceFinished, output.EventSourceFailed:
				m.setInteractiveDisplaySource(typed.Event.SourceID)
			}
			switch typed.Event.Event {
			case output.EventSourceStarted:
				m.interactivePhase = tuiInteractivePhaseSyncing
				if m.interactiveTracker != nil && m.interactiveTracker.startedAt.IsZero() {
					m.interactiveTracker.MarkRuntimeStarted(time.Now())
				}
			case output.EventSourcePreflight:
				if m.interactivePhase == tuiInteractivePhaseIdle {
					m.interactivePhase = tuiInteractivePhasePreflight
				}
			}
		}
		if !m.isInteractiveSyncWorkflow() && typed.Event.Event == output.EventSourceStarted && m.runStartedAt.IsZero() {
			m.runStartedAt = time.Now()
		}
		m.progress.ObserveEvent(typed.Event)
		if failure := tuiFailureStateFromEvent(typed.Event); failure != nil && !m.isInteractiveSyncWorkflow() {
			m.lastFailure = failure
		}
		outcomes := m.progress.DrainTrackOutcomes()
		for _, outcome := range outcomes {
			m.appendEventHistoryLine(output.FormatCompactTrackOutcome(outcome, output.CompactTrackStatusNames))
		}
		historyLine, historyOK := tuiSyncHistoryLine(typed.Event)
		if historyOK {
			m.appendEventHistoryLine(historyLine)
		}
		if m.isInteractiveSyncWorkflow() && m.interactiveTracker != nil {
			m.interactiveTracker.ObserveEvent(typed.Event, outcomes, historyLine, historyOK)
			if state := m.ensureInteractiveSelectionForEventSource(typed.Event.SourceID); state != nil {
				m.interactiveTracker.applyToSelectionState(state)
			}
			if m.planPrompt != nil && m.planPrompt.sourceID == typed.Event.SourceID {
				m.interactiveTracker.applyToSelectionState(m.planPrompt.tuiInteractiveSelectionState)
			}
		}
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
		if m.isInteractiveSyncWorkflow() {
			m.interactivePhase = tuiInteractivePhaseDone
			if m.interactiveTracker != nil {
				m.interactiveTracker.MarkRunFinished(time.Now())
			}
		}
		m.result = typed.Result
		m.runErr = typed.Err
		m.runFinishedAt = time.Now()
		m.syncDisplayedInteractiveSelection()
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
	return m.interactiveSelectionForSource(m.currentInteractiveDisplaySourceID())
}

func (m tuiSyncModel) currentShellLayout() tuiShellLayout {
	return newTUIShellLayout(m.width, m.height)
}

func (m tuiSyncModel) interactiveFilterPhase() tuiInteractiveSyncPhase {
	if m.planPrompt != nil {
		return tuiInteractivePhaseReview
	}
	if tuiInteractiveRuntimePhase(m.interactivePhase) {
		return m.interactivePhase
	}
	return tuiInteractivePhaseReview
}

func (m *tuiSyncModel) handleInteractiveSelectionBrowseKey(key string) bool {
	if m == nil {
		return false
	}
	state := m.currentInteractiveSelection()
	if state == nil || len(state.rows) == 0 {
		return false
	}
	filterPhase := m.interactiveFilterPhase()
	state.syncFilterForPhase(filterPhase)
	switch key {
	case "d", "t", "l", "[", "]", "u":
		return true
	}
	if key == "tab" {
		state.focusFilters = !state.focusFilters
		if !state.focusFilters {
			state.ensureCursorVisible(filterPhase)
		}
		return true
	}
	if state.focusFilters {
		filters := state.filtersForPhase(filterPhase)
		switch key {
		case "up", "k":
			if state.filterCursor > 0 {
				state.filterCursor--
			}
			return true
		case "down", "j":
			if state.filterCursor < len(filters)-1 {
				state.filterCursor++
			}
			return true
		case " ", "enter":
			state.filter = filters[state.filterCursor]
			state.focusFilters = false
			state.ensureCursorVisible(filterPhase)
			return true
		default:
			return false
		}
	}
	switch key {
	case "up", "k":
		state.moveCursor(-1, filterPhase)
		return true
	case "down", "j":
		state.moveCursor(1, filterPhase)
		return true
	case " ", "enter", "a", "n":
		return true
	default:
		return false
	}
}

func (m tuiSyncModel) elapsedLabel() string {
	if m.isInteractiveSyncWorkflow() && m.interactiveTracker != nil {
		return m.interactiveTracker.ElapsedLabel(time.Now())
	}
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
	case output.EventSyncStarted:
		return "", false
	case output.EventSourcePreflight:
		if strings.TrimSpace(event.SourceID) == "" {
			return "", false
		}
		planned, ok := tuiDetailInt(event.Details, "planned_download_count")
		if ok {
			return fmt.Sprintf("[%s] preflight ready (planned=%d)", event.SourceID, planned), true
		}
		return fmt.Sprintf("[%s] preflight ready", event.SourceID), true
	case output.EventSourceStarted:
		if strings.TrimSpace(event.SourceID) == "" {
			return "", false
		}
		return fmt.Sprintf("[%s] source started", event.SourceID), true
	default:
		if strings.TrimSpace(event.Message) == "" {
			return "", false
		}
		return event.Message, true
	}
}

func (m tuiSyncModel) lastFailureLines() []string {
	failure := m.lastFailure
	if m.isInteractiveSyncWorkflow() && m.interactiveTracker != nil {
		failure = m.interactiveTracker.LastFailure()
	}
	if failure == nil {
		return nil
	}
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

func (m *tuiSyncModel) appendEventHistoryLine(line string) {
	if m == nil {
		return
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	m.events = append(m.events, line)
	const maxHistory = 50
	if len(m.events) > maxHistory {
		m.events = append([]string(nil), m.events[len(m.events)-maxHistory:]...)
	}
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

func tuiDetailFloat(details map[string]any, key string) (float64, bool) {
	if details == nil {
		return 0, false
	}
	raw, ok := details[key]
	if !ok {
		return 0, false
	}
	switch value := raw.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
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
