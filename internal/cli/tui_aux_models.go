package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	workflows "github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/doctor"
)

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
