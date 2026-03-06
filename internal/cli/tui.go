package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

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
		return !m.syncModel.running && !m.syncModel.hasActivePlanPrompt() && !m.syncModel.hasActivePlanLimitInput()
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
	planEnabled          bool
	planLimit            int
	running              bool
	done                 bool
	result               engine.SyncResult
	runErr               error
	events               []string
	eventCh              chan tea.Msg
	planPrompt           *tuiPlanPromptState
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

type tuiPlanPromptState struct {
	sourceID string
	rows     []engine.PlanRow
	details  planSourceDetails
	reply    chan tuiPlanSelectResult
	cursor   int
	selected map[int]bool
}

func newTUISyncModel(app *AppContext) tuiSyncModel {
	return tuiSyncModel{
		app:       app,
		selected:  map[string]bool{},
		events:    []string{},
		planLimit: tuiDefaultPlanLimit,
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
	case tea.KeyMsg:
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
		if !m.cfgLoaded || m.cfgErr != nil || m.running {
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
			return m, nil
		case "l":
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
			return m, nil
		case "[":
			if m.planLimit == 0 {
				m.planLimit = tuiDefaultPlanLimit
			} else if m.planLimit > tuiMinPlanLimit {
				m.planLimit--
			}
			return m, nil
		case "u":
			if m.planLimit == 0 {
				m.planLimit = tuiDefaultPlanLimit
			} else {
				m.planLimit = 0
			}
			return m, nil
		case "enter":
			m.running = true
			m.done = false
			m.runErr = nil
			m.events = []string{}
			m.eventCh = make(chan tea.Msg, 256)
			start := m.startRunCmd()
			return m, tea.Batch(start, m.waitRunMsgCmd())
		}
	case tuiSyncEventMsg:
		m.events = append(m.events, typed.Event.Message)
		return m, m.waitRunMsgCmd()
	case tuiSyncDoneMsg:
		m.running = false
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

func (m tuiSyncModel) startRunCmd() tea.Cmd {
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
			DryRun:           m.app.Opts.DryRun,
			Plan:             m.planEnabled,
			PlanLimit:        m.planLimit,
			AllowPrompt:      false,
			AskOnExisting:    false,
			AskOnExistingSet: false,
			ScanGaps:         false,
			NoPreflight:      false,
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
		result, err := useCase.Run(context.Background(), cfg, req, interaction)
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
	lines := []string{
		"Sync Workflow",
		"j/k: move  space: toggle source  p: toggle plan  [/]: plan-limit  l: type limit  u: unlimited  enter: run  esc: back",
		fmt.Sprintf("plan_mode=%t", m.planEnabled),
		fmt.Sprintf("plan_limit=%s", formatPlanLimit(m.planLimit)),
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
		lines = append(lines, "Running sync...")
	}
	if m.done {
		if m.runErr != nil {
			lines = append(lines, fmt.Sprintf("Run failed: %v", m.runErr))
		} else {
			lines = append(lines, fmt.Sprintf("Run finished: attempted=%d succeeded=%d failed=%d skipped=%d", m.result.Attempted, m.result.Succeeded, m.result.Failed, m.result.Skipped))
		}
	}
	if len(m.events) > 0 {
		lines = append(lines, "", "Event Log:")
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

func (m tuiSyncModel) hasActivePlanLimitInput() bool {
	return m.planLimitInputActive
}

func formatPlanLimit(limit int) string {
	if limit == 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%d", limit)
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
	return defaultYes, nil
}

func (i *tuiSyncInteraction) Input(prompt string) (string, error) {
	return "", nil
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
	app    *AppContext
	done   bool
	result string
}

func newTUIInitModel(app *AppContext) tuiInitModel {
	return tuiInitModel{app: app}
}

type tuiInitDoneMsg struct {
	Result string
}

func (m tuiInitModel) Init() tea.Cmd {
	return func() tea.Msg {
		res, err := (workflows.InitUseCase{}).Run(workflows.InitRequest{
			ConfigPath: m.app.Opts.ConfigPath,
			Force:      false,
			NoInput:    true,
			IsTTY:      false,
		}, workflows.NoopInteraction{})
		if err != nil {
			return tuiInitDoneMsg{Result: "Init failed: " + err.Error()}
		}
		if res.Canceled {
			return tuiInitDoneMsg{Result: "Initialization canceled."}
		}
		return tuiInitDoneMsg{Result: fmt.Sprintf("Wrote config: %s\nEnsured state dir: %s", res.ConfigPath, res.StateDir)}
	}
}

func (m tuiInitModel) Update(msg tea.Msg) (tuiInitModel, tea.Cmd) {
	switch typed := msg.(type) {
	case tuiInitDoneMsg:
		m.done = true
		m.result = typed.Result
	}
	return m, nil
}

func (m tuiInitModel) View() string {
	lines := []string{"Init Workflow", "esc: back", ""}
	if !m.done {
		lines = append(lines, "Running init...")
		return strings.Join(lines, "\n")
	}
	lines = append(lines, m.result)
	return strings.Join(lines, "\n")
}
