package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type tuiShellLayout struct {
	Width        int
	Height       int
	Compact      bool
	SidebarWidth int
	MainWidth    int
}

type tuiShellState struct {
	AppLabel         string
	ScreenTitle      string
	SidebarSections  []tuiSidebarSection
	Badges           []tuiBadge
	CommandSummary   []string
	Shortcuts        []tuiShortcut
	BodyTitle        string
	Body             string
	DenseBody        bool
	FooterStats      []tuiFooterStat
	Banner           *tuiBanner
	Modal            *tuiModalState
	AllowBack        bool
	DebugMessageType string
}

type tuiSidebarSection struct {
	Title string
	Items []tuiSidebarItem
}

type tuiSidebarItem struct {
	Label    string
	Meta     string
	Active   bool
	Disabled bool
}

type tuiBadge struct {
	Label    string
	Tone     string
	Disabled bool
}

type tuiShortcut struct {
	Key      string
	Label    string
	Disabled bool
}

type tuiFooterStat struct {
	Label string
	Value string
	Tone  string
}

type tuiModalState struct {
	Title string
	Lines []string
	Tone  string
}

type tuiBanner struct {
	Text string
	Tone string
}

const (
	tuiShellDefaultWidth      = 120
	tuiShellDefaultHeight     = 36
	tuiShellCompactBreakpoint = 110
	tuiShellSidebarWidth      = 30
	tuiShellBackgroundColor   = "235"
)

func newTUIShellLayout(width, height int) tuiShellLayout {
	if width <= 0 {
		width = tuiShellDefaultWidth
	}
	if height <= 0 {
		height = tuiShellDefaultHeight
	}
	layout := tuiShellLayout{
		Width:  width,
		Height: height,
	}
	layout.Compact = width < tuiShellCompactBreakpoint
	if !layout.Compact {
		layout.SidebarWidth = tuiShellSidebarWidth
	}
	layout.MainWidth = width - layout.SidebarWidth
	if layout.MainWidth < 40 {
		layout.MainWidth = width
	}
	return layout
}

type tuiShellTheme struct {
	frame         lipgloss.Style
	titlebar      lipgloss.Style
	topbar        lipgloss.Style
	sidebar       lipgloss.Style
	navStrip      lipgloss.Style
	main          lipgloss.Style
	row           lipgloss.Style
	sectionLabel  lipgloss.Style
	sidebarItem   lipgloss.Style
	sidebarActive lipgloss.Style
	sidebarMeta   lipgloss.Style
	badgeBase     lipgloss.Style
	commandBar    lipgloss.Style
	shortcuts     lipgloss.Style
	bodyPanel     lipgloss.Style
	bodyTitle     lipgloss.Style
	bodyText      lipgloss.Style
	footer        lipgloss.Style
	footerValue   lipgloss.Style
	muted         lipgloss.Style
	info          lipgloss.Style
	success       lipgloss.Style
	warning       lipgloss.Style
	danger        lipgloss.Style
	disabled      lipgloss.Style
	dimmed        lipgloss.Style
	modalBox      lipgloss.Style
	modalTitle    lipgloss.Style
	backdrop      lipgloss.Style
}

func newTUIShellTheme() tuiShellTheme {
	base := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color(tuiShellBackgroundColor))
	return tuiShellTheme{
		frame:         base.Copy().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("236")),
		titlebar:      base.Copy().Background(lipgloss.Color(tuiShellBackgroundColor)).Foreground(lipgloss.Color("245")).Padding(0, 1),
		topbar:        base.Copy().Background(lipgloss.Color(tuiShellBackgroundColor)).BorderBottom(true).BorderForeground(lipgloss.Color("236")).Padding(0, 1),
		sidebar:       base.Copy().Background(lipgloss.Color(tuiShellBackgroundColor)).BorderRight(true).BorderForeground(lipgloss.Color("236")).Padding(1, 1),
		navStrip:      base.Copy().Background(lipgloss.Color(tuiShellBackgroundColor)).BorderBottom(true).BorderForeground(lipgloss.Color("236")).Padding(0, 1),
		main:          base.Copy().Padding(0, 0),
		row:           base.Copy().Padding(0, 1),
		sectionLabel:  base.Copy().Foreground(lipgloss.Color("241")).Bold(true),
		sidebarItem:   base.Copy().Foreground(lipgloss.Color("245")).Padding(0, 1),
		sidebarActive: base.Copy().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("236")).Bold(true).Padding(0, 1),
		sidebarMeta:   base.Copy().Foreground(lipgloss.Color("241")),
		badgeBase:     base.Copy().Padding(0, 1).Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("239")),
		commandBar:    base.Copy().Foreground(lipgloss.Color("245")).BorderBottom(true).BorderForeground(lipgloss.Color("236")).Padding(0, 1),
		shortcuts:     base.Copy().Foreground(lipgloss.Color("245")).BorderBottom(true).BorderForeground(lipgloss.Color("236")).Padding(0, 1),
		bodyPanel:     base.Copy().Padding(1, 1),
		bodyTitle:     base.Copy().Bold(true).Foreground(lipgloss.Color("252")),
		bodyText:      base.Copy().Foreground(lipgloss.Color("252")),
		footer:        base.Copy().Background(lipgloss.Color(tuiShellBackgroundColor)).Foreground(lipgloss.Color("245")).BorderTop(true).BorderForeground(lipgloss.Color("236")).Padding(0, 1),
		footerValue:   base.Copy().Foreground(lipgloss.Color("81")).Bold(true),
		muted:         base.Copy().Foreground(lipgloss.Color("241")),
		info:          base.Copy().Foreground(lipgloss.Color("81")),
		success:       base.Copy().Foreground(lipgloss.Color("78")),
		warning:       base.Copy().Foreground(lipgloss.Color("179")),
		danger:        base.Copy().Foreground(lipgloss.Color("203")),
		disabled:      base.Copy().Foreground(lipgloss.Color("240")),
		dimmed:        base.Copy().Faint(true),
		modalBox:      base.Copy().Background(lipgloss.Color(tuiShellBackgroundColor)).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("239")).Padding(1, 2),
		modalTitle:    base.Copy().Bold(true).Foreground(lipgloss.Color("252")),
		backdrop:      base.Copy().Faint(true),
	}
}

func renderTUIShell(state tuiShellState, layout tuiShellLayout) string {
	theme := newTUIShellTheme()

	sidebar := ""
	if layout.Compact {
		sidebar = renderTUITopNav(state, theme, layout)
	} else {
		sidebar = renderTUISidebar(state, theme, layout)
	}

	mainParts := []string{
		renderTUITitlebar(state, theme, layout),
	}
	if sidebar != "" && layout.Compact {
		mainParts = append(mainParts, sidebar)
	}
	if badges := renderTUIBadges(state, theme, layout); badges != "" {
		mainParts = append(mainParts, badges)
	}
	if commandBar := renderTUICommandBar(state, theme, layout); commandBar != "" {
		mainParts = append(mainParts, commandBar)
	}
	if shortcuts := renderTUIShortcuts(state, theme, layout); shortcuts != "" {
		mainParts = append(mainParts, shortcuts)
	}
	if banner := renderTUIBanner(state, theme, layout); banner != "" {
		mainParts = append(mainParts, banner)
	}
	mainParts = append(mainParts, renderTUIBody(state, theme, layout))
	mainParts = append(mainParts, renderTUIFooter(state, theme, layout))

	main := strings.Join(filterEmptyStrings(mainParts), "\n")
	if !layout.Compact {
		main = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, main)
	}
	main = theme.frame.Render(main)

	if state.Modal != nil {
		main = renderTUIModal(main, state, theme, layout)
	}
	return lipgloss.NewStyle().
		Width(layout.Width).
		Height(layout.Height).
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
	dots := strings.Join([]string{
		lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render("●"),
		lipgloss.NewStyle().Foreground(lipgloss.Color("179")).Render("●"),
		lipgloss.NewStyle().Foreground(lipgloss.Color("78")).Render("●"),
	}, " ")
	row := dots + "  " + label
	return theme.titlebar.Width(shellMainSectionWidth(layout)).Render(row)
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
	return theme.sidebar.Width(layout.SidebarWidth).Render(sidebar)
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
	labelWidth := layout.SidebarWidth - 4
	if labelWidth < 8 {
		labelWidth = 8
	}
	label := truncateForWidth(item.Label, labelWidth)
	line := style.Width(labelWidth + 2).Render(prefix + label)
	if strings.TrimSpace(item.Meta) == "" {
		return line
	}
	meta := theme.sidebarMeta.Width(labelWidth + 2).Render("  " + truncateForWidth(item.Meta, labelWidth))
	return line + "\n" + meta
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
	return theme.navStrip.Width(layout.Width - 2).Render(strings.Join(items, "  "))
}

func renderTUIBadges(state tuiShellState, theme tuiShellTheme, layout tuiShellLayout) string {
	if len(state.Badges) == 0 {
		return ""
	}
	rendered := make([]string, 0, len(state.Badges))
	for _, badge := range state.Badges {
		rendered = append(rendered, renderTUIBadge(badge, theme))
	}
	return theme.topbar.Width(shellMainSectionWidth(layout)).Render(strings.Join(rendered, " "))
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
	return theme.commandBar.Width(shellMainSectionWidth(layout)).Render(summary)
}

func renderTUIShortcuts(state tuiShellState, theme tuiShellTheme, layout tuiShellLayout) string {
	if len(state.Shortcuts) == 0 {
		return ""
	}
	rendered := make([]string, 0, len(state.Shortcuts))
	for _, shortcut := range state.Shortcuts {
		chunk := fmt.Sprintf("[%s] %s", shortcut.Key, shortcut.Label)
		style := theme.muted
		if shortcut.Disabled {
			style = theme.disabled
		}
		rendered = append(rendered, style.Render(chunk))
	}
	return theme.shortcuts.Width(shellMainSectionWidth(layout)).Render(strings.Join(rendered, "  "))
}

func renderTUIBanner(state tuiShellState, theme tuiShellTheme, layout tuiShellLayout) string {
	if state.Banner == nil || strings.TrimSpace(state.Banner.Text) == "" {
		return ""
	}
	style := shellToneStyle(theme, state.Banner.Tone)
	return lipgloss.NewStyle().Padding(0, 1).Width(shellMainSectionWidth(layout)).Render(style.Render(state.Banner.Text))
}

func renderTUIBody(state tuiShellState, theme tuiShellTheme, layout tuiShellLayout) string {
	parts := []string{}
	if strings.TrimSpace(state.BodyTitle) != "" {
		parts = append(parts, theme.bodyTitle.Render(state.BodyTitle))
	}
	body := state.Body
	if strings.TrimSpace(body) == "" {
		body = theme.muted.Render("(empty)")
	}
	parts = append(parts, theme.bodyText.Render(body))
	separator := "\n\n"
	style := theme.bodyPanel
	if state.DenseBody {
		separator = "\n"
		style = style.Padding(0, 1)
	}
	panel := strings.Join(parts, separator)
	width := shellMainSectionWidth(layout)
	return style.Width(width).Render(panel)
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
	return theme.footer.Width(shellMainSectionWidth(layout)).Render(line)
}

func renderTUIModal(base string, state tuiShellState, theme tuiShellTheme, layout tuiShellLayout) string {
	lines := append([]string{theme.modalTitle.Render(state.Modal.Title)}, state.Modal.Lines...)
	boxWidth := minInt(shellMainSectionWidth(layout)-6, 72)
	if boxWidth < 24 {
		boxWidth = 24
	}
	box := theme.modalBox.Width(boxWidth).Render(strings.Join(lines, "\n"))
	centered := lipgloss.Place(shellMainSectionWidth(layout), 0, lipgloss.Center, lipgloss.Top, box)
	return theme.backdrop.Render(base) + "\n\n" + centered
}

func shellToneStyle(theme tuiShellTheme, tone string) lipgloss.Style {
	switch tone {
	case "success":
		return theme.success
	case "warning":
		return theme.warning
	case "danger":
		return theme.danger
	case "muted":
		return theme.muted
	default:
		return theme.info
	}
}

func filterEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func shellMainSectionWidth(layout tuiShellLayout) int {
	if layout.Compact {
		return layout.Width - 2
	}
	width := layout.MainWidth
	if width < 20 {
		return layout.Width - 2
	}
	return width
}

func truncateForWidth(value string, width int) string {
	value = strings.TrimSpace(value)
	if width <= 0 || len(value) <= width {
		return value
	}
	if width <= 3 {
		return value[:width]
	}
	return value[:width-3] + "..."
}

func shellTitle(label string) string {
	parts := strings.Fields(strings.TrimSpace(label))
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	return strings.Join(parts, " ")
}

func (m tuiRootModel) shellState(layout tuiShellLayout) tuiShellState {
	switch m.screen {
	case tuiScreenInteractiveSync, tuiScreenSync:
		return buildSyncShellState(m, layout)
	case tuiScreenDoctor:
		return buildDoctorShellState(m, layout)
	case tuiScreenValidate:
		return buildValidateShellState(m, layout)
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
		ScreenTitle:     "Workflow Launcher",
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

func buildSyncShellState(m tuiRootModel, layout tuiShellLayout) tuiShellState {
	syncModel := m.syncModel
	state := tuiShellState{
		AppLabel:         "UDL",
		ScreenTitle:      syncModel.workflowTitle(),
		SidebarSections:  syncModel.sidebarSections(m.screen),
		Badges:           syncModel.shellBadges(),
		CommandSummary:   syncModel.shellCommandSummary(),
		Shortcuts:        syncModel.shellShortcuts(),
		BodyTitle:        syncModel.shellBodyTitle(),
		Body:             syncModel.shellBody(layout),
		DenseBody:        syncModel.planPrompt != nil,
		FooterStats:      syncModel.shellFooterStats(),
		Banner:           syncModel.shellBanner(),
		AllowBack:        m.canReturnToMenuOnEsc(),
		DebugMessageType: m.lastMsgTypeIfEnabled(),
	}
	if syncModel.interactionPrompt != nil {
		state.Modal = syncModel.interactionPromptModal()
	}
	return state
}

func buildDoctorShellState(m tuiRootModel, layout tuiShellLayout) tuiShellState {
	model := m.doctorModel
	return tuiShellState{
		AppLabel:         "UDL",
		ScreenTitle:      "Doctor",
		SidebarSections:  workflowNavigationItems(m.screen, m.menuCursor, m.menuItems),
		Badges:           model.shellBadges(),
		CommandSummary:   []string{"udl doctor"},
		Shortcuts:        []tuiShortcut{{Key: "esc", Label: "back", Disabled: !m.canReturnToMenuOnEsc()}},
		BodyTitle:        "Doctor",
		Body:             model.View(),
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
		Body:             model.View(),
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
		Shortcuts:        []tuiShortcut{{Key: "esc", Label: "back", Disabled: !m.canReturnToMenuOnEsc()}},
		BodyTitle:        "Init",
		Body:             model.View(),
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
		case tuiScreenInteractiveSync:
			active = item == "interactive sync"
		case tuiScreenSync:
			active = item == "sync"
		case tuiScreenDoctor:
			active = item == "doctor"
		case tuiScreenValidate:
			active = item == "validate"
		case tuiScreenInit:
			active = item == "init"
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
	case "interactive sync":
		return "interactive"
	case "sync":
		return "runtime"
	case "doctor":
		return "checks"
	case "validate":
		return "config"
	case "init":
		return "setup"
	case "quit":
		return "exit"
	default:
		return ""
	}
}

func buildLandingBody(selected string) string {
	summary := landingWorkflowSummary(selected)
	mode := "lightweight"
	if selected == "interactive sync" || selected == "sync" {
		mode = "interactive"
	}
	lines := []string{
		summary,
		"",
		"Actions:",
		"  enter: open selected workflow",
		"  j/k or up/down: move selection",
		"  q or ctrl+c: quit the TUI",
		"",
		"Profile: " + mode,
	}
	return strings.Join(lines, "\n")
}

func landingWorkflowSummary(item string) string {
	switch item {
	case "interactive sync":
		return "Review enabled sources, set plan options, and launch the sync --plan flow."
	case "sync":
		return "Run the standard sync workflow with runtime-focused flags."
	case "doctor":
		return "Run environment and configuration checks."
	case "validate":
		return "Validate the current configuration file."
	case "init":
		return "Create or update the starter configuration."
	case "quit":
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

func (m tuiSyncModel) shellBadges() []tuiBadge {
	badges := []tuiBadge{
		{Label: "STATE: " + m.runStateLabel(), Tone: m.runStateTone()},
		{Label: "DRY-RUN: " + boolLabel(m.dryRun), Tone: boolTone(m.dryRun)},
	}
	if m.isInteractiveSyncWorkflow() {
		badges = append(badges,
			tuiBadge{Label: "LIMIT: " + formatPlanLimit(m.planLimit), Tone: "info"},
			tuiBadge{Label: "TIMEOUT: " + formatTimeoutOverride(m.timeoutOverride), Tone: "muted"},
		)
		return badges
	}
	badges = append(badges,
		tuiBadge{Label: "ASK: " + formatAskOnExisting(m.askOnExistingSet), Tone: "info"},
		tuiBadge{Label: "GAPS: " + boolLabel(m.scanGaps), Tone: boolTone(m.scanGaps)},
		tuiBadge{Label: "NO-PREFLIGHT: " + boolLabel(m.noPreflight), Tone: boolTone(m.noPreflight)},
		tuiBadge{Label: "TIMEOUT: " + formatTimeoutOverride(m.timeoutOverride), Tone: "muted"},
	)
	return badges
}

func (m tuiSyncModel) shellCommandSummary() []string {
	parts := []string{"udl", "sync"}
	if m.isInteractiveSyncWorkflow() {
		parts = append(parts, "--plan", "plan_limit="+formatPlanLimit(m.planLimit))
	} else {
		parts = append(parts,
			"ask_on_existing="+formatAskOnExisting(m.askOnExistingSet),
			fmt.Sprintf("scan_gaps=%t", m.scanGaps),
			fmt.Sprintf("no_preflight=%t", m.noPreflight),
		)
	}
	parts = append(parts,
		fmt.Sprintf("selected_sources=%d/%d", m.selectedSourceCount(), len(m.sources)),
		"timeout="+formatTimeoutOverride(m.timeoutOverride),
	)
	if m.dryRun {
		parts = append(parts, "--dry-run")
	}
	return parts
}

func (m tuiSyncModel) shellShortcuts() []tuiShortcut {
	shortcuts := []tuiShortcut{
		{Key: "j/k", Label: "move"},
		{Key: "space", Label: "toggle source"},
		{Key: "d", Label: "dry-run"},
		{Key: "t", Label: "timeout"},
		{Key: "enter", Label: "run"},
	}
	if m.isInteractiveSyncWorkflow() {
		shortcuts = append(shortcuts,
			tuiShortcut{Key: "[/]", Label: "plan limit"},
			tuiShortcut{Key: "l", Label: "type limit"},
			tuiShortcut{Key: "u", Label: "unlimited"},
		)
	} else {
		shortcuts = append(shortcuts,
			tuiShortcut{Key: "a", Label: "ask-existing"},
			tuiShortcut{Key: "g", Label: "scan-gaps"},
			tuiShortcut{Key: "f", Label: "no-preflight"},
		)
	}
	shortcuts = append(shortcuts, tuiShortcut{Key: "x", Label: "cancel active run", Disabled: !m.running})
	return shortcuts
}

func (m tuiSyncModel) shellFooterStats() []tuiFooterStat {
	stats := []tuiFooterStat{
		{Label: "state", Value: m.runStateLabel(), Tone: m.runStateTone()},
		{Label: "sources", Value: fmt.Sprintf("%d/%d", m.selectedSourceCount(), len(m.sources)), Tone: "info"},
	}
	if m.done {
		stats = append(stats,
			tuiFooterStat{Label: "attempted", Value: fmt.Sprintf("%d", m.result.Attempted), Tone: "info"},
			tuiFooterStat{Label: "failed", Value: fmt.Sprintf("%d", m.result.Failed), Tone: failureCountTone(m.result.Failed)},
		)
		return stats
	}
	total := 0
	if m.progress != nil {
		total = m.progress.EffectiveTotal()
	}
	if total > 0 {
		stats = append(stats, tuiFooterStat{Label: "progress", Value: fmt.Sprintf("%d/%d", m.progress.CurrentIndex(), total), Tone: "info"})
	}
	return stats
}

func (m tuiSyncModel) shellBanner() *tuiBanner {
	if m.validationErr != "" {
		return &tuiBanner{Text: "validation_error: " + m.validationErr, Tone: "danger"}
	}
	if m.cancelRequested {
		return &tuiBanner{Text: "Cancellation requested, waiting for adapter shutdown...", Tone: "warning"}
	}
	if m.runErr != nil && m.done {
		return &tuiBanner{Text: "Run failed: " + m.runErr.Error(), Tone: "danger"}
	}
	return nil
}

func (m tuiSyncModel) planPromptBody(layout tuiShellLayout) string {
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
		fmt.Sprintf("source=%s  mode=%s  plan-limit=%s", state.sourceID, modeLabel, limitLabel),
		fmt.Sprintf("type=%s  adapter=%s", state.details.SourceType, state.details.Adapter),
		fmt.Sprintf("target_dir=%s", state.details.TargetDir),
		fmt.Sprintf("state_file=%s", state.details.StateFile),
		fmt.Sprintf("url=%s", state.details.URL),
		"",
		"up/down or j/k: move   space: toggle   a: select all   n: clear all   enter: confirm   q/esc: cancel",
	}
	if len(state.rows) == 0 {
		lines = append(lines, "No tracks found in selected preflight window.")
	} else {
		start, end := planSelectorWindow(len(state.rows), state.cursor, shellPlanPromptWindowHeight(layout))
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
		lines = append(lines, "", fmt.Sprintf("Selected: %d/%d toggleable tracks", state.selectedCount(), state.toggleableCount()))
	}
	return strings.Join(lines, "\n")
}

func (m tuiSyncModel) interactionPromptModal() *tuiModalState {
	state := m.interactionPrompt
	if state == nil {
		return nil
	}
	lines := []string{}
	switch state.kind {
	case tuiPromptKindConfirm:
		defaultLabel := "no"
		if state.defaultYes {
			defaultLabel = "yes"
		}
		lines = append(lines,
			fmt.Sprintf("[%s] confirm", state.sourceID),
			state.prompt,
			fmt.Sprintf("y: yes  n: no  enter: default (%s)  esc/q: cancel run", defaultLabel),
		)
	case tuiPromptKindInput:
		displayInput := state.input
		if state.maskInput {
			displayInput = strings.Repeat("*", len(state.input))
		}
		lines = append(lines,
			fmt.Sprintf("[%s] input", state.sourceID),
			state.prompt,
			fmt.Sprintf("value=%q", displayInput),
			"type to edit  backspace: delete  enter: submit  esc/q: cancel run",
		)
	}
	return &tuiModalState{Title: "Prompt", Lines: lines, Tone: "info"}
}

func (m *tuiPlanPromptState) toggleableCount() int {
	if m == nil {
		return 0
	}
	count := 0
	for _, row := range m.rows {
		if row.Toggleable {
			count++
		}
	}
	return count
}

func (m *tuiPlanPromptState) selectedCount() int {
	if m == nil {
		return 0
	}
	count := 0
	for _, row := range m.rows {
		if row.Toggleable && m.selected[row.Index] {
			count++
		}
	}
	return count
}

func (m tuiDoctorModel) shellBadges() []tuiBadge {
	if !m.done {
		return []tuiBadge{{Label: "RUNNING", Tone: "warning"}}
	}
	if m.runErr != nil {
		return []tuiBadge{{Label: "FAILED", Tone: "danger"}}
	}
	return []tuiBadge{{Label: "COMPLETE", Tone: "success"}}
}

func (m tuiDoctorModel) shellFooterStats() []tuiFooterStat {
	if !m.done {
		return []tuiFooterStat{{Label: "state", Value: "running", Tone: "warning"}}
	}
	if m.runErr != nil {
		return []tuiFooterStat{{Label: "state", Value: "failed", Tone: "danger"}}
	}
	return []tuiFooterStat{
		{Label: "state", Value: "complete", Tone: "success"},
		{Label: "checks", Value: fmt.Sprintf("%d", len(m.lines)), Tone: "info"},
	}
}

func (m tuiValidateModel) shellBadges() []tuiBadge {
	if !m.done {
		return []tuiBadge{{Label: "RUNNING", Tone: "warning"}}
	}
	if strings.HasPrefix(m.result, "Validate failed:") {
		return []tuiBadge{{Label: "FAILED", Tone: "danger"}}
	}
	return []tuiBadge{{Label: "COMPLETE", Tone: "success"}}
}

func (m tuiValidateModel) shellFooterStats() []tuiFooterStat {
	if !m.done {
		return []tuiFooterStat{{Label: "state", Value: "running", Tone: "warning"}}
	}
	if strings.HasPrefix(m.result, "Validate failed:") {
		return []tuiFooterStat{{Label: "state", Value: "failed", Tone: "danger"}}
	}
	return []tuiFooterStat{{Label: "state", Value: "valid", Tone: "success"}}
}

func (m tuiInitModel) shellBadges() []tuiBadge {
	if m.running && !m.done {
		return []tuiBadge{{Label: "RUNNING", Tone: "warning"}}
	}
	if m.done && strings.HasPrefix(m.result, "Init failed:") {
		return []tuiBadge{{Label: "FAILED", Tone: "danger"}}
	}
	if m.done {
		return []tuiBadge{{Label: "COMPLETE", Tone: "success"}}
	}
	return []tuiBadge{{Label: "READY", Tone: "info"}}
}

func (m tuiInitModel) shellFooterStats() []tuiFooterStat {
	if m.running && !m.done {
		return []tuiFooterStat{{Label: "state", Value: "running", Tone: "warning"}}
	}
	if m.done && strings.HasPrefix(m.result, "Init failed:") {
		return []tuiFooterStat{{Label: "state", Value: "failed", Tone: "danger"}}
	}
	if m.done {
		return []tuiFooterStat{{Label: "state", Value: "complete", Tone: "success"}}
	}
	return []tuiFooterStat{{Label: "state", Value: "ready", Tone: "info"}}
}

func (m tuiInitModel) promptModal() *tuiModalState {
	state := m.prompt
	if state == nil {
		return nil
	}
	lines := []string{}
	switch state.kind {
	case tuiPromptKindConfirm:
		defaultLabel := "no"
		if state.defaultYes {
			defaultLabel = "yes"
		}
		lines = append(lines,
			state.prompt,
			fmt.Sprintf("y: yes  n: no  enter: default (%s)  esc/q: cancel", defaultLabel),
		)
	case tuiPromptKindInput:
		displayInput := state.input
		if state.maskInput {
			displayInput = strings.Repeat("*", len(state.input))
		}
		lines = append(lines,
			state.prompt,
			fmt.Sprintf("value=%q", displayInput),
			"type to edit  backspace: delete  enter: submit  esc/q: cancel",
		)
	}
	return &tuiModalState{Title: "Init Prompt", Lines: lines, Tone: "info"}
}

func (m tuiSyncModel) selectedSourceCount() int {
	count := 0
	for _, source := range m.sources {
		if m.selected[source.ID] {
			count++
		}
	}
	return count
}

func (m tuiSyncModel) shellBodyTitle() string {
	if m.planPrompt != nil {
		return "Plan Selection"
	}
	return m.workflowTitle()
}

func (m tuiSyncModel) shellBody(layout tuiShellLayout) string {
	if m.planPrompt != nil {
		return m.planPromptBody(layout)
	}
	return m.bodyView(false)
}

func shellPlanPromptWindowHeight(layout tuiShellLayout) int {
	height := layout.Height - 18
	if height < 0 {
		return 0
	}
	return height
}

func (m tuiSyncModel) bodyView(includeSources bool) string {
	if !m.cfgLoaded {
		return "Loading config..."
	}
	if m.cfgErr != nil {
		return fmt.Sprintf("Config load failed: %v", m.cfgErr)
	}
	lines := []string{
		fmt.Sprintf("dry_run=%t  timeout=%s", m.dryRun, formatTimeoutOverride(m.timeoutOverride)),
	}
	if m.isInteractiveSyncWorkflow() {
		lines = append(lines, fmt.Sprintf("plan_limit=%s", formatPlanLimit(m.planLimit)))
	} else {
		lines = append(lines, fmt.Sprintf("ask_on_existing=%s  scan_gaps=%t  no_preflight=%t", formatAskOnExisting(m.askOnExistingSet), m.scanGaps, m.noPreflight))
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
	if includeSources {
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
	}
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
	if failureLines := m.lastFailureLines(); len(failureLines) > 0 {
		lines = append(lines, "", "Last Failure:")
		for _, line := range failureLines {
			lines = append(lines, "  "+line)
		}
	}
	return strings.Join(lines, "\n")
}

func (m tuiSyncModel) sidebarSections(screen tuiScreen) []tuiSidebarSection {
	sections := []tuiSidebarSection{
		{
			Title: "workflow",
			Items: []tuiSidebarItem{
				{
					Label:  m.sidebarWorkflowLabel(),
					Meta:   "esc to launcher",
					Active: true,
				},
			},
		},
	}
	sourceItems := make([]tuiSidebarItem, 0, len(m.sources))
	for idx, source := range m.sources {
		marker := "[ ]"
		if m.selected[source.ID] {
			marker = "[x]"
		}
		label := marker + " " + source.ID
		meta := string(source.Type) + "/" + source.Adapter.Kind
		sourceItems = append(sourceItems, tuiSidebarItem{
			Label:  label,
			Meta:   meta,
			Active: idx == m.cursor,
		})
	}
	if len(sourceItems) == 0 {
		sourceItems = append(sourceItems, tuiSidebarItem{Label: "(no enabled sources)", Disabled: true})
	}
	sections = append(sections, tuiSidebarSection{Title: "sources", Items: sourceItems})
	return sections
}

func (m tuiSyncModel) sidebarWorkflowLabel() string {
	if m.isInteractiveSyncWorkflow() {
		return "interactive sync"
	}
	return "sync"
}

func (m tuiSyncModel) runStateLabel() string {
	switch {
	case m.running:
		return "running"
	case m.done && (m.runErr != nil || m.result.Failed > 0):
		return "done-with-errors"
	case m.done:
		return "complete"
	default:
		return "ready"
	}
}

func (m tuiSyncModel) runStateTone() string {
	switch m.runStateLabel() {
	case "running":
		return "warning"
	case "done-with-errors":
		return "danger"
	case "complete":
		return "success"
	default:
		return "info"
	}
}

func boolLabel(v bool) string {
	if v {
		return "ON"
	}
	return "OFF"
}

func boolTone(v bool) string {
	if v {
		return "success"
	}
	return "muted"
}

func failureCountTone(count int) string {
	if count > 0 {
		return "danger"
	}
	return "success"
}
