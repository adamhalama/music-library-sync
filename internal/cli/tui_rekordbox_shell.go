package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/jaa/update-downloads/internal/rekordbox/playlistsync"
	"github.com/jaa/update-downloads/internal/rekordbox/syncconfig"
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
	} else if model.setup.DeleteConfirm {
		state.Modal = model.deleteMappingModal()
	} else if model.setup.Input != nil {
		state.Modal = model.setupInputModal()
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
	case tuiRekordboxPhaseSetupDiscovering, tuiRekordboxPhaseSetupList, tuiRekordboxPhaseSetupSource, tuiRekordboxPhaseSetupTarget, tuiRekordboxPhaseSetupReview, tuiRekordboxPhaseSetupSaving:
		return []string{"udl", "rekordbox", "config", "show", "path=" + firstNonEmpty(m.setup.ConfigPath, "auto")}
	case tuiRekordboxPhaseReview, tuiRekordboxPhaseConfirm, tuiRekordboxPhaseApplying, tuiRekordboxPhaseDone:
		parts = append(parts, "apply", "mapping="+m.selectedJobLabel())
	default:
		parts = append(parts, "plan", "mapping="+m.selectedJobLabel())
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
		shortcuts := []tuiShortcut{
			{Key: "j/k", Label: "job"},
			{Key: "s/n", Label: "setup"},
			{Key: "e", Label: "edit mapping"},
			{Key: "x", Label: "delete", Disabled: !m.hasSelectedFolderMapping()},
			{Key: "d", Label: "dry-run"},
			{Key: "enter", Label: "plan"},
			{Key: "esc", Label: "back"},
		}
		return shortcuts
	case tuiRekordboxPhaseSetupDiscovering, tuiRekordboxPhaseSetupSaving:
		return []tuiShortcut{{Key: "x", Label: "cancel"}, {Key: "ctrl+c", Label: "cancel"}}
	case tuiRekordboxPhaseSetupList:
		return []tuiShortcut{{Key: "j/k", Label: "mapping"}, {Key: "n", Label: "new"}, {Key: "e/enter", Label: "edit"}, {Key: "x", Label: "delete"}, {Key: "esc", Label: "done"}}
	case tuiRekordboxPhaseSetupSource:
		return []tuiShortcut{{Key: "j/k", Label: "folder"}, {Key: "m", Label: "manual"}, {Key: "enter", Label: "select"}, {Key: "esc", Label: "back"}}
	case tuiRekordboxPhaseSetupTarget:
		return []tuiShortcut{{Key: "j/k", Label: "folder"}, {Key: "m", Label: "manual/new"}, {Key: "i", Label: "id"}, {Key: "enter", Label: "select"}, {Key: "esc", Label: "back"}}
	case tuiRekordboxPhaseSetupReview:
		return []tuiShortcut{{Key: "enter/s", Label: "save"}, {Key: "esc", Label: "back"}}
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
	if m.phase == tuiRekordboxPhaseSetupList && m.setup.Saved {
		return &tuiBanner{Text: "Rekordbox mapping saved. Generate a plan to preview the sync.", Tone: "success"}
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
	case tuiRekordboxPhaseSetupDiscovering:
		return renderPlanSection("Discovering", []string{"Reading Music.app folders.", "Inspecting Rekordbox folders. Keep Rekordbox closed.", "x: cancel"}, width)
	case tuiRekordboxPhaseSetupList:
		return strings.Join([]string{
			renderPlanSection("Config", m.setupConfigLines(), width),
			renderPlanSection("Mappings", m.setupMappingLines(), width),
		}, "\n")
	case tuiRekordboxPhaseSetupSource:
		return strings.Join([]string{
			renderPlanSection("Source Music Folder", m.setupSourceLines(width), width),
			renderPlanSection("Current Mapping", m.setupCurrentMappingLines(), width),
		}, "\n")
	case tuiRekordboxPhaseSetupTarget:
		return strings.Join([]string{
			renderPlanSection("Target Rekordbox Folder", m.setupTargetLines(width), width),
			renderPlanSection("Current Mapping", m.setupCurrentMappingLines(), width),
		}, "\n")
	case tuiRekordboxPhaseSetupReview:
		return renderPlanSection("Review Mapping", m.setupReviewLines(), width)
	case tuiRekordboxPhaseSetupSaving:
		return renderPlanSection("Saving", []string{"Writing Rekordbox sync config atomically.", "Main udl.yaml is not modified."}, width)
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
	if mapping, ok := m.selectedFolderMapping(); ok {
		lines := []string{
			"Mapping: " + m.selectedJobLabel(),
			"Music folder: " + firstNonEmpty(mapping.MusicFolder, mapping.MusicFolderID),
			"Rekordbox folder: " + firstNonEmpty(mapping.RekordboxFolder, mapping.RekordboxFolderID),
			"DB dir: " + m.resolved.RekordboxDBDir,
			"Backup dir: " + m.resolved.BackupDir,
			"Python: " + m.resolved.PythonBin,
		}
		if m.runtimeStatus.Healthy {
			lines = append(lines, "Runtime: "+m.runtimeStatus.Message)
		}
		lines = append(lines, "enter: generate folder plan")
		return lines
	}
	lines := []string{
		"Mapping: " + m.selectedJobLabel(),
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
	if !m.rbCfg.HasFolderMappings() {
		lines = append(lines, "s: set up folder mappings")
	}
	lines = append(lines, "enter: generate plan")
	return lines
}

func (m tuiRekordboxModel) selectedFolderMapping() (syncconfig.FolderMapping, bool) {
	options := m.selectedJobOptions()
	if strings.TrimSpace(options.MappingID) == "" {
		return syncconfig.FolderMapping{}, false
	}
	return m.rbCfg.FolderMapping(options.MappingID)
}

func (m tuiRekordboxModel) hasSelectedFolderMapping() bool {
	_, ok := m.selectedFolderMapping()
	return ok
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

func (m tuiRekordboxModel) setupConfigLines() []string {
	lines := []string{
		"Path: " + firstNonEmpty(m.setup.ConfigPath, "not resolved yet"),
		"Path kind: " + firstNonEmpty(m.setup.ConfigKind, "unknown"),
		fmt.Sprintf("Music folders discovered: %d", len(m.musicFolders())),
		fmt.Sprintf("Rekordbox folders discovered: %d", len(m.rbFolders())),
	}
	if m.setup.DiscoverErr != nil {
		lines = append(lines, "Discovery warning: "+m.setup.DiscoverErr.Error())
		lines = append(lines, "Manual entry is still available.")
	}
	if len(m.rbCfg.Sync.Folders) == 0 {
		lines = append(lines, "n: create first folder mapping")
	}
	return lines
}

func (m tuiRekordboxModel) setupMappingLines() []string {
	if len(m.rbCfg.Sync.Folders) == 0 {
		return []string{"No folder mappings configured yet.", "n: new mapping"}
	}
	lines := make([]string, 0, len(m.rbCfg.Sync.Folders))
	for idx, mapping := range m.rbCfg.Sync.Folders {
		prefix := "  "
		if idx == m.setup.Cursor {
			prefix = "> "
		}
		line := fmt.Sprintf("%s%s  %s -> %s",
			prefix,
			mapping.ID,
			firstNonEmpty(mapping.MusicFolder, mapping.MusicFolderID),
			firstNonEmpty(mapping.RekordboxFolder, mapping.RekordboxFolderID),
		)
		lines = append(lines, line)
	}
	return lines
}

func (m tuiRekordboxModel) setupSourceLines(width int) []string {
	folders := m.musicFolders()
	if len(folders) == 0 {
		return []string{"No Music playlist folders were discovered.", "m or enter: type a source folder name manually"}
	}
	lines := []string{"Select a Music playlist folder, or press m to type one manually."}
	for idx, folder := range folders {
		prefix := "  "
		if idx == m.setup.SourceCursor {
			prefix = "> "
		}
		line := fmt.Sprintf("%s%s  id=%s  children=%d", prefix, folder.Name, folder.PersistentID, m.musicChildCount(folder.PersistentID))
		lines = append(lines, ansi.Truncate(line, width-4, ""))
	}
	return lines
}

func (m tuiRekordboxModel) setupTargetLines(width int) []string {
	folders := m.rbFolders()
	if len(folders) == 0 {
		return []string{"No Rekordbox folders were discovered.", "m or enter: type a target folder name; UDL can create it during apply"}
	}
	lines := []string{"Select an existing Rekordbox folder, or press m to type a new folder name."}
	for idx, folder := range folders {
		prefix := "  "
		if idx == m.setup.TargetCursor {
			prefix = "> "
		}
		line := fmt.Sprintf("%s%s  id=%s  playlists=%d", prefix, folder.Name, folder.ID, m.rbChildCount(folder.ID))
		lines = append(lines, ansi.Truncate(line, width-4, ""))
	}
	return lines
}

func (m tuiRekordboxModel) setupCurrentMappingLines() []string {
	mapping := m.setup.Mapping
	return []string{
		"ID: " + firstNonEmpty(mapping.ID, "will be generated"),
		"Music folder: " + firstNonEmpty(mapping.MusicFolder, mapping.MusicFolderID, "not selected"),
		"Rekordbox folder: " + firstNonEmpty(mapping.RekordboxFolder, mapping.RekordboxFolderID, "not selected"),
		"Playlist names: mirror source child names",
	}
}

func (m tuiRekordboxModel) setupReviewLines() []string {
	lines := append([]string{}, m.setupCurrentMappingLines()...)
	lines = append(lines,
		"Config path: "+firstNonEmpty(m.setup.ConfigPath, "not resolved"),
		"Mode: mirror",
		"Missing tracks: fail",
		"Create folders: true",
		"Create playlists: true",
		"enter/s: save mapping",
	)
	if m.setup.SaveErr != nil {
		lines = append(lines, "Save error: "+m.setup.SaveErr.Error())
	}
	return lines
}

func (m tuiRekordboxModel) musicChildCount(parentID string) int {
	count := 0
	for _, item := range m.setup.MusicItems {
		if item.ParentID == parentID && !item.Folder {
			count++
		}
	}
	return count
}

func (m tuiRekordboxModel) rbChildCount(parentID string) int {
	count := 0
	for _, item := range m.setup.RBInspect.Playlists {
		if item.ParentID == parentID && item.Attribute == 0 {
			count++
		}
	}
	return count
}

func (m tuiRekordboxModel) planSummaryLines() []string {
	if m.plan == nil {
		return []string{"No plan generated yet."}
	}
	plan := m.plan
	if plan.Version == playlistsync.PlanVersionFolder {
		lines := []string{
			fmt.Sprintf("Music folder: %s (%d playlists, %d tracks)", plan.MusicFolder.Name, plan.MusicFolder.ChildCount, plan.Summary.MusicTotal),
			fmt.Sprintf("Rekordbox folder: %s (ID %s)", plan.RekordboxFolder.Name, firstNonEmpty(plan.RekordboxFolder.ID, "will be created")),
			fmt.Sprintf("Matched by path: %d", plan.Summary.MatchedByPath),
			fmt.Sprintf("Missing in RB: %d", plan.Summary.MissingInRekordbox),
			fmt.Sprintf("Final playlists: %d", len(plan.Operations)),
			fmt.Sprintf("Final tracks: %d", plan.Summary.FinalTargetCount),
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
	if m.plan == nil {
		return []string{"No tracks in plan."}
	}
	if m.plan.Version == playlistsync.PlanVersionFolder {
		return m.folderOperationLines(layout, width)
	}
	if len(m.plan.Rows) == 0 {
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

func (m tuiRekordboxModel) folderOperationLines(layout tuiShellLayout, width int) []string {
	if m.plan == nil || len(m.plan.Operations) == 0 {
		return []string{"No playlist operations in plan."}
	}
	maxRows := layout.Height - 22
	if maxRows < 5 {
		maxRows = 5
	}
	if maxRows > 14 {
		maxRows = 14
	}
	if m.scroll > len(m.plan.Operations)-1 {
		m.scroll = len(m.plan.Operations) - 1
	}
	end := m.scroll + maxRows
	if end > len(m.plan.Operations) {
		end = len(m.plan.Operations)
	}
	lines := []string{fmt.Sprintf("Showing playlists %d-%d of %d", m.scroll+1, end, len(m.plan.Operations))}
	for _, op := range m.plan.Operations[m.scroll:end] {
		line := fmt.Sprintf("%s -> %s  add=%d move=%d remove=%d final=%d",
			op.MusicPlaylist.Name,
			op.RekordboxPlaylist.Name,
			op.Summary.WillAdd,
			op.Summary.WillMove,
			op.Summary.WillRemove,
			op.Summary.FinalTargetCount,
		)
		lines = append(lines, ansi.Truncate(line, width-4, ""))
	}
	return lines
}

func (m tuiRekordboxModel) doneLines() []string {
	if m.lastDryRun {
		return []string{"Dry run validated successfully.", "No backup or DB changes were written.", "r: regenerate plan"}
	}
	if m.plan != nil && m.plan.Version == playlistsync.PlanVersionFolder {
		return []string{
			"Rekordbox folder updated: " + firstNonEmpty(m.applyBatchResp.FolderName, m.plan.RekordboxFolder.Name),
			fmt.Sprintf("Final playlists: %d", m.applyBatchResp.FinalPlaylistCount),
			fmt.Sprintf("Final tracks: %d", m.applyBatchResp.FinalTrackCount),
			"Backup written: " + m.backupPath,
			"r: regenerate plan",
		}
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
		fmt.Sprintf("Target: %s", m.confirmTargetName()),
		fmt.Sprintf("Final target count: %d", m.plan.Summary.FinalTargetCount),
		"Backup root: " + m.plan.BackupDir,
		"",
		"y: apply  n/enter/esc: cancel",
	}
	return &tuiModalState{Title: "Confirm Rekordbox Apply", Lines: lines, Tone: "warning"}
}

func (m tuiRekordboxModel) deleteMappingModal() *tuiModalState {
	label := m.selectedJobLabel()
	if m.phase == tuiRekordboxPhaseSetupList && m.setup.Cursor >= 0 && m.setup.Cursor < len(m.rbCfg.Sync.Folders) {
		label = m.rbCfg.Sync.Folders[m.setup.Cursor].ID
	}
	return &tuiModalState{
		Title: "Delete Rekordbox Mapping",
		Lines: []string{
			"Mapping: " + label,
			"This only removes the UDL mapping from rekordbox.yaml.",
			"Rekordbox and Music.app are not changed.",
			"",
			"y: delete  n/enter/esc: cancel",
		},
		Tone: "warning",
	}
}

func (m tuiRekordboxModel) setupInputModal() *tuiModalState {
	if m.setup.Input == nil {
		return nil
	}
	lines := []string{
		tuiConfigEditorRenderInputValue(m.setup.Input),
		"",
		"enter: accept  esc: cancel",
	}
	return &tuiModalState{Title: m.setup.Input.Title, Lines: lines, Tone: "info"}
}

func (m tuiRekordboxModel) confirmTargetName() string {
	if m.plan != nil && m.plan.Version == playlistsync.PlanVersionFolder {
		return "folder " + m.plan.RekordboxFolder.Name
	}
	if m.plan != nil {
		return m.plan.RekordboxPlaylist.Name
	}
	return ""
}

func (m tuiRekordboxModel) phaseTone() string {
	switch m.phase {
	case tuiRekordboxPhaseDone:
		return "success"
	case tuiRekordboxPhaseFailed:
		return "danger"
	case tuiRekordboxPhasePlanning, tuiRekordboxPhaseApplying, tuiRekordboxPhaseConfirm, tuiRekordboxPhaseDeps, tuiRekordboxPhaseRepairing, tuiRekordboxPhaseSetupDiscovering, tuiRekordboxPhaseSetupSaving:
		return "warning"
	case tuiRekordboxPhaseSetupList, tuiRekordboxPhaseSetupSource, tuiRekordboxPhaseSetupTarget, tuiRekordboxPhaseSetupReview:
		return "info"
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
