package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/jaa/update-downloads/internal/rekordbox/playlistsync"
)

func buildRekordboxShellState(m tuiRootModel, layout tuiShellLayout) tuiShellState {
	model := m.rekordboxModel
	state := tuiShellState{
		AppLabel:         "UDL",
		ScreenTitle:      "Rekordbox Sync",
		SidebarSections:  workflowNavigationItems(m),
		Badges:           model.shellBadges(),
		CommandSummary:   model.shellCommandSummary(),
		Shortcuts:        model.shellShortcuts(),
		BodyTitle:        "Rekordbox Playlist Sync",
		Body:             model.shellBody(layout),
		DenseBody:        true,
		StyledBody:       true,
		FooterStats:      model.shellFooterStats(),
		Banner:           model.shellBanner(),
		AllowBack:        m.canReturnToMenuOnEsc(),
		DebugMessageType: m.lastMsgTypeIfEnabled(),
	}
	if model.phase == tuiRekordboxPhaseConfirm {
		state.Modal = model.confirmModal()
	}
	return state
}

func (m tuiRekordboxModel) shellBadges() []tuiBadge {
	badges := []tuiBadge{
		{Label: "STATE: " + strings.ToUpper(string(m.phase)), Tone: m.phaseTone()},
		{Label: "DRY-RUN: " + boolLabel(m.dryRun), Tone: boolTone(m.dryRun)},
	}
	if m.plan != nil {
		badges = append(badges, tuiBadge{Label: fmt.Sprintf("MATCHED: %d/%d", m.plan.Summary.MatchedByPath, m.plan.Summary.MusicTotal), Tone: m.planTone()})
	}
	return badges
}

func (m tuiRekordboxModel) shellCommandSummary() []string {
	parts := []string{"udl", "rekordbox", "playlist-sync"}
	switch m.phase {
	case tuiRekordboxPhaseReview, tuiRekordboxPhaseConfirm, tuiRekordboxPhaseApplying, tuiRekordboxPhaseDone:
		parts = append(parts, "apply", "job="+m.selectedJobLabel())
	default:
		parts = append(parts, "plan", "job="+m.selectedJobLabel())
	}
	if m.resolved.MusicPlaylist != "" {
		parts = append(parts, "music="+m.resolved.MusicPlaylist)
	}
	if m.resolved.RekordboxPlaylist != "" {
		parts = append(parts, "target="+m.resolved.RekordboxPlaylist)
	}
	return parts
}

func (m tuiRekordboxModel) shellShortcuts() []tuiShortcut {
	switch m.phase {
	case tuiRekordboxPhaseReady:
		return []tuiShortcut{
			{Key: "j/k", Label: "job"},
			{Key: "d", Label: "dry-run"},
			{Key: "enter", Label: "plan"},
			{Key: "esc", Label: "back"},
		}
	case tuiRekordboxPhaseDeps:
		return []tuiShortcut{{Key: "enter", Label: "install/repair"}, {Key: "r", Label: "recheck"}, {Key: "esc", Label: "back"}}
	case tuiRekordboxPhasePlanning, tuiRekordboxPhaseApplying, tuiRekordboxPhaseRepairing:
		return []tuiShortcut{{Key: "x", Label: "cancel active run"}, {Key: "ctrl+c", Label: "cancel"}}
	case tuiRekordboxPhaseReview:
		return []tuiShortcut{
			{Key: "j/k", Label: "scroll"},
			{Key: "d", Label: "dry-run"},
			{Key: "r", Label: "regenerate"},
			{Key: "enter", Label: "apply", Disabled: !m.canApplyPlan()},
			{Key: "esc", Label: "back"},
		}
	case tuiRekordboxPhaseConfirm:
		return []tuiShortcut{{Key: "y", Label: "apply"}, {Key: "n/enter", Label: "cancel"}}
	case tuiRekordboxPhaseDone, tuiRekordboxPhaseFailed:
		return []tuiShortcut{{Key: "r", Label: "regenerate"}, {Key: "d", Label: "dry-run"}, {Key: "esc", Label: "back"}}
	default:
		return nil
	}
}

func (m tuiRekordboxModel) shellFooterStats() []tuiFooterStat {
	stats := []tuiFooterStat{{Label: "state", Value: string(m.phase), Tone: m.phaseTone()}}
	if len(m.jobs) > 0 {
		stats = append(stats, tuiFooterStat{Label: "jobs", Value: fmt.Sprintf("%d", len(m.jobs)), Tone: "info"})
	}
	if m.plan != nil {
		stats = append(stats,
			tuiFooterStat{Label: "add", Value: fmt.Sprintf("%d", m.plan.Summary.WillAdd), Tone: "success"},
			tuiFooterStat{Label: "move", Value: fmt.Sprintf("%d", m.plan.Summary.WillMove), Tone: "info"},
			tuiFooterStat{Label: "remove", Value: fmt.Sprintf("%d", m.plan.Summary.WillRemove), Tone: "warning"},
			tuiFooterStat{Label: "final", Value: fmt.Sprintf("%d", m.plan.Summary.FinalTargetCount), Tone: "info"},
		)
	}
	return stats
}

func (m tuiRekordboxModel) shellBanner() *tuiBanner {
	if m.err != nil && m.phase == tuiRekordboxPhaseFailed {
		return &tuiBanner{Text: "Rekordbox sync failed: " + m.err.Error(), Tone: "danger"}
	}
	if blocker := m.applyBlocker(); blocker != "" && m.phase == tuiRekordboxPhaseReview {
		return &tuiBanner{Text: "Apply blocked: " + blocker, Tone: "warning"}
	}
	if m.phase == tuiRekordboxPhasePlanning {
		return &tuiBanner{Text: "Reading Music.app and inspecting Rekordbox. Keep Rekordbox closed.", Tone: "info"}
	}
	if m.phase == tuiRekordboxPhaseDeps {
		return &tuiBanner{Text: "Rekordbox sync needs a Python runtime with pyrekordbox. Press enter to install or repair UDL's managed runtime.", Tone: "warning"}
	}
	if m.phase == tuiRekordboxPhaseRepairing {
		return &tuiBanner{Text: "Installing Rekordbox Python dependencies. This can take a minute on first run.", Tone: "info"}
	}
	if m.phase == tuiRekordboxPhaseApplying {
		return &tuiBanner{Text: "Applying playlist sync. Keep Rekordbox closed.", Tone: "warning"}
	}
	return nil
}

func (m tuiRekordboxModel) shellBody(layout tuiShellLayout) string {
	width := shellMainSectionWidth(layout, newTUIShellTheme()) - 4
	if width < 40 {
		width = shellMainSectionWidth(layout, newTUIShellTheme())
	}
	switch m.phase {
	case tuiRekordboxPhaseLoading:
		return renderPlanSection("Loading", []string{"Loading config and Rekordbox sync jobs..."}, width)
	case tuiRekordboxPhaseDeps:
		return renderPlanSection("Dependencies", m.depsLines(), width)
	case tuiRekordboxPhaseRepairing:
		return renderPlanSection("Repairing", []string{"Creating or repairing UDL's managed Rekordbox Python runtime.", "Installing pyrekordbox and SQLCipher support.", "x: cancel"}, width)
	case tuiRekordboxPhaseReady:
		return strings.Join([]string{
			renderPlanSection("Selected Job", m.readyLines(), width),
			renderPlanSection("Jobs", m.jobLines(), width),
		}, "\n")
	case tuiRekordboxPhasePlanning:
		return renderPlanSection("Planning", []string{"Checking Rekordbox is closed.", "Reading Music.app playlist.", "Inspecting Rekordbox collection.", "Building signed plan."}, width)
	case tuiRekordboxPhaseReview, tuiRekordboxPhaseConfirm:
		return strings.Join([]string{
			renderPlanSection("Plan Summary", m.planSummaryLines(), width),
			renderPlanSection("Tracks", m.trackLines(layout, width), width),
		}, "\n")
	case tuiRekordboxPhaseApplying:
		return renderPlanSection("Applying", []string{"Rechecking Rekordbox is closed.", "Validating plan preconditions.", "Creating backup before any DB write.", "Replacing target playlist membership."}, width)
	case tuiRekordboxPhaseDone:
		return renderPlanSection("Complete", m.doneLines(), width)
	case tuiRekordboxPhaseFailed:
		return renderPlanSection("Failed", m.failedLines(), width)
	default:
		return ""
	}
}

func (m tuiRekordboxModel) readyLines() []string {
	lines := []string{
		"Job: " + m.selectedJobLabel(),
		"Music playlist: " + firstNonEmpty(m.resolved.MusicPlaylist, playlistsync.DefaultMusicPlaylist),
		"Rekordbox playlist: " + firstNonEmpty(m.resolved.RekordboxPlaylist, playlistsync.DefaultRekordboxPlaylist),
		"DB dir: " + m.resolved.RekordboxDBDir,
		"Backup dir: " + m.resolved.BackupDir,
		"Python: " + m.resolved.PythonBin,
	}
	if m.runtimeStatus.Healthy {
		lines = append(lines, "Runtime: "+m.runtimeStatus.Message)
	}
	if strings.TrimSpace(m.resolved.PythonPath) != "" {
		lines = append(lines, "Python path: "+m.resolved.PythonPath)
	}
	lines = append(lines, "enter: generate plan")
	return lines
}

func (m tuiRekordboxModel) depsLines() []string {
	status := m.runtimeStatus
	lines := []string{
		"Status: " + firstNonEmpty(status.Message, "Rekordbox Python runtime is not ready."),
		"Python: " + firstNonEmpty(status.PythonBin, "managed runtime"),
	}
	if status.Managed {
		lines = append(lines, "Managed venv: "+status.VenvDir)
	}
	if status.Version != "" {
		lines = append(lines, "pyrekordbox: "+status.Version)
	}
	lines = append(lines,
		"enter: install or repair managed runtime",
		"r: recheck status",
	)
	return lines
}

func (m tuiRekordboxModel) jobLines() []string {
	if len(m.jobs) == 0 {
		return []string{"default"}
	}
	lines := make([]string, 0, len(m.jobs))
	for idx, job := range m.jobs {
		prefix := "  "
		if idx == m.jobCursor {
			prefix = "> "
		}
		lines = append(lines, prefix+job.Label)
	}
	return lines
}

func (m tuiRekordboxModel) planSummaryLines() []string {
	if m.plan == nil {
		return []string{"No plan generated yet."}
	}
	plan := m.plan
	lines := []string{
		fmt.Sprintf("Music playlist: %s (%d tracks)", plan.MusicPlaylist.Name, plan.Summary.MusicTotal),
		fmt.Sprintf("Rekordbox playlist: %s (ID %s)", plan.RekordboxPlaylist.Name, firstNonEmpty(plan.RekordboxPlaylist.ID, "will be created")),
		fmt.Sprintf("Matched by path: %d", plan.Summary.MatchedByPath),
		fmt.Sprintf("Missing in RB: %d", plan.Summary.MissingInRekordbox),
		fmt.Sprintf("Current target count: %d", plan.Summary.CurrentTargetCount),
		fmt.Sprintf("Final target count: %d", plan.Summary.FinalTargetCount),
		fmt.Sprintf("Changes: add=%d move=%d remove=%d keep=%d", plan.Summary.WillAdd, plan.Summary.WillMove, plan.Summary.WillRemove, plan.Summary.WillKeep),
		"Plan file: " + m.planPath,
	}
	if blocker := m.applyBlocker(); blocker != "" {
		lines = append(lines, "Apply blocked: "+blocker)
	} else if m.dryRun {
		lines = append(lines, "enter: validate dry-run apply")
	} else {
		lines = append(lines, "enter: confirm real apply with backup")
	}
	return lines
}

func (m tuiRekordboxModel) trackLines(layout tuiShellLayout, width int) []string {
	if m.plan == nil || len(m.plan.Rows) == 0 {
		return []string{"No tracks in plan."}
	}
	maxRows := layout.Height - 22
	if maxRows < 5 {
		maxRows = 5
	}
	if maxRows > 14 {
		maxRows = 14
	}
	if m.scroll > len(m.plan.Rows)-1 {
		m.scroll = len(m.plan.Rows) - 1
	}
	end := m.scroll + maxRows
	if end > len(m.plan.Rows) {
		end = len(m.plan.Rows)
	}
	lines := []string{fmt.Sprintf("Showing %d-%d of %d", m.scroll+1, end, len(m.plan.Rows))}
	for _, row := range m.plan.Rows[m.scroll:end] {
		title := firstNonEmpty(row.Title, row.RekordboxTitle, row.Path)
		line := fmt.Sprintf("%3d  %-14s  %s - %s", row.MusicIndex, row.Action, row.Artist, title)
		if row.MatchStatus != "matched_path" {
			line = fmt.Sprintf("%3d  %-14s  %s", row.MusicIndex, row.MatchStatus, title)
		}
		lines = append(lines, ansi.Truncate(line, width-4, ""))
	}
	return lines
}

func (m tuiRekordboxModel) doneLines() []string {
	if m.lastDryRun {
		return []string{"Dry run validated successfully.", "No backup or DB changes were written.", "r: regenerate plan"}
	}
	return []string{
		"Rekordbox playlist updated: " + firstNonEmpty(m.applyResp.PlaylistName, m.resolved.RekordboxPlaylist),
		fmt.Sprintf("Final target count: %d", m.applyResp.FinalTrackCount),
		"Backup written: " + m.backupPath,
		"r: regenerate plan",
	}
}

func (m tuiRekordboxModel) failedLines() []string {
	if m.err == nil {
		return []string{"Unknown failure.", "r: retry"}
	}
	lines := tuiWrapLines(tuiSplitDetailLines(m.err.Error()), 86)
	lines = append(lines, "r: retry  esc: back")
	return lines
}

func (m tuiRekordboxModel) confirmModal() *tuiModalState {
	if m.plan == nil {
		return nil
	}
	mode := "real apply"
	if m.dryRun {
		mode = "dry-run validation"
	}
	lines := []string{
		fmt.Sprintf("Mode: %s", mode),
		fmt.Sprintf("Target: %s", m.plan.RekordboxPlaylist.Name),
		fmt.Sprintf("Final target count: %d", m.plan.Summary.FinalTargetCount),
		"Backup root: " + m.plan.BackupDir,
		"",
		"y: apply  n/enter/esc: cancel",
	}
	return &tuiModalState{Title: "Confirm Rekordbox Apply", Lines: lines, Tone: "warning"}
}

func (m tuiRekordboxModel) phaseTone() string {
	switch m.phase {
	case tuiRekordboxPhaseDone:
		return "success"
	case tuiRekordboxPhaseFailed:
		return "danger"
	case tuiRekordboxPhasePlanning, tuiRekordboxPhaseApplying, tuiRekordboxPhaseConfirm, tuiRekordboxPhaseDeps, tuiRekordboxPhaseRepairing:
		return "warning"
	default:
		return "info"
	}
}

func (m tuiRekordboxModel) planTone() string {
	if m.plan == nil {
		return "muted"
	}
	if m.plan.Summary.MissingInRekordbox > 0 || m.plan.Summary.AmbiguousInRB > 0 {
		return "warning"
	}
	return "success"
}
