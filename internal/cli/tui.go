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
	"github.com/charmbracelet/lipgloss"
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
	tuiScreenSync
	tuiScreenDoctor
	tuiScreenValidate
	tuiScreenInit
)

const tuiDefaultPlanLimit = 10
const tuiMinPlanLimit = 1

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
		menuItems:     []string{"sync", "doctor", "validate", "init", "quit"},
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
		return m, nil
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
				case "sync":
					m.screen = tuiScreenSync
					m.syncModel = newTUISyncModel(m.app)
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
	case tuiScreenSync:
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
	case tuiScreenSync:
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
	header := lipgloss.NewStyle().Bold(true).Render("UDL TUI")
	footer := "up/down: move  enter: select  esc: back  q: quit"
	body := ""

	switch m.screen {
	case tuiScreenMenu:
		lines := []string{"", "Workflows:"}
		for i, item := range m.menuItems {
			cursor := " "
			if i == m.menuCursor {
				cursor = ">"
			}
			lines = append(lines, fmt.Sprintf("%s %s", cursor, item))
		}
		body = strings.Join(lines, "\n")
	case tuiScreenSync:
		body = m.syncModel.View()
	case tuiScreenDoctor:
		body = m.doctorModel.View()
	case tuiScreenValidate:
		body = m.validateModel.View()
	case tuiScreenInit:
		body = m.initModel.View()
	}

	if m.debugMessages {
		footer = footer + "  |  last_msg=" + m.lastMsgType
	}
	content := lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", footer)
	return lipgloss.NewStyle().Padding(1, 2).Render(content)
}

type tuiSyncModel struct {
	app                  *AppContext
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
	planEnabled          bool
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
	eventCh              chan tea.Msg
	planPrompt           *tuiPlanPromptState
	interactionPrompt    *tuiInteractionPromptState
	planLimitInputActive bool
	planLimitInput       string
	planLimitInputErr    string
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

type tuiPlanPromptState struct {
	sourceID string
	rows     []engine.PlanRow
	details  planSourceDetails
	reply    chan tuiPlanSelectResult
	cursor   int
	selected map[int]bool
}

func newTUISyncModel(app *AppContext) tuiSyncModel {
	dryRun := false
	if app != nil {
		dryRun = app.Opts.DryRun
	}
	return tuiSyncModel{
		app:       app,
		selected:  map[string]bool{},
		events:    []string{},
		dryRun:    dryRun,
		planLimit: tuiDefaultPlanLimit,
		progress:  output.NewStructuredProgressTracker(nil),
	}
}

func (m tuiSyncModel) Init() tea.Cmd {
	return func() tea.Msg {
		cfg, err := loadConfig(m.app)
		return tuiConfigLoadedMsg{cfg: cfg, err: err}
	}
}

func (m tuiSyncModel) Update(msg tea.Msg) (tuiSyncModel, tea.Cmd) {
	switch typed := msg.(type) {
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
		m.planPrompt = newTUIPlanPromptState(typed)
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
		if m.planPrompt != nil {
			switch typed.String() {
			case "up", "k":
				if m.planPrompt.cursor > 0 {
					m.planPrompt.cursor--
				}
				return m, nil
			case "down", "j":
				if m.planPrompt.cursor < len(m.planPrompt.rows)-1 {
					m.planPrompt.cursor++
				}
				return m, nil
			case " ":
				if len(m.planPrompt.rows) == 0 {
					return m, nil
				}
				row := m.planPrompt.rows[m.planPrompt.cursor]
				if !row.Toggleable {
					return m, nil
				}
				m.planPrompt.selected[row.Index] = !m.planPrompt.selected[row.Index]
				return m, nil
			case "a":
				for _, row := range m.planPrompt.rows {
					if row.Toggleable {
						m.planPrompt.selected[row.Index] = true
					}
				}
				return m, nil
			case "n":
				for _, row := range m.planPrompt.rows {
					if row.Toggleable {
						m.planPrompt.selected[row.Index] = false
					}
				}
				return m, nil
			case "enter":
				m.planPrompt.reply <- tuiPlanSelectResult{SelectedIndices: m.planPrompt.selectedIndices()}
				m.planPrompt = nil
				return m, m.waitRunMsgCmd()
			case "ctrl+c", "q", "esc":
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
		case "p":
			m.planEnabled = !m.planEnabled
			m.validationErr = ""
			return m, nil
		case "d":
			m.dryRun = !m.dryRun
			m.validationErr = ""
			return m, nil
		case "a":
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
			m.scanGaps = !m.scanGaps
			m.validationErr = ""
			return m, nil
		case "f":
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
			m.timeoutInputActive = false
			m.timeoutInput = ""
			m.timeoutInputErr = ""
			m.planLimitInputActive = true
			m.planLimitInput = ""
			m.planLimitInputErr = ""
			return m, nil
		case "]":
			if m.planLimit == 0 {
				m.planLimit = tuiDefaultPlanLimit
			} else {
				m.planLimit++
			}
			m.validationErr = ""
			return m, nil
		case "[":
			if m.planLimit == 0 {
				m.planLimit = tuiDefaultPlanLimit
			} else if m.planLimit > tuiMinPlanLimit {
				m.planLimit--
			}
			m.validationErr = ""
			return m, nil
		case "u":
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
		for _, outcome := range m.progress.DrainTrackOutcomes() {
			m.events = append(m.events, output.FormatCompactTrackOutcome(outcome, output.CompactTrackStatusNames))
		}
		if line, ok := tuiSyncHistoryLine(typed.Event); ok {
			m.events = append(m.events, line)
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
		m.result = typed.Result
		m.runErr = typed.Err
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
		req := workflows.SyncRequest{
			SourceIDs:        selectedIDs,
			DryRun:           m.dryRun,
			TimeoutOverride:  m.timeoutOverride,
			Plan:             m.planEnabled,
			PlanLimit:        m.planLimit,
			AllowPrompt:      m.app != nil && !m.app.Opts.NoInput,
			AskOnExisting:    m.askOnExisting,
			AskOnExistingSet: m.askOnExistingSet,
			ScanGaps:         m.scanGaps,
			NoPreflight:      m.noPreflight,
			TrackStatus:      engine.TrackStatusNames,
		}
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
	if !m.cfgLoaded {
		return "Loading config..."
	}
	if m.cfgErr != nil {
		return fmt.Sprintf("Config load failed: %v", m.cfgErr)
	}
	if m.planPrompt != nil {
		return m.planPromptView()
	}
	if m.interactionPrompt != nil {
		return m.interactionPromptView()
	}
	lines := []string{
		"Sync Workflow",
		"j/k: move  space: toggle source  p: plan  d: dry-run  a: ask-existing  g: scan-gaps  f: no-preflight  t: timeout  enter: run  esc: back",
		"[/]: plan-limit  l: type limit  u: unlimited  x/ctrl+c: cancel active run",
		fmt.Sprintf("dry_run=%t  timeout=%s", m.dryRun, formatTimeoutOverride(m.timeoutOverride)),
		fmt.Sprintf("plan_mode=%t  plan_limit=%s", m.planEnabled, formatPlanLimit(m.planLimit)),
		fmt.Sprintf("ask_on_existing=%s  scan_gaps=%t  no_preflight=%t", formatAskOnExisting(m.askOnExistingSet), m.scanGaps, m.noPreflight),
	}
	if m.planLimitInputActive {
		lines = append(lines,
			"plan_limit_input: type number (0 = unlimited), enter apply, esc cancel",
			fmt.Sprintf("current_input=%q", m.planLimitInput),
		)
		if m.planLimitInputErr != "" {
			lines = append(lines, "input_error: "+m.planLimitInputErr)
		}
	}
	if m.timeoutInputActive {
		lines = append(lines,
			"timeout_input: type Go duration (e.g. 10m, 90s), enter apply, esc cancel, empty = default",
			fmt.Sprintf("current_input=%q", m.timeoutInput),
		)
		if m.timeoutInputErr != "" {
			lines = append(lines, "input_error: "+m.timeoutInputErr)
		}
	}
	if m.validationErr != "" {
		lines = append(lines, "validation_error: "+m.validationErr)
	}
	lines = append(lines, "", "Sources:")
	if len(m.sources) == 0 {
		lines = append(lines, "  (no enabled sources)")
	}
	for idx, source := range m.sources {
		cursor := " "
		if idx == m.cursor {
			cursor = ">"
		}
		marker := "[ ]"
		if m.selected[source.ID] {
			marker = "[x]"
		}
		lines = append(lines, fmt.Sprintf("%s %s %s (%s/%s)", cursor, marker, source.ID, source.Type, source.Adapter.Kind))
	}
	lines = append(lines, "")
	if m.running {
		lines = append(lines, "Running sync... (press x or ctrl+c to cancel)")
		if m.cancelRequested {
			lines = append(lines, "Cancellation requested, waiting for adapter shutdown...")
		}
	}
	if progressLines := m.progressLines(); len(progressLines) > 0 {
		lines = append(lines, "", "Progress:")
		for _, line := range progressLines {
			lines = append(lines, "  "+line)
		}
	}
	if m.done {
		if m.runErr != nil {
			lines = append(lines, fmt.Sprintf("Run failed: %v", m.runErr))
		} else {
			lines = append(lines, fmt.Sprintf("Run finished: attempted=%d succeeded=%d failed=%d skipped=%d", m.result.Attempted, m.result.Succeeded, m.result.Failed, m.result.Skipped))
		}
	}
	if len(m.events) > 0 {
		lines = append(lines, "", "Activity:")
		start := 0
		if len(m.events) > 12 {
			start = len(m.events) - 12
		}
		for _, line := range m.events[start:] {
			lines = append(lines, "  "+line)
		}
	}
	return strings.Join(lines, "\n")
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

func validateTUISyncOptions(m tuiSyncModel) string {
	if m.planEnabled && m.scanGaps {
		return "plan mode cannot be combined with scan-gaps"
	}
	if m.planEnabled && m.askOnExistingSet {
		return "plan mode cannot be combined with ask-on-existing"
	}
	if m.planEnabled && m.noPreflight {
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
			"Sync Workflow",
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
			"Sync Workflow",
			fmt.Sprintf("[%s] input", state.sourceID),
			state.prompt,
			fmt.Sprintf("value=%q", displayInput),
			"type to edit  backspace: delete  enter: submit  esc/q: cancel run",
		}, "\n")
	default:
		return "Sync Workflow\nUnknown prompt state"
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
		"Sync Workflow",
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
		marker := "[-]"
		if row.Toggleable {
			if state.selected[row.Index] {
				marker = "[x]"
			} else {
				marker = "[ ]"
			}
		}
		title := strings.TrimSpace(row.Title)
		if title == "" {
			title = "(untitled)"
		}
		lines = append(lines, fmt.Sprintf("%s %s %3d  %-16s  %s (%s)", cursor, marker, row.Index, string(row.Status), title, row.RemoteID))
	}
	selectedCount := 0
	totalToggleable := 0
	for _, row := range state.rows {
		if row.Toggleable {
			totalToggleable++
			if state.selected[row.Index] {
				selectedCount++
			}
		}
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("Selected: %d/%d toggleable tracks", selectedCount, totalToggleable))
	return strings.Join(lines, "\n")
}

func newTUIPlanPromptState(req tuiPlanSelectRequestMsg) *tuiPlanPromptState {
	selected := map[int]bool{}
	for _, row := range req.Rows {
		if row.Toggleable && row.SelectedByDefault {
			selected[row.Index] = true
		}
	}
	return &tuiPlanPromptState{
		sourceID: req.SourceID,
		rows:     append([]engine.PlanRow{}, req.Rows...),
		details:  req.Details,
		reply:    req.Reply,
		selected: selected,
	}
}

func (s *tuiPlanPromptState) selectedIndices() []int {
	if s == nil {
		return nil
	}
	out := make([]int, 0, len(s.selected))
	for _, row := range s.rows {
		if !row.Toggleable {
			continue
		}
		if s.selected[row.Index] {
			out = append(out, row.Index)
		}
	}
	sort.Ints(out)
	return out
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
	lines := []string{"Doctor Workflow", "esc: back", ""}
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
	lines := []string{"Validate Workflow", "esc: back", ""}
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
	if m.prompt != nil {
		return m.promptView()
	}
	lines := []string{"Init Workflow", "esc: back", ""}
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
