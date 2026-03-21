package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/output"
)

func newEmptyTUIInteractiveSelectionState() *tuiInteractiveSelectionState {
	return &tuiInteractiveSelectionState{
		selected: map[int]bool{},
		filter:   tuiTrackFilterAll,
	}
}

func mergeInteractiveSelectionState(existing, next *tuiInteractiveSelectionState) *tuiInteractiveSelectionState {
	if next == nil {
		return existing
	}
	if existing == nil {
		return next
	}
	if existing.activityCollapseConfigured && !next.activityCollapseConfigured {
		next.activityCollapseConfigured = true
		next.activityCollapsed = existing.activityCollapsed
	}
	if next.sourceID == "" {
		next.sourceID = existing.sourceID
	}
	if next.details.SourceID == "" {
		next.details = existing.details
	}
	return next
}

func (m *tuiSyncModel) resetInteractiveSourceLifecycle() {
	if m == nil {
		return
	}
	if m.interactiveTracker == nil {
		m.interactiveTracker = newTUISyncRunTracker()
	}
	m.interactiveTracker.Reset(m.sources)
}

func (m *tuiSyncModel) setInteractiveSourceLifecycle(sourceID string, lifecycle tuiInteractiveSourceLifecycle) {
	if m == nil || strings.TrimSpace(sourceID) == "" {
		return
	}
	if m.interactiveTracker == nil {
		m.interactiveTracker = newTUISyncRunTracker()
	}
	m.interactiveTracker.SetSourceLifecycle(sourceID, lifecycle)
}

func (m *tuiSyncModel) storeInteractiveSelection(state *tuiInteractiveSelectionState) {
	if m == nil || state == nil {
		return
	}
	sourceID := strings.TrimSpace(state.sourceID)
	if sourceID == "" {
		return
	}
	if m.interactiveSelections == nil {
		m.interactiveSelections = map[string]*tuiInteractiveSelectionState{}
	}
	state = mergeInteractiveSelectionState(m.interactiveSelections[sourceID], state)
	m.interactiveSelections[sourceID] = state
	if strings.TrimSpace(m.interactiveDisplayID) == "" {
		m.interactiveDisplayID = sourceID
	}
}

func (m tuiSyncModel) interactiveSelectionForSource(sourceID string) *tuiInteractiveSelectionState {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || m.interactiveSelections == nil {
		return nil
	}
	return m.interactiveSelections[sourceID]
}

func (m *tuiSyncModel) ensureInteractiveSelectionForSource(sourceID string) *tuiInteractiveSelectionState {
	if m == nil {
		return nil
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil
	}
	if state := m.interactiveSelectionForSource(sourceID); state != nil {
		if state.details.SourceID == "" {
			if source, ok := m.interactiveSourceByID(sourceID); ok {
				state.details = m.planSourceDetailsForSource(source)
			}
		}
		return state
	}
	state := newEmptyTUIInteractiveSelectionState()
	state.sourceID = sourceID
	if source, ok := m.interactiveSourceByID(sourceID); ok {
		state.details = m.planSourceDetailsForSource(source)
	}
	m.storeInteractiveSelection(state)
	return state
}

func (m *tuiSyncModel) ensureInteractiveSelectionForEventSource(sourceID string) *tuiInteractiveSelectionState {
	if m == nil {
		return nil
	}
	sourceID = strings.TrimSpace(sourceID)
	if m.planPrompt != nil && sourceID != "" && strings.TrimSpace(m.planPrompt.sourceID) == sourceID {
		m.storeInteractiveSelection(m.planPrompt.tuiInteractiveSelectionState)
		return m.planPrompt.tuiInteractiveSelectionState
	}
	return m.ensureInteractiveSelectionForSource(sourceID)
}

func (m *tuiSyncModel) confirmInteractiveSelection(sourceID string) {
	if m == nil {
		return
	}
	sourceID = strings.TrimSpace(sourceID)
	if m.planPrompt != nil && sourceID != "" && strings.TrimSpace(m.planPrompt.sourceID) == sourceID {
		m.storeInteractiveSelection(m.planPrompt.tuiInteractiveSelectionState)
	}
	if state := m.ensureInteractiveSelectionForSource(sourceID); state != nil && m.interactiveTracker != nil {
		m.interactiveTracker.ConfirmSelection(state)
	}
}

func (m *tuiSyncModel) setInteractiveDisplaySource(sourceID string) {
	if m == nil {
		return
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	m.interactiveDisplayID = sourceID
}

func (m *tuiSyncModel) syncDisplayedInteractiveSelection() {
	if m == nil || !m.isInteractiveSyncWorkflow() {
		return
	}
	if m.planPrompt != nil {
		m.interactiveDisplayID = strings.TrimSpace(m.planPrompt.sourceID)
		return
	}
	if m.interactiveDisplayID != "" {
		if _, ok := m.interactiveSourceByID(m.interactiveDisplayID); ok {
			return
		}
		if m.interactiveSelectionForSource(m.interactiveDisplayID) != nil {
			return
		}
	}
	if source, ok := m.focusedInteractiveSource(); ok {
		m.interactiveDisplayID = source.ID
		return
	}
	for _, source := range m.sources {
		if m.interactiveSelectionForSource(source.ID) != nil {
			m.interactiveDisplayID = source.ID
			return
		}
	}
	for sourceID := range m.interactiveSelections {
		m.interactiveDisplayID = sourceID
		return
	}
	m.interactiveDisplayID = ""
}

func (m tuiSyncModel) currentInteractiveDisplaySourceID() string {
	if m.planPrompt != nil {
		return strings.TrimSpace(m.planPrompt.sourceID)
	}
	if strings.TrimSpace(m.interactiveDisplayID) != "" {
		return strings.TrimSpace(m.interactiveDisplayID)
	}
	if source, ok := m.focusedInteractiveSource(); ok {
		return source.ID
	}
	for _, source := range m.sources {
		if m.interactiveSelectionForSource(source.ID) != nil {
			return source.ID
		}
	}
	for sourceID := range m.interactiveSelections {
		return sourceID
	}
	return ""
}

func (m tuiSyncModel) interactiveSourceByID(sourceID string) (config.Source, bool) {
	for _, source := range m.sources {
		if source.ID == sourceID {
			return source, true
		}
	}
	return config.Source{}, false
}

func (m *tuiSyncModel) resetStandardSyncState() {
	if m == nil {
		return
	}
	m.standardSummaries = map[string]*tuiStandardSyncSourceSummary{}
	m.standardActiveSource = ""
	m.standardLastSource = ""
}

func (m *tuiSyncModel) ensureStandardSummary(sourceID string) *tuiStandardSyncSourceSummary {
	if m == nil {
		return nil
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil
	}
	if m.standardSummaries == nil {
		m.standardSummaries = map[string]*tuiStandardSyncSourceSummary{}
	}
	if summary, ok := m.standardSummaries[sourceID]; ok {
		return summary
	}
	summary := &tuiStandardSyncSourceSummary{
		SourceID:  sourceID,
		Lifecycle: tuiStandardSyncSourceIdle,
		Planned:   -1,
	}
	m.standardSummaries[sourceID] = summary
	return summary
}

func (m *tuiSyncModel) observeStandardSyncEvent(event output.Event) {
	if m == nil || m.isInteractiveSyncWorkflow() {
		return
	}
	sourceID := strings.TrimSpace(event.SourceID)
	if sourceID != "" {
		m.standardLastSource = sourceID
	}
	if event.Event == output.EventSyncStarted {
		m.resetStandardSyncState()
		return
	}
	summary := m.ensureStandardSummary(sourceID)
	if summary == nil {
		return
	}
	if total, ok := tuiDetailInt(event.Details, "total"); ok && summary.Planned < 0 {
		summary.Planned = total
	}
	if trackName := tuiStandardSyncTrackName(event); trackName != "" {
		summary.LastTrack = trackName
	}
	switch event.Event {
	case output.EventSourcePreflight:
		summary.Lifecycle = tuiStandardSyncSourcePreflight
		if planned, ok := tuiDetailInt(event.Details, "planned_download_count"); ok {
			summary.Planned = planned
		}
	case output.EventSourceStarted:
		summary.Lifecycle = tuiStandardSyncSourceRunning
		m.standardActiveSource = sourceID
	case output.EventSourceFinished:
		summary.Lifecycle = tuiStandardSyncSourceFinished
		if m.standardActiveSource == sourceID {
			m.standardActiveSource = ""
		}
	case output.EventSourceFailed:
		summary.Lifecycle = tuiStandardSyncSourceFailed
		if m.standardActiveSource == sourceID {
			m.standardActiveSource = ""
		}
	case output.EventTrackStarted, output.EventTrackProgress:
		summary.Lifecycle = tuiStandardSyncSourceRunning
		m.standardActiveSource = sourceID
	case output.EventTrackDone:
		summary.Lifecycle = tuiStandardSyncSourceRunning
		summary.Done++
		summary.LastOutcome = "done"
		m.standardActiveSource = sourceID
	case output.EventTrackSkip:
		summary.Lifecycle = tuiStandardSyncSourceRunning
		summary.Skipped++
		summary.LastOutcome = tuiStandardSyncOutcomeLabel("skip", tuiDetailString(event.Details, "reason"))
		m.standardActiveSource = sourceID
	case output.EventTrackFail:
		summary.Lifecycle = tuiStandardSyncSourceRunning
		summary.Failed++
		summary.LastOutcome = tuiStandardSyncOutcomeLabel("fail", tuiDetailString(event.Details, "reason"))
		m.standardActiveSource = sourceID
	}
}

func tuiStandardSyncTrackName(event output.Event) string {
	name := strings.TrimSpace(tuiDetailString(event.Details, "track_name"))
	if name != "" {
		return name
	}
	return strings.TrimSpace(tuiDetailString(event.Details, "track_id"))
}

func tuiStandardSyncOutcomeLabel(kind, reason string) string {
	kind = strings.TrimSpace(kind)
	reason = strings.TrimSpace(reason)
	if kind == "" {
		return ""
	}
	if reason == "" {
		return kind
	}
	return kind + ": " + reason
}

func (m tuiSyncModel) standardSourceSummaryRows() []*tuiStandardSyncSourceSummary {
	if len(m.standardSummaries) == 0 {
		return nil
	}
	rows := make([]*tuiStandardSyncSourceSummary, 0, len(m.standardSummaries))
	seen := map[string]bool{}
	for _, source := range m.sources {
		summary, ok := m.standardSummaries[source.ID]
		if !ok {
			continue
		}
		rows = append(rows, summary)
		seen[source.ID] = true
	}
	extraIDs := make([]string, 0, len(m.standardSummaries))
	for sourceID := range m.standardSummaries {
		if seen[sourceID] {
			continue
		}
		extraIDs = append(extraIDs, sourceID)
	}
	sort.Strings(extraIDs)
	for _, sourceID := range extraIDs {
		rows = append(rows, m.standardSummaries[sourceID])
	}
	return rows
}

func (m tuiSyncModel) standardAggregateCounts() (done, skipped, failed int) {
	for _, summary := range m.standardSourceSummaryRows() {
		done += summary.Done
		skipped += summary.Skipped
		failed += summary.Failed
	}
	return done, skipped, failed
}

func (m tuiSyncModel) standardCurrentSourceID() string {
	if strings.TrimSpace(m.standardActiveSource) != "" {
		return strings.TrimSpace(m.standardActiveSource)
	}
	return strings.TrimSpace(m.standardLastSource)
}

func (m tuiSyncModel) standardActivityCollapsedFor(layout tuiShellLayout) bool {
	if m.standardActivityState.CollapseConfigured {
		return m.standardActivityState.Collapsed
	}
	return layout.Compact
}

func (m *tuiSyncModel) toggleStandardActivity(layout tuiShellLayout) {
	if m == nil {
		return
	}
	if m.standardActivityState.CollapseConfigured {
		m.standardActivityState.Collapsed = !m.standardActivityState.Collapsed
		return
	}
	m.standardActivityState.Collapsed = !layout.Compact
	m.standardActivityState.CollapseConfigured = true
}

func newTUIInteractiveSelectionState(req tuiPlanSelectRequestMsg) *tuiInteractiveSelectionState {
	selected := map[int]bool{}
	rows := make([]tuiPlanTrackRow, 0, len(req.Rows))
	for _, row := range req.Rows {
		if row.Toggleable && row.SelectedByDefault {
			selected[row.Index] = true
		}
		rows = append(rows, tuiPlanTrackRow{
			SourceID:          req.SourceID,
			SourceLabel:       req.Details.SourceID,
			RemoteID:          row.RemoteID,
			Title:             row.Title,
			Index:             row.Index,
			Toggleable:        row.Toggleable,
			PlanStatus:        row.Status,
			PlanClass:         tuiTrackPlanClassFromPlanStatus(row.Status),
			SelectedByDefault: row.SelectedByDefault,
		})
	}
	return &tuiInteractiveSelectionState{
		sourceID:     req.SourceID,
		rows:         rows,
		details:      req.Details,
		selected:     selected,
		filter:       tuiTrackFilterAll,
		filterCursor: 0,
	}
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
		if s.isSelected(row.Index) {
			out = append(out, row.Index)
		}
	}
	sort.Ints(out)
	return out
}

func tuiInteractiveRuntimePhase(phase tuiInteractiveSyncPhase) bool {
	return phase == tuiInteractivePhaseSyncing || phase == tuiInteractivePhaseDone
}

func (s *tuiInteractiveSelectionState) filtersForPhase(phase tuiInteractiveSyncPhase) []tuiPlanPromptFilter {
	if tuiInteractiveRuntimePhase(phase) {
		return []tuiPlanPromptFilter{
			tuiTrackFilterAll,
			tuiTrackFilterInRun,
			tuiTrackFilterRemaining,
			tuiTrackFilterDownloaded,
			tuiTrackFilterSkipped,
			tuiTrackFilterFailed,
			tuiTrackFilterAlreadyHave,
		}
	}
	return []tuiPlanPromptFilter{
		tuiTrackFilterAll,
		tuiTrackFilterWillSync,
		tuiTrackFilterMissingNew,
		tuiTrackFilterKnownGap,
		tuiTrackFilterAlreadyHave,
	}
}

func (s *tuiInteractiveSelectionState) syncFilterForPhase(phase tuiInteractiveSyncPhase) {
	if s == nil {
		return
	}
	filters := s.filtersForPhase(phase)
	if len(filters) == 0 {
		s.filter = tuiTrackFilterAll
		s.filterCursor = 0
		return
	}
	remapped := false
	if !containsTUITrackFilter(filters, s.filter) {
		switch s.filter {
		case tuiTrackFilterAll:
			s.filter = tuiTrackFilterAll
		case tuiTrackFilterAlreadyHave:
			s.filter = tuiTrackFilterAlreadyHave
		default:
			if tuiInteractiveRuntimePhase(phase) {
				s.filter = tuiTrackFilterInRun
			} else {
				s.filter = tuiTrackFilterWillSync
			}
		}
		remapped = true
	}
	if remapped || s.filterCursor < 0 || s.filterCursor >= len(filters) {
		s.filterCursor = indexOfTUITrackFilter(filters, s.filter)
	}
	if s.filterCursor < 0 {
		s.filterCursor = 0
		s.filter = filters[0]
	}
}

func (s *tuiInteractiveSelectionState) filteredRowsForPhase(rows []tuiTrackRowState, phase tuiInteractiveSyncPhase) []tuiTrackRowState {
	if s == nil {
		return nil
	}
	filtered := make([]tuiTrackRowState, 0, len(rows))
	for _, row := range rows {
		if tuiTrackRowMatchesFilter(row, phase, s.filter) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func tuiTrackRowMatchesFilter(row tuiTrackRowState, phase tuiInteractiveSyncPhase, filter tuiPlanPromptFilter) bool {
	if tuiInteractiveRuntimePhase(phase) {
		switch filter {
		case tuiTrackFilterInRun:
			return row.RunScope == tuiTrackRunScopeIncluded
		case tuiTrackFilterRemaining:
			return row.RunScope == tuiTrackRunScopeIncluded &&
				(row.RuntimeStatus == tuiTrackStatusQueued || row.RuntimeStatus == tuiTrackStatusDownloading)
		case tuiTrackFilterDownloaded:
			return row.RunScope == tuiTrackRunScopeIncluded && row.RuntimeStatus == tuiTrackStatusDownloaded
		case tuiTrackFilterSkipped:
			return row.RunScope == tuiTrackRunScopeIncluded && row.RuntimeStatus == tuiTrackStatusSkipped
		case tuiTrackFilterFailed:
			return row.RunScope == tuiTrackRunScopeIncluded && row.RuntimeStatus == tuiTrackStatusFailed
		case tuiTrackFilterAlreadyHave:
			return row.RunScope == tuiTrackRunScopeLocked && row.PlanClass == tuiTrackPlanClassAlreadyHave
		default:
			return filter == tuiTrackFilterAll
		}
	}
	switch filter {
	case tuiTrackFilterWillSync:
		return row.RunScope == tuiTrackRunScopeIncluded
	case tuiTrackFilterMissingNew:
		return row.PlanClass == tuiTrackPlanClassNew
	case tuiTrackFilterKnownGap:
		return row.PlanClass == tuiTrackPlanClassKnownGap
	case tuiTrackFilterAlreadyHave:
		return row.PlanClass == tuiTrackPlanClassAlreadyHave
	default:
		return filter == tuiTrackFilterAll
	}
}

func (s *tuiInteractiveSelectionState) visibleRowIndicesForPhase(rows []tuiTrackRowState, phase tuiInteractiveSyncPhase) []int {
	if s == nil {
		return nil
	}
	indices := make([]int, 0, len(rows))
	for idx, row := range rows {
		if tuiTrackRowMatchesFilter(row, phase, s.filter) {
			indices = append(indices, idx)
		}
	}
	return indices
}

func (s *tuiInteractiveSelectionState) ensureCursorVisible(rows []tuiTrackRowState, phase tuiInteractiveSyncPhase) {
	if s == nil {
		return
	}
	s.syncFilterForPhase(phase)
	visible := s.visibleRowIndicesForPhase(rows, phase)
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

func (s *tuiInteractiveSelectionState) moveCursor(rows []tuiTrackRowState, delta int, phase tuiInteractiveSyncPhase) {
	if s == nil {
		return
	}
	s.syncFilterForPhase(phase)
	visible := s.visibleRowIndicesForPhase(rows, phase)
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

func (s *tuiInteractiveSelectionState) currentRow(rows []tuiTrackRowState, phase tuiInteractiveSyncPhase) (tuiTrackRowState, bool) {
	if s == nil {
		return tuiTrackRowState{}, false
	}
	s.syncFilterForPhase(phase)
	visible := s.visibleRowIndicesForPhase(rows, phase)
	for _, idx := range visible {
		if idx == s.cursor {
			return rows[idx], true
		}
	}
	return tuiTrackRowState{}, false
}

func (s *tuiInteractiveSelectionState) filterDisplayLabel(filter tuiPlanPromptFilter) string {
	switch filter {
	case tuiTrackFilterWillSync:
		return "Will Sync"
	case tuiTrackFilterMissingNew:
		return "New"
	case tuiTrackFilterKnownGap:
		return "Known Gap"
	case tuiTrackFilterAlreadyHave:
		return "Already Have"
	case tuiTrackFilterInRun:
		return "In Run"
	case tuiTrackFilterRemaining:
		return "Remaining"
	case tuiTrackFilterDownloaded:
		return "Downloaded"
	case tuiTrackFilterSkipped:
		return "Skipped"
	case tuiTrackFilterFailed:
		return "Failed"
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

func (s *tuiInteractiveSelectionState) filterCount(rows []tuiTrackRowState, filter tuiPlanPromptFilter, phase tuiInteractiveSyncPhase) int {
	if s == nil {
		return 0
	}
	count := 0
	for _, row := range rows {
		if tuiTrackRowMatchesFilter(row, phase, filter) {
			count++
		}
	}
	return count
}

func (s *tuiInteractiveSelectionState) selectedCount() int {
	if s == nil {
		return 0
	}
	count := 0
	for _, row := range s.rows {
		if row.Toggleable && s.isSelected(row.Index) {
			count++
		}
	}
	return count
}

func (s *tuiInteractiveSelectionState) alreadyHaveCount() int {
	if s == nil {
		return 0
	}
	count := 0
	for _, row := range s.rows {
		if row.PlanClass == tuiTrackPlanClassAlreadyHave {
			count++
		}
	}
	return count
}

func (s *tuiInteractiveSelectionState) newCount() int {
	if s == nil {
		return 0
	}
	count := 0
	for _, row := range s.rows {
		if row.PlanClass == tuiTrackPlanClassNew {
			count++
		}
	}
	return count
}

func (s *tuiInteractiveSelectionState) knownGapCount() int {
	if s == nil {
		return 0
	}
	count := 0
	for _, row := range s.rows {
		if row.PlanClass == tuiTrackPlanClassKnownGap {
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
}

func (s *tuiInteractiveSelectionState) isSelected(index int) bool {
	if s == nil {
		return false
	}
	return s.selected[index]
}

func (m tuiSyncModel) interactiveAggregateCounts() (selected, completed, skipped, failed int, progressPercent float64) {
	if m.interactiveTracker != nil {
		return m.interactiveTracker.AggregateCounts(m.interactivePhase == tuiInteractivePhaseDone && m.runErr == nil)
	}
	return 0, 0, 0, 0, 0
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

func tuiRuntimeStatusFromPlanStatus(status engine.PlanRowStatus) tuiTrackRuntimeStatus {
	switch status {
	case engine.PlanRowAlreadyDownloaded:
		return tuiTrackStatusIdle
	default:
		return tuiTrackStatusQueued
	}
}

func tuiTrackPlanClassFromPlanStatus(status engine.PlanRowStatus) tuiTrackPlanClass {
	switch status {
	case engine.PlanRowMissingKnownGap:
		return tuiTrackPlanClassKnownGap
	case engine.PlanRowAlreadyDownloaded:
		return tuiTrackPlanClassAlreadyHave
	default:
		return tuiTrackPlanClassNew
	}
}

func tuiTrackRunScopeForRow(toggleable, selected bool) tuiTrackRunScope {
	if !toggleable {
		return tuiTrackRunScopeLocked
	}
	if selected {
		return tuiTrackRunScopeIncluded
	}
	return tuiTrackRunScopeExcluded
}

func tuiDisplayRowFromPlanRow(row tuiPlanTrackRow, selected bool) tuiTrackRowState {
	runtimeStatus := tuiRuntimeStatusFromPlanStatus(row.PlanStatus)
	return tuiTrackRowState{
		SourceID:        row.SourceID,
		SourceLabel:     row.SourceLabel,
		RemoteID:        row.RemoteID,
		Title:           row.Title,
		Index:           row.Index,
		Toggleable:      row.Toggleable,
		PlanStatus:      row.PlanStatus,
		PlanClass:       row.PlanClass,
		Selected:        row.Toggleable && selected,
		RunScope:        tuiTrackRunScopeForRow(row.Toggleable, selected),
		RuntimeStatus:   runtimeStatus,
		StatusLabel:     tuiTrackStatusLabel(runtimeStatus, 0, false, ""),
		FailureDetail:   "",
		ProgressKnown:   false,
		ProgressPercent: 0,
	}
}

func containsTUITrackFilter(filters []tuiPlanPromptFilter, filter tuiPlanPromptFilter) bool {
	return indexOfTUITrackFilter(filters, filter) >= 0
}

func indexOfTUITrackFilter(filters []tuiPlanPromptFilter, filter tuiPlanPromptFilter) int {
	for idx, candidate := range filters {
		if candidate == filter {
			return idx
		}
	}
	return -1
}

func tuiTrackStatusLabel(status tuiTrackRuntimeStatus, percent float64, progressKnown bool, failureDetail string) string {
	switch status {
	case tuiTrackStatusIdle:
		return "idle"
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
