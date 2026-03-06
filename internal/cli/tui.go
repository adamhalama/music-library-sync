package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
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
		if typed.String() == "esc" {
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
	app         *AppContext
	cfg         config.Config
	cfgLoaded   bool
	cfgErr      error
	sources     []config.Source
	selected    map[string]bool
	cursor      int
	planEnabled bool
	running     bool
	done        bool
	result      engine.SyncResult
	runErr      error
	events      []string
	eventCh     chan tea.Msg
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

func newTUISyncModel(app *AppContext) tuiSyncModel {
	return tuiSyncModel{
		app:      app,
		selected: map[string]bool{},
		events:   []string{},
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
	case tea.KeyMsg:
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
			PlanLimit:        10,
			AllowPrompt:      false,
			AskOnExisting:    false,
			AskOnExistingSet: false,
			ScanGaps:         false,
			NoPreflight:      false,
			TrackStatus:      engine.TrackStatusNames,
		}
		result, err := useCase.Run(context.Background(), cfg, req, workflows.NoopInteraction{})
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
	lines := []string{
		"Sync Workflow",
		"j/k: move  space: toggle source  p: toggle plan  enter: run  esc: back",
		fmt.Sprintf("plan_mode=%t", m.planEnabled),
		"",
		"Sources:",
	}
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
