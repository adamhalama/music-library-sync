package cli

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jaa/update-downloads/internal/auth"
)

type tuiScreen int

const (
	tuiScreenMenu tuiScreen = iota
	tuiScreenGetStarted
	tuiScreenCredentials
	tuiScreenInteractiveSync
	tuiScreenSync
	tuiScreenDoctor
	tuiScreenValidate
	tuiScreenConfigEditor
	tuiScreenInit
)

type tuiRootModel struct {
	app           *AppContext
	width         int
	height        int
	debugMessages bool
	lastMsgType   string

	screen           tuiScreen
	menuCursor       int
	menuItems        []string
	startupAttention *tuiStartupAttentionState

	onboardingModel  tuiOnboardingModel
	credentialsModel tuiCredentialsModel
	syncModel        tuiSyncModel
	doctorModel      tuiDoctorModel
	validateModel    tuiValidateModel
	configModel      tuiConfigEditorModel
	initModel        tuiInitModel
}

func newTUIRootModel(app *AppContext, debugMessages bool) tuiRootModel {
	model := tuiRootModel{
		app:           app,
		debugMessages: debugMessages,
		menuItems:     []string{"Run Sync", "Get Started", "Credentials", "Check System", "Advanced Config", "Quit"},
		screen:        tuiScreenMenu,
	}
	if startup, needsOnboarding := tuiDetectOnboardingState(app); needsOnboarding {
		model.screen = tuiScreenGetStarted
		model.onboardingModel = newTUIOnboardingModel(app, startup)
	} else {
		model.refreshStartupAttention()
	}
	return model
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
		case tuiScreenGetStarted:
			next, cmd := m.onboardingModel.Update(msg)
			m.onboardingModel = next
			return m, cmd
		case tuiScreenCredentials:
			next, cmd := m.credentialsModel.Update(msg)
			m.credentialsModel = next
			return m, cmd
		case tuiScreenDoctor:
			next, cmd := m.doctorModel.Update(msg)
			m.doctorModel = next
			return m, cmd
		case tuiScreenValidate:
			next, cmd := m.validateModel.Update(msg)
			m.validateModel = next
			return m, cmd
		case tuiScreenConfigEditor:
			next, cmd := m.configModel.Update(msg)
			m.configModel = next
			return m, cmd
		case tuiScreenInit:
			next, cmd := m.initModel.Update(msg)
			m.initModel = next
			return m, cmd
		default:
			return m, nil
		}
	case tuiConfigEditorExitAcceptedMsg:
		if m.screen == tuiScreenConfigEditor {
			m.screen = tuiScreenMenu
			m.refreshStartupAttention()
		}
		return m, nil
	case tea.KeyMsg:
		if typed.String() == "c" && m.canOpenCredentialsShortcut() {
			return m.openCredentials(m.recommendedCredentialFocus())
		}
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
				case "Get Started":
					startup, _ := tuiDetectOnboardingState(m.app)
					m.screen = tuiScreenGetStarted
					m.onboardingModel = newTUIOnboardingModel(m.app, startup)
					return m, m.onboardingModel.Init()
				case "Credentials":
					return m.openCredentials(m.recommendedCredentialFocus())
				case "Run Sync":
					m.screen = tuiScreenInteractiveSync
					m.syncModel = newTUISyncModel(m.app, tuiSyncWorkflowInteractive)
					return m, m.syncModel.Init()
				case "Check System":
					m.screen = tuiScreenDoctor
					m.doctorModel = newTUIDoctorModel(m.app)
					return m, m.doctorModel.Init()
				case "Advanced Config":
					m.screen = tuiScreenConfigEditor
					m.configModel = newTUIConfigEditorModel(m.app)
					return m, m.configModel.Init()
				default:
					return m, tea.Quit
				}
			}
		}
		if typed.String() == "esc" && m.canReturnToMenuOnEsc() {
			m.screen = tuiScreenMenu
			m.refreshStartupAttention()
			return m, nil
		}
	}

	switch m.screen {
	case tuiScreenGetStarted:
		next, cmd := m.onboardingModel.Update(msg)
		m.onboardingModel = next
		return m, cmd
	case tuiScreenCredentials:
		next, cmd := m.credentialsModel.Update(msg)
		m.credentialsModel = next
		return m, cmd
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
	case tuiScreenConfigEditor:
		next, cmd := m.configModel.Update(msg)
		m.configModel = next
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
	case tuiScreenGetStarted:
		return m.onboardingModel.allowBack()
	case tuiScreenCredentials:
		return m.credentialsModel.allowBack()
	case tuiScreenInteractiveSync, tuiScreenSync:
		return !m.syncModel.running &&
			!m.syncModel.hasActivePlanPrompt() &&
			!m.syncModel.hasActiveInteractionPrompt() &&
			!m.syncModel.hasActivePlanLimitInput() &&
			!m.syncModel.hasActiveTimeoutInput()
	case tuiScreenInit:
		return m.initModel.allowBack()
	case tuiScreenConfigEditor:
		return m.configModel.allowBack()
	case tuiScreenMenu:
		return false
	default:
		return true
	}
}

func (m tuiRootModel) canOpenCredentialsShortcut() bool {
	switch m.screen {
	case tuiScreenMenu:
		return m.startupAttention != nil
	case tuiScreenDoctor:
		return true
	case tuiScreenInteractiveSync, tuiScreenSync:
		return !m.syncModel.running &&
			!m.syncModel.hasActivePlanPrompt() &&
			!m.syncModel.hasActiveInteractionPrompt() &&
			!m.syncModel.hasActivePlanLimitInput() &&
			!m.syncModel.hasActiveTimeoutInput()
	default:
		return false
	}
}

func (m tuiRootModel) recommendedCredentialFocus() auth.CredentialKind {
	switch m.screen {
	case tuiScreenMenu:
		if m.startupAttention != nil {
			return m.startupAttention.PrimaryKind
		}
		return ""
	case tuiScreenDoctor:
		return m.doctorModel.recommendedCredentialKind()
	case tuiScreenInteractiveSync, tuiScreenSync:
		return m.syncModel.recommendedCredentialKind()
	default:
		return ""
	}
}

func (m tuiRootModel) openCredentials(focus auth.CredentialKind) (tuiRootModel, tea.Cmd) {
	m.screen = tuiScreenCredentials
	if focus == "" && m.startupAttention != nil {
		focus = m.startupAttention.PrimaryKind
	}
	m.credentialsModel = newTUICredentialsModel(m.app, focus)
	return m, m.credentialsModel.Init()
}

func (m *tuiRootModel) refreshStartupAttention() {
	m.startupAttention = tuiDetectStartupAttentionFn(m.app)
}

func (m tuiRootModel) View() string {
	layout := newTUIShellLayout(m.width, m.height)
	return renderTUIShell(m.shellState(layout), layout)
}
