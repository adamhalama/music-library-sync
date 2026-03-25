package cli

import (
	"fmt"
	"strings"

	"github.com/jaa/update-downloads/internal/config"
)

func buildOnboardingShellState(m tuiRootModel, layout tuiShellLayout) tuiShellState {
	model := m.onboardingModel
	state := tuiShellState{
		AppLabel:         "UDL",
		ScreenTitle:      "Get Started",
		SidebarSections:  workflowNavigationItems(m),
		Badges:           model.shellBadges(),
		CommandSummary:   model.shellCommandSummary(),
		Shortcuts:        model.shellShortcuts(),
		BodyTitle:        "Get Started",
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

func (m tuiOnboardingModel) shellBadges() []tuiBadge {
	badges := []tuiBadge{
		{Label: "STEP: " + strings.ToUpper(string(m.phase)), Tone: "info"},
		{Label: "SOURCE: " + strings.ToUpper(string(m.sourceType)), Tone: "muted"},
	}
	if m.sourceType == config.SourceTypeSpotify {
		badges = append(badges, tuiBadge{Label: "BETA", Tone: "warning"})
	}
	if m.phase == tuiOnboardingPhaseDone && m.saveErr == nil && !m.doctorReport.HasErrors() {
		badges = append(badges, tuiBadge{Label: "READY", Tone: "success"})
	}
	return badges
}

func (m tuiOnboardingModel) shellCommandSummary() []string {
	return []string{"udl", "tui", "get_started", "phase=" + string(m.phase)}
}

func (m tuiOnboardingModel) shellShortcuts() []tuiShortcut {
	if m.edit != nil {
		return []tuiShortcut{
			{Key: "type", Label: "insert"},
			{Key: "←/→", Label: "move"},
			{Key: "enter", Label: "apply"},
			{Key: "esc", Label: "cancel"},
		}
	}
	switch m.phase {
	case tuiOnboardingPhaseIntro:
		return []tuiShortcut{{Key: "enter", Label: "start"}, {Key: "esc", Label: "back"}}
	case tuiOnboardingPhaseLocations, tuiOnboardingPhaseSource:
		return []tuiShortcut{
			{Key: "j/k", Label: "move"},
			{Key: "enter", Label: "edit/apply"},
			{Key: "space", Label: "toggle type", Disabled: m.phase != tuiOnboardingPhaseSource},
			{Key: "esc", Label: "back"},
		}
	case tuiOnboardingPhaseCredentials:
		return []tuiShortcut{
			{Key: "j/k", Label: "move"},
			{Key: "enter", Label: "edit/apply"},
			{Key: "esc", Label: "back"},
		}
	case tuiOnboardingPhaseReview:
		return []tuiShortcut{{Key: "s", Label: "save"}, {Key: "enter", Label: "save"}, {Key: "esc", Label: "back"}}
	case tuiOnboardingPhaseDone:
		return []tuiShortcut{{Key: "esc", Label: "back"}}
	default:
		return nil
	}
}

func (m tuiOnboardingModel) shellFooterStats() []tuiFooterStat {
	stats := []tuiFooterStat{
		{Label: "phase", Value: string(m.phase), Tone: "info"},
		{Label: "source", Value: string(m.sourceType), Tone: "info"},
	}
	if m.phase == tuiOnboardingPhaseDone && m.saveErr == nil {
		stats = append(stats,
			tuiFooterStat{Label: "errors", Value: fmt.Sprintf("%d", m.doctorSummary.ErrorCount), Tone: failureCountTone(m.doctorSummary.ErrorCount)},
			tuiFooterStat{Label: "warnings", Value: fmt.Sprintf("%d", m.doctorSummary.WarnCount), Tone: warningCountTone(m.doctorSummary.WarnCount)},
		)
	}
	return stats
}

func (m tuiOnboardingModel) shellBanner() *tuiBanner {
	if m.saveErr != nil {
		return &tuiBanner{Text: "Setup failed: " + m.saveErr.Error(), Tone: "danger"}
	}
	if m.phase == tuiOnboardingPhaseReview && len(m.validationProblems) > 0 {
		return &tuiBanner{Text: "Finish the required fields before saving your setup.", Tone: "warning"}
	}
	if m.sourceType == config.SourceTypeSpotify && m.phase == tuiOnboardingPhaseSource {
		return &tuiBanner{Text: "Spotify works, but this release treats it as beta and will save credentials to macOS Keychain, not to YAML.", Tone: "warning"}
	}
	return nil
}

func (m tuiOnboardingModel) modalState() *tuiModalState {
	if m.edit == nil {
		return nil
	}
	value := tuiConfigEditorRenderInputValue(&tuiConfigEditorInlineEditState{
		Buffer: m.edit.Buffer,
		Cursor: m.edit.Cursor,
	})
	if tuiOnboardingFieldShouldMask(m.edit.Field) {
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
	return &tuiModalState{Title: "Edit Field", Lines: lines, Tone: "info"}
}

func (m tuiOnboardingModel) shellBody(layout tuiShellLayout) string {
	width := shellMainSectionWidth(layout, newTUIShellTheme()) - 4
	if width < 36 {
		width = shellMainSectionWidth(layout, newTUIShellTheme())
	}
	switch m.phase {
	case tuiOnboardingPhaseIntro:
		return strings.Join([]string{
			renderPlanSection("Welcome", []string{
				m.startupHeadline(),
				"UDL keeps your local music folders in sync from SoundCloud or Spotify sources.",
				"The guided path below creates a starter config without storing secrets in YAML.",
			}, width),
			renderPlanSection("What You Will Set Up", []string{
				"A music folder root for your downloads",
				"A state folder for sync progress and archive data",
				"One source to start with",
			}, width),
			renderPlanSection("Current Context", append([]string{"Config target: " + m.startup.ConfigPath}, m.startup.DetailLines...), width),
			renderPlanSection("Actions", []string{"enter: start guided setup", "esc: back"}, width),
		}, "\n")
	case tuiOnboardingPhaseLocations:
		lines := []string{
			renderCursorLineWithValue(m.locationsCursor == 0, "Music folder root", m.libraryRoot),
			renderCursorLineWithValue(m.locationsCursor == 1, "State folder", m.stateDir),
			renderCursorLineWithValue(m.locationsCursor == 2, "Continue to source setup", "enter"),
		}
		return strings.Join([]string{
			renderPlanSection("Locations", lines, width),
			renderPlanSection("Preview", []string{
				"Example source folder: " + m.currentTargetDir(),
				"State files stay in: " + m.stateDir,
			}, width),
		}, "\n")
	case tuiOnboardingPhaseSource:
		lines := []string{
			renderCursorLineWithValue(m.sourceCursor == 0, "Source type", m.sourceTypeLabel()),
			renderCursorLineWithValue(m.sourceCursor == 1, "Source name", m.sourceID),
			renderCursorLineWithValue(m.sourceCursor == 2, "Source URL", m.sourceURL),
			renderCursorLineWithValue(m.sourceCursor == 3, "Credentials and review", "enter"),
		}
		noteLines := []string{
			"Target folder: " + m.currentTargetDir(),
			"State file: " + tuiConfigEditorSuggestedStateFile(strings.TrimSpace(m.sourceID), m.sourceType),
		}
		noteLines = append(noteLines, m.sourceURLHelp()...)
		return strings.Join([]string{
			renderPlanSection("First Source", lines, width),
			renderPlanSection("Notes", noteLines, width),
		}, "\n")
	case tuiOnboardingPhaseCredentials:
		if m.sourceType == config.SourceTypeSpotify {
			return strings.Join([]string{
				renderPlanSection("Credentials", []string{
					renderCursorLineWithValue(m.credentialsCursor == 0, "Deezer ARL", tuiCredentialPreview(m.deemixARL)),
					renderCursorLineWithValue(m.credentialsCursor == 1, "Spotify Client ID", tuiCredentialPreview(m.spotifyClientID)),
					renderCursorLineWithValue(m.credentialsCursor == 2, "Spotify Client Secret", tuiCredentialPreview(m.spotifyClientSecret)),
					renderCursorLineWithValue(m.credentialsCursor == 3, "Continue to review", "enter"),
				}, width),
				renderPlanSection("Notes", []string{
					"UDL stores these values in macOS Keychain and keeps them out of udl.yaml.",
					"You can skip them for now and repair them later from the Credentials screen.",
				}, width),
			}, "\n")
		}
		return strings.Join([]string{
			renderPlanSection("Credentials", []string{
				renderCursorLineWithValue(m.credentialsCursor == 0, "SoundCloud Client ID", tuiCredentialPreview(m.soundCloudClientID)),
				renderCursorLineWithValue(m.credentialsCursor == 1, "Continue to review", "enter"),
			}, width),
			renderPlanSection("Notes", []string{
				"UDL stores this value in macOS Keychain and keeps it out of udl.yaml.",
				"If you skip it now, you can add it later from the Credentials screen.",
			}, width),
		}, "\n")
	case tuiOnboardingPhaseReview:
		validationLines := []string{"No blocking issues in the starter config."}
		if len(m.validationProblems) > 0 {
			validationLines = append([]string{"Fix these before saving:"}, m.validationProblems...)
		}
		return strings.Join([]string{
			renderPlanSection("Setup Summary", []string{
				"Config target: " + m.startup.ConfigPath,
				"Music folder root: " + m.libraryRoot,
				"State folder: " + m.stateDir,
				"Source type: " + m.sourceTypeLabel(),
				"Source name: " + m.sourceID,
				"Source URL: " + m.sourceURL,
				"Target folder: " + m.currentTargetDir(),
			}, width),
			renderPlanSection("Security", []string{
				"UDL does not write SoundCloud, Deezer, or Spotify secrets into the config file.",
				"Managed credentials are saved in macOS Keychain.",
			}, width),
			renderPlanSection("Validation", validationLines, width),
		}, "\n")
	case tuiOnboardingPhaseSaving:
		return renderPlanSection("Saving", []string{
			"Writing your starter config.",
			"Running `doctor` so the next step is explicit.",
		}, width)
	case tuiOnboardingPhaseDone:
		if m.saveErr != nil {
			return strings.Join([]string{
				renderPlanSection("Result", []string{"Setup failed."}, width),
				renderPlanSection("Details", tuiSplitDetailLines(m.saveErr.Error()), width),
			}, "\n")
		}
		checkLines := []string{"No checks reported."}
		if len(m.doctorChecks) > 0 {
			checkLines = make([]string, 0, len(m.doctorChecks))
			for _, check := range m.doctorChecks {
				checkLines = append(checkLines, tuiDoctorCheckLine(check))
			}
		}
		return strings.Join([]string{
			renderPlanSection("Result", []string{
				"Starter setup saved.",
				"Config: " + m.saveResult.Path,
				"State dir: " + m.saveResult.StateDir,
				"First target folder: " + m.saveResult.TargetDir,
			}, width),
			renderPlanSection("Check System", append([]string{
				fmt.Sprintf("Errors: %d", m.doctorSummary.ErrorCount),
				fmt.Sprintf("Warnings: %d", m.doctorSummary.WarnCount),
			}, checkLines...), width),
			renderPlanSection("Next Step", m.nextStepLines(), width),
		}, "\n")
	default:
		return renderPlanSection("Get Started", []string{"Unknown onboarding phase."}, width)
	}
}

func renderCursorLineWithValue(active bool, label string, value string) string {
	prefix := "  "
	if active {
		prefix = "> "
	}
	return prefix + label + ": " + firstNonEmpty(value, "(empty)")
}

func tuiCredentialPreview(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(not set)"
	}
	return "(ready)"
}

func tuiOnboardingFieldShouldMask(field string) bool {
	switch field {
	case "deemix_arl", "spotify_client_secret":
		return true
	default:
		return false
	}
}
