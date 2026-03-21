package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/jaa/update-downloads/internal/engine"
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
	StyledBody       bool
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
	Tone     string
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
	if !item.Active && !item.Disabled {
		switch item.Tone {
		case "info":
			style = style.Copy().Foreground(lipgloss.Color("81"))
		case "success":
			style = style.Copy().Foreground(lipgloss.Color("78"))
		case "warning":
			style = style.Copy().Foreground(lipgloss.Color("179"))
		case "muted":
			style = style.Copy().Foreground(lipgloss.Color("243"))
		}
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
		chunk := fmt.Sprintf("[%s] %s", shortcut.Key, shortcut.Label)
		style := theme.muted
		if shortcut.Disabled {
			style = theme.disabled
		}
		rendered = append(rendered, style.Render(chunk))
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func shellMainSectionWidth(layout tuiShellLayout, theme tuiShellTheme) int {
	frameInnerWidth := layout.Width - theme.frame.GetHorizontalFrameSize()
	if frameInnerWidth < 20 {
		frameInnerWidth = layout.Width
	}
	if layout.Compact {
		return frameInnerWidth
	}
	sidebarWidth := shellSidebarWidth(layout, theme)
	mainWidth := frameInnerWidth - sidebarWidth
	if mainWidth < 20 {
		return frameInnerWidth
	}
	return mainWidth
}

func shellSidebarWidth(layout tuiShellLayout, theme tuiShellTheme) int {
	if layout.Compact {
		return 0
	}
	frameInnerWidth := layout.Width - theme.frame.GetHorizontalFrameSize()
	if frameInnerWidth < 40 {
		return 0
	}
	sidebarWidth := layout.SidebarWidth
	if sidebarWidth < 20 {
		sidebarWidth = 20
	}
	maxSidebar := frameInnerWidth - 40
	if maxSidebar < 20 {
		maxSidebar = 20
	}
	if sidebarWidth > maxSidebar {
		sidebarWidth = maxSidebar
	}
	return sidebarWidth
}

func styleContentWidth(totalWidth int, style lipgloss.Style) int {
	width := totalWidth - style.GetHorizontalFrameSize()
	if width < 0 {
		return 0
	}
	return width
}

func shellBodyHeight(layout tuiShellLayout, theme tuiShellTheme, fixedParts []string) int {
	height := layout.Height - theme.frame.GetVerticalFrameSize()
	for _, part := range fixedParts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		height -= lipgloss.Height(part)
	}
	if count := len(filterEmptyStrings(fixedParts)); count > 1 {
		height -= count - 1
	}
	if height < 1 {
		return 1
	}
	return height
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
		DenseBody:        syncModel.planPrompt != nil || syncModel.isInteractiveSyncWorkflow(),
		StyledBody:       syncModel.planPrompt != nil || syncModel.isInteractiveSyncWorkflow(),
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
	if m.isInteractiveSyncWorkflow() {
		if state := m.currentInteractiveSelection(); state != nil && len(state.rows) > 0 {
			return nil
		}
		if m.running {
			return []tuiShortcut{
				{Key: "p", Label: "activity"},
				{Key: "x", Label: "cancel active run"},
			}
		}
		if m.done {
			return []tuiShortcut{
				{Key: "p", Label: "activity"},
			}
		}
		shortcuts := []tuiShortcut{
			{Key: "j/k", Label: "move"},
			{Key: "space", Label: "toggle"},
			{Key: "d", Label: "dry-run"},
			{Key: "t", Label: "timeout"},
			{Key: "[/]", Label: "plan limit"},
			{Key: "l", Label: "type limit"},
			{Key: "u", Label: "unlimited"},
			{Key: "p", Label: "activity"},
			{Key: "enter", Label: "run"},
			{Key: "x", Label: "cancel active run", Disabled: !m.running},
		}
		return shortcuts
	}
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
	if m.isInteractiveSyncWorkflow() {
		state := m.currentInteractiveSelection()
		stats := []tuiFooterStat{{Label: "state", Value: m.runStateLabel(), Tone: m.runStateTone()}}
		if !m.running && !m.done {
			selected := 0
			pending := 0
			skipped := 0
			if state != nil {
				selected = state.selectedCount()
				pending = state.toggleableCount()
				skipped = state.skippedCount()
			}
			stats = append(stats,
				tuiFooterStat{Label: "selected", Value: fmt.Sprintf("%d", selected), Tone: "info"},
				tuiFooterStat{Label: "pending", Value: fmt.Sprintf("%d", pending), Tone: "info"},
				tuiFooterStat{Label: "skipped", Value: fmt.Sprintf("%d", skipped), Tone: "muted"},
				tuiFooterStat{Label: "progress", Value: tuiRenderProgressBar(0, 10), Tone: "muted"},
			)
			return stats
		}
		total := 0
		current := 0
		doneCount := m.result.Succeeded
		skippedCount := m.result.Skipped
		failedCount := m.result.Failed
		if m.progress != nil {
			total = m.progress.EffectiveTotal()
			current = m.progress.CurrentIndex()
			if m.running {
				doneCount = m.progress.Snapshot().Progress.Global.Completed
			}
		}
		progressPercent := 0.0
		if m.progress != nil {
			progressPercent = m.progress.GlobalProgressPercent()
		} else if total > 0 {
			progressPercent = float64(current) * 100 / float64(total)
		}
		progressLabel := tuiRenderProgressBar(progressPercent, 10)
		stats = append(stats,
			tuiFooterStat{Label: "done", Value: fmt.Sprintf("%d", doneCount), Tone: "success"},
			tuiFooterStat{Label: "skipped", Value: fmt.Sprintf("%d", skippedCount), Tone: "muted"},
			tuiFooterStat{Label: "failed", Value: fmt.Sprintf("%d", failedCount), Tone: failureCountTone(failedCount)},
			tuiFooterStat{Label: "progress", Value: progressLabel, Tone: "info"},
			tuiFooterStat{Label: "elapsed", Value: m.elapsedLabel(), Tone: "muted"},
		)
		return stats
	}
	stats := []tuiFooterStat{
		{Label: "state", Value: m.runStateLabel(), Tone: m.runStateTone()},
		{Label: "sources", Value: fmt.Sprintf("%d/%d", m.selectedSourceCount(), len(m.sources)), Tone: "info"},
	}
	if m.planPrompt != nil {
		focus := "tracks"
		if m.planPrompt.focusFilters {
			focus = "filters"
		}
		stats = append(stats,
			tuiFooterStat{Label: "filter", Value: m.planPrompt.filterLabel(), Tone: "info"},
			tuiFooterStat{Label: "focus", Value: focus, Tone: "warning"},
		)
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
	state := m.currentInteractiveSelection()
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
	state.ensureCursorVisible()
	lines := planPromptHeaderLines(state, modeLabel, limitLabel, layout)
	filteredRows := state.filteredRows()
	if len(filteredRows) == 0 {
		lines = append(lines, "No tracks found in selected preflight window.")
	} else {
		lines = append(lines, strings.Split(renderPlanPromptTable(state, layout), "\n")...)
	}
	lines = append(lines, fmt.Sprintf("Selected: %d/%d toggleable tracks  |  Showing: %d/%d", state.selectedCount(), state.toggleableCount(), len(filteredRows), len(state.rows)))
	theme := newTUIShellTheme()
	bodyStyle := theme.bodyPanel.Padding(0, 1)
	width := shellMainSectionWidth(layout, theme) - bodyStyle.GetHorizontalFrameSize() - 4
	if width < 24 {
		width = shellMainSectionWidth(layout, theme)
	}
	bodyLines := make([]string, 0, len(lines)+1)
	bodyLines = append(bodyLines, strings.Repeat("─", maxInt(8, width)))
	for _, line := range lines {
		bodyLines = append(bodyLines, ansi.Truncate(line, width, ""))
	}
	return strings.Join(bodyLines, "\n")
}

func planPromptHeaderLines(state *tuiInteractiveSelectionState, modeLabel, limitLabel string, layout tuiShellLayout) []string {
	infoBar := renderPlanPromptInfoBar(state, modeLabel, limitLabel)
	controls := renderPlanPromptControls(state)
	if layout.Height < 24 {
		return []string{
			infoBar,
			renderPlanPromptPathLine(
				fmt.Sprintf("target %s", filepath.Base(state.details.TargetDir)),
				fmt.Sprintf("state %s", filepath.Base(state.details.StateFile)),
			),
			controls,
		}
	}
	return []string{
		infoBar,
		renderPlanPromptPathLine("target "+state.details.TargetDir, "state "+state.details.StateFile),
		renderPlanPromptPathLine("url "+state.details.URL, ""),
		controls,
	}
}

func renderPlanPromptInfoBar(state *tuiInteractiveSelectionState, modeLabel, limitLabel string) string {
	parts := []string{
		planPromptChip("Plan Selection", "info"),
		planPromptField("source", state.sourceID),
		planPromptField("mode", modeLabel),
		planPromptField("limit", limitLabel),
		planPromptField("type", fmt.Sprintf("%s/%s", state.details.SourceType, state.details.Adapter)),
	}
	return lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Padding(0, 1).
		Render(strings.Join(parts, "  "))
}

func renderPlanPromptPathLine(left, right string) string {
	parts := []string{
		lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(left),
	}
	if strings.TrimSpace(right) != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("•"))
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(right))
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Render(strings.Join(parts, "  "))
}

func renderPlanPromptControls(state *tuiInteractiveSelectionState) string {
	focusTone := "warning"
	if !state.focusFilters {
		focusTone = "info"
	}
	parts := []string{
		planPromptChip("focus "+state.focusLabel(), focusTone),
		planPromptChip("filter "+state.filterLabel(), "muted"),
		renderPlanPromptKey("tab", "switch"),
		renderPlanPromptKey("j/k", "move"),
		renderPlanPromptKey("space", "toggle/apply"),
		renderPlanPromptKey("a", "all visible"),
		renderPlanPromptKey("n", "clear visible"),
		renderPlanPromptKey("enter", "confirm"),
		renderPlanPromptKey("esc", "cancel"),
	}
	return lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Padding(0, 1).
		Render(strings.Join(parts, "  "))
}

func renderPlanPromptKey(keyLabel, label string) string {
	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Background(lipgloss.Color("239")).
		Bold(true).
		Padding(0, 1)
	textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	return keyStyle.Render(keyLabel) + " " + textStyle.Render(label)
}

func planPromptChip(label, tone string) string {
	style := lipgloss.NewStyle().Padding(0, 1).Bold(true)
	switch tone {
	case "warning":
		style = style.Foreground(lipgloss.Color("179")).Background(lipgloss.Color("52"))
	case "success":
		style = style.Foreground(lipgloss.Color("78")).Background(lipgloss.Color("22"))
	case "muted":
		style = style.Foreground(lipgloss.Color("245")).Background(lipgloss.Color("238"))
	default:
		style = style.Foreground(lipgloss.Color("81")).Background(lipgloss.Color("17"))
	}
	return style.Render(label)
}

func planPromptField(label, value string) string {
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Bold(true)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	return labelStyle.Render(strings.ToLower(label)+"=") + valueStyle.Render(value)
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
	if m.isInteractiveSyncWorkflow() {
		return ""
	}
	return m.workflowTitle()
}

func (m tuiSyncModel) shellBody(layout tuiShellLayout) string {
	if m.isInteractiveSyncWorkflow() {
		return m.interactiveSyncBody(layout)
	}
	return m.bodyView(false)
}

func (m tuiSyncModel) interactiveSyncBody(layout tuiShellLayout) string {
	if !m.cfgLoaded {
		return renderPlanSection("Interactive Sync", []string{"Loading config..."}, shellMainSectionWidth(layout, newTUIShellTheme())-4)
	}
	if m.cfgErr != nil {
		return renderPlanSection("Interactive Sync", []string{fmt.Sprintf("Config load failed: %v", m.cfgErr)}, shellMainSectionWidth(layout, newTUIShellTheme())-4)
	}
	state := m.currentInteractiveSelection()
	width := shellMainSectionWidth(layout, newTUIShellTheme()) - 4
	if width < 36 {
		width = shellMainSectionWidth(layout, newTUIShellTheme())
	}
	selectionSection := renderPlanSection("Selection", m.interactiveSelectionSummaryLines(state, layout), width)
	tracksSection := renderPlanSection("Tracks", m.interactiveTrackLines(state, layout), width)
	activitySection := renderPlanSection("Activity", m.interactiveActivityLines(state, layout), width)
	sections := []string{selectionSection, tracksSection, activitySection}
	if state != nil && len(state.rows) > 0 && layout.Height < 24 {
		sections = []string{tracksSection, selectionSection, activitySection}
	}
	return strings.Join(sections, "\n")
}

func (m tuiSyncModel) interactiveSelectionSummaryLines(state *tuiInteractiveSelectionState, layout tuiShellLayout) []string {
	lines := []string{}
	if state != nil && len(state.rows) > 0 {
		lines = append(lines, m.interactiveSelectionContextLines(state, layout)...)
		lines = append(lines, fmt.Sprintf("selected=%d  pending=%d  skipped=%d", state.selectedCount(), state.toggleableCount(), state.skippedCount()))
	} else {
		lines = append(lines,
			fmt.Sprintf("selected_sources=%d/%d", m.selectedSourceCount(), len(m.sources)),
			fmt.Sprintf("dry_run=%t  timeout=%s  plan_limit=%s", m.dryRun, formatTimeoutOverride(m.timeoutOverride), formatPlanLimit(m.planLimit)),
			"Rows appear here after interactive preflight starts.",
			"Press enter to run `udl sync --plan` for the selected sources.",
		)
	}
	if m.planLimitInputActive {
		lines = append(lines, fmt.Sprintf("plan_limit_input=%q  enter apply  esc cancel", m.planLimitInput))
		if m.planLimitInputErr != "" {
			lines = append(lines, "input_error: "+m.planLimitInputErr)
		}
	}
	if m.timeoutInputActive {
		lines = append(lines, fmt.Sprintf("timeout_input=%q  enter apply  esc cancel", m.timeoutInput))
		if m.timeoutInputErr != "" {
			lines = append(lines, "input_error: "+m.timeoutInputErr)
		}
	}
	if m.validationErr != "" {
		lines = append(lines, "validation_error: "+m.validationErr)
	}
	if m.running {
		lines = append(lines, "Running sync... press x or ctrl+c to cancel.")
		if m.cancelRequested {
			lines = append(lines, "Cancellation requested, waiting for adapter shutdown...")
		}
	}
	if m.done {
		if m.runErr != nil {
			lines = append(lines, "Run failed: "+m.runErr.Error())
		} else {
			lines = append(lines, fmt.Sprintf("Run finished: attempted=%d succeeded=%d failed=%d skipped=%d", m.result.Attempted, m.result.Succeeded, m.result.Failed, m.result.Skipped))
		}
	}
	return lines
}

func (m tuiSyncModel) interactiveSelectionContextLines(state *tuiInteractiveSelectionState, layout tuiShellLayout) []string {
	modeLabel := "run"
	if state.details.DryRun {
		modeLabel = "dry-run"
	}
	limitLabel := "unlimited"
	if state.details.PlanLimit > 0 {
		limitLabel = fmt.Sprintf("%d", state.details.PlanLimit)
	}
	lines := []string{renderPlanPromptInfoBar(state, modeLabel, limitLabel)}
	if layout.Height < 24 {
		lines = append(lines, renderPlanPromptPathLine(
			fmt.Sprintf("target %s", filepath.Base(state.details.TargetDir)),
			fmt.Sprintf("state %s", filepath.Base(state.details.StateFile)),
		))
	} else {
		lines = append(lines, renderPlanPromptPathLine("target "+state.details.TargetDir, "state "+state.details.StateFile))
		lines = append(lines, renderPlanPromptPathLine("url "+state.details.URL, ""))
	}
	lines = append(lines, m.renderInteractiveSelectionControls(state))
	return lines
}

func (m tuiSyncModel) renderInteractiveSelectionControls(state *tuiInteractiveSelectionState) string {
	if m.planPrompt != nil {
		return renderPlanPromptControls(state)
	}
	focusTone := "warning"
	if !state.focusFilters {
		focusTone = "info"
	}
	parts := []string{
		planPromptChip("focus "+state.focusLabel(), focusTone),
		planPromptChip("filter "+state.filterLabel(), "muted"),
		renderPlanPromptKey("tab", "switch"),
		renderPlanPromptKey("j/k", "move"),
	}
	if state.focusFilters {
		parts = append(parts, renderPlanPromptKey("space/enter", "apply filter"))
	}
	parts = append(parts, renderPlanPromptKey("p", "activity"))
	if m.running {
		parts = append(parts, renderPlanPromptKey("x", "cancel run"))
	}
	return lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Padding(0, 1).
		Render(strings.Join(parts, "  "))
}

func (m tuiSyncModel) interactiveTrackLines(state *tuiInteractiveSelectionState, layout tuiShellLayout) []string {
	if state == nil || len(state.rows) == 0 {
		return []string{
			"SEL  #  STATUS  TRACK  ID",
			"no tracks yet",
			"Press enter to start preflight and populate the inline selector.",
		}
	}
	state.ensureCursorVisible()
	filteredRows := state.filteredRows()
	lines := []string{}
	if len(filteredRows) == 0 {
		lines = append(lines, "No tracks match the current filter.")
	} else {
		lines = append(lines, strings.Split(renderPlanPromptTable(state, layout), "\n")...)
	}
	lines = append(lines, fmt.Sprintf("Showing %d/%d rows", len(filteredRows), len(state.rows)))
	return lines
}

func (m tuiSyncModel) interactiveActivityLines(state *tuiInteractiveSelectionState, layout tuiShellLayout) []string {
	if state == nil {
		state = newEmptyTUIInteractiveSelectionState()
	}
	if state.activityCollapsedFor(layout) {
		return []string{"collapsed", "press p to expand"}
	}
	lines := []string{"p: collapse"}
	if len(state.activity) == 0 {
		lines = append(lines,
			"no activity yet",
			"activity updates appear here after preflight and runtime events",
		)
	} else {
		for _, entry := range state.activity {
			lines = append(lines, formatInteractiveActivityEntry(entry))
		}
	}
	if failureLines := m.lastFailureLines(); len(failureLines) > 0 {
		lines = append(lines, "", "last failure:")
		lines = append(lines, failureLines...)
	}
	return lines
}

func formatInteractiveActivityEntry(entry tuiActivityEntry) string {
	ts := "--:--:--"
	if !entry.Timestamp.IsZero() {
		ts = entry.Timestamp.Format("15:04:05")
	}
	return fmt.Sprintf("%s  %s", ts, entry.Message)
}

func renderPlanSection(title string, lines []string, width int) string {
	style := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("236")).
		Padding(0, 0)
	contentWidth := styleContentWidth(width, style)
	if contentWidth < 20 {
		contentWidth = 20
	}
	header := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Bold(true).
		Render(ansi.Truncate(title, contentWidth, ""))
	bodyLines := make([]string, 0, len(lines))
	for _, line := range lines {
		bodyLines = append(bodyLines, ansi.Truncate(line, contentWidth, ""))
	}
	body := strings.Join(bodyLines, "\n")
	return style.
		Width(contentWidth).
		Render(header + "\n" + body)
}

func renderPlanPromptTable(state *tuiInteractiveSelectionState, layout tuiShellLayout) string {
	theme := newTUIShellTheme()
	bodyStyle := theme.bodyPanel.Padding(0, 1)
	width := shellMainSectionWidth(layout, theme) - bodyStyle.GetHorizontalFrameSize() - 4
	if width < 48 {
		width = 48
	}
	idWidth := 12
	statusWidth := 20
	indexWidth := 4
	selectWidth := 4
	gapWidth := 8
	titleWidth := width - idWidth - statusWidth - indexWidth - selectWidth - gapWidth
	if titleWidth < 16 {
		titleWidth = 16
	}
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245"))
	header := strings.Join([]string{
		headerStyle.Width(selectWidth).Render("SEL"),
		headerStyle.Width(indexWidth).Render("#"),
		headerStyle.Width(statusWidth).Render("STATUS"),
		headerStyle.Width(titleWidth).Render("TRACK"),
		headerStyle.Width(idWidth).Render("ID"),
	}, "  ")
	header = lipgloss.NewStyle().Background(lipgloss.Color("237")).Padding(0, 1).Render(header)

	filtered := state.filteredRows()
	visibleIndices := state.visibleRowIndices()
	filteredCursor := 0
	for i, idx := range visibleIndices {
		if idx == state.cursor {
			filteredCursor = i
			break
		}
	}
	start, end := shellPlanPromptRowWindow(len(filtered), filteredCursor, layout)
	lines := []string{header, lipgloss.NewStyle().Foreground(lipgloss.Color("239")).Render(strings.Repeat("─", maxInt(16, width)))}
	for i := start; i < end; i++ {
		row := filtered[i]
		isCursor := i == filteredCursor
		lines = append(lines, renderPlanPromptRow(row, isCursor, selectWidth, indexWidth, statusWidth, titleWidth, idWidth))
	}
	return strings.Join(lines, "\n")
}

func renderPlanPromptRow(row tuiTrackRowState, isCursor bool, selectWidth, indexWidth, statusWidth, titleWidth, idWidth int) string {
	cursorPrefix := " "
	if isCursor {
		cursorPrefix = ">"
	}
	selectLabel := "[-]"
	selectTone := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	if row.Toggleable {
		selectLabel = "[ ]"
		selectTone = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		if row.Selected {
			selectLabel = "[x]"
			selectTone = lipgloss.NewStyle().Foreground(lipgloss.Color("78")).Bold(true)
		}
	}
	statusLabel, statusStyle := planPromptStatusChip(row, statusWidth)
	title := strings.TrimSpace(row.Title)
	if title == "" {
		title = "(untitled)"
	}
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	if row.RuntimeStatus == tuiTrackStatusExisting {
		titleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	}
	idStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	line := strings.Join([]string{
		selectTone.Width(selectWidth).Render(cursorPrefix + selectLabel),
		lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Width(indexWidth).Render(fmt.Sprintf("%d", row.Index)),
		statusStyle.Width(statusWidth).Render(statusLabel),
		titleStyle.Width(titleWidth).Render(ansi.Truncate(title, titleWidth, "")),
		idStyle.Width(idWidth).Render(ansi.Truncate(row.RemoteID, idWidth, "")),
	}, "  ")
	if isCursor {
		return lipgloss.NewStyle().Background(lipgloss.Color("236")).Render(line)
	}
	return line
}

func planPromptStatusChip(row tuiTrackRowState, statusWidth int) (string, lipgloss.Style) {
	switch row.RuntimeStatus {
	case tuiTrackStatusDownloading:
		label := " downloading "
		if row.ProgressKnown {
			barWidth := statusWidth - 11
			if barWidth < 4 {
				barWidth = 4
			}
			label = fmt.Sprintf(" dl %3.0f%% %s ", row.ProgressPercent, tuiRenderMiniBar(row.ProgressPercent, barWidth))
		}
		return label, lipgloss.NewStyle().Foreground(lipgloss.Color("179")).Background(lipgloss.Color("52")).Bold(true)
	case tuiTrackStatusDownloaded:
		return " downloaded ", lipgloss.NewStyle().Foreground(lipgloss.Color("78")).Background(lipgloss.Color("22")).Bold(true)
	case tuiTrackStatusFailed:
		return " failed ", lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Background(lipgloss.Color("52")).Bold(true)
	case tuiTrackStatusSkipped:
		return " skipped ", lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Background(lipgloss.Color("238")).Bold(true)
	case tuiTrackStatusExisting:
		return " have-it ", lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Background(lipgloss.Color("237"))
	default:
		switch row.PlanStatus {
		case engine.PlanRowMissingNew:
			return " new ", lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Background(lipgloss.Color("17")).Bold(true)
		case engine.PlanRowMissingKnownGap:
			return " known-gap ", lipgloss.NewStyle().Foreground(lipgloss.Color("179")).Background(lipgloss.Color("52")).Bold(true)
		default:
			return " pending ", lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Background(lipgloss.Color("17")).Bold(true)
		}
	}
}

func shellPlanPromptRowWindow(total, cursor int, layout tuiShellLayout) (int, int) {
	maxRows := layout.Height - 22
	if maxRows < 4 {
		maxRows = 4
	}
	if total <= 0 {
		return 0, 0
	}
	if total <= maxRows {
		return 0, total
	}
	start := cursor - (maxRows / 2)
	if start < 0 {
		start = 0
	}
	end := start + maxRows
	if end > total {
		end = total
		start = end - maxRows
	}
	return start, end
}

func tuiRenderProgressBar(percent float64, width int) string {
	if width < 4 {
		width = 4
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := int((percent/100)*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + fmt.Sprintf(" %3.0f%%", percent)
}

func tuiRenderMiniBar(percent float64, width int) string {
	if width < 2 {
		width = 2
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := int((percent/100)*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
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
	if state := m.currentInteractiveSelection(); state != nil && len(state.rows) > 0 {
		filterItems := make([]tuiSidebarItem, 0, len(state.filters()))
		for idx, filter := range state.filters() {
			count := state.filterCount(filter)
			item := tuiSidebarItem{
				Label:  fmt.Sprintf("%s (%d)", state.filterDisplayLabel(filter), count),
				Active: m.planPrompt != nil && state.focusFilters && idx == state.filterCursor,
			}
			if state.filter == filter {
				item.Tone = "info"
			}
			switch filter {
			case tuiPlanFilterMissingNew:
				item.Tone = "success"
			case tuiPlanFilterKnownGap:
				item.Tone = "warning"
			case tuiPlanFilterDownloaded:
				item.Tone = "muted"
			}
			filterItems = append(filterItems, item)
		}
		sections = append(sections, tuiSidebarSection{Title: "filters", Items: filterItems})
	}
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
