package cli

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type tuiScreen int

const (
	tuiScreenMenu tuiScreen = iota
	tuiScreenInteractiveSync
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
