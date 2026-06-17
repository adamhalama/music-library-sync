package cli

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jaa/update-downloads/internal/freedl"
)

func buildFreeDLShellState(m tuiRootModel, layout tuiShellLayout) tuiShellState {
	model := m.freeDLModel
	state := tuiShellState{
		AppLabel:         "UDL",
		ScreenTitle:      "SoundCloud Free DL",
		SidebarSections:  workflowNavigationItems(m),
		Badges:           model.shellBadges(),
		CommandSummary:   model.shellCommandSummary(),
		Shortcuts:        model.shellShortcuts(),
		BodyTitle:        "SoundCloud Free DL",
		Body:             model.shellBody(layout),
		DenseBody:        true,
		StyledBody:       true,
		FooterStats:      model.shellFooterStats(),
		Banner:           model.shellBanner(),
		AllowBack:        m.canReturnToMenuOnEsc(),
		DebugMessageType: m.lastMsgTypeIfEnabled(),
	}
	if model.phase == tuiFreeDLPhaseConfirm {
		state.Modal = &tuiModalState{
			Title: "Confirm Promotion",
			Lines: model.confirmLines(),
			Tone:  "warning",
		}
	}
	if model.phase == tuiFreeDLPhaseConfig {
		if model.configEdit != nil {
			lines := append([]string{tuiConfigEditorRenderInputValue(model.configEdit)}, model.configEdit.Help...)
			state.Modal = &tuiModalState{Title: model.configEdit.Title, Lines: lines, Tone: "info"}
		} else if model.configDeleteConfirm {
			jobID := "(unknown)"
			if job := model.currentConfigJob(); job != nil {
				jobID = job.ID
			}
			state.Modal = &tuiModalState{Title: "Delete Free DL Job", Lines: []string{"Delete job " + jobID + " from the in-memory config?", "y/enter: delete  n/esc: cancel"}, Tone: "warning"}
		} else if model.configDiscardConfirm {
			state.Modal = &tuiModalState{Title: "Discard Free DL Config Changes", Lines: []string{"Discard unsaved Free DL config changes?", "y/enter: discard  n/esc: stay"}, Tone: "warning"}
		}
	}
	return state
}

func (m tuiFreeDLModel) shellBadges() []tuiBadge {
	tone := "info"
	switch m.phase {
	case tuiFreeDLPhaseDone:
		tone = "success"
	case tuiFreeDLPhaseFailed:
		tone = "danger"
	case tuiFreeDLPhasePlanning, tuiFreeDLPhaseCapturing, tuiFreeDLPhaseApplying, tuiFreeDLPhaseConfig:
		tone = "warning"
	}
	badges := []tuiBadge{{Label: "STATE: " + strings.ToUpper(string(m.phase)), Tone: tone}}
	if m.phase == tuiFreeDLPhaseConfig {
		badges = append(badges, tuiBadge{Label: "CONFIG: " + strings.ToUpper(string(m.configStep)), Tone: "info"})
		if m.configDirty {
			badges = append(badges, tuiBadge{Label: "UNSAVED", Tone: "warning"})
		}
	}
	if m.phase == tuiFreeDLPhaseSelect {
		badges = append(badges, tuiBadge{Label: "LIMIT: " + formatPlanLimit(m.planLimit), Tone: "info"})
	}
	if m.plan != nil {
		badges = append(badges, tuiBadge{Label: fmt.Sprintf("RUN: %s", m.plan.RunID), Tone: "muted"})
		badges = append(badges, tuiBadge{Label: fmt.Sprintf("SELECTED: %d", selectedCaptureCount(m.plan)), Tone: "info"})
	}
	if m.promoPlan != nil {
		badges = append(badges, tuiBadge{Label: "FORMAT: " + strings.ToUpper(m.promoPlan.TargetFormat), Tone: "info"})
	}
	return badges
}

func (m tuiFreeDLModel) shellCommandSummary() []string {
	parts := []string{"udl", "tui", "freedl", "phase=" + string(m.phase)}
	if job, ok := m.currentJob(); ok {
		parts = append(parts, "job="+job.ID)
	}
	return parts
}

func (m tuiFreeDLModel) shellShortcuts() []tuiShortcut {
	switch m.phase {
	case tuiFreeDLPhaseSelect:
		return []tuiShortcut{{Key: "j/k", Label: "move"}, {Key: "e", Label: "manage"}, {Key: "a", Label: "add job"}, {Key: "[/]", Label: "limit"}, {Key: "l", Label: "type limit"}, {Key: "u", Label: "unlimited"}, {Key: "enter", Label: "plan"}, {Key: "esc", Label: "back"}}
	case tuiFreeDLPhaseConfig:
		return []tuiShortcut{{Key: "j/k", Label: "move"}, {Key: "tab", Label: "pane"}, {Key: "a/d/D", Label: "jobs"}, {Key: "space", Label: "toggle"}, {Key: "r", Label: "review"}, {Key: "s", Label: "save"}, {Key: "esc", Label: "back"}}
	case tuiFreeDLPhasePlan:
		return []tuiShortcut{{Key: "j/k", Label: "move"}, {Key: "space", Label: "toggle"}, {Key: "c/enter", Label: "capture"}, {Key: "r", Label: "replan"}, {Key: "esc", Label: "back"}}
	case tuiFreeDLPhaseCapturing, tuiFreeDLPhasePlanning, tuiFreeDLPhaseApplying:
		return []tuiShortcut{{Key: "x", Label: "cancel"}}
	case tuiFreeDLPhasePromote:
		return []tuiShortcut{{Key: "j/k", Label: "move"}, {Key: "space", Label: "toggle"}, {Key: "t", Label: "format"}, {Key: "enter", Label: "confirm"}, {Key: "esc", Label: "back"}}
	case tuiFreeDLPhaseConfirm:
		return []tuiShortcut{{Key: "y/enter", Label: "promote"}, {Key: "n/esc", Label: "back"}}
	case tuiFreeDLPhaseDone, tuiFreeDLPhaseFailed:
		return []tuiShortcut{{Key: "r", Label: "replan"}, {Key: "esc", Label: "back"}}
	default:
		return []tuiShortcut{{Key: "esc", Label: "back"}}
	}
}

func (m tuiFreeDLModel) shellFooterStats() []tuiFooterStat {
	stats := []tuiFooterStat{{Label: "phase", Value: string(m.phase), Tone: "info"}, {Label: "jobs", Value: fmt.Sprintf("%d", len(m.jobs)), Tone: "info"}}
	if m.plan != nil {
		stats = append(stats, tuiFooterStat{Label: "capture", Value: fmt.Sprintf("%d/%d", selectedCaptureCount(m.plan), len(m.plan.Rows)), Tone: "info"})
	}
	if m.promoPlan != nil {
		stats = append(stats, tuiFooterStat{Label: "promote", Value: fmt.Sprintf("%d/%d", selectedPromotionCount(m.promoPlan), len(m.promoPlan.Rows)), Tone: "warning"})
	}
	if m.phase == tuiFreeDLPhaseConfig {
		stats = append(stats, tuiFooterStat{Label: "config jobs", Value: strconv.Itoa(len(m.configCfg.Jobs)), Tone: "info"})
	}
	return stats
}

func (m tuiFreeDLModel) shellBanner() *tuiBanner {
	if m.err != nil {
		return &tuiBanner{Text: m.err.Error(), Tone: "danger"}
	}
	if m.phase == tuiFreeDLPhaseConfig {
		if m.configErr != nil {
			return &tuiBanner{Text: m.configErr.Error(), Tone: "danger"}
		}
		if m.configSaveErr != nil {
			return &tuiBanner{Text: m.configSaveErr.Error(), Tone: "danger"}
		}
		if m.configProjectWarning != "" {
			return &tuiBanner{Text: m.configProjectWarning, Tone: "warning"}
		}
	}
	return nil
}

func (m tuiFreeDLModel) shellBody(layout tuiShellLayout) string {
	width := shellMainSectionWidth(layout, newTUIShellTheme()) - 4
	if width < 40 {
		width = shellMainSectionWidth(layout, newTUIShellTheme())
	}
	switch m.phase {
	case tuiFreeDLPhaseLoading:
		return renderPlanSection("Loading", []string{"Reading freedl.yaml and legacy scdl-freedl sources."}, width)
	case tuiFreeDLPhaseSelect:
		return renderPlanSection("Jobs", m.jobLines(width), width)
	case tuiFreeDLPhasePlanning:
		return strings.Join([]string{
			renderPlanSection("Planning", m.planningSummaryLines(), width),
			renderPlanSection("Tracks", m.planRowLines(width), width),
		}, "\n")
	case tuiFreeDLPhasePlan:
		return strings.Join([]string{
			renderPlanSection("Plan", m.planSummaryLines(), width),
			renderPlanSection("Tracks", m.planRowLines(width), width),
		}, "\n")
	case tuiFreeDLPhaseCapturing:
		return renderPlanSection("Capturing", []string{
			fmt.Sprintf("Downloading selected Free DL tracks into %s", shortPath(m.plan.BufferRoot)),
			"Library files are not touched during capture.",
		}, width)
	case tuiFreeDLPhasePromote:
		return strings.Join([]string{
			renderPlanSection("Promotion Plan", m.promotionSummaryLines(), width),
			renderPlanSection("Upgrades", m.promotionRowLines(width), width),
		}, "\n")
	case tuiFreeDLPhaseApplying:
		return renderPlanSection("Promoting", []string{
			"Copying each original into the configured backup folder before replacement.",
			"Rows whose backup fails are not promoted.",
		}, width)
	case tuiFreeDLPhaseConfig:
		return m.configBody(layout, width)
	case tuiFreeDLPhaseDone:
		return renderPlanSection("Done", m.doneLines(), width)
	case tuiFreeDLPhaseFailed:
		lines := []string{"SoundCloud Free DL workflow failed."}
		if m.err != nil {
			lines = append(lines, tuiSplitDetailLines(m.err.Error())...)
		}
		return renderPlanSection("Failed", lines, width)
	default:
		return renderPlanSection("SoundCloud Free DL", []string{"Unknown phase."}, width)
	}
}

func (m tuiFreeDLModel) jobLines(width int) []string {
	if len(m.jobs) == 0 {
		return []string{"No enabled Free DL jobs found.", "Press e to create or enable a Free DL job."}
	}
	lines := []string{
		"Plan limit: " + formatPlanLimit(m.planLimit),
		"Use [/] to adjust, u for unlimited, l to type a count, e to manage jobs.",
	}
	if m.limitEditing {
		lines = append(lines, fmt.Sprintf("limit_input=%q  enter apply  esc cancel", m.limitInput))
		if m.limitInputErr != "" {
			lines = append(lines, "input_error: "+m.limitInputErr)
		}
	}
	for idx, job := range m.jobs {
		prefix := "  "
		if idx == m.jobCursor {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s%s  limit=%d  library=%s", prefix, job.ID, job.PlanLimit, shortPath(job.LibraryDir)))
		lines = append(lines, fmt.Sprintf("   buffer=%s", shortPath(job.BufferDir)))
		lines = append(lines, fmt.Sprintf("   backup=%s", shortPath(job.BackupDir)))
	}
	return lines
}

func (m tuiFreeDLModel) configBody(layout tuiShellLayout, width int) string {
	switch m.configStep {
	case tuiFreeDLConfigStepReview, tuiFreeDLConfigStepSave:
		return m.configReviewBody(width)
	default:
		return m.configEditBody(layout, width)
	}
}

func (m tuiFreeDLModel) configEditBody(layout tuiShellLayout, width int) string {
	listLines := m.configListLines()
	formLines := m.configFormLines()
	if layout.Compact || width < 90 {
		return strings.Join([]string{
			renderPlanSection("Free DL Jobs", listLines, width),
			renderPlanSection("Job Editor", formLines, width),
		}, "\n")
	}
	listWidth := width / 3
	if listWidth < 30 {
		listWidth = 30
	}
	formWidth := width - listWidth - 2
	left := renderPlanSection("Free DL Jobs", listLines, listWidth)
	right := renderPlanSection("Job Editor", formLines, formWidth)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
}

func (m tuiFreeDLModel) configListLines() []string {
	lines := []string{
		"Target: " + firstNonEmpty(m.configPath, "(unresolved)"),
		fmt.Sprintf("pane=%s", m.configPane),
		"a: add  d: duplicate  D: delete  space: enable",
	}
	if m.configProjectWarning != "" {
		lines = append(lines, m.configProjectWarning)
	}
	if len(m.configCfg.Jobs) == 0 {
		return append(lines, "(no jobs)", "Press a to add a Free DL job.")
	}
	for idx, job := range m.configCfg.Jobs {
		prefix := " "
		if idx == m.configJobCursor {
			prefix = ">"
		}
		enabled := "off"
		if job.Enabled {
			enabled = "on"
		}
		lines = append(lines, fmt.Sprintf("%s %s  %s  %s", prefix, job.ID, enabled, shortPath(job.LibraryDir)))
	}
	return lines
}

func (m tuiFreeDLModel) configFormLines() []string {
	fields := m.configFields()
	lines := []string{
		fmt.Sprintf("pane=%s", m.configPane),
		"tab: switch pane  enter: edit/apply  space: toggle/select",
	}
	for idx, field := range fields {
		lines = append(lines, renderFreeDLCursorLine(m.configPane == tuiFreeDLConfigPaneForm && idx == m.configFieldCursor, field.Label, field.Value, field.ReadOnly))
	}
	if len(m.configValidation) > 0 {
		lines = append(lines, "", "validation:")
		lines = append(lines, m.configValidation...)
	}
	return lines
}

func (m tuiFreeDLModel) configReviewBody(width int) string {
	summary := []string{
		"Target: " + firstNonEmpty(m.configPath, "(unresolved)"),
		fmt.Sprintf("Jobs: %d total", len(m.configCfg.Jobs)),
		fmt.Sprintf("Enabled: %d", len(freedl.EnabledJobs(m.configCfg))),
	}
	if m.configSaved {
		summary = append(summary, "Saved: "+m.configPath)
	} else if m.configStep == tuiFreeDLConfigStepSave {
		summary = append(summary, "Press s or enter to write freedl.yaml.")
	}
	validation := []string{"No blocking issues. Press s to save or esc to return."}
	if len(m.configValidation) > 0 {
		validation = append([]string{"Blocking issues prevent saving:"}, m.configValidation...)
	}
	if m.configSaveErr != nil {
		validation = append(validation, "Save error: "+m.configSaveErr.Error())
	}
	return strings.Join([]string{
		renderPlanSection("Summary", summary, width),
		renderPlanSection("Validation", validation, width),
		renderPlanSection("Preview", m.configPreviewLines(), width),
	}, "\n")
}

func (m tuiFreeDLModel) configPreviewLines() []string {
	payload, err := freedl.MarshalCanonical(m.configCfg, m.mainConfig)
	if err != nil {
		return []string{"preview unavailable: " + err.Error()}
	}
	lines := strings.Split(strings.TrimRight(string(payload), "\n"), "\n")
	if len(lines) > 22 {
		lines = append(lines[:22], "... preview truncated ...")
	}
	return lines
}

func renderFreeDLCursorLine(active bool, label, value string, readOnly bool) string {
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
	return fmt.Sprintf("%s%-32s %s", prefix, truncateForWidth(label, 32), value)
}

func (m tuiFreeDLModel) planSummaryLines() []string {
	if m.plan == nil {
		return []string{"No plan."}
	}
	available := 0
	for _, row := range m.plan.Rows {
		if row.FreeDLProbe.Status == "available" {
			available++
		}
	}
	return []string{
		fmt.Sprintf("Rows: %d", len(m.plan.Rows)),
		fmt.Sprintf("Free DL available: %d", available),
		fmt.Sprintf("Selected for capture: %d", selectedCaptureCount(m.plan)),
		"Capture target: " + shortPath(m.plan.BufferRoot),
		"Logs: " + shortPath(m.plan.LogDir),
	}
}

func (m tuiFreeDLModel) planningSummaryLines() []string {
	lines := []string{
		"Rows appear as SoundCloud enumeration returns them.",
		"Capture unlocks after state, local quality, and Free DL probes finish.",
	}
	for _, stage := range []string{"playlist", "state_archive", "local_quality", "free_dl"} {
		status := m.planningStages[stage]
		if status == "" {
			status = "pending"
		}
		lines = append(lines, fmt.Sprintf("%s: %s", stage, status))
	}
	if m.plan != nil {
		lines = append(lines, fmt.Sprintf("Rows visible: %d", len(m.plan.Rows)))
		lines = append(lines, fmt.Sprintf("Selectable now: %d", selectableCaptureCount(m.plan)))
	}
	return lines
}

func (m tuiFreeDLModel) planRowLines(width int) []string {
	if m.plan == nil || len(m.plan.Rows) == 0 {
		return []string{"Waiting for SoundCloud rows."}
	}
	lines := []string{"SEL  #   LOCAL QUALITY        FREE DL             TITLE"}
	start, end := visibleWindow(m.rowCursor, len(m.plan.Rows), 14)
	for idx := start; idx < end; idx++ {
		row := m.plan.Rows[idx]
		cursor := " "
		if idx == m.rowCursor {
			cursor = ">"
		}
		sel := "[ ]"
		if row.Selected {
			sel = "[x]"
		}
		if !row.Selectable {
			sel = " - "
		}
		line := fmt.Sprintf("%s%s %-3d %-20s %-19s %s", cursor, sel, row.Index, truncateForWidth(qualityLabel(row.LocalQuality), 20), truncateForWidth(freeDLStatusLabel(row), 19), row.Title)
		lines = append(lines, truncateForWidth(line, width-4))
	}
	return lines
}

func (m tuiFreeDLModel) promotionSummaryLines() []string {
	if m.promoPlan == nil {
		return []string{"Building promotion plan from captured files."}
	}
	return []string{
		"Target format: " + m.promoPlan.TargetFormat,
		fmt.Sprintf("Matched upgrades: %d", len(m.promoPlan.Rows)),
		fmt.Sprintf("Selected for promotion: %d", selectedPromotionCount(m.promoPlan)),
		"Backups: " + shortPath(m.promoPlan.BackupRoot),
		"Logs: " + shortPath(m.promoPlan.LogDir),
	}
}

func (m tuiFreeDLModel) promotionRowLines(width int) []string {
	if m.promoPlan == nil || len(m.promoPlan.Rows) == 0 {
		return []string{"No captured files matched the library."}
	}
	lines := []string{"SEL  #   QUALITY CHANGE                  ACTION       TITLE"}
	start, end := visibleWindow(m.promoCursor, len(m.promoPlan.Rows), 14)
	for idx := start; idx < end; idx++ {
		row := m.promoPlan.Rows[idx]
		cursor := " "
		if idx == m.promoCursor {
			cursor = ">"
		}
		sel := "[ ]"
		if row.Selected {
			sel = "[x]"
		}
		if row.Action == freedl.PromotionSkip {
			sel = " - "
		}
		change := fmt.Sprintf("%s -> %s", qualityLabel(row.OriginalQuality), qualityLabel(row.SourceQuality))
		line := fmt.Sprintf("%s%s %-3d %-31s %-12s %s", cursor, sel, row.Index, truncateForWidth(change, 31), row.Action, row.Title)
		lines = append(lines, truncateForWidth(line, width-4))
		if idx == m.promoCursor {
			lines = append(lines, truncateForWidth("   backup "+shortPath(row.BackupPath), width-4))
		}
	}
	return lines
}

func (m tuiFreeDLModel) confirmLines() []string {
	count := selectedPromotionCount(m.promoPlan)
	lines := []string{
		fmt.Sprintf("Promote %d track(s)?", count),
		"Every selected original will be copied to backup before replacement.",
	}
	if m.promoPlan != nil {
		lines = append(lines, "Backup root: "+m.promoPlan.BackupRoot)
		lines = append(lines, "Log dir: "+m.promoPlan.LogDir)
	}
	lines = append(lines, "", "y/enter: promote  n/esc: review")
	return lines
}

func (m tuiFreeDLModel) doneLines() []string {
	if m.result == nil {
		return []string{"Workflow complete."}
	}
	lines := []string{
		fmt.Sprintf("Replaced: %d", m.result.Replaced),
		fmt.Sprintf("Skipped: %d", m.result.Skipped),
		fmt.Sprintf("Failed: %d", m.result.Failed),
	}
	if m.promoPlan != nil {
		lines = append(lines, "Backups: "+m.promoPlan.BackupRoot)
		lines = append(lines, "Logs: "+m.promoPlan.LogDir)
	}
	return lines
}

func (m tuiFreeDLModel) currentJob() (freedl.Job, bool) {
	if len(m.jobs) == 0 || m.jobCursor < 0 || m.jobCursor >= len(m.jobs) {
		return freedl.Job{}, false
	}
	return m.jobs[m.jobCursor], true
}

func visibleWindow(cursor, total, size int) (int, int) {
	if total <= size {
		return 0, total
	}
	start := cursor - size/2
	if start < 0 {
		start = 0
	}
	if start+size > total {
		start = total - size
	}
	return start, start + size
}

func shortPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	if home := "~"; strings.HasPrefix(path, home) {
		return path
	}
	return filepath.Clean(path)
}
