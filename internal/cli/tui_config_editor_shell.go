package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/jaa/update-downloads/internal/config"
)

func buildConfigEditorShellState(m tuiRootModel, layout tuiShellLayout) tuiShellState {
	model := m.configModel
	state := tuiShellState{
		AppLabel:         "UDL",
		ScreenTitle:      "Config Editor",
		SidebarSections:  model.sidebarSections(layout),
		Badges:           model.shellBadges(),
		CommandSummary:   model.shellCommandSummary(),
		Shortcuts:        model.shellShortcuts(),
		BodyTitle:        "Config Editor",
		Body:             model.shellBody(layout),
		DenseBody:        true,
		StyledBody:       true,
		FooterStats:      model.shellFooterStats(),
		Banner:           model.shellBanner(),
		AllowBack:        m.canReturnToMenuOnEsc(),
		DebugMessageType: m.lastMsgTypeIfEnabled(),
	}
	if model.modal != nil {
		state.Modal = model.modalState()
	}
	return state
}

func (m tuiConfigEditorModel) sidebarSections(layout tuiShellLayout) []tuiSidebarSection {
	flowItems := []tuiSidebarItem{}
	for _, phase := range []tuiConfigEditorPhase{
		tuiConfigEditorPhaseTarget,
		tuiConfigEditorPhaseDefaults,
		tuiConfigEditorPhaseSources,
		tuiConfigEditorPhaseReview,
		tuiConfigEditorPhaseSave,
	} {
		meta := ""
		switch phase {
		case tuiConfigEditorPhaseTarget:
			meta = string(m.targetKind)
		case tuiConfigEditorPhaseDefaults:
			meta = "global"
		case tuiConfigEditorPhaseSources:
			meta = fmt.Sprintf("%d source(s)", len(m.sources))
		case tuiConfigEditorPhaseReview:
			meta = fmt.Sprintf("%d issue(s)", len(m.validationProblems))
		case tuiConfigEditorPhaseSave:
			if m.saveResult != nil {
				meta = "saved"
			} else {
				meta = "ready"
			}
		}
		flowItems = append(flowItems, tuiSidebarItem{
			Label:  shellTitle(string(phase)),
			Meta:   meta,
			Active: m.phase == phase,
		})
	}
	sections := []tuiSidebarSection{{Title: "flow", Items: flowItems}}
	if m.phase == tuiConfigEditorPhaseSources {
		sourceItems := make([]tuiSidebarItem, 0, len(m.sources))
		for idx, source := range m.sources {
			meta := fmt.Sprintf("%s/%s", source.Source.Type, source.Source.Adapter.Kind)
			if !source.Source.Enabled {
				meta += " disabled"
			}
			sourceItems = append(sourceItems, tuiSidebarItem{
				Label:  source.Source.ID,
				Meta:   meta,
				Active: idx == m.sourceListCursor,
				Tone:   m.sourceItemTone(source.Source),
			})
		}
		if len(sourceItems) == 0 {
			sourceItems = append(sourceItems, tuiSidebarItem{Label: "(no sources)", Meta: "press a to add", Disabled: true})
		}
		sections = append(sections, tuiSidebarSection{Title: "sources", Items: sourceItems})
	}
	return sections
}

func (m tuiConfigEditorModel) sourceItemTone(source config.Source) string {
	if !source.Enabled {
		return "muted"
	}
	if source.Type == config.SourceTypeSpotify {
		return "info"
	}
	return "success"
}

func (m tuiConfigEditorModel) shellBadges() []tuiBadge {
	targetLabel := strings.ToUpper(string(m.targetKind))
	if m.fileExists && m.targetKind != tuiConfigEditorTargetExplicit && m.targetKind != tuiConfigEditorTargetUser {
		targetLabel = "EXISTING"
	}
	validTone := "success"
	validLabel := "VALID"
	if m.parseErr != nil || len(m.validationProblems) > 0 {
		validTone = "danger"
		validLabel = "INVALID"
	}
	dirtyLabel := "CLEAN"
	dirtyTone := "muted"
	if m.dirty {
		dirtyLabel = "DIRTY"
		dirtyTone = "warning"
	}
	badges := []tuiBadge{
		{Label: "STEP: " + strings.ToUpper(string(m.phase)), Tone: "info"},
		{Label: "TARGET: " + targetLabel, Tone: "muted"},
		{Label: dirtyLabel, Tone: dirtyTone},
		{Label: validLabel, Tone: validTone},
	}
	if m.saveResult != nil {
		badges = append(badges, tuiBadge{Label: "SAVED", Tone: "success"})
	}
	return badges
}

func (m tuiConfigEditorModel) shellCommandSummary() []string {
	parts := []string{
		"udl",
		"tui",
		"config_editor",
		"phase=" + string(m.phase),
	}
	if trimmed := strings.TrimSpace(m.targetPath); trimmed != "" {
		parts = append(parts, "target="+trimmed)
	}
	return parts
}

func (m tuiConfigEditorModel) shellShortcuts() []tuiShortcut {
	if m.edit != nil {
		return []tuiShortcut{
			{Key: "type", Label: "edit"},
			{Key: "enter", Label: "apply"},
			{Key: "esc", Label: "cancel"},
		}
	}
	if m.modal != nil {
		return nil
	}
	shortcuts := []tuiShortcut{{Key: "esc", Label: "back"}}
	switch m.phase {
	case tuiConfigEditorPhaseTarget:
		shortcuts = append(shortcuts, tuiShortcut{Key: "enter", Label: "continue"})
	case tuiConfigEditorPhaseDefaults:
		shortcuts = append(shortcuts,
			tuiShortcut{Key: "j/k", Label: "move"},
			tuiShortcut{Key: "enter", Label: "edit/apply"},
		)
	case tuiConfigEditorPhaseSources:
		shortcuts = append(shortcuts,
			tuiShortcut{Key: "j/k", Label: "move"},
			tuiShortcut{Key: "tab", Label: "switch pane"},
			tuiShortcut{Key: "a", Label: "add"},
			tuiShortcut{Key: "d", Label: "duplicate"},
			tuiShortcut{Key: "D", Label: "delete"},
			tuiShortcut{Key: "[/]", Label: "reorder"},
		)
	case tuiConfigEditorPhaseReview:
		shortcuts = append(shortcuts,
			tuiShortcut{Key: "enter", Label: "save step"},
			tuiShortcut{Key: "s", Label: "save"},
			tuiShortcut{Key: "p", Label: "preview"},
		)
	case tuiConfigEditorPhaseSave:
		shortcuts = append(shortcuts,
			tuiShortcut{Key: "s", Label: "save"},
			tuiShortcut{Key: "p", Label: "preview"},
		)
	}
	return shortcuts
}

func (m tuiConfigEditorModel) shellFooterStats() []tuiFooterStat {
	stats := []tuiFooterStat{
		{Label: "phase", Value: string(m.phase), Tone: "info"},
		{Label: "sources", Value: fmt.Sprintf("%d", len(m.sources)), Tone: "info"},
		{Label: "issues", Value: fmt.Sprintf("%d", len(m.validationProblems)), Tone: failureCountTone(len(m.validationProblems))},
	}
	if m.saveResult != nil {
		stats = append(stats, tuiFooterStat{Label: "state dir", Value: m.saveResult.StateDir, Tone: "muted"})
	}
	return stats
}

func (m tuiConfigEditorModel) shellBanner() *tuiBanner {
	switch {
	case m.prepareErr != nil:
		return &tuiBanner{Text: m.prepareErr.Error(), Tone: "danger"}
	case m.parseErr != nil:
		return &tuiBanner{Text: "Target file is invalid YAML. Use enter to review recovery options.", Tone: "danger"}
	case m.validationErr != "":
		return &tuiBanner{Text: m.validationErr, Tone: "warning"}
	case m.saveErr != nil:
		return &tuiBanner{Text: "Save failed: " + m.saveErr.Error(), Tone: "danger"}
	default:
		return nil
	}
}

func (m tuiConfigEditorModel) modalState() *tuiModalState {
	if m.modal == nil {
		return nil
	}
	return &tuiModalState{Title: m.modal.Title, Lines: m.modal.Lines, Tone: "warning"}
}

func (m tuiConfigEditorModel) shellBody(layout tuiShellLayout) string {
	switch m.phase {
	case tuiConfigEditorPhaseTarget:
		return m.targetBody(layout)
	case tuiConfigEditorPhaseDefaults:
		return m.defaultsBody(layout)
	case tuiConfigEditorPhaseSources:
		return m.sourcesBody(layout)
	case tuiConfigEditorPhaseReview:
		return m.reviewBody(layout, false)
	case tuiConfigEditorPhaseSave:
		return m.reviewBody(layout, true)
	default:
		return renderPlanSection("Config Editor", []string{"Unknown phase."}, shellMainSectionWidth(layout, newTUIShellTheme())-4)
	}
}

func (m tuiConfigEditorModel) targetBody(layout tuiShellLayout) string {
	width := shellMainSectionWidth(layout, newTUIShellTheme()) - 4
	if width < 36 {
		width = shellMainSectionWidth(layout, newTUIShellTheme())
	}
	status := "new file"
	if m.fileExists {
		status = "existing file"
	}
	if m.parseErr != nil {
		status = "existing file (invalid yaml)"
	}
	targetLines := []string{
		"Path: " + m.targetPath,
		"Kind: " + string(m.targetKind),
		"Status: " + status,
	}
	actions := []string{"enter: continue"}
	if !m.fileExists {
		actions = []string{"enter: create in-memory config from defaults"}
	}
	if m.parseErr != nil {
		actions = []string{"enter: open reset options", "esc: back"}
	}
	sections := []string{
		renderPlanSection("Target", targetLines, width),
		renderPlanSection("Actions", actions, width),
	}
	if m.parseErr != nil {
		sections = append(sections, renderPlanSection("Recovery", tuiWrapLines(tuiSplitDetailLines(m.parseErr.Error()), width-4), width))
	}
	return strings.Join(sections, "\n")
}

func (m tuiConfigEditorModel) defaultsBody(layout tuiShellLayout) string {
	width := shellMainSectionWidth(layout, newTUIShellTheme()) - 4
	if width < 36 {
		width = shellMainSectionWidth(layout, newTUIShellTheme())
	}
	cursor := m.defaultsCursor
	fields := []string{
		m.renderCursorLine(cursor == 0, "version", "1", true),
		m.renderCursorLine(cursor == 1, "defaults.state_dir", m.defaults.StateDir, false),
		m.renderCursorLine(cursor == 2, "defaults.archive_file", m.defaults.ArchiveFile, false),
		m.renderCursorLine(cursor == 3, "defaults.threads", strconv.Itoa(m.defaults.Threads), false),
		m.renderCursorLine(cursor == 4, "defaults.continue_on_error", tuiBoolLabel(m.defaults.ContinueOnError), false),
		m.renderCursorLine(cursor == 5, "defaults.command_timeout_seconds", strconv.Itoa(m.defaults.CommandTimeoutSeconds), false),
		m.renderCursorLine(cursor == 6, "Continue to Sources", "enter", false),
	}
	resolvedStateDir, _ := config.ExpandPath(m.defaults.StateDir)
	notes := []string{"enter: edit/apply", "space: toggle bool", "Move to `Continue to Sources` and press enter to advance."}
	if m.edit != nil {
		notes = append(notes, fmt.Sprintf("editing %s = %q", m.edit.Title, m.edit.Buffer))
	}
	sections := []string{
		renderPlanSection("Defaults", fields, width),
		renderPlanSection("Resolved Paths", []string{
			"state_dir => " + firstNonEmpty(resolvedStateDir, "(unresolved)"),
			"archive_file => " + firstNonEmpty(strings.TrimSpace(m.defaults.ArchiveFile), "(empty)"),
		}, width),
		renderPlanSection("Notes", append(notes, m.defaultsValidationProblems()...), width),
	}
	return strings.Join(sections, "\n")
}

func (m tuiConfigEditorModel) sourcesBody(layout tuiShellLayout) string {
	theme := newTUIShellTheme()
	totalWidth := shellMainSectionWidth(layout, theme) - 4
	if totalWidth < 44 {
		totalWidth = shellMainSectionWidth(layout, theme)
	}
	listWidth := totalWidth / 3
	if listWidth < 28 {
		listWidth = 28
	}
	formWidth := totalWidth - listWidth - 2
	if formWidth < 28 {
		formWidth = totalWidth
	}
	listLines := m.sourceListLines()
	formLines := m.sourceFormLines()
	if layout.Compact || totalWidth < 90 {
		return strings.Join([]string{
			renderPlanSection("Source List", listLines, totalWidth),
			renderPlanSection("Source Editor", formLines, totalWidth),
		}, "\n")
	}
	left := renderPlanSection("Source List", listLines, listWidth)
	right := renderPlanSection("Source Editor", formLines, formWidth)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
}

func (m tuiConfigEditorModel) reviewBody(layout tuiShellLayout, saveMode bool) string {
	theme := newTUIShellTheme()
	totalWidth := shellMainSectionWidth(layout, theme) - 4
	if totalWidth < 44 {
		totalWidth = shellMainSectionWidth(layout, theme)
	}
	cfg := m.buildConfig()
	summaryLines := []string{
		"Target: " + m.targetPath,
		fmt.Sprintf("Defaults: threads=%d continue_on_error=%t timeout=%ds", cfg.Defaults.Threads, cfg.Defaults.ContinueOnError, cfg.Defaults.CommandTimeoutSeconds),
		fmt.Sprintf("Sources: %d total", len(cfg.Sources)),
		fmt.Sprintf("Enabled: %d", tuiEnabledSourceCount(cfg)),
	}
	if saveMode {
		if m.saveResult != nil {
			summaryLines = append(summaryLines, "Saved: "+m.saveResult.Path, "State dir ensured: "+m.saveResult.StateDir)
		} else {
			summaryLines = append(summaryLines, "Press s or enter to write the canonical YAML file.")
		}
	}
	validationLines := []string{"Config valid. Ready to save."}
	if len(m.validationProblems) > 0 {
		validationLines = append([]string{"Config invalid. Fix these issues before saving:"}, m.validationProblems...)
	}
	if m.saveErr != nil {
		validationLines = append(validationLines, "Save error: "+m.saveErr.Error())
	}
	yamlPreview, err := config.MarshalCanonical(cfg)
	yamlLines := []string{"preview unavailable"}
	if err == nil {
		yamlLines = strings.Split(strings.TrimSpace(string(yamlPreview)), "\n")
	}
	if layout.Compact && !m.previewVisible {
		yamlLines = []string{"preview hidden", "press p to toggle canonical YAML"}
	}
	sections := []string{
		renderPlanSection("Summary", summaryLines, totalWidth),
		renderPlanSection("Validation", tuiWrapLines(validationLines, totalWidth-4), totalWidth),
	}
	if !layout.Compact {
		previewWidth := totalWidth / 2
		left := lipgloss.JoinVertical(lipgloss.Left, sections...)
		right := renderPlanSection("Canonical YAML", yamlLines, previewWidth)
		return lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
	}
	sections = append(sections, renderPlanSection("Canonical YAML", yamlLines, totalWidth))
	return strings.Join(sections, "\n")
}

func (m tuiConfigEditorModel) sourceListLines() []string {
	lines := []string{
		fmt.Sprintf("pane=%s", m.sourcePane),
		"a: add  d: duplicate  D: delete  [/]: reorder  space: enable",
	}
	if len(m.sources) == 0 {
		lines = append(lines, "(no sources)", "Press a to add a source.")
		return lines
	}
	for idx, source := range m.sources {
		prefix := " "
		if idx == m.sourceListCursor {
			prefix = ">"
		}
		enabled := "on"
		if !source.Source.Enabled {
			enabled = "off"
		}
		lines = append(lines, fmt.Sprintf("%s %s  %s/%s  %s", prefix, source.Source.ID, source.Source.Type, source.Source.Adapter.Kind, enabled))
	}
	return lines
}

func (m tuiConfigEditorModel) sourceFormLines() []string {
	fields := m.currentSourceFields()
	lines := []string{
		fmt.Sprintf("pane=%s", m.sourcePane),
		"tab: switch pane  enter: edit/apply",
	}
	if len(fields) == 0 {
		return append(lines, "No source selected.")
	}
	for idx, field := range fields {
		lines = append(lines, m.renderCursorLine(m.sourcePane == tuiConfigEditorPaneForm && idx == m.sourceFieldCursor, field.Label, field.Value, field.ReadOnly))
	}
	if m.edit != nil {
		lines = append(lines, "", fmt.Sprintf("editing %s = %q", m.edit.Title, m.edit.Buffer))
	}
	if problems := m.sourceValidationProblems(); len(problems) > 0 {
		lines = append(lines, "", "current validation:")
		lines = append(lines, problems...)
	}
	return lines
}

func (m tuiConfigEditorModel) renderCursorLine(active bool, label, value string, readOnly bool) string {
	prefix := "  "
	if active {
		prefix = "> "
	}
	if strings.TrimSpace(value) == "" {
		value = "(empty)"
	}
	if readOnly {
		value += " [read-only]"
	}
	return fmt.Sprintf("%s%-32s %s", prefix, ansi.Truncate(label, 32, ""), value)
}
