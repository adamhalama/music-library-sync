package cli

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func renderTUIShell(state tuiShellState, layout tuiShellLayout) string {
	theme := newTUIShellTheme()

	sidebar := ""
	if layout.Compact {
		sidebar = renderTUITopNav(state, theme, layout)
	} else {
		sidebar = renderTUISidebar(state, theme, layout)
	}

	titlebar := renderTUITitlebar(state, theme, layout)
	badges := renderTUIBadges(state, theme, layout)
	commandBar := renderTUICommandBar(state, theme, layout)
	shortcuts := renderTUIShortcuts(state, theme, layout)
	banner := renderTUIBanner(state, theme, layout)
	footer := renderTUIFooter(state, theme, layout)

	fixedParts := []string{titlebar}
	if sidebar != "" && layout.Compact {
		fixedParts = append(fixedParts, sidebar)
	}
	fixedParts = append(fixedParts, badges, commandBar, shortcuts, banner, footer)

	bodyHeight := shellBodyHeight(layout, theme, fixedParts)
	body := renderTUIBody(state, theme, layout, bodyHeight)

	mainParts := []string{titlebar}
	if sidebar != "" && layout.Compact {
		mainParts = append(mainParts, sidebar)
	}
	if badges != "" {
		mainParts = append(mainParts, badges)
	}
	if commandBar != "" {
		mainParts = append(mainParts, commandBar)
	}
	if shortcuts != "" {
		mainParts = append(mainParts, shortcuts)
	}
	if banner != "" {
		mainParts = append(mainParts, banner)
	}
	mainParts = append(mainParts, body, footer)

	main := strings.Join(filterEmptyStrings(mainParts), "\n")
	if !layout.Compact {
		main = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, main)
	}
	main = theme.frame.
		Width(styleContentWidth(layout.Width, theme.frame)).
		Render(main)

	if state.Modal != nil {
		main = renderTUIModal(main, state, theme, layout)
	}
	return lipgloss.NewStyle().
		Width(layout.Width).
		MaxHeight(layout.Height).
		AlignVertical(lipgloss.Top).
		Background(lipgloss.Color(tuiShellBackgroundColor)).
		Render(main)
}

func renderTUITitlebar(state tuiShellState, theme tuiShellTheme, layout tuiShellLayout) string {
	label := strings.ToUpper(strings.TrimSpace(state.AppLabel))
	if label == "" {
		label = "UDL"
	}
	title := strings.TrimSpace(state.ScreenTitle)
	if title != "" {
		label = label + " · " + strings.ToUpper(title)
	}
	return theme.titlebar.Width(styleContentWidth(shellMainSectionWidth(layout, theme), theme.titlebar)).Render(label)
}

func renderTUISidebar(state tuiShellState, theme tuiShellTheme, layout tuiShellLayout) string {
	lines := []string{}
	for _, section := range state.SidebarSections {
		if strings.TrimSpace(section.Title) != "" {
			lines = append(lines, theme.sectionLabel.Render(strings.ToUpper(section.Title)))
		}
		for _, item := range section.Items {
			lines = append(lines, renderTUISidebarItem(item, theme, layout))
		}
		lines = append(lines, "")
	}
	sidebar := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	return theme.sidebar.Width(styleContentWidth(shellSidebarWidth(layout, theme), theme.sidebar)).Render(sidebar)
}

func renderTUISidebarItem(item tuiSidebarItem, theme tuiShellTheme, layout tuiShellLayout) string {
	prefix := "  "
	style := theme.sidebarItem
	if item.Disabled {
		style = theme.disabled
	}
	if item.Active {
		prefix = "> "
		style = theme.sidebarActive
	}
	if !item.Disabled {
		style = applySidebarTone(style, item.Tone)
	}
	labelWidth := shellSidebarWidth(layout, theme) - theme.sidebar.GetHorizontalFrameSize() - 2
	if labelWidth < 8 {
		labelWidth = 8
	}
	label := truncateForWidth(item.Label, labelWidth)
	line := style.Render(prefix + label)
	if strings.TrimSpace(item.Meta) == "" {
		return line
	}
	meta := theme.sidebarMeta.Render("  " + truncateForWidth(item.Meta, labelWidth))
	return line + "\n" + meta
}

func applySidebarTone(style lipgloss.Style, tone string) lipgloss.Style {
	switch tone {
	case "info":
		return style.Copy().Foreground(lipgloss.Color("81"))
	case "success":
		return style.Copy().Foreground(lipgloss.Color("78"))
	case "warning":
		return style.Copy().Foreground(lipgloss.Color("179"))
	case "danger":
		return style.Copy().Foreground(lipgloss.Color("203"))
	case "muted":
		return style.Copy().Foreground(lipgloss.Color("243"))
	default:
		return style
	}
}

func renderTUITopNav(state tuiShellState, theme tuiShellTheme, layout tuiShellLayout) string {
	items := []string{}
	for _, section := range state.SidebarSections {
		for _, item := range section.Items {
			label := item.Label
			style := theme.muted
			if item.Disabled {
				style = theme.disabled
			}
			if item.Active {
				style = theme.sidebarActive
			}
			items = append(items, style.Render(label))
		}
	}
	return theme.navStrip.Width(styleContentWidth(shellMainSectionWidth(layout, theme), theme.navStrip)).Render(strings.Join(items, "  "))
}

func renderTUIBadges(state tuiShellState, theme tuiShellTheme, layout tuiShellLayout) string {
	if len(state.Badges) == 0 {
		return ""
	}
	rendered := make([]string, 0, len(state.Badges))
	for _, badge := range state.Badges {
		rendered = append(rendered, renderTUIBadge(badge, theme))
	}
	return theme.topbar.Width(styleContentWidth(shellMainSectionWidth(layout, theme), theme.topbar)).Render(strings.Join(rendered, " "))
}

func renderTUIBadge(badge tuiBadge, theme tuiShellTheme) string {
	if badge.Disabled {
		return theme.disabled.Copy().Background(lipgloss.Color(tuiShellBackgroundColor)).Padding(0, 1).Render(badge.Label)
	}
	switch badge.Tone {
	case "success":
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("78")).
			Background(lipgloss.Color("22")).
			Bold(true).
			Padding(0, 1).
			Render(badge.Label)
	case "warning":
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("179")).
			Background(lipgloss.Color("52")).
			Bold(true).
			Padding(0, 1).
			Render(badge.Label)
	case "danger":
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("203")).
			Background(lipgloss.Color("52")).
			Bold(true).
			Padding(0, 1).
			Render(badge.Label)
	case "muted":
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Background(lipgloss.Color("236")).
			Padding(0, 1).
			Render(badge.Label)
	default:
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("81")).
			Background(lipgloss.Color("17")).
			Bold(true).
			Padding(0, 1).
			Render(badge.Label)
	}
}

func renderTUICommandBar(state tuiShellState, theme tuiShellTheme, layout tuiShellLayout) string {
	if len(state.CommandSummary) == 0 {
		return ""
	}
	summary := strings.Join(state.CommandSummary, "  ")
	if layout.Compact {
		summary = strings.Join(state.CommandSummary, " | ")
	}
	return theme.commandBar.Width(styleContentWidth(shellMainSectionWidth(layout, theme), theme.commandBar)).Render(summary)
}

func renderTUIShortcuts(state tuiShellState, theme tuiShellTheme, layout tuiShellLayout) string {
	if len(state.Shortcuts) == 0 {
		return ""
	}
	rendered := make([]string, 0, len(state.Shortcuts))
	for _, shortcut := range state.Shortcuts {
		keyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("81")).
			Background(lipgloss.Color("236")).
			Bold(true).
			Padding(0, 1)
		labelStyle := theme.muted
		if shortcut.Disabled {
			keyStyle = keyStyle.Foreground(lipgloss.Color("240")).Background(lipgloss.Color("235"))
			labelStyle = theme.disabled
		}
		chunk := keyStyle.Render(shortcut.Key) + " " + labelStyle.Render(shortcut.Label)
		rendered = append(rendered, chunk)
	}
	return theme.shortcuts.Width(styleContentWidth(shellMainSectionWidth(layout, theme), theme.shortcuts)).Render(strings.Join(rendered, "  "))
}

func renderTUIBanner(state tuiShellState, theme tuiShellTheme, layout tuiShellLayout) string {
	if state.Banner == nil || strings.TrimSpace(state.Banner.Text) == "" {
		return ""
	}
	style := shellToneStyle(theme, state.Banner.Tone)
	bannerStyle := lipgloss.NewStyle().Padding(0, 1)
	return bannerStyle.Width(styleContentWidth(shellMainSectionWidth(layout, theme), bannerStyle)).Render(style.Render(state.Banner.Text))
}

func renderTUIBody(state tuiShellState, theme tuiShellTheme, layout tuiShellLayout, bodyHeight int) string {
	parts := []string{}
	if strings.TrimSpace(state.BodyTitle) != "" {
		parts = append(parts, theme.bodyTitle.Render(state.BodyTitle))
	}
	body := state.Body
	if strings.TrimSpace(body) == "" {
		body = theme.muted.Render("(empty)")
	}
	if state.StyledBody {
		parts = append(parts, body)
	} else {
		parts = append(parts, theme.bodyText.Render(body))
	}
	separator := "\n\n"
	style := theme.bodyPanel
	if state.DenseBody {
		separator = "\n"
		style = style.Padding(0, 1)
	}
	panel := strings.Join(parts, separator)
	width := shellMainSectionWidth(layout, theme)
	if bodyHeight > 0 {
		style = style.MaxHeight(bodyHeight)
	}
	return style.Width(styleContentWidth(width, style)).Render(panel)
}

func renderTUIFooter(state tuiShellState, theme tuiShellTheme, layout tuiShellLayout) string {
	stats := make([]string, 0, len(state.FooterStats)+2)
	for _, stat := range state.FooterStats {
		value := stat.Value
		if value != "" {
			value = shellToneStyle(theme, stat.Tone).Render(value)
			stats = append(stats, stat.Label+": "+value)
			continue
		}
		stats = append(stats, stat.Label)
	}
	if state.AllowBack {
		stats = append(stats, theme.muted.Render("esc: back"))
	}
	if state.DebugMessageType != "" {
		stats = append(stats, theme.muted.Render("last_msg="+state.DebugMessageType))
	}
	separator := theme.muted.Render(" · ")
	line := strings.Join(stats, separator)
	if layout.Compact && len(stats) > 3 {
		line = strings.Join(stats[:3], separator)
	}
	return theme.footer.Width(styleContentWidth(shellMainSectionWidth(layout, theme), theme.footer)).Render(line)
}

func renderTUIModal(base string, state tuiShellState, theme tuiShellTheme, layout tuiShellLayout) string {
	lines := append([]string{theme.modalTitle.Render(state.Modal.Title)}, state.Modal.Lines...)
	boxWidth := minInt(shellMainSectionWidth(layout, theme)-6, 72)
	if boxWidth < 24 {
		boxWidth = 24
	}
	box := theme.modalBox.Width(styleContentWidth(boxWidth, theme.modalBox)).Render(strings.Join(lines, "\n"))
	centered := lipgloss.Place(shellMainSectionWidth(layout, theme), 0, lipgloss.Center, lipgloss.Top, box)
	return theme.backdrop.Render(base) + "\n\n" + centered
}

func (m tuiRootModel) shellState(layout tuiShellLayout) tuiShellState {
	switch m.screen {
	case tuiScreenGetStarted:
		return buildOnboardingShellState(m, layout)
	case tuiScreenCredentials:
		return buildCredentialsShellState(m, layout)
	case tuiScreenInteractiveSync, tuiScreenSync:
		return buildSyncShellState(m, layout)
	case tuiScreenDoctor:
		return buildDoctorShellState(m, layout)
	case tuiScreenValidate:
		return buildValidateShellState(m, layout)
	case tuiScreenConfigEditor:
		return buildConfigEditorShellState(m, layout)
	case tuiScreenInit:
		return buildInitShellState(m, layout)
	default:
		return buildLandingShellState(m, layout)
	}
}

func buildLandingShellState(m tuiRootModel, layout tuiShellLayout) tuiShellState {
	selected := m.selectedMenuItem()
	return tuiShellState{
		AppLabel:        "UDL",
		ScreenTitle:     "Home",
		SidebarSections: workflowNavigationItems(m.screen, m.menuCursor, m.menuItems),
		Badges: []tuiBadge{
			{Label: "READY", Tone: "success"},
		},
		CommandSummary: []string{
			"udl tui",
			"selected=" + selected,
		},
		Shortcuts: []tuiShortcut{
			{Key: "j/k", Label: "move"},
			{Key: "enter", Label: "open"},
			{Key: "q", Label: "quit"},
			{Key: "ctrl+c", Label: "force quit"},
		},
		BodyTitle:        shellTitle(selected),
		Body:             buildLandingBody(selected),
		FooterStats:      landingFooterStats(selected),
		AllowBack:        false,
		DebugMessageType: m.lastMsgTypeIfEnabled(),
	}
}

func buildDoctorShellState(m tuiRootModel, layout tuiShellLayout) tuiShellState {
	model := m.doctorModel
	shortcuts := []tuiShortcut{{Key: "esc", Label: "back", Disabled: !m.canReturnToMenuOnEsc()}}
	if model.recommendedCredentialKind() != "" {
		shortcuts = append(shortcuts, tuiShortcut{Key: "c", Label: "credentials"})
	}
	return tuiShellState{
		AppLabel:         "UDL",
		ScreenTitle:      "Doctor",
		SidebarSections:  workflowNavigationItems(m.screen, m.menuCursor, m.menuItems),
		Badges:           model.shellBadges(),
		CommandSummary:   []string{"udl doctor"},
		Shortcuts:        shortcuts,
		BodyTitle:        "Doctor",
		Body:             model.shellBody(layout),
		DenseBody:        true,
		StyledBody:       true,
		FooterStats:      model.shellFooterStats(),
		AllowBack:        m.canReturnToMenuOnEsc(),
		DebugMessageType: m.lastMsgTypeIfEnabled(),
	}
}

func buildValidateShellState(m tuiRootModel, layout tuiShellLayout) tuiShellState {
	model := m.validateModel
	return tuiShellState{
		AppLabel:         "UDL",
		ScreenTitle:      "Validate",
		SidebarSections:  workflowNavigationItems(m.screen, m.menuCursor, m.menuItems),
		Badges:           model.shellBadges(),
		CommandSummary:   []string{"udl validate"},
		Shortcuts:        []tuiShortcut{{Key: "esc", Label: "back", Disabled: !m.canReturnToMenuOnEsc()}},
		BodyTitle:        "Validate",
		Body:             model.shellBody(layout),
		DenseBody:        true,
		StyledBody:       true,
		FooterStats:      model.shellFooterStats(),
		AllowBack:        m.canReturnToMenuOnEsc(),
		DebugMessageType: m.lastMsgTypeIfEnabled(),
	}
}

func buildInitShellState(m tuiRootModel, layout tuiShellLayout) tuiShellState {
	model := m.initModel
	state := tuiShellState{
		AppLabel:         "UDL",
		ScreenTitle:      "Init",
		SidebarSections:  workflowNavigationItems(m.screen, m.menuCursor, m.menuItems),
		Badges:           model.shellBadges(),
		CommandSummary:   []string{"udl init"},
		Shortcuts:        model.shellShortcuts(),
		BodyTitle:        "Init",
		Body:             model.shellBody(layout),
		DenseBody:        true,
		StyledBody:       true,
		FooterStats:      model.shellFooterStats(),
		AllowBack:        m.canReturnToMenuOnEsc(),
		DebugMessageType: m.lastMsgTypeIfEnabled(),
	}
	if model.prompt != nil {
		state.Modal = model.promptModal()
	}
	return state
}

func workflowNavigationItems(screen tuiScreen, menuCursor int, menuItems []string) []tuiSidebarSection {
	items := make([]tuiSidebarItem, 0, len(menuItems))
	for idx, item := range menuItems {
		active := false
		switch screen {
		case tuiScreenMenu:
			active = idx == menuCursor
		case tuiScreenGetStarted:
			active = item == "Get Started"
		case tuiScreenCredentials:
			active = item == "Credentials"
		case tuiScreenInteractiveSync:
			active = item == "Run Sync"
		case tuiScreenSync:
			active = item == "Run Sync"
		case tuiScreenDoctor:
			active = item == "Check System"
		case tuiScreenValidate:
			active = item == "Check System"
		case tuiScreenConfigEditor:
			active = item == "Advanced Config"
		case tuiScreenInit:
			active = item == "Get Started"
		}
		items = append(items, tuiSidebarItem{
			Label:  item,
			Meta:   landingWorkflowMeta(item),
			Active: active,
		})
	}
	return []tuiSidebarSection{{Title: "workflows", Items: items}}
}

func landingWorkflowMeta(item string) string {
	switch item {
	case "Get Started":
		return "setup"
	case "Credentials":
		return "keychain"
	case "Check System":
		return "checks"
	case "Run Sync":
		return "interactive"
	case "Advanced Config":
		return "editor"
	case "Quit":
		return "exit"
	default:
		return ""
	}
}

func buildLandingBody(selected string) string {
	summary := landingWorkflowSummary(selected)
	lines := []string{
		summary,
		"",
		"Actions:",
		"  enter: open selected workflow",
		"  j/k or up/down: move selection",
		"  q or ctrl+c: quit the TUI",
	}
	return strings.Join(lines, "\n")
}

func landingWorkflowSummary(item string) string {
	switch item {
	case "Get Started":
		return "Create a starter setup, choose folders, and add your first source."
	case "Credentials":
		return "Manage SoundCloud, Deezer, and Spotify secrets in macOS Keychain."
	case "Check System":
		return "Verify tools, credentials, and folder access before syncing."
	case "Run Sync":
		return "Review enabled sources, preview the plan, and run a sync."
	case "Advanced Config":
		return "Open the full config editor for raw source and adapter settings."
	case "Quit":
		return "Exit the TUI shell."
	default:
		return ""
	}
}

func landingFooterStats(selected string) []tuiFooterStat {
	return []tuiFooterStat{
		{Label: "state", Value: "ready", Tone: "success"},
		{Label: "selected", Value: selected, Tone: "info"},
	}
}

func (m tuiRootModel) selectedMenuItem() string {
	if len(m.menuItems) == 0 {
		return ""
	}
	if m.menuCursor < 0 || m.menuCursor >= len(m.menuItems) {
		return m.menuItems[0]
	}
	return m.menuItems[m.menuCursor]
}

func (m tuiRootModel) lastMsgTypeIfEnabled() string {
	if !m.debugMessages {
		return ""
	}
	return m.lastMsgType
}
