package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jaa/update-downloads/internal/auth"
	workflows "github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/doctor"
)

type tuiDoctorPhase string

const (
	tuiDoctorPhaseRunning  tuiDoctorPhase = "running"
	tuiDoctorPhaseComplete tuiDoctorPhase = "complete"
)

type tuiDoctorSummaryState struct {
	Total      int
	ErrorCount int
	WarnCount  int
	InfoCount  int
}

type tuiDoctorModel struct {
	app      *AppContext
	phase    tuiDoctorPhase
	report   doctor.Report
	checks   []doctor.Check
	summary  tuiDoctorSummaryState
	setupErr error
}

type tuiDoctorDoneMsg struct {
	Report doctor.Report
	Err    error
}

func newTUIDoctorModel(app *AppContext) tuiDoctorModel {
	return tuiDoctorModel{
		app:   app,
		phase: tuiDoctorPhaseRunning,
	}
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
		return tuiDoctorDoneMsg{Report: report}
	}
}

func (m tuiDoctorModel) Update(msg tea.Msg) (tuiDoctorModel, tea.Cmd) {
	switch typed := msg.(type) {
	case tuiDoctorDoneMsg:
		m.phase = tuiDoctorPhaseComplete
		m.setupErr = typed.Err
		m.report = typed.Report
		m.checks = tuiSortedDoctorChecks(typed.Report.Checks)
		m.summary = tuiDoctorSummary(typed.Report.Checks)
	}
	return m, nil
}

func (m tuiDoctorModel) View() string {
	if m.phase != tuiDoctorPhaseComplete {
		return "Running checks..."
	}
	if m.setupErr != nil {
		return "Doctor failed: " + m.setupErr.Error()
	}
	lines := []string{
		fmt.Sprintf("Doctor complete: %d checks", m.summary.Total),
		fmt.Sprintf("errors=%d warnings=%d infos=%d", m.summary.ErrorCount, m.summary.WarnCount, m.summary.InfoCount),
	}
	for _, check := range m.checks {
		lines = append(lines, fmt.Sprintf("[%s] %s: %s", strings.ToUpper(string(check.Severity)), check.Name, check.Message))
	}
	return strings.Join(lines, "\n")
}

func (m tuiDoctorModel) recommendedCredentialKind() auth.CredentialKind {
	for _, check := range m.checks {
		lower := strings.ToLower(check.Message)
		switch {
		case strings.Contains(lower, "soundcloud client id"):
			return auth.CredentialKindSoundCloudClientID
		case strings.Contains(lower, "deezer arl"):
			return auth.CredentialKindDeemixARL
		case strings.Contains(lower, "spotify app credentials"):
			return auth.CredentialKindSpotifyApp
		}
	}
	return ""
}

type tuiValidatePhase string

const (
	tuiValidatePhaseRunning  tuiValidatePhase = "running"
	tuiValidatePhaseComplete tuiValidatePhase = "complete"
)

type tuiValidateFailureKind string

const (
	tuiValidateFailureNone     tuiValidateFailureKind = ""
	tuiValidateFailureLoad     tuiValidateFailureKind = "load"
	tuiValidateFailureValidate tuiValidateFailureKind = "validate"
)

type tuiValidateResultState struct {
	Valid              bool
	ConfigContextLabel string
	ConfigLoaded       bool
	SourceCount        int
	EnabledSourceCount int
	FailureKind        tuiValidateFailureKind
	DetailLines        []string
}

type tuiValidateModel struct {
	app    *AppContext
	phase  tuiValidatePhase
	result tuiValidateResultState
}

func newTUIValidateModel(app *AppContext) tuiValidateModel {
	return tuiValidateModel{
		app:   app,
		phase: tuiValidatePhaseRunning,
	}
}

type tuiValidateDoneMsg struct {
	Result tuiValidateResultState
}

func (m tuiValidateModel) Init() tea.Cmd {
	return func() tea.Msg {
		result := tuiValidateResultState{
			ConfigContextLabel: tuiValidateConfigContextLabel(m.app),
		}
		cfg, err := loadConfig(m.app)
		if err != nil {
			result.FailureKind = tuiValidateFailureLoad
			result.DetailLines = tuiSplitDetailLines(err.Error())
			return tuiValidateDoneMsg{Result: result}
		}
		result.ConfigLoaded = true
		result.SourceCount = len(cfg.Sources)
		result.EnabledSourceCount = tuiEnabledSourceCount(cfg)
		if err := (workflows.ValidateUseCase{}).Run(cfg); err != nil {
			result.FailureKind = tuiValidateFailureValidate
			result.DetailLines = tuiSplitDetailLines(err.Error())
			return tuiValidateDoneMsg{Result: result}
		}
		result.Valid = true
		result.DetailLines = []string{"Config schema and source definitions passed validation."}
		return tuiValidateDoneMsg{Result: result}
	}
}

func (m tuiValidateModel) Update(msg tea.Msg) (tuiValidateModel, tea.Cmd) {
	switch typed := msg.(type) {
	case tuiValidateDoneMsg:
		m.phase = tuiValidatePhaseComplete
		m.result = typed.Result
	}
	return m, nil
}

func (m tuiValidateModel) View() string {
	if m.phase != tuiValidatePhaseComplete {
		return "Validating..."
	}
	if m.result.Valid {
		return "Config is valid."
	}
	if m.result.FailureKind == tuiValidateFailureLoad {
		return "Config load failed."
	}
	return "Validation failed."
}

type tuiInitPhase string

const (
	tuiInitPhaseIntro    tuiInitPhase = "intro"
	tuiInitPhaseRunning  tuiInitPhase = "running"
	tuiInitPhaseDone     tuiInitPhase = "done"
	tuiInitPhaseCanceled tuiInitPhase = "canceled"
	tuiInitPhaseFailed   tuiInitPhase = "failed"
)

type tuiInitIntroState struct {
	ConfigPath   string
	StateDir     string
	ConfigExists bool
	PrepareErr   error
}

type tuiInitResultState struct {
	ConfigPath   string
	StateDir     string
	DetailLines  []string
	ConfigExists bool
}

type tuiInitModel struct {
	app     *AppContext
	phase   tuiInitPhase
	intro   tuiInitIntroState
	result  tuiInitResultState
	eventCh chan tea.Msg
	prompt  *tuiInteractionPromptState
}

func newTUIInitModel(app *AppContext) tuiInitModel {
	return tuiInitModel{
		app:   app,
		phase: tuiInitPhaseIntro,
		intro: tuiBuildInitIntroState(app),
	}
}

type tuiInitDoneMsg struct {
	ConfigPath string
	StateDir   string
	Canceled   bool
	Err        error
}

func (m tuiInitModel) Init() tea.Cmd {
	return nil
}

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
			ch <- tuiInitDoneMsg{
				ConfigPath: m.intro.ConfigPath,
				StateDir:   m.intro.StateDir,
				Err:        err,
			}
			close(ch)
			return nil
		}
		ch <- tuiInitDoneMsg{
			ConfigPath: res.ConfigPath,
			StateDir:   res.StateDir,
			Canceled:   res.Canceled,
		}
		close(ch)
		return nil
	}
}

func (m tuiInitModel) Update(msg tea.Msg) (tuiInitModel, tea.Cmd) {
	switch typed := msg.(type) {
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
	case tuiInitDoneMsg:
		m.eventCh = nil
		m.result = tuiInitResultState{
			ConfigPath:   firstNonEmpty(typed.ConfigPath, m.intro.ConfigPath),
			StateDir:     firstNonEmpty(typed.StateDir, m.intro.StateDir),
			ConfigExists: m.intro.ConfigExists,
		}
		switch {
		case typed.Err != nil:
			m.phase = tuiInitPhaseFailed
			m.result.DetailLines = tuiSplitDetailLines(typed.Err.Error())
		case typed.Canceled:
			m.phase = tuiInitPhaseCanceled
			m.result.DetailLines = []string{"Initialization canceled before writing a new config."}
		default:
			m.phase = tuiInitPhaseDone
			m.result.DetailLines = []string{"Starter config written.", "State directory ensured."}
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
		if typed.String() == "enter" && m.phase == tuiInitPhaseIntro && m.intro.PrepareErr == nil {
			m.phase = tuiInitPhaseRunning
			m.result = tuiInitResultState{}
			m.eventCh = make(chan tea.Msg, 32)
			return m, tea.Batch(m.startRunCmd(), m.waitRunMsgCmd())
		}
		return m, nil
	}
	return m, nil
}

func (m tuiInitModel) View() string {
	switch m.phase {
	case tuiInitPhaseRunning:
		return "Running init..."
	case tuiInitPhaseDone:
		return fmt.Sprintf("Wrote config: %s\nEnsured state dir: %s", m.result.ConfigPath, m.result.StateDir)
	case tuiInitPhaseCanceled:
		return "Initialization canceled."
	case tuiInitPhaseFailed:
		if len(m.result.DetailLines) > 0 {
			return "Init failed: " + strings.Join(m.result.DetailLines, "; ")
		}
		return "Init failed."
	default:
		if m.intro.PrepareErr != nil {
			return "Preparing init failed: " + m.intro.PrepareErr.Error()
		}
		return fmt.Sprintf("Ready to create config at %s", m.intro.ConfigPath)
	}
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

func (m tuiInitModel) allowBack() bool {
	if m.hasActivePrompt() {
		return false
	}
	return m.phase != tuiInitPhaseRunning
}

func tuiSortedDoctorChecks(checks []doctor.Check) []doctor.Check {
	sorted := append([]doctor.Check{}, checks...)
	sort.SliceStable(sorted, func(i, j int) bool {
		left := sorted[i]
		right := sorted[j]
		if tuiDoctorSeverityRank(left.Severity) != tuiDoctorSeverityRank(right.Severity) {
			return tuiDoctorSeverityRank(left.Severity) < tuiDoctorSeverityRank(right.Severity)
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.Message < right.Message
	})
	return sorted
}

func tuiDoctorSeverityRank(severity doctor.Severity) int {
	switch severity {
	case doctor.SeverityError:
		return 0
	case doctor.SeverityWarn:
		return 1
	default:
		return 2
	}
}

func tuiDoctorSummary(checks []doctor.Check) tuiDoctorSummaryState {
	summary := tuiDoctorSummaryState{Total: len(checks)}
	for _, check := range checks {
		switch check.Severity {
		case doctor.SeverityError:
			summary.ErrorCount++
		case doctor.SeverityWarn:
			summary.WarnCount++
		default:
			summary.InfoCount++
		}
	}
	return summary
}

func tuiEnabledSourceCount(cfg config.Config) int {
	count := 0
	for _, source := range cfg.Sources {
		if source.Enabled {
			count++
		}
	}
	return count
}

func tuiValidateConfigContextLabel(app *AppContext) string {
	if app == nil {
		return "default search path"
	}
	if explicit := strings.TrimSpace(app.Opts.ConfigPath); explicit != "" {
		if expanded, err := config.ExpandPath(explicit); err == nil && strings.TrimSpace(expanded) != "" {
			return expanded
		}
		return explicit
	}
	wd, err := os.Getwd()
	if err != nil {
		return "user config + project udl.yaml"
	}
	userPath, userErr := config.UserConfigPath()
	if userErr != nil {
		return config.ProjectConfigPath(wd)
	}
	return fmt.Sprintf("%s + %s", userPath, config.ProjectConfigPath(wd))
}

func tuiSplitDetailLines(text string) []string {
	lines := []string{}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func tuiBuildInitIntroState(app *AppContext) tuiInitIntroState {
	state := tuiInitIntroState{}
	configPath, err := tuiResolveInitConfigPath(app)
	if err != nil {
		state.PrepareErr = err
		return state
	}
	state.ConfigPath = configPath
	stateDir, err := config.ExpandPath(config.DefaultConfig().Defaults.StateDir)
	if err != nil {
		state.PrepareErr = fmt.Errorf("resolve default state directory: %w", err)
		return state
	}
	state.StateDir = stateDir
	if _, err := os.Stat(configPath); err == nil {
		state.ConfigExists = true
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		state.PrepareErr = fmt.Errorf("inspect config path %s: %w", configPath, err)
	}
	return state
}

func tuiResolveInitConfigPath(app *AppContext) (string, error) {
	if app != nil && strings.TrimSpace(app.Opts.ConfigPath) != "" {
		return config.ExpandPath(strings.TrimSpace(app.Opts.ConfigPath))
	}
	return config.UserConfigPath()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
