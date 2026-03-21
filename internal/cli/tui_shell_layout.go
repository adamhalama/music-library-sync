package cli

import (
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
