package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/jaa/update-downloads/internal/playlists"
)

func buildPlaylistShellState(m tuiRootModel, layout tuiShellLayout) tuiShellState {
	model := m.playlistModel
	return tuiShellState{
		AppLabel:         "UDL",
		ScreenTitle:      "Playlists",
		SidebarSections:  workflowNavigationItems(m),
		Badges:           model.shellBadges(),
		CommandSummary:   model.shellCommandSummary(),
		Shortcuts:        model.shellShortcuts(),
		BodyTitle:        "Standalone Playlists",
		Body:             model.shellBody(layout),
		DenseBody:        true,
		StyledBody:       true,
		FooterStats:      model.shellFooterStats(),
		Banner:           model.shellBanner(),
		AllowBack:        m.canReturnToMenuOnEsc(),
		DebugMessageType: m.lastMsgTypeIfEnabled(),
	}
}

func (m tuiPlaylistModel) shellBadges() []tuiBadge {
	tone := "info"
	switch m.phase {
	case tuiPlaylistPhaseList, tuiPlaylistPhaseDetail:
		tone = "success"
	case tuiPlaylistPhaseRefreshing, tuiPlaylistPhaseSetupDiscovering:
		tone = "warning"
	case tuiPlaylistPhaseFailed:
		tone = "danger"
	}
	badges := []tuiBadge{{Label: "STATE: " + strings.ToUpper(string(m.phase)), Tone: tone}}
	if snapshot, ok := m.currentSnapshot(); ok {
		badges = append(badges, tuiBadge{Label: fmt.Sprintf("TRACKS: %d", len(snapshot.Tracks)), Tone: "info"})
	}
	return badges
}

func (m tuiPlaylistModel) shellCommandSummary() []string {
	parts := []string{"udl", "playlist"}
	if definition, ok := m.currentDefinition(); ok {
		parts = append(parts, "show", definition.ID)
	}
	if m.phase == tuiPlaylistPhaseRefreshing {
		parts = append(parts, "refresh")
	}
	return parts
}

func (m tuiPlaylistModel) shellShortcuts() []tuiShortcut {
	switch m.phase {
	case tuiPlaylistPhaseSetup:
		return []tuiShortcut{{Key: "j/k", Label: "source"}, {Key: "enter", Label: "create"}, {Key: "r", Label: "rediscover"}}
	case tuiPlaylistPhaseList:
		return []tuiShortcut{{Key: "j/k", Label: "playlist"}, {Key: "enter", Label: "open"}, {Key: "n", Label: "new"}, {Key: "esc", Label: "back"}}
	case tuiPlaylistPhaseDetail:
		_, hasSnapshot := m.currentSnapshot()
		return []tuiShortcut{
			{Key: "j/k", Label: "tracks"},
			{Key: "r", Label: "refresh"},
			{Key: "f", Label: "FreeDL", Disabled: !hasSnapshot},
			{Key: "b", Label: "Rekordbox", Disabled: !hasSnapshot},
			{Key: "esc", Label: "playlists"},
		}
	case tuiPlaylistPhaseRefreshing, tuiPlaylistPhaseSetupDiscovering:
		return []tuiShortcut{{Key: "x", Label: "cancel"}}
	case tuiPlaylistPhaseFailed:
		return []tuiShortcut{{Key: "r", Label: "retry"}, {Key: "esc", Label: "back"}}
	default:
		return nil
	}
}

func (m tuiPlaylistModel) shellFooterStats() []tuiFooterStat {
	stats := []tuiFooterStat{
		{Label: "state", Value: string(m.phase), Tone: "info"},
		{Label: "configured", Value: fmt.Sprintf("%d", len(m.cfg.Playlists)), Tone: "info"},
	}
	if snapshot, ok := m.currentSnapshot(); ok {
		stats = append(stats, tuiFooterStat{Label: "snapshot", Value: snapshotAge(snapshot.RefreshedAt), Tone: snapshotAgeTone(snapshot.RefreshedAt)})
	}
	return stats
}

func (m tuiPlaylistModel) shellBanner() *tuiBanner {
	if m.refreshErr != nil {
		return &tuiBanner{Text: "Refresh failed; saved snapshot preserved: " + m.refreshErr.Error(), Tone: "danger"}
	}
	if m.lastChanges != nil {
		return &tuiBanner{
			Text: fmt.Sprintf("Snapshot refreshed: +%d -%d unchanged=%d", m.lastChanges.Added, m.lastChanges.Removed, m.lastChanges.Kept),
			Tone: "success",
		}
	}
	if m.err != nil {
		return &tuiBanner{Text: m.err.Error(), Tone: "danger"}
	}
	return nil
}

func (m tuiPlaylistModel) shellBody(layout tuiShellLayout) string {
	width := shellMainSectionWidth(layout, newTUIShellTheme()) - 4
	if width < 40 {
		width = shellMainSectionWidth(layout, newTUIShellTheme())
	}
	switch m.phase {
	case tuiPlaylistPhaseLoading:
		return renderPlanSection("Loading", []string{"Reading playlists.yaml and saved snapshots.", "Apple Music is not accessed while opening this screen."}, width)
	case tuiPlaylistPhaseSetupDiscovering:
		return renderPlanSection("Discovering Apple Music", []string{"Finding playlists for initial setup.", "No standalone snapshot is refreshed or written during discovery."}, width)
	case tuiPlaylistPhaseSetup:
		return renderPlanSection("Create Standalone Playlist", m.setupLines(width), width)
	case tuiPlaylistPhaseList:
		return renderPlanSection("Playlists", m.listLines(), width)
	case tuiPlaylistPhaseDetail, tuiPlaylistPhaseRefreshing:
		return m.detailBody(width, layout.Compact || layout.Height < 32)
	case tuiPlaylistPhaseFailed:
		return renderPlanSection("Failed", append([]string{"Playlist Hub could not load."}, tuiSplitDetailLines(m.err.Error())...), width)
	default:
		return ""
	}
}

func (m tuiPlaylistModel) setupLines(width int) []string {
	if len(m.setupItems) == 0 {
		return []string{"No Apple Music playlists were discovered.", "Press r to retry discovery."}
	}
	lines := []string{
		"Choose the Apple Music playlist that should back the standalone playlist.",
		"The suggested Favorites setup is selected when available.",
		"Creating it does not refresh the snapshot; press r from its detail screen when ready.",
	}
	start, end := visibleWindow(m.setupCursor, len(m.setupItems), 14)
	for idx := start; idx < end; idx++ {
		item := m.setupItems[idx]
		prefix := "  "
		if idx == m.setupCursor {
			prefix = "> "
		}
		lines = append(lines, truncateForWidth(fmt.Sprintf("%s%s  tracks=%d  id=%s", prefix, item.Name, item.TrackCount, item.ID), width-4))
	}
	return lines
}

func (m tuiPlaylistModel) listLines() []string {
	if len(m.cfg.Playlists) == 0 {
		return []string{"No standalone playlists configured.", "Press n to create one from Apple Music."}
	}
	lines := []string{"Opening a playlist uses only its saved snapshot. Press r inside it to refresh explicitly."}
	for idx, definition := range m.cfg.Playlists {
		prefix := "  "
		if idx == m.cursor {
			prefix = "> "
		}
		status := "not refreshed"
		if snapshot, ok := m.snapshots[definition.ID]; ok {
			status = fmt.Sprintf("%d tracks · %s", len(snapshot.Tracks), snapshotAge(snapshot.RefreshedAt))
		} else if snapshotErr := m.snapshotErrs[definition.ID]; snapshotErr != "" {
			status = "snapshot error"
		}
		lines = append(lines, fmt.Sprintf("%s%s  [%s]  %s", prefix, definition.Name, definition.Provider, status))
		lines = append(lines, "   source="+firstNonEmpty(definition.ProviderPlaylist, definition.ProviderPlaylistID))
	}
	return lines
}

func (m tuiPlaylistModel) detailBody(width int, compact bool) string {
	definition, ok := m.currentDefinition()
	if !ok {
		return renderPlanSection("Playlist", []string{"No playlist selected."}, width)
	}
	summary := []string{
		"Name: " + definition.Name,
		"Source: Apple Music / " + firstNonEmpty(definition.ProviderPlaylist, definition.ProviderPlaylistID),
	}
	if compact {
		summary = append(summary, "Defaults: FreeDL="+firstNonEmpty(definition.DefaultFreeDLJob, "choose")+" · Rekordbox="+firstNonEmpty(definition.DefaultRekordboxTarget, "choose"))
	} else {
		summary = append(summary,
			"Default FreeDL job: "+firstNonEmpty(definition.DefaultFreeDLJob, "choose at run time"),
			"Default Rekordbox target: "+firstNonEmpty(definition.DefaultRekordboxTarget, "choose at run time"),
		)
	}
	snapshot, hasSnapshot := m.currentSnapshot()
	if !hasSnapshot {
		summary = append(summary,
			"Snapshot: not created",
			"Press r to explicitly read Apple Music and create the first snapshot.",
		)
		return renderPlanSection("Playlist", summary, width)
	}
	if compact {
		summary = append(summary, fmt.Sprintf("Snapshot: %d tracks · refreshed %s", len(snapshot.Tracks), snapshotAge(snapshot.RefreshedAt)))
	} else {
		summary = append(summary,
			fmt.Sprintf("Snapshot tracks: %d", len(snapshot.Tracks)),
			"Last refreshed: "+snapshot.RefreshedAt.Format("2006-01-02 15:04:05 MST"),
			"Freshness: "+snapshotAge(snapshot.RefreshedAt),
		)
	}
	if m.phase == tuiPlaylistPhaseRefreshing {
		summary = append(summary, "Refreshing from Apple Music… The saved snapshot remains active until the refresh succeeds.")
	}
	return strings.Join([]string{
		renderPlanSection("Playlist", summary, width),
		renderPlanSection("Tracks", m.trackLines(snapshot, width), width),
	}, "\n")
}

func (m tuiPlaylistModel) trackLines(snapshot playlists.Snapshot, width int) []string {
	if len(snapshot.Tracks) == 0 {
		return []string{"Snapshot contains no tracks."}
	}
	lines := []string{"#    LOCAL       ARTIST — TITLE"}
	start, end := visibleWindow(m.trackCursor, len(snapshot.Tracks), 15)
	for idx := start; idx < end; idx++ {
		track := snapshot.Tracks[idx]
		cursor := " "
		if idx == m.trackCursor {
			cursor = ">"
		}
		local := "ready"
		tone := lipgloss.NewStyle().Foreground(lipgloss.Color("78"))
		if track.MissingLocal {
			local = "missing"
			tone = lipgloss.NewStyle().Foreground(lipgloss.Color("179"))
		}
		title := strings.TrimSpace(strings.TrimSpace(track.Artist) + " — " + strings.TrimSpace(track.Title))
		line := fmt.Sprintf("%s %-4d %s  %s", cursor, track.Index, tone.Width(9).Render(local), ansi.Truncate(title, maxInt(16, width-24), ""))
		lines = append(lines, line)
		if idx == m.trackCursor {
			lines = append(lines, truncateForWidth("   "+firstNonEmpty(track.Path, "(no local path)"), width-4))
		}
	}
	return lines
}

func snapshotAge(refreshedAt time.Time) string {
	if refreshedAt.IsZero() {
		return "unknown"
	}
	age := time.Since(refreshedAt)
	switch {
	case age < time.Hour:
		return fmt.Sprintf("%dm old", maxInt(0, int(age.Minutes())))
	case age < 48*time.Hour:
		return fmt.Sprintf("%dh old", int(age.Hours()))
	default:
		return fmt.Sprintf("%dd old", int(age.Hours()/24))
	}
}

func snapshotAgeTone(refreshedAt time.Time) string {
	if time.Since(refreshedAt) > 30*24*time.Hour {
		return "warning"
	}
	return "success"
}
