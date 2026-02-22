package compact

import "sync"

type StateMachine struct {
	mu    sync.Mutex
	state ProgressModel
}

func NewStateMachine() *StateMachine {
	m := &StateMachine{}
	m.Reset()
	return m
}

func (m *StateMachine) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = ProgressModel{
		Source: SourceProgress{Lifecycle: SourceLifecycleIdle},
		Track:  TrackProgress{Lifecycle: TrackLifecycleIdle},
	}
}

func (m *StateMachine) BeginSource(sourceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Source.ID = sourceID
	m.state.Source.Lifecycle = SourceLifecycleRunning
}

func (m *StateMachine) SetPlanningSource(sourceID string, plannedTotal int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Source.ID = sourceID
	m.state.Source.Lifecycle = SourceLifecyclePlanning
	m.state.Source.PlannedTotal = clampCount(plannedTotal)
	m.state.Source.ItemTotal = 0
	m.state.Source.ItemIndex = 0
	m.state.Source.Completed = 0
	m.state.Global = GlobalProgress{
		Total:     m.effectiveTotalLocked(),
		Completed: m.state.Source.Completed,
	}
}

func (m *StateMachine) FinishSource(sourceID string, failed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Source.ID = sourceID
	if failed {
		m.state.Source.Lifecycle = SourceLifecycleFailed
	} else {
		m.state.Source.Lifecycle = SourceLifecycleFinished
	}
}

func (m *StateMachine) SetItemTotal(total int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Source.ItemTotal = clampCount(total)
	m.state.Source.ItemIndex = 0
	m.state.Source.Completed = 0
	m.state.Global = GlobalProgress{
		Total:     m.effectiveTotalLocked(),
		Completed: m.state.Source.Completed,
	}
}

func (m *StateMachine) SetItemIndex(index int, total int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Source.ItemIndex = clampCount(index)
	if total >= 0 {
		m.state.Source.ItemTotal = clampCount(total)
	}
	m.state.Global = GlobalProgress{
		Total:     m.effectiveTotalLocked(),
		Completed: m.state.Source.Completed,
	}
}

func (m *StateMachine) CompleteTrack() {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := m.effectiveTotalLocked()
	if total > 0 && m.state.Source.Completed < total {
		m.state.Source.Completed++
	}
	m.state.Global = GlobalProgress{
		Total:     total,
		Completed: m.state.Source.Completed,
	}
}

func (m *StateMachine) SetTrack(name string, lifecycle TrackLifecycle, progressPercent float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Track = TrackProgress{
		Name:            name,
		Lifecycle:       lifecycle,
		ProgressPercent: ClampPercent(progressPercent),
	}
}

func (m *StateMachine) EffectiveTotal() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.effectiveTotalLocked()
}

func (m *StateMachine) Completed() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.Source.Completed
}

func (m *StateMachine) CurrentIndex() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := m.effectiveTotalLocked()
	if total <= 0 {
		return 0
	}
	index := m.state.Source.Completed + 1
	if index < 1 {
		index = 1
	}
	if index > total {
		index = total
	}
	return index
}

func (m *StateMachine) GlobalProgressPercent(partial float64) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := m.effectiveTotalLocked()
	if total <= 0 {
		return 0
	}
	done := m.state.Source.Completed
	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}
	partial = ClampPercent(partial*100) / 100.0
	return ClampPercent(((float64(done) + partial) / float64(total)) * 100.0)
}

func (m *StateMachine) Snapshot() ProgressModel {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

func (m *StateMachine) effectiveTotalLocked() int {
	if m.state.Source.PlannedTotal > 0 {
		return m.state.Source.PlannedTotal
	}
	return m.state.Source.ItemTotal
}

func clampCount(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
