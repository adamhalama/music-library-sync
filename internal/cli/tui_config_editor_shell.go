package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/jaa/update-downloads/internal/config"
	"gopkg.in/yaml.v3"
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
	if model.modal != nil || model.edit != nil {
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
	} else if m.phase == tuiConfigEditorPhaseReview || m.phase == tuiConfigEditorPhaseSave {
		items := []tuiSidebarItem{{Label: "All sources", Meta: "full config", Active: m.reviewSourceCursor == 0, Tone: "info"}}
		for idx, source := range m.sources {
			items = append(items, tuiSidebarItem{
				Label:  source.Source.ID,
				Meta:   fmt.Sprintf("%s/%s", source.Source.Type, source.Source.Adapter.Kind),
				Active: m.reviewSourceCursor == idx+1,
				Tone:   m.sourceItemTone(source.Source),
			})
		}
		sections = append(sections, tuiSidebarSection{Title: "review", Items: items})
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
			{Key: "type", Label: "insert"},
			{Key: "←/→", Label: "move"},
			{Key: "home/end", Label: "bounds"},
			{Key: "del", Label: "delete"},
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
			tuiShortcut{Key: "r", Label: "review"},
			tuiShortcut{Key: "s", Label: "save"},
		)
	case tuiConfigEditorPhaseSources:
		shortcuts = append(shortcuts,
			tuiShortcut{Key: "j/k", Label: "move"},
			tuiShortcut{Key: "tab", Label: "switch pane"},
			tuiShortcut{Key: "a", Label: "add"},
			tuiShortcut{Key: "d", Label: "duplicate"},
			tuiShortcut{Key: "D", Label: "delete"},
			tuiShortcut{Key: "[/]", Label: "reorder"},
			tuiShortcut{Key: "r", Label: "review"},
			tuiShortcut{Key: "s", Label: "save"},
		)
	case tuiConfigEditorPhaseReview:
		shortcuts = append(shortcuts,
			tuiShortcut{Key: "j/k", Label: "scope"},
			tuiShortcut{Key: "enter", Label: "save step"},
			tuiShortcut{Key: "s", Label: "save"},
			tuiShortcut{Key: "p", Label: "toggle preview"},
		)
	case tuiConfigEditorPhaseSave:
		shortcuts = append(shortcuts,
			tuiShortcut{Key: "j/k", Label: "scope"},
			tuiShortcut{Key: "s", Label: "save"},
			tuiShortcut{Key: "p", Label: "toggle preview"},
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
	if m.edit != nil {
		cursorLine := tuiConfigEditorRenderInputValue(m.edit)
		lines := []string{
			"Field",
			m.edit.Title,
			"",
			"Value",
			cursorLine,
		}
		if len(m.edit.Help) > 0 {
			lines = append(lines, "", "Help")
			lines = append(lines, m.edit.Help...)
		}
		lines = append(lines, "", "type to edit  left/right move  home/end jump  backspace/delete remove  enter apply  esc cancel")
		return &tuiModalState{Title: "Edit Field", Lines: lines, Tone: "info"}
	}
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
	scopeTitle := "All sources"
	yamlLines := []string{"preview unavailable"}
	if preview, err := m.reviewPreviewYAML(); err == nil {
		yamlLines = preview
	}
	if m.reviewSourceCursor > 0 && m.reviewSourceCursor <= len(m.sources) {
		scopeTitle = m.sources[m.reviewSourceCursor-1].Source.ID
	}
	selectorLines := []string{m.reviewSelectorLine(totalWidth)}
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
	validationLines := []string{"No blocking issues. Press `s` to save or `esc` to return."}
	if len(m.validationProblems) > 0 {
		validationLines = append([]string{"Blocking issues prevent saving. Fix these fields first:"}, m.validationProblems...)
	}
	if m.saveErr != nil {
		validationLines = append(validationLines, "Save error: "+m.saveErr.Error())
	}
	if layout.Compact && !m.previewVisible {
		yamlLines = []string{"preview hidden", "press p to toggle canonical YAML"}
	}
	if layout.Compact {
		if !m.previewVisible {
			yamlLines = []string{"Preview hidden.", "Press `p` to show the YAML preview again."}
		}
		sections := []string{
			renderPlanSection("Summary", summaryLines, totalWidth),
			renderPlanSection("Validation", tuiWrapLines(validationLines, totalWidth-4), totalWidth),
			renderPlanSection("Preview Scope", selectorLines, totalWidth),
			renderPlanSection("Preview · "+scopeTitle, yamlLines, totalWidth),
		}
		return strings.Join(sections, "\n")
	}
	leftWidth := totalWidth / 2
	if leftWidth < 40 {
		leftWidth = 40
	}
	rightWidth := totalWidth - leftWidth - 2
	if rightWidth < 36 {
		rightWidth = 36
		leftWidth = totalWidth - rightWidth - 2
	}
	left := lipgloss.JoinVertical(
		lipgloss.Left,
		renderPlanSection("Summary", summaryLines, leftWidth),
		renderPlanSection("Validation", tuiWrapLines(validationLines, leftWidth-4), leftWidth),
		renderPlanSection("Next Action", m.reviewNextActionLines(saveMode), leftWidth),
	)
	if !m.previewVisible {
		left = lipgloss.JoinVertical(
			lipgloss.Left,
			left,
			renderPlanSection("Preview", []string{"Preview hidden.", "Press `p` to show the YAML preview again."}, leftWidth),
		)
		return left
	}
	right := lipgloss.JoinVertical(
		lipgloss.Left,
		renderPlanSection("Preview Scope", selectorLines, rightWidth),
		renderPlanSection("Preview · "+scopeTitle, yamlLines, rightWidth),
	)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
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
	helperLines := m.sourceFieldHelpLines(fields)
	if len(helperLines) > 0 {
		lines = append(lines, "", "field help:")
		lines = append(lines, helperLines...)
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

func tuiConfigEditorRenderInputValue(edit *tuiConfigEditorInlineEditState) string {
	if edit == nil {
		return ""
	}
	runes := []rune(edit.Buffer)
	cursor := edit.Cursor
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(runes) {
		cursor = len(runes)
	}
	left := string(runes[:cursor])
	right := string(runes[cursor:])
	if len(runes) == 0 && strings.TrimSpace(edit.Placeholder) != "" {
		right = edit.Placeholder
	}
	return left + "▌" + right
}

func (m tuiConfigEditorModel) sourceFieldHelpLines(fields []tuiConfigEditorFormField) []string {
	if m.sourcePane != tuiConfigEditorPaneForm || m.sourceFieldCursor < 0 || m.sourceFieldCursor >= len(fields) {
		return nil
	}
	field := fields[m.sourceFieldCursor]
	switch field.Key {
	case "source.adapter.scdl.managed":
		return []string{
			"UDL always manages: -f, --embed-thumbnail, --embed-metadata, --download-archive, and break/no-break behavior.",
			"Playlist item filtering is also managed at runtime when a selection is present.",
		}
	case "source.adapter.scdl.advanced_raw":
		return []string{
			"Add extra yt-dlp flags here. Keep optional common flags in the dedicated toggles above.",
			"Example: --concurrent-fragments 4",
		}
	case "source.adapter.scdl.direct_raw":
		return []string{
			"Add direct scdl flags here. UDL still injects -f and sync/archive wiring automatically.",
			"Example: --hide-progress --error",
		}
	case "source.adapter.min_version":
		state := m.currentSourceState()
		if state != nil && state.Source.Adapter.Kind == "scdl" {
			return []string{
				"Doctor compatibility hint only. UDL expects scdl >= 3.0.0 for --yt-dlp-args support.",
				"scdl v3 is a yt-dlp wrapper. Run `udl doctor` to verify your local binary.",
			}
		}
		return []string{"Doctor compatibility hint for this adapter. Use `udl doctor` to verify the installed tool version."}
	case "action.review":
		return []string{"Open the review screen from anywhere to inspect validation, preview YAML, or save."}
	default:
		if strings.HasPrefix(field.Key, "source.adapter.scdl.toggle.") {
			specKey := strings.TrimPrefix(field.Key, "source.adapter.scdl.toggle.")
			for _, spec := range tuiConfigEditorSCDLRecommendedArgSpecs {
				if spec.Key == specKey {
					return []string{spec.Description}
				}
			}
		}
	}
	return nil
}

func (m tuiConfigEditorModel) reviewSelectorLine(width int) string {
	segments := make([]string, 0, len(m.sources)+1)
	allStyle := newTUIShellTheme().sidebarItem
	if m.reviewSourceCursor == 0 {
		allStyle = newTUIShellTheme().sidebarActive
	}
	segments = append(segments, allStyle.Render("[All]"))
	for idx, source := range m.sources {
		style := newTUIShellTheme().sidebarItem
		if m.reviewSourceCursor == idx+1 {
			style = newTUIShellTheme().sidebarActive
		}
		segments = append(segments, style.Render(source.Source.ID))
	}
	return ansi.Truncate(strings.Join(segments, "  "), maxInt(24, width-4), "")
}

func (m tuiConfigEditorModel) reviewNextActionLines(saveMode bool) []string {
	if saveMode {
		if m.saveResult != nil {
			return []string{"Save complete.", "Press esc to return to editing."}
		}
		return []string{"Ready to save.", "enter or s: write config", "esc: return to editing"}
	}
	return []string{"s: save immediately", "enter: open save step", "esc: return to editing"}
}

func (m tuiConfigEditorModel) reviewPreviewYAML() ([]string, error) {
	if m.reviewSourceCursor <= 0 || m.reviewSourceCursor > len(m.sources) {
		payload, err := config.MarshalCanonical(m.buildConfig())
		if err != nil {
			return nil, err
		}
		return strings.Split(strings.TrimSpace(string(payload)), "\n"), nil
	}
	source := m.sources[m.reviewSourceCursor-1].Source
	payload, err := yaml.Marshal(source)
	if err != nil {
		return nil, err
	}
	lines := []string{"source:", strings.TrimRight(string(payload), "\n")}
	return strings.Split(strings.Join(lines, "\n"), "\n"), nil
}
