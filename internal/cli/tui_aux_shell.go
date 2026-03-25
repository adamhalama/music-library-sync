package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jaa/update-downloads/internal/doctor"
)

func (m tuiDoctorModel) shellBody(layout tuiShellLayout) string {
	width := tuiAuxSectionWidth(layout)
	if m.phase != tuiDoctorPhaseComplete {
		return renderPlanSection("Summary", []string{
			"Doctor checks are running.",
			"Inspecting dependencies, auth, and filesystem readiness.",
		}, width)
	}
	if m.setupErr != nil {
		sections := []string{
			renderPlanSection("Summary", []string{
				"Outcome: setup failed",
				"Doctor could not start because config loading or validation failed.",
			}, width),
			renderPlanSection("Details", tuiWrapLines(tuiSplitDetailLines(m.setupErr.Error()), width-4), width),
		}
		return strings.Join(sections, "\n")
	}
	sections := []string{
		renderPlanSection("Summary", []string{
			"Outcome: " + m.overallLabel(),
			fmt.Sprintf("Total Checks: %d", m.summary.Total),
			fmt.Sprintf("Errors: %d", m.summary.ErrorCount),
			fmt.Sprintf("Warnings: %d", m.summary.WarnCount),
			fmt.Sprintf("Infos: %d", m.summary.InfoCount),
		}, width),
		renderPlanSection("Checks", m.shellCheckLines(width), width),
	}
	if nextLines := m.shellNextStepLines(); len(nextLines) > 0 {
		sections = append(sections, renderPlanSection("Next Step", nextLines, width))
	}
	return strings.Join(sections, "\n")
}

func (m tuiDoctorModel) shellCheckLines(width int) []string {
	if len(m.checks) == 0 {
		return []string{"No checks reported."}
	}
	lines := make([]string, 0, len(m.checks))
	for _, check := range m.checks {
		lines = append(lines, tuiDoctorCheckLine(check))
	}
	return lines
}

func (m tuiDoctorModel) shellNextStepLines() []string {
	if hasDoctorCheckContaining(m.checks, "no sources configured yet") {
		return []string{"Choose `Get Started` in the TUI to create your first source before syncing."}
	}
	if kind := m.recommendedCredentialKind(); kind != "" {
		return []string{"Press `c` to open Credentials and repair the missing or stale auth entry, then rerun Check System."}
	}
	switch {
	case m.summary.ErrorCount > 0:
		return []string{"Fix blocking dependency, auth, or filesystem issues before running sync."}
	case m.summary.WarnCount > 0:
		return []string{"Review warnings before syncing to avoid degraded or risky runs."}
	default:
		return nil
	}
}

func hasDoctorCheckContaining(checks []doctor.Check, fragment string) bool {
	needle := strings.ToLower(strings.TrimSpace(fragment))
	if needle == "" {
		return false
	}
	for _, check := range checks {
		if strings.Contains(strings.ToLower(check.Message), needle) {
			return true
		}
	}
	return false
}

func (m tuiDoctorModel) overallLabel() string {
	switch {
	case m.summary.ErrorCount > 0:
		return "blocked"
	case m.summary.WarnCount > 0:
		return "warnings present"
	default:
		return "ready"
	}
}

func (m tuiDoctorModel) shellBadges() []tuiBadge {
	if m.phase != tuiDoctorPhaseComplete {
		return []tuiBadge{{Label: "RUNNING", Tone: "warning"}}
	}
	if m.setupErr != nil {
		return []tuiBadge{{Label: "FAILED", Tone: "danger"}}
	}
	return []tuiBadge{{Label: "COMPLETE", Tone: "success"}}
}

func (m tuiDoctorModel) shellFooterStats() []tuiFooterStat {
	if m.phase != tuiDoctorPhaseComplete {
		return []tuiFooterStat{{Label: "state", Value: "running", Tone: "warning"}}
	}
	if m.setupErr != nil {
		return []tuiFooterStat{{Label: "state", Value: "failed", Tone: "danger"}}
	}
	return []tuiFooterStat{
		{Label: "state", Value: "complete", Tone: "success"},
		{Label: "errors", Value: fmt.Sprintf("%d", m.summary.ErrorCount), Tone: failureCountTone(m.summary.ErrorCount)},
		{Label: "warnings", Value: fmt.Sprintf("%d", m.summary.WarnCount), Tone: warningCountTone(m.summary.WarnCount)},
		{Label: "checks", Value: fmt.Sprintf("%d", m.summary.Total), Tone: "info"},
	}
}

func (m tuiValidateModel) shellBody(layout tuiShellLayout) string {
	width := tuiAuxSectionWidth(layout)
	if m.phase != tuiValidatePhaseComplete {
		return renderPlanSection("Status", []string{
			"Validation is running.",
			"Checking config schema and source definitions.",
		}, width)
	}
	statusLines := []string{"Validation failed"}
	if m.result.Valid {
		statusLines[0] = "Config valid"
	} else if m.result.FailureKind == tuiValidateFailureLoad {
		statusLines[0] = "Config load failed"
	}
	contextLines := []string{"Config: " + m.result.ConfigContextLabel}
	if m.result.ConfigLoaded {
		contextLines = append(contextLines,
			fmt.Sprintf("Sources: %d total", m.result.SourceCount),
			fmt.Sprintf("Enabled: %d", m.result.EnabledSourceCount),
		)
	} else {
		contextLines = append(contextLines, "Sources: unavailable because config did not load")
	}
	detailLines := m.result.DetailLines
	if len(detailLines) == 0 {
		detailLines = []string{"No further details."}
	}
	sections := []string{
		renderPlanSection("Status", statusLines, width),
		renderPlanSection("Context", tuiWrapLines(contextLines, width-4), width),
		renderPlanSection("Details", tuiWrapLines(detailLines, width-4), width),
	}
	return strings.Join(sections, "\n")
}

func (m tuiValidateModel) shellBadges() []tuiBadge {
	if m.phase != tuiValidatePhaseComplete {
		return []tuiBadge{{Label: "RUNNING", Tone: "warning"}}
	}
	if m.result.Valid {
		return []tuiBadge{{Label: "VALID", Tone: "success"}}
	}
	return []tuiBadge{{Label: "INVALID", Tone: "danger"}}
}

func (m tuiValidateModel) shellFooterStats() []tuiFooterStat {
	if m.phase != tuiValidatePhaseComplete {
		return []tuiFooterStat{{Label: "state", Value: "running", Tone: "warning"}}
	}
	stats := []tuiFooterStat{}
	if m.result.Valid {
		stats = append(stats, tuiFooterStat{Label: "state", Value: "valid", Tone: "success"})
	} else {
		stats = append(stats, tuiFooterStat{Label: "state", Value: "invalid", Tone: "danger"})
	}
	if m.result.ConfigLoaded {
		stats = append(stats,
			tuiFooterStat{Label: "sources", Value: fmt.Sprintf("%d", m.result.SourceCount), Tone: "info"},
			tuiFooterStat{Label: "enabled", Value: fmt.Sprintf("%d", m.result.EnabledSourceCount), Tone: "info"},
		)
	}
	return stats
}

func (m tuiInitModel) shellBody(layout tuiShellLayout) string {
	width := tuiAuxSectionWidth(layout)
	switch m.phase {
	case tuiInitPhaseRunning:
		status := []string{
			"Initializing starter config and state directory.",
			"Waiting for overwrite confirmation if the target config already exists.",
		}
		if !m.intro.ConfigExists {
			status[1] = "Writing starter config and ensuring state directory."
		}
		return strings.Join([]string{
			renderPlanSection("Status", status, width),
			renderPlanSection("Paths", tuiInitPathLines(m.intro.ConfigPath, m.intro.StateDir), width),
		}, "\n")
	case tuiInitPhaseDone:
		return strings.Join([]string{
			renderPlanSection("Result", append([]string{"Initialization complete."}, m.result.DetailLines...), width),
			renderPlanSection("Paths", tuiInitPathLines(m.result.ConfigPath, m.result.StateDir), width),
			renderPlanSection("Next Step", []string{"Review the generated config and run `udl validate` before your first sync."}, width),
		}, "\n")
	case tuiInitPhaseCanceled:
		return strings.Join([]string{
			renderPlanSection("Result", append([]string{"Initialization canceled."}, m.result.DetailLines...), width),
			renderPlanSection("Paths", tuiInitPathLines(m.result.ConfigPath, m.result.StateDir), width),
		}, "\n")
	case tuiInitPhaseFailed:
		return strings.Join([]string{
			renderPlanSection("Result", []string{"Initialization failed."}, width),
			renderPlanSection("Paths", tuiInitPathLines(m.result.ConfigPath, m.result.StateDir), width),
			renderPlanSection("Details", tuiWrapLines(m.result.DetailLines, width-4), width),
		}, "\n")
	default:
		if m.intro.PrepareErr != nil {
			return strings.Join([]string{
				renderPlanSection("Plan", []string{"Unable to prepare init workflow."}, width),
				renderPlanSection("Details", tuiWrapLines(tuiSplitDetailLines(m.intro.PrepareErr.Error()), width-4), width),
			}, "\n")
		}
		overwriteLine := "Overwrite: no existing config detected"
		if m.intro.ConfigExists {
			overwriteLine = "Overwrite: existing config detected, confirmation will be required"
		}
		return strings.Join([]string{
			renderPlanSection("Plan", []string{
				"`udl init` will create a starter config and ensure the default state directory exists.",
				overwriteLine,
			}, width),
			renderPlanSection("Paths", tuiInitPathLines(m.intro.ConfigPath, m.intro.StateDir), width),
			renderPlanSection("Actions", []string{"enter: start init", "esc: back"}, width),
		}, "\n")
	}
}

func (m tuiInitModel) shellShortcuts() []tuiShortcut {
	if m.prompt != nil {
		return nil
	}
	switch m.phase {
	case tuiInitPhaseIntro:
		if m.intro.PrepareErr != nil {
			return []tuiShortcut{{Key: "esc", Label: "back"}}
		}
		return []tuiShortcut{
			{Key: "enter", Label: "start"},
			{Key: "esc", Label: "back"},
		}
	case tuiInitPhaseRunning:
		return nil
	default:
		return []tuiShortcut{{Key: "esc", Label: "back"}}
	}
}

func (m tuiInitModel) shellBadges() []tuiBadge {
	switch m.phase {
	case tuiInitPhaseRunning:
		return []tuiBadge{{Label: "RUNNING", Tone: "warning"}}
	case tuiInitPhaseDone:
		return []tuiBadge{{Label: "COMPLETE", Tone: "success"}}
	case tuiInitPhaseCanceled:
		return []tuiBadge{{Label: "CANCELED", Tone: "muted"}}
	case tuiInitPhaseFailed:
		return []tuiBadge{{Label: "FAILED", Tone: "danger"}}
	default:
		return []tuiBadge{{Label: "INTRO", Tone: "info"}}
	}
}

func (m tuiInitModel) shellFooterStats() []tuiFooterStat {
	switch m.phase {
	case tuiInitPhaseRunning:
		return []tuiFooterStat{{Label: "state", Value: "running", Tone: "warning"}}
	case tuiInitPhaseDone:
		return []tuiFooterStat{{Label: "state", Value: "complete", Tone: "success"}}
	case tuiInitPhaseCanceled:
		return []tuiFooterStat{{Label: "state", Value: "canceled", Tone: "muted"}}
	case tuiInitPhaseFailed:
		return []tuiFooterStat{{Label: "state", Value: "failed", Tone: "danger"}}
	default:
		exists := "no"
		if m.intro.ConfigExists {
			exists = "yes"
		}
		stats := []tuiFooterStat{
			{Label: "state", Value: "intro", Tone: "info"},
			{Label: "config_exists", Value: exists, Tone: "muted"},
		}
		if m.intro.PrepareErr != nil {
			stats[0] = tuiFooterStat{Label: "state", Value: "blocked", Tone: "danger"}
		}
		return stats
	}
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

func tuiDoctorCheckLine(check doctor.Check) string {
	label := strings.ToUpper(string(check.Severity))
	tone := "info"
	switch check.Severity {
	case doctor.SeverityError:
		tone = "danger"
	case doctor.SeverityWarn:
		tone = "warning"
	case doctor.SeverityInfo:
		tone = "info"
	}
	chip := lipgloss.NewStyle().Bold(true)
	chip = shellToneStyle(newTUIShellTheme(), tone).Inherit(chip)
	return fmt.Sprintf("%s %s: %s", chip.Render("["+label+"]"), check.Name, check.Message)
}

func tuiInitPathLines(configPath, stateDir string) []string {
	return []string{
		"Config: " + configPath,
		"State Dir: " + stateDir,
	}
}

func tuiAuxSectionWidth(layout tuiShellLayout) int {
	width := shellMainSectionWidth(layout, newTUIShellTheme()) - 4
	if width < 40 {
		width = shellMainSectionWidth(layout, newTUIShellTheme())
	}
	if width < 24 {
		width = 24
	}
	return width
}

func tuiWrapLines(lines []string, width int) []string {
	if width < 16 {
		return lines
	}
	wrapped := []string{}
	for _, line := range lines {
		wrapped = append(wrapped, tuiWrapLine(line, width)...)
	}
	if len(wrapped) == 0 {
		return []string{"No details."}
	}
	return wrapped
}

func tuiWrapLine(line string, width int) []string {
	line = strings.TrimSpace(line)
	if line == "" {
		return []string{""}
	}
	if len([]rune(line)) <= width {
		return []string{line}
	}
	words := strings.Fields(line)
	if len(words) == 0 {
		return []string{line}
	}
	lines := []string{}
	current := words[0]
	for _, word := range words[1:] {
		candidate := current + " " + word
		if len([]rune(candidate)) > width {
			lines = append(lines, current)
			current = word
			continue
		}
		current = candidate
	}
	lines = append(lines, current)
	return lines
}

func warningCountTone(count int) string {
	if count > 0 {
		return "warning"
	}
	return "muted"
}
