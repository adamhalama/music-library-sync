package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/jaa/update-downloads/internal/config"
)

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
		DenseBody:        true,
		StyledBody:       true,
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
		return nil
	}
	shortcuts := []tuiShortcut{
		{Key: "j/k", Label: "move"},
		{Key: "space", Label: "toggle source"},
		{Key: "d", Label: "dry-run"},
		{Key: "t", Label: "timeout"},
		{Key: "p", Label: "activity"},
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
		filterPhase := m.interactiveFilterPhase()
		if state != nil {
			state.syncFilterForPhase(filterPhase)
		}
		if !m.interactiveRuntimeActive() && m.interactivePhase != tuiInteractivePhaseDone {
			willSync := 0
			newCount := 0
			knownGapCount := 0
			alreadyHaveCount := 0
			if state != nil {
				willSync = state.selectedCount()
				newCount = state.newCount()
				knownGapCount = state.knownGapCount()
				alreadyHaveCount = state.alreadyHaveCount()
			}
			stats = append(stats,
				tuiFooterStat{Label: "will sync", Value: fmt.Sprintf("%d", willSync), Tone: "info"},
				tuiFooterStat{Label: "new", Value: fmt.Sprintf("%d", newCount), Tone: "success"},
				tuiFooterStat{Label: "known gap", Value: fmt.Sprintf("%d", knownGapCount), Tone: "warning"},
				tuiFooterStat{Label: "already have", Value: fmt.Sprintf("%d", alreadyHaveCount), Tone: "muted"},
				tuiFooterStat{Label: "progress", Value: tuiRenderProgressBar(0, 10), Tone: "muted"},
			)
			return stats
		}
		inRunCount, completedCount, skippedCount, failedCount, progressPercent := m.interactiveAggregateCounts()
		progressLabel := tuiRenderProgressBar(progressPercent, 10)
		stats = append(stats,
			tuiFooterStat{Label: "in run", Value: fmt.Sprintf("%d", inRunCount), Tone: "info"},
			tuiFooterStat{Label: "completed", Value: fmt.Sprintf("%d", completedCount), Tone: "success"},
			tuiFooterStat{Label: "skipped", Value: fmt.Sprintf("%d", skippedCount), Tone: "muted"},
			tuiFooterStat{Label: "failed", Value: fmt.Sprintf("%d", failedCount), Tone: failureCountTone(failedCount)},
			tuiFooterStat{Label: "progress", Value: progressLabel, Tone: "info"},
		)
		if !m.runStartedAt.IsZero() {
			stats = append(stats, tuiFooterStat{Label: "elapsed", Value: m.elapsedLabel(), Tone: "muted"})
		}
		return stats
	}
	stats := []tuiFooterStat{{Label: "state", Value: m.runStateLabel(), Tone: m.runStateTone()}}
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
	progressPercent := 0.0
	if m.progress != nil {
		progressPercent = m.progress.GlobalProgressPercent()
	}
	progressLabel := tuiRenderProgressBar(progressPercent, 10)
	if !m.running && !m.done {
		stats = append(stats,
			tuiFooterStat{Label: "sources", Value: fmt.Sprintf("%d/%d", m.selectedSourceCount(), len(m.sources)), Tone: "info"},
			tuiFooterStat{Label: "progress", Value: progressLabel, Tone: "muted"},
		)
		return stats
	}
	done, skipped, failed := m.standardAggregateCounts()
	stats = append(stats,
		tuiFooterStat{Label: "done", Value: fmt.Sprintf("%d", done), Tone: "success"},
		tuiFooterStat{Label: "skipped", Value: fmt.Sprintf("%d", skipped), Tone: "muted"},
		tuiFooterStat{Label: "failed", Value: fmt.Sprintf("%d", failed), Tone: failureCountTone(failed)},
		tuiFooterStat{Label: "progress", Value: progressLabel, Tone: "info"},
	)
	if !m.runStartedAt.IsZero() {
		stats = append(stats, tuiFooterStat{Label: "elapsed", Value: m.elapsedLabel(), Tone: "muted"})
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
	filterPhase := tuiInteractivePhaseReview
	state.syncFilterForPhase(filterPhase)
	limitLabel := "unlimited"
	if state.details.PlanLimit > 0 {
		limitLabel = fmt.Sprintf("%d", state.details.PlanLimit)
	}
	modeLabel := "run"
	if state.details.DryRun {
		modeLabel = "dry-run"
	}
	state.ensureCursorVisible(filterPhase)
	lines := planPromptHeaderLines(state, modeLabel, limitLabel, layout)
	filteredRows := state.filteredRowsForPhase(filterPhase)
	if len(filteredRows) == 0 {
		lines = append(lines, "No tracks found in selected preflight window.")
	} else {
		lines = append(lines, strings.Split(renderPlanPromptTable(state, filterPhase, layout), "\n")...)
	}
	lines = append(lines, fmt.Sprintf("Will Sync: %d tracks  |  Showing: %d/%d", state.selectedCount(), len(filteredRows), len(state.rows)))
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
	controls := renderPlanPromptControls(state, layout)
	if layout.Height < 24 {
		lines := []string{
			infoBar,
			renderPlanPromptPathLine(
				fmt.Sprintf("target %s", filepath.Base(state.details.TargetDir)),
				fmt.Sprintf("state %s", filepath.Base(state.details.StateFile)),
			),
		}
		return append(lines, controls...)
	}
	lines := []string{
		infoBar,
		renderPlanPromptPathLine("target "+state.details.TargetDir, "state "+state.details.StateFile),
		renderPlanPromptPathLine("url "+state.details.URL, ""),
	}
	return append(lines, controls...)
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

func renderPlanPromptControls(state *tuiInteractiveSelectionState, layout tuiShellLayout) []string {
	state.syncFilterForPhase(tuiInteractivePhaseReview)
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
	return renderPlanPromptControlLines(parts, layout)
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
	return ""
}

func (m tuiSyncModel) shellBody(layout tuiShellLayout) string {
	if m.isInteractiveSyncWorkflow() {
		return m.interactiveSyncBody(layout)
	}
	return m.standardSyncBody(layout)
}

func (m tuiSyncModel) standardSyncBody(layout tuiShellLayout) string {
	width := shellMainSectionWidth(layout, newTUIShellTheme()) - 4
	if width < 36 {
		width = shellMainSectionWidth(layout, newTUIShellTheme())
	}
	if !m.cfgLoaded {
		return strings.Join([]string{
			renderPlanSection("Selection", []string{"Loading config..."}, width),
			renderPlanSection("Run", []string{"Waiting for config before runtime state can render."}, width),
			renderPlanSection("Activity", []string{"no activity yet"}, width),
		}, "\n")
	}
	if m.cfgErr != nil {
		return strings.Join([]string{
			renderPlanSection("Selection", []string{fmt.Sprintf("Config load failed: %v", m.cfgErr)}, width),
			renderPlanSection("Run", []string{"Runtime state unavailable while config is invalid."}, width),
			renderPlanSection("Activity", []string{"no activity yet"}, width),
		}, "\n")
	}
	sections := []string{
		renderPlanSection("Selection", m.standardSelectionLines(), width),
		renderPlanSection("Run", m.standardRunLines(layout), width),
		renderPlanSection("Activity", m.standardActivityLines(layout), width),
	}
	return strings.Join(sections, "\n")
}

func (m tuiSyncModel) standardSelectionLines() []string {
	lines := []string{fmt.Sprintf("Selected Sources: %d/%d", m.selectedSourceCount(), len(m.sources))}
	if len(m.sources) == 0 {
		lines = append(lines, "Focused: (no enabled sources)")
	} else {
		cursor := m.cursor
		if cursor < 0 || cursor >= len(m.sources) {
			cursor = 0
		}
		source := m.sources[cursor]
		lines = append(lines, fmt.Sprintf("Focused: %s (%s/%s)", source.ID, source.Type, source.Adapter.Kind))
	}
	lines = append(lines,
		fmt.Sprintf("Options: dry_run=%t  ask_on_existing=%s  scan_gaps=%t  no_preflight=%t  timeout=%s", m.dryRun, formatAskOnExisting(m.askOnExistingSet), m.scanGaps, m.noPreflight, formatTimeoutOverride(m.timeoutOverride)),
	)
	if m.timeoutInputActive {
		lines = append(lines, fmt.Sprintf("timeout_input=%q  enter apply  esc cancel", m.timeoutInput))
		if m.timeoutInputErr != "" {
			lines = append(lines, "input_error: "+m.timeoutInputErr)
		}
	}
	if m.validationErr != "" {
		lines = append(lines, "validation_error: "+m.validationErr)
	}
	return lines
}

func (m tuiSyncModel) standardRunLines(layout tuiShellLayout) []string {
	lines := []string{m.standardCurrentSourceHeadline()}
	if progressLines := m.progressLines(); len(progressLines) > 0 {
		lines = append(lines, "")
		lines = append(lines, progressLines...)
	}
	summaries := m.standardSourceSummaryRows()
	if len(summaries) == 0 {
		lines = append(lines, "", "No source activity yet.")
		return lines
	}
	lines = append(lines, "")
	if layout.Compact {
		lines = append(lines, m.renderStandardSyncCompactSummaryRows(summaries)...)
		return lines
	}
	lines = append(lines, strings.Split(m.renderStandardSyncSummaryTable(summaries, layout), "\n")...)
	return lines
}

func (m tuiSyncModel) standardCurrentSourceHeadline() string {
	sourceID := m.standardCurrentSourceID()
	if sourceID == "" {
		switch {
		case m.running:
			return "Current Source: waiting for source events"
		case m.done:
			return "Current Source: run complete"
		default:
			return "Current Source: ready"
		}
	}
	if summary, ok := m.standardSummaries[sourceID]; ok && summary != nil {
		return fmt.Sprintf("Current Source: %s (%s)", sourceID, m.standardLifecycleLabel(summary.Lifecycle))
	}
	return "Current Source: " + sourceID
}

func (m tuiSyncModel) renderStandardSyncSummaryTable(summaries []*tuiStandardSyncSourceSummary, layout tuiShellLayout) string {
	theme := newTUIShellTheme()
	bodyStyle := theme.bodyPanel.Padding(0, 1)
	width := shellMainSectionWidth(layout, theme) - bodyStyle.GetHorizontalFrameSize() - 4
	if width < 72 {
		width = 72
	}
	sourceWidth := 18
	stateWidth := 11
	planWidth := 6
	doneWidth := 6
	skipWidth := 6
	failWidth := 6
	gapWidth := 14
	trackWidth := 16
	outcomeWidth := width - sourceWidth - stateWidth - planWidth - doneWidth - skipWidth - failWidth - trackWidth - gapWidth
	if outcomeWidth < 12 {
		outcomeWidth = 12
		trackWidth = width - sourceWidth - stateWidth - planWidth - doneWidth - skipWidth - failWidth - outcomeWidth - gapWidth
		if trackWidth < 12 {
			trackWidth = 12
		}
	}
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245"))
	header := strings.Join([]string{
		headerStyle.Width(sourceWidth).Render("SOURCE"),
		headerStyle.Width(stateWidth).Render("STATE"),
		headerStyle.Width(planWidth).Render("PLAN"),
		headerStyle.Width(doneWidth).Render("DONE"),
		headerStyle.Width(skipWidth).Render("SKIP"),
		headerStyle.Width(failWidth).Render("FAIL"),
		headerStyle.Width(trackWidth).Render("TRACK"),
		headerStyle.Width(outcomeWidth).Render("OUTCOME"),
	}, "  ")
	header = lipgloss.NewStyle().Background(lipgloss.Color("237")).Padding(0, 1).Render(header)
	lines := []string{header, lipgloss.NewStyle().Foreground(lipgloss.Color("239")).Render(strings.Repeat("─", maxInt(16, width)))}
	for _, summary := range summaries {
		lines = append(lines, m.renderStandardSyncSummaryRow(summary, sourceWidth, stateWidth, planWidth, doneWidth, skipWidth, failWidth, trackWidth, outcomeWidth))
	}
	return strings.Join(lines, "\n")
}

func (m tuiSyncModel) renderStandardSyncSummaryRow(summary *tuiStandardSyncSourceSummary, sourceWidth, stateWidth, planWidth, doneWidth, skipWidth, failWidth, trackWidth, outcomeWidth int) string {
	if summary == nil {
		return ""
	}
	prefix := " "
	if strings.TrimSpace(summary.SourceID) == m.standardCurrentSourceID() && m.running {
		prefix = ">"
	}
	sourceStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	stateStyle := shellToneStyle(newTUIShellTheme(), m.standardLifecycleTone(summary.Lifecycle))
	track := summary.LastTrack
	if strings.TrimSpace(track) == "" {
		track = "-"
	}
	outcome := summary.LastOutcome
	if strings.TrimSpace(outcome) == "" {
		outcome = "-"
	}
	line := strings.Join([]string{
		sourceStyle.Width(sourceWidth).Render(ansi.Truncate(prefix+summary.SourceID, sourceWidth, "")),
		stateStyle.Width(stateWidth).Render(m.standardLifecycleLabel(summary.Lifecycle)),
		lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(planWidth).Render(m.standardPlannedLabel(summary.Planned)),
		lipgloss.NewStyle().Foreground(lipgloss.Color("78")).Width(doneWidth).Render(fmt.Sprintf("%d", summary.Done)),
		lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(skipWidth).Render(fmt.Sprintf("%d", summary.Skipped)),
		lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Width(failWidth).Render(fmt.Sprintf("%d", summary.Failed)),
		lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Width(trackWidth).Render(ansi.Truncate(track, trackWidth, "")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(outcomeWidth).Render(ansi.Truncate(outcome, outcomeWidth, "")),
	}, "  ")
	if prefix == ">" {
		return lipgloss.NewStyle().Background(lipgloss.Color("236")).Render(line)
	}
	return line
}

func (m tuiSyncModel) renderStandardSyncCompactSummaryRows(summaries []*tuiStandardSyncSourceSummary) []string {
	lines := []string{"SRC  STATE  PLAN  DONE  SKIP  FAIL"}
	for _, summary := range summaries {
		if summary == nil {
			continue
		}
		line := fmt.Sprintf("%s%s  %s  %s  %d  %d  %d",
			m.standardCompactSourcePrefix(summary.SourceID),
			summary.SourceID,
			m.standardLifecycleLabel(summary.Lifecycle),
			m.standardPlannedLabel(summary.Planned),
			summary.Done,
			summary.Skipped,
			summary.Failed,
		)
		lines = append(lines, line)
	}
	return lines
}

func (m tuiSyncModel) standardCompactSourcePrefix(sourceID string) string {
	if strings.TrimSpace(sourceID) == m.standardCurrentSourceID() && m.running {
		return ">"
	}
	return " "
}

func (m tuiSyncModel) standardActivityLines(layout tuiShellLayout) []string {
	if m.standardActivityCollapsedFor(layout) {
		return []string{"collapsed", "press p to expand"}
	}
	lines := []string{"p: collapse"}
	if len(m.events) == 0 {
		lines = append(lines, "no activity yet")
	} else {
		limit := 10
		if layout.Compact {
			limit = 6
		}
		start := 0
		if len(m.events) > limit {
			start = len(m.events) - limit
		}
		lines = append(lines, m.events[start:]...)
	}
	if failureLines := m.lastFailureLines(); len(failureLines) > 0 {
		lines = append(lines, "", "last failure:")
		lines = append(lines, failureLines...)
	}
	return lines
}

func (m tuiSyncModel) standardLifecycleLabel(lifecycle tuiStandardSyncSourceLifecycle) string {
	switch lifecycle {
	case tuiStandardSyncSourcePreflight:
		return "preflight"
	case tuiStandardSyncSourceRunning:
		return "running"
	case tuiStandardSyncSourceFinished:
		return "done"
	case tuiStandardSyncSourceFailed:
		return "failed"
	default:
		return "idle"
	}
}

func (m tuiSyncModel) standardLifecycleTone(lifecycle tuiStandardSyncSourceLifecycle) string {
	switch lifecycle {
	case tuiStandardSyncSourcePreflight, tuiStandardSyncSourceRunning:
		return "warning"
	case tuiStandardSyncSourceFinished:
		return "success"
	case tuiStandardSyncSourceFailed:
		return "danger"
	default:
		return "muted"
	}
}

func (m tuiSyncModel) standardPlannedLabel(planned int) string {
	if planned < 0 {
		return "-"
	}
	return fmt.Sprintf("%d", planned)
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
	displayState := m.interactiveSelectionDisplayState(state)
	if displayState != nil {
		lines = append(lines, m.interactiveSelectionContextLines(displayState, layout)...)
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
	if len(lines) == 0 {
		lines = append(lines, "No enabled sources available.")
	}
	return lines
}

func (m tuiSyncModel) interactiveSelectionDisplayState(state *tuiInteractiveSelectionState) *tuiInteractiveSelectionState {
	if state != nil && len(state.rows) > 0 {
		if state.details.SourceID == "" {
			if source, ok := m.interactiveSourceByID(state.sourceID); ok {
				state.details = m.planSourceDetailsForSource(source)
			}
		}
		return state
	}
	sourceID := m.currentInteractiveDisplaySourceID()
	source, ok := m.interactiveSourceByID(sourceID)
	if !ok {
		source, ok = m.focusedInteractiveSource()
	}
	if !ok {
		return nil
	}
	base := state
	if base == nil {
		base = newEmptyTUIInteractiveSelectionState()
	}
	display := *base
	display.sourceID = source.ID
	display.details = m.planSourceDetailsForSource(source)
	return &display
}

func (m tuiSyncModel) focusedInteractiveSource() (config.Source, bool) {
	if len(m.sources) == 0 {
		return config.Source{}, false
	}
	cursor := m.cursor
	if cursor < 0 || cursor >= len(m.sources) {
		cursor = 0
	}
	return m.sources[cursor], true
}

func (m tuiSyncModel) planSourceDetailsForSource(source config.Source) planSourceDetails {
	return planSourceDetails{
		SourceID:   source.ID,
		SourceType: string(source.Type),
		Adapter:    source.Adapter.Kind,
		URL:        source.URL,
		TargetDir:  source.TargetDir,
		StateFile:  source.StateFile,
		PlanLimit:  m.planLimit,
		DryRun:     m.dryRun,
	}
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
	lines = append(lines, m.renderInteractiveSelectionControls(state, layout)...)
	return lines
}

func (m tuiSyncModel) renderInteractiveSelectionControls(state *tuiInteractiveSelectionState, layout tuiShellLayout) []string {
	if m.planPrompt != nil {
		return renderPlanPromptControls(state, layout)
	}
	if state == nil || len(state.rows) == 0 {
		return renderInteractiveIdleControls(layout)
	}
	filterPhase := m.interactiveFilterPhase()
	state.syncFilterForPhase(filterPhase)
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
		if m.isInteractiveSyncWorkflow() && !m.interactiveRuntimeActive() {
			return renderPlanPromptControlLines(parts, layout)
		}
		parts = append(parts, renderPlanPromptKey("x", "cancel run"))
	}
	return renderPlanPromptControlLines(parts, layout)
}

func renderInteractiveIdleControls(layout tuiShellLayout) []string {
	parts := []string{
		planPromptChip("source controls", "info"),
		renderPlanPromptKey("j/k", "move"),
		renderPlanPromptKey("space", "toggle source"),
		renderPlanPromptKey("d", "dry-run"),
		renderPlanPromptKey("t", "timeout"),
		renderPlanPromptKey("[/]", "plan limit"),
		renderPlanPromptKey("l", "type limit"),
		renderPlanPromptKey("u", "unlimited"),
		renderPlanPromptKey("p", "activity"),
		renderPlanPromptKey("enter", "run"),
	}
	return renderPlanPromptControlLines(parts, layout)
}

func renderPlanPromptControlLines(parts []string, layout tuiShellLayout) []string {
	style := lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Padding(0, 1)
	maxWidth := shellMainSectionWidth(layout, newTUIShellTheme()) - 10
	if maxWidth < 40 {
		maxWidth = 40
	}
	lines := []string{}
	current := ""
	for _, part := range parts {
		candidate := part
		if current != "" {
			candidate = current + "  " + part
		}
		if current != "" && lipgloss.Width(candidate) > maxWidth {
			lines = append(lines, style.Render(current))
			current = part
			continue
		}
		current = candidate
	}
	if current != "" {
		lines = append(lines, style.Render(current))
	}
	return lines
}

func (m tuiSyncModel) interactiveTrackLines(state *tuiInteractiveSelectionState, layout tuiShellLayout) []string {
	if state == nil || len(state.rows) == 0 {
		lines := []string{
			"SEL  #  STATUS  TRACK  ID",
		}
		switch {
		case m.interactivePhase == tuiInteractivePhasePreflight:
			lines = append(lines, "Preflight running... loading tracks for selection.")
		case m.interactivePhase == tuiInteractivePhaseReview:
			lines = append(lines, "Preflight complete. Review the plan to continue.")
		case m.done:
			if m.runErr != nil {
				lines = append(lines, "Preflight failed before track rows were loaded.")
			} else {
				lines = append(lines, "No track rows were returned. Source is up to date or no downloads were planned.")
			}
		default:
			lines = append(lines, renderInteractiveTrackLaunchHint())
		}
		return lines
	}
	filterPhase := m.interactiveFilterPhase()
	state.syncFilterForPhase(filterPhase)
	state.ensureCursorVisible(filterPhase)
	filteredRows := state.filteredRowsForPhase(filterPhase)
	lines := []string{}
	if len(filteredRows) == 0 {
		lines = append(lines, "No tracks match the current filter.")
	} else {
		lines = append(lines, strings.Split(renderPlanPromptTable(state, filterPhase, layout), "\n")...)
	}
	if tuiInteractiveRuntimePhase(filterPhase) {
		lines = append(lines, fmt.Sprintf("In Run: %d tracks  |  Showing %d/%d rows", state.runtimeSelectedCount(), len(filteredRows), len(state.rows)))
	} else {
		lines = append(lines, fmt.Sprintf("Will Sync: %d tracks  |  Showing %d/%d rows", state.selectedCount(), len(filteredRows), len(state.rows)))
	}
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

func renderInteractiveTrackLaunchHint() string {
	return lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Padding(0, 1).
		Render(planPromptChip("press", "info") + "  " + renderPlanPromptKey("enter", "to start preflight"))
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

func renderPlanPromptTable(state *tuiInteractiveSelectionState, phase tuiInteractiveSyncPhase, layout tuiShellLayout) string {
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

	state.syncFilterForPhase(phase)
	filtered := state.filteredRowsForPhase(phase)
	visibleIndices := state.visibleRowIndicesForPhase(phase)
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
		lines = append(lines, renderPlanPromptRow(row, phase, isCursor, selectWidth, indexWidth, statusWidth, titleWidth, idWidth))
	}
	return strings.Join(lines, "\n")
}

func renderPlanPromptRow(row tuiTrackRowState, phase tuiInteractiveSyncPhase, isCursor bool, selectWidth, indexWidth, statusWidth, titleWidth, idWidth int) string {
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
	statusCell := renderTrackStatusCell(row, phase, statusWidth)
	title := strings.TrimSpace(row.Title)
	if title == "" {
		title = "(untitled)"
	}
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	if row.PlanClass == tuiTrackPlanClassAlreadyHave || (tuiInteractiveRuntimePhase(phase) && row.RunScope != tuiTrackRunScopeIncluded) {
		titleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	}
	idStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	line := strings.Join([]string{
		selectTone.Width(selectWidth).Render(cursorPrefix + selectLabel),
		lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Width(indexWidth).Render(fmt.Sprintf("%d", row.Index)),
		statusCell,
		titleStyle.Width(titleWidth).Render(ansi.Truncate(title, titleWidth, "")),
		idStyle.Width(idWidth).Render(ansi.Truncate(row.RemoteID, idWidth, "")),
	}, "  ")
	if isCursor {
		return lipgloss.NewStyle().Background(lipgloss.Color("236")).Render(line)
	}
	return line
}

func renderTrackStatusCell(row tuiTrackRowState, phase tuiInteractiveSyncPhase, statusWidth int) string {
	primaryLabel, primaryStyle := planPromptStatusChip(row, phase, statusWidth)
	secondaryLabel := trackStatusSecondaryLabel(row, phase)
	if strings.TrimSpace(secondaryLabel) == "" {
		return primaryStyle.Width(statusWidth).Render(primaryLabel)
	}
	secondaryStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	if row.PlanClass == tuiTrackPlanClassAlreadyHave || row.RunScope != tuiTrackRunScopeIncluded {
		secondaryStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	}
	availableSecondaryWidth := statusWidth - lipgloss.Width(primaryLabel)
	if availableSecondaryWidth <= 0 {
		return primaryStyle.Width(statusWidth).Render(primaryLabel)
	}
	secondaryRendered := secondaryStyle.Render(ansi.Truncate(secondaryLabel, availableSecondaryWidth, ""))
	return lipgloss.NewStyle().Width(statusWidth).Render(primaryStyle.Render(primaryLabel) + secondaryRendered)
}

func planPromptStatusChip(row tuiTrackRowState, phase tuiInteractiveSyncPhase, statusWidth int) (string, lipgloss.Style) {
	if !tuiInteractiveRuntimePhase(phase) {
		switch row.PlanClass {
		case tuiTrackPlanClassAlreadyHave:
			return " have-it ", lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Background(lipgloss.Color("237"))
		case tuiTrackPlanClassKnownGap:
			return " known-gap ", lipgloss.NewStyle().Foreground(lipgloss.Color("179")).Background(lipgloss.Color("52")).Bold(true)
		default:
			return " new ", lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Background(lipgloss.Color("17")).Bold(true)
		}
	}
	if row.RunScope == tuiTrackRunScopeLocked && row.PlanClass == tuiTrackPlanClassAlreadyHave {
		return " have-it ", lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Background(lipgloss.Color("237"))
	}
	if row.RunScope == tuiTrackRunScopeExcluded {
		return " not-run ", lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Background(lipgloss.Color("238"))
	}
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
	default:
		return " pending ", lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Background(lipgloss.Color("17")).Bold(true)
	}
}

func trackStatusSecondaryLabel(row tuiTrackRowState, phase tuiInteractiveSyncPhase) string {
	if !tuiInteractiveRuntimePhase(phase) {
		return ""
	}
	switch row.PlanClass {
	case tuiTrackPlanClassNew:
		return " · new"
	case tuiTrackPlanClassKnownGap:
		return " · gap"
	default:
		return ""
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
		tone := m.sourceSidebarTone(source.ID)
		sourceItems = append(sourceItems, tuiSidebarItem{
			Label:  label,
			Meta:   meta,
			Active: idx == m.cursor,
			Tone:   tone,
		})
	}
	if len(sourceItems) == 0 {
		sourceItems = append(sourceItems, tuiSidebarItem{Label: "(no enabled sources)", Disabled: true})
	}
	sections = append(sections, tuiSidebarSection{Title: "sources", Items: sourceItems})
	if state := m.currentInteractiveSelection(); state != nil && len(state.rows) > 0 {
		filterPhase := m.interactiveFilterPhase()
		state.syncFilterForPhase(filterPhase)
		filters := state.filtersForPhase(filterPhase)
		filterItems := make([]tuiSidebarItem, 0, len(filters))
		for idx, filter := range filters {
			count := state.filterCount(filter, filterPhase)
			item := tuiSidebarItem{
				Label:  fmt.Sprintf("%s (%d)", state.filterDisplayLabel(filter), count),
				Active: state.focusFilters && idx == state.filterCursor,
			}
			if state.filter == filter {
				item.Tone = "info"
			}
			switch filter {
			case tuiTrackFilterMissingNew, tuiTrackFilterDownloaded:
				item.Tone = "success"
			case tuiTrackFilterKnownGap:
				item.Tone = "warning"
			case tuiTrackFilterAlreadyHave, tuiTrackFilterSkipped:
				item.Tone = "muted"
			case tuiTrackFilterFailed:
				item.Tone = "danger"
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

func (m tuiSyncModel) sourceSidebarTone(sourceID string) string {
	switch m.sourceLifecycle[sourceID] {
	case tuiSourceLifecycleFinished:
		return "success"
	case tuiSourceLifecycleFailed:
		return "danger"
	case tuiSourceLifecyclePreflight, tuiSourceLifecycleRunning:
		return "warning"
	default:
		return ""
	}
}

func (m tuiSyncModel) runStateLabel() string {
	if m.isInteractiveSyncWorkflow() {
		switch m.interactivePhase {
		case tuiInteractivePhasePreflight:
			return "preflight"
		case tuiInteractivePhaseReview:
			return "review"
		case tuiInteractivePhaseSyncing:
			return "running"
		case tuiInteractivePhaseDone:
			if m.runErr != nil || m.result.Failed > 0 {
				return "done-with-errors"
			}
			return "complete"
		default:
			return "ready"
		}
	}
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
	case "review":
		return "info"
	case "preflight":
		return "warning"
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

func (m tuiSyncModel) interactiveRuntimeActive() bool {
	return m.isInteractiveSyncWorkflow() && m.interactivePhase == tuiInteractivePhaseSyncing
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
