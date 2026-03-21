package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/output"
)

func newEmptyTUIInteractiveSelectionState() *tuiInteractiveSelectionState {
	return &tuiInteractiveSelectionState{
		selected: map[int]bool{},
		filter:   tuiPlanFilterAll,
	}
}

func (m *tuiSyncModel) resetInteractiveSourceLifecycle() {
	if m == nil {
		return
	}
	m.sourceLifecycle = map[string]tuiInteractiveSourceLifecycle{}
	for _, source := range m.sources {
		m.sourceLifecycle[source.ID] = tuiSourceLifecycleIdle
	}
}

func (m *tuiSyncModel) setInteractiveSourceLifecycle(sourceID string, lifecycle tuiInteractiveSourceLifecycle) {
	if m == nil || strings.TrimSpace(sourceID) == "" {
		return
	}
	if m.sourceLifecycle == nil {
		m.sourceLifecycle = map[string]tuiInteractiveSourceLifecycle{}
	}
	m.sourceLifecycle[sourceID] = lifecycle
}

func newTUIInteractiveSelectionState(req tuiPlanSelectRequestMsg) *tuiInteractiveSelectionState {
	selected := map[int]bool{}
	rows := make([]tuiTrackRowState, 0, len(req.Rows))
	for _, row := range req.Rows {
		if row.Toggleable && row.SelectedByDefault {
			selected[row.Index] = true
		}
		rows = append(rows, tuiTrackRowState{
			SourceID:          req.SourceID,
			SourceLabel:       req.Details.SourceID,
			RemoteID:          row.RemoteID,
			Title:             row.Title,
			Index:             row.Index,
			Toggleable:        row.Toggleable,
			Selected:          row.Toggleable && row.SelectedByDefault,
			PlanStatus:        row.Status,
			RuntimeStatus:     tuiRuntimeStatusFromPlanRow(row),
			StatusLabel:       tuiTrackStatusLabel(tuiRuntimeStatusFromPlanRow(row), 0, false, ""),
			SelectedByDefault: row.SelectedByDefault,
		})
	}
	state := &tuiInteractiveSelectionState{
		sourceID:     req.SourceID,
		rows:         rows,
		details:      req.Details,
		selected:     selected,
		filter:       tuiPlanFilterAll,
		filterCursor: 0,
	}
	state.syncSelectedRows()
	return state
}

func newTUIPlanPromptState(req tuiPlanSelectRequestMsg) *tuiPlanPromptState {
	return &tuiPlanPromptState{
		tuiInteractiveSelectionState: newTUIInteractiveSelectionState(req),
		reply:                        req.Reply,
	}
}

func (s *tuiInteractiveSelectionState) selectedIndices() []int {
	if s == nil {
		return nil
	}
	out := make([]int, 0, len(s.selected))
	for _, row := range s.rows {
		if !row.Toggleable {
			continue
		}
		if row.Selected {
			out = append(out, row.Index)
		}
	}
	sort.Ints(out)
	return out
}

func (s *tuiInteractiveSelectionState) filters() []tuiPlanPromptFilter {
	return []tuiPlanPromptFilter{
		tuiPlanFilterAll,
		tuiPlanFilterSelected,
		tuiPlanFilterMissingNew,
		tuiPlanFilterKnownGap,
		tuiPlanFilterDownloaded,
	}
}

func (s *tuiInteractiveSelectionState) filteredRows() []tuiTrackRowState {
	if s == nil {
		return nil
	}
	rows := make([]tuiTrackRowState, 0, len(s.rows))
	for _, row := range s.rows {
		if s.matchesFilter(row) {
			rows = append(rows, row)
		}
	}
	return rows
}

func (s *tuiInteractiveSelectionState) matchesFilter(row tuiTrackRowState) bool {
	switch s.filter {
	case tuiPlanFilterSelected:
		return row.Toggleable && row.Selected
	case tuiPlanFilterMissingNew:
		return row.RuntimeStatus == tuiTrackStatusQueued && row.PlanStatus == engine.PlanRowMissingNew
	case tuiPlanFilterKnownGap:
		return row.RuntimeStatus == tuiTrackStatusQueued && row.PlanStatus == engine.PlanRowMissingKnownGap
	case tuiPlanFilterDownloaded:
		return row.RuntimeStatus == tuiTrackStatusExisting || row.RuntimeStatus == tuiTrackStatusDownloaded
	default:
		return true
	}
}

func (s *tuiInteractiveSelectionState) visibleRowIndices() []int {
	if s == nil {
		return nil
	}
	indices := make([]int, 0, len(s.rows))
	for idx, row := range s.rows {
		if s.matchesFilter(row) {
			indices = append(indices, idx)
		}
	}
	return indices
}

func (s *tuiInteractiveSelectionState) ensureCursorVisible() {
	if s == nil {
		return
	}
	visible := s.visibleRowIndices()
	if len(visible) == 0 {
		s.cursor = 0
		return
	}
	for _, idx := range visible {
		if idx == s.cursor {
			return
		}
	}
	s.cursor = visible[0]
}

func (s *tuiInteractiveSelectionState) moveCursor(delta int) {
	if s == nil {
		return
	}
	visible := s.visibleRowIndices()
	if len(visible) == 0 {
		s.cursor = 0
		return
	}
	current := 0
	for i, idx := range visible {
		if idx == s.cursor {
			current = i
			break
		}
	}
	current += delta
	if current < 0 {
		current = 0
	}
	if current >= len(visible) {
		current = len(visible) - 1
	}
	s.cursor = visible[current]
}

func (s *tuiInteractiveSelectionState) currentRow() (tuiTrackRowState, bool) {
	if s == nil {
		return tuiTrackRowState{}, false
	}
	visible := s.visibleRowIndices()
	for _, idx := range visible {
		if idx == s.cursor {
			return s.rows[idx], true
		}
	}
	return tuiTrackRowState{}, false
}

func (s *tuiInteractiveSelectionState) filterDisplayLabel(filter tuiPlanPromptFilter) string {
	switch filter {
	case tuiPlanFilterSelected:
		return "Selected"
	case tuiPlanFilterMissingNew:
		return "New"
	case tuiPlanFilterKnownGap:
		return "Known Gap"
	case tuiPlanFilterDownloaded:
		return "Downloaded"
	default:
		return "All"
	}
}

func (s *tuiInteractiveSelectionState) filterLabel() string {
	if s == nil {
		return "all"
	}
	return strings.ToLower(s.filterDisplayLabel(s.filter))
}

func (s *tuiInteractiveSelectionState) focusLabel() string {
	if s == nil {
		return "tracks"
	}
	if s.focusFilters {
		return "filters"
	}
	return "tracks"
}

func (s *tuiInteractiveSelectionState) filterCount(filter tuiPlanPromptFilter) int {
	if s == nil {
		return 0
	}
	original := s.filter
	s.filter = filter
	rows := s.filteredRows()
	s.filter = original
	return len(rows)
}

func (s *tuiInteractiveSelectionState) selectedCount() int {
	if s == nil {
		return 0
	}
	count := 0
	for _, row := range s.rows {
		if row.Toggleable && row.Selected {
			count++
		}
	}
	return count
}

func (s *tuiInteractiveSelectionState) toggleableCount() int {
	if s == nil {
		return 0
	}
	count := 0
	for _, row := range s.rows {
		if row.Toggleable {
			count++
		}
	}
	return count
}

func (s *tuiInteractiveSelectionState) skippedCount() int {
	if s == nil {
		return 0
	}
	count := 0
	for _, row := range s.rows {
		if row.RuntimeStatus == tuiTrackStatusExisting || row.RuntimeStatus == tuiTrackStatusSkipped {
			count++
		}
	}
	return count
}

func (s *tuiInteractiveSelectionState) completedCount() int {
	if s == nil {
		return 0
	}
	count := 0
	for _, row := range s.rows {
		if row.RuntimeStatus == tuiTrackStatusDownloaded {
			count++
		}
	}
	return count
}

func (s *tuiInteractiveSelectionState) failedCount() int {
	if s == nil {
		return 0
	}
	count := 0
	for _, row := range s.rows {
		if row.RuntimeStatus == tuiTrackStatusFailed {
			count++
		}
	}
	return count
}

func (s *tuiInteractiveSelectionState) skippedTrackCount() int {
	if s == nil {
		return 0
	}
	count := 0
	for _, row := range s.rows {
		if row.RuntimeStatus == tuiTrackStatusSkipped {
			count++
		}
	}
	return count
}

func (s *tuiInteractiveSelectionState) setSelected(index int, selected bool) {
	if s == nil {
		return
	}
	if s.selected == nil {
		s.selected = map[int]bool{}
	}
	s.selected[index] = selected
	s.syncSelectedRows()
}

func (s *tuiInteractiveSelectionState) syncSelectedRows() {
	if s == nil {
		return
	}
	for idx := range s.rows {
		row := &s.rows[idx]
		row.Selected = row.Toggleable && s.selected[row.Index]
	}
}

func (s *tuiInteractiveSelectionState) selectionMarker(row tuiTrackRowState) string {
	if !row.Toggleable {
		return "[-]"
	}
	if row.Selected {
		return "[x]"
	}
	return "[ ]"
}

func (s *tuiInteractiveSelectionState) activityCollapsedFor(layout tuiShellLayout) bool {
	if s == nil {
		return layout.Compact
	}
	if s.activityCollapseConfigured {
		return s.activityCollapsed
	}
	return layout.Compact
}

func (s *tuiInteractiveSelectionState) toggleActivity(layout tuiShellLayout) {
	if s == nil {
		return
	}
	if s.activityCollapseConfigured {
		s.activityCollapsed = !s.activityCollapsed
		return
	}
	s.activityCollapsed = !layout.Compact
	s.activityCollapseConfigured = true
}

func (s *tuiInteractiveSelectionState) appendActivity(entry tuiActivityEntry) {
	if s == nil || strings.TrimSpace(entry.Message) == "" {
		return
	}
	s.activity = append(s.activity, entry)
	const maxEntries = 18
	if len(s.activity) > maxEntries {
		s.activity = append([]tuiActivityEntry(nil), s.activity[len(s.activity)-maxEntries:]...)
	}
}

func (s *tuiInteractiveSelectionState) observeTrackEvent(event output.Event) {
	if s == nil || strings.TrimSpace(event.SourceID) != strings.TrimSpace(s.sourceID) {
		return
	}
	row := s.resolveRowForEvent(event)
	if row == nil {
		return
	}
	reason := strings.TrimSpace(tuiDetailString(event.Details, "reason"))
	switch event.Event {
	case output.EventTrackStarted:
		row.RuntimeStatus = tuiTrackStatusDownloading
		row.ProgressKnown = false
		row.ProgressPercent = 0
		row.FailureDetail = ""
	case output.EventTrackProgress:
		row.RuntimeStatus = tuiTrackStatusDownloading
		if percent, ok := tuiDetailFloat(event.Details, "percent"); ok {
			row.ProgressKnown = true
			row.ProgressPercent = percent
		}
	case output.EventTrackDone:
		row.RuntimeStatus = tuiTrackStatusDownloaded
		row.ProgressKnown = true
		row.ProgressPercent = 100
		row.FailureDetail = ""
	case output.EventTrackSkip:
		row.RuntimeStatus = tuiTrackStatusSkipped
		row.ProgressKnown = false
		row.ProgressPercent = 0
		row.FailureDetail = reason
	case output.EventTrackFail:
		row.RuntimeStatus = tuiTrackStatusFailed
		row.ProgressKnown = false
		row.ProgressPercent = 0
		row.FailureDetail = reason
	default:
		return
	}
	row.StatusLabel = tuiTrackStatusLabel(row.RuntimeStatus, row.ProgressPercent, row.ProgressKnown, row.FailureDetail)
}

func (s *tuiInteractiveSelectionState) resolveRowForEvent(event output.Event) *tuiTrackRowState {
	if s == nil {
		return nil
	}
	if idx, ok := tuiDetailInt(event.Details, "index"); ok {
		for i := range s.rows {
			if s.rows[i].Index == idx {
				return &s.rows[i]
			}
		}
	}
	trackID := strings.TrimSpace(tuiDetailString(event.Details, "track_id"))
	if trackID != "" {
		for i := range s.rows {
			if strings.TrimSpace(s.rows[i].RemoteID) == trackID {
				return &s.rows[i]
			}
		}
	}
	trackName := strings.TrimSpace(tuiDetailString(event.Details, "track_name"))
	if trackName != "" {
		for i := range s.rows {
			if strings.TrimSpace(s.rows[i].Title) == trackName {
				return &s.rows[i]
			}
		}
	}
	return nil
}

func tuiRuntimeStatusFromPlanRow(row engine.PlanRow) tuiTrackRuntimeStatus {
	switch row.Status {
	case engine.PlanRowAlreadyDownloaded:
		return tuiTrackStatusExisting
	default:
		return tuiTrackStatusQueued
	}
}

func tuiTrackStatusLabel(status tuiTrackRuntimeStatus, percent float64, progressKnown bool, failureDetail string) string {
	switch status {
	case tuiTrackStatusExisting:
		return "have it"
	case tuiTrackStatusQueued:
		return "pending"
	case tuiTrackStatusDownloading:
		if progressKnown {
			return fmt.Sprintf("downloading %.0f%%", percent)
		}
		return "downloading"
	case tuiTrackStatusDownloaded:
		return "downloaded"
	case tuiTrackStatusSkipped:
		if strings.TrimSpace(failureDetail) != "" {
			return "skipped: " + strings.TrimSpace(failureDetail)
		}
		return "skipped"
	case tuiTrackStatusFailed:
		if strings.TrimSpace(failureDetail) != "" {
			return "failed: " + strings.TrimSpace(failureDetail)
		}
		return "failed"
	default:
		return string(status)
	}
}
