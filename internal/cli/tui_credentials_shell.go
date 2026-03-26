package cli

import (
	"fmt"
	"strings"
)

func buildCredentialsShellState(m tuiRootModel, layout tuiShellLayout) tuiShellState {
	model := m.credentialsModel
	state := tuiShellState{
		AppLabel:         "UDL",
		ScreenTitle:      "Credentials",
		SidebarSections:  workflowNavigationItems(m),
		Badges:           model.shellBadges(),
		CommandSummary:   []string{"udl", "tui", "credentials"},
		Shortcuts:        model.shellShortcuts(),
		BodyTitle:        "Credentials",
		Body:             model.shellBody(layout),
		DenseBody:        true,
		StyledBody:       true,
		FooterStats:      model.shellFooterStats(),
		Banner:           model.shellBanner(),
		AllowBack:        m.canReturnToMenuOnEsc(),
		DebugMessageType: m.lastMsgTypeIfEnabled(),
	}
	if model.edit != nil {
		state.Modal = model.modalState()
	}
	return state
}

func (m tuiCredentialsModel) shellBadges() []tuiBadge {
	if m.loading {
		return []tuiBadge{{Label: "LOADING", Tone: "warning"}}
	}
	missing := 0
	refresh := 0
	for _, card := range m.cards {
		if card.StatusLabel == "missing" {
			missing++
		}
		if card.StatusLabel == "needs refresh" {
			refresh++
		}
	}
	badges := []tuiBadge{{Label: "KEYCHAIN", Tone: "info"}}
	if refresh > 0 {
		badges = append(badges, tuiBadge{Label: fmt.Sprintf("REFRESH: %d", refresh), Tone: "danger"})
	}
	if missing > 0 {
		badges = append(badges, tuiBadge{Label: fmt.Sprintf("MISSING: %d", missing), Tone: "warning"})
	}
	if missing == 0 && refresh == 0 && len(m.cards) > 0 {
		badges = append(badges, tuiBadge{Label: "READY", Tone: "success"})
	}
	return badges
}

func (m tuiCredentialsModel) shellShortcuts() []tuiShortcut {
	if m.edit != nil {
		return []tuiShortcut{
			{Key: "type", Label: "insert"},
			{Key: "enter", Label: "apply"},
			{Key: "esc", Label: "cancel"},
		}
	}
	return []tuiShortcut{
		{Key: "j/k", Label: "move"},
		{Key: "enter", Label: "edit/save"},
		{Key: "x", Label: "clear keychain", Disabled: m.selectedCard() == nil || !m.selectedCard().Clearable},
		{Key: "r", Label: "reload"},
		{Key: "esc", Label: "back"},
	}
}

func (m tuiCredentialsModel) shellFooterStats() []tuiFooterStat {
	stats := []tuiFooterStat{
		{Label: "items", Value: fmt.Sprintf("%d", len(m.cards)), Tone: "info"},
	}
	if strings.TrimSpace(m.stateDir) != "" {
		stats = append(stats, tuiFooterStat{Label: "state_dir", Value: m.stateDir, Tone: "muted"})
	}
	return stats
}

func (m tuiCredentialsModel) shellBanner() *tuiBanner {
	if m.err != nil {
		return &tuiBanner{Text: "Credential update failed: " + m.err.Error(), Tone: "danger"}
	}
	if strings.TrimSpace(m.flash) != "" {
		return &tuiBanner{Text: m.flash, Tone: "success"}
	}
	return nil
}

func (m tuiCredentialsModel) modalState() *tuiModalState {
	if m.edit == nil {
		return nil
	}
	value := tuiConfigEditorRenderInputValue(&tuiConfigEditorInlineEditState{
		Buffer: m.edit.Buffer,
		Cursor: m.edit.Cursor,
	})
	if m.edit.MaskInput {
		value = tuiMaskedInputValue(m.edit.Buffer, m.edit.Cursor)
	}
	lines := []string{
		"Field",
		m.edit.Title,
		"",
		"Value",
		value,
	}
	if len(m.edit.Help) > 0 {
		lines = append(lines, "", "Help")
		lines = append(lines, m.edit.Help...)
	}
	lines = append(lines, "", "type to edit  left/right move  home/end jump  backspace/delete remove  enter apply  esc cancel")
	return &tuiModalState{Title: "Edit Credential", Lines: lines, Tone: "info"}
}

func (m tuiCredentialsModel) shellBody(layout tuiShellLayout) string {
	width := shellMainSectionWidth(layout, newTUIShellTheme()) - 4
	if width < 36 {
		width = shellMainSectionWidth(layout, newTUIShellTheme())
	}
	if m.loading {
		return renderPlanSection("Status", []string{
			"Loading credential state.",
			"Inspecting environment overrides, Keychain entries, and compatibility fallbacks.",
		}, width)
	}
	if len(m.cards) == 0 {
		return renderPlanSection("Status", []string{"No credentials available."}, width)
	}
	card := m.selectedCard()
	rows := make([]string, 0, len(m.cards))
	for idx, entry := range m.cards {
		prefix := "  "
		if idx == m.clampCursor() {
			prefix = "> "
		}
		rows = append(rows, fmt.Sprintf("%s%s  |  %s  |  %s", prefix, entry.Title, entry.StatusLabel, entry.StorageLabel))
	}
	detailLines := []string{}
	if card != nil {
		detailLines = []string{
			"Status: " + card.StatusLabel,
			"Storage: " + card.StorageLabel,
			"Affects: " + card.AffectedWorkflows,
			"Action: " + card.ActionLabel,
			"Last checked: " + card.LastCheckedLabel,
			"",
			card.Summary,
		}
	}
	return strings.Join([]string{
		renderPlanSection("Credential Board", rows, width),
		renderPlanSection("Selected", detailLines, width),
		renderPlanSection("Security", []string{
			"UDL stores managed credentials in macOS Keychain and keeps them out of udl.yaml.",
			"Environment variables and ~/.spotdl/config.json still work as compatibility sources, but the TUI treats them as external.",
		}, width),
	}, "\n")
}

func tuiMaskedInputValue(value string, cursor int) string {
	masked := strings.Repeat("*", utf8RuneCount(value))
	return tuiConfigEditorRenderInputValue(&tuiConfigEditorInlineEditState{
		Buffer: masked,
		Cursor: minInt(cursor, utf8RuneCount(masked)),
	})
}
