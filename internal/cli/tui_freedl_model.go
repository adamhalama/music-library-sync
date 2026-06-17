package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jaa/update-downloads/internal/adapters/deemix"
	"github.com/jaa/update-downloads/internal/adapters/scdl"
	"github.com/jaa/update-downloads/internal/adapters/scdlfreedl"
	"github.com/jaa/update-downloads/internal/adapters/spotdl"
	workflows "github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/freedl"
	"github.com/jaa/update-downloads/internal/output"
)

type tuiFreeDLPhase string

const (
	tuiFreeDLPhaseLoading   tuiFreeDLPhase = "loading"
	tuiFreeDLPhaseSelect    tuiFreeDLPhase = "select"
	tuiFreeDLPhasePlanning  tuiFreeDLPhase = "planning"
	tuiFreeDLPhasePlan      tuiFreeDLPhase = "plan"
	tuiFreeDLPhaseCapturing tuiFreeDLPhase = "capturing"
	tuiFreeDLPhasePromote   tuiFreeDLPhase = "promote"
	tuiFreeDLPhaseConfirm   tuiFreeDLPhase = "confirm"
	tuiFreeDLPhaseConfig    tuiFreeDLPhase = "config"
	tuiFreeDLPhaseApplying  tuiFreeDLPhase = "applying"
	tuiFreeDLPhaseDone      tuiFreeDLPhase = "done"
	tuiFreeDLPhaseFailed    tuiFreeDLPhase = "failed"
)

type tuiFreeDLConfigPane string

const (
	tuiFreeDLConfigPaneList tuiFreeDLConfigPane = "list"
	tuiFreeDLConfigPaneForm tuiFreeDLConfigPane = "form"
)

type tuiFreeDLConfigStep string

const (
	tuiFreeDLConfigStepEdit   tuiFreeDLConfigStep = "edit"
	tuiFreeDLConfigStepReview tuiFreeDLConfigStep = "review"
	tuiFreeDLConfigStepSave   tuiFreeDLConfigStep = "save"
)

type tuiFreeDLModel struct {
	app                *AppContext
	width              int
	height             int
	phase              tuiFreeDLPhase
	mainConfig         config.Config
	cfg                freedl.Config
	jobs               []freedl.Job
	jobCursor          int
	rowCursor          int
	promoCursor        int
	formatCursor       int
	planLimit          int
	formats            []string
	plan               *freedl.CapturePlan
	promoPlan          *freedl.PromotionPlan
	result             *freedl.PromotionResult
	captureResult      *engine.SyncResult
	err                error
	cancel             context.CancelFunc
	limitInput         string
	limitInputErr      string
	limitEditing       bool
	planningStages     map[string]string
	selectionOverrides map[string]bool

	configStep           tuiFreeDLConfigStep
	configPane           tuiFreeDLConfigPane
	configPath           string
	configFileExists     bool
	configProjectWarning string
	configCfg            freedl.Config
	configDirty          bool
	configJobCursor      int
	configFieldCursor    int
	configReviewCursor   int
	configValidation     []string
	configErr            error
	configSaveErr        error
	configSaved          bool
	configEdit           *tuiConfigEditorInlineEditState
	configDeleteConfirm  bool
	configDiscardConfirm bool
}

type tuiFreeDLLoadedMsg struct {
	Main config.Config
	Cfg  freedl.Config
	Jobs []freedl.Job
	Err  error
}

type tuiFreeDLPlanMsg struct {
	Plan freedl.CapturePlan
	Err  error
}

type tuiFreeDLPlanEventMsg struct {
	Event  freedl.CapturePlanEvent
	Events <-chan freedl.CapturePlanEvent
}

type tuiFreeDLCaptureMsg struct {
	Result engine.SyncResult
	Err    error
}

type tuiFreeDLPromotionPlanMsg struct {
	Plan freedl.PromotionPlan
	Err  error
}

type tuiFreeDLApplyMsg struct {
	Result freedl.PromotionResult
}

func newTUIFreeDLModel(app *AppContext) tuiFreeDLModel {
	return tuiFreeDLModel{
		app:     app,
		phase:   tuiFreeDLPhaseLoading,
		formats: []string{freedl.TargetAuto, freedl.TargetMP3320, freedl.TargetAAC256, freedl.TargetWAV},
	}
}

func (m tuiFreeDLModel) Init() tea.Cmd {
	return func() tea.Msg {
		mainCfg, err := loadConfig(m.app)
		if err != nil {
			return tuiFreeDLLoadedMsg{Err: err}
		}
		cfg, err := freedl.Load(freedl.LoadOptions{
			ExplicitPath: m.app.Opts.FreeDLConfigPath,
			MainConfig:   mainCfg,
		})
		if err == nil {
			err = freedl.Validate(cfg)
		}
		if err != nil {
			return tuiFreeDLLoadedMsg{Err: err}
		}
		return tuiFreeDLLoadedMsg{Main: mainCfg, Cfg: cfg, Jobs: freedl.EnabledJobs(cfg)}
	}
}

func (m tuiFreeDLModel) Update(msg tea.Msg) (tuiFreeDLModel, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = typed.Width
		m.height = typed.Height
	case tuiFreeDLLoadedMsg:
		m.mainConfig = typed.Main
		m.cfg = typed.Cfg
		m.jobs = typed.Jobs
		m.err = typed.Err
		if typed.Err != nil {
			m.phase = tuiFreeDLPhaseFailed
		} else {
			m.planLimit = m.jobPlanLimit()
			if len(typed.Jobs) == 0 {
				m.openConfigEditor(true)
			} else {
				m.phase = tuiFreeDLPhaseSelect
			}
		}
	case tuiFreeDLPlanMsg:
		m.cancel = nil
		if typed.Err != nil {
			m.err = typed.Err
			m.phase = tuiFreeDLPhaseFailed
			return m, nil
		}
		m.plan = &typed.Plan
		m.phase = tuiFreeDLPhasePlan
		m.rowCursor = 0
	case tuiFreeDLPlanEventMsg:
		return m.updatePlanEvent(typed)
	case tuiFreeDLCaptureMsg:
		m.cancel = nil
		if typed.Err != nil {
			m.err = typed.Err
			m.phase = tuiFreeDLPhaseFailed
			return m, nil
		}
		m.captureResult = &typed.Result
		m.phase = tuiFreeDLPhasePromote
		return m, m.buildPromotionPlanCmd()
	case tuiFreeDLPromotionPlanMsg:
		if typed.Err != nil {
			m.err = typed.Err
			m.phase = tuiFreeDLPhaseFailed
			return m, nil
		}
		m.promoPlan = &typed.Plan
		m.phase = tuiFreeDLPhasePromote
		m.promoCursor = 0
	case tuiFreeDLApplyMsg:
		m.cancel = nil
		m.result = &typed.Result
		m.phase = tuiFreeDLPhaseDone
	case tea.KeyMsg:
		return m.updateKey(typed)
	}
	return m, nil
}

func (m tuiFreeDLModel) updateKey(msg tea.KeyMsg) (tuiFreeDLModel, tea.Cmd) {
	if m.phase == tuiFreeDLPhaseConfig {
		return m.updateConfigKey(msg)
	}
	if m.limitEditing {
		switch msg.String() {
		case "enter":
			limit, err := parsePlanLimitInput(m.limitInput)
			if err != nil {
				m.limitInputErr = err.Error()
				return m, nil
			}
			m.planLimit = limit
			m.limitInput = ""
			m.limitInputErr = ""
			m.limitEditing = false
			return m, nil
		case "esc":
			m.limitInput = ""
			m.limitInputErr = ""
			m.limitEditing = false
			return m, nil
		case "backspace", "ctrl+h":
			if len(m.limitInput) > 0 {
				m.limitInput = m.limitInput[:len(m.limitInput)-1]
			}
			m.limitInputErr = ""
			return m, nil
		default:
			if len(msg.Runes) == 1 && msg.Runes[0] >= '0' && msg.Runes[0] <= '9' {
				m.limitInput += string(msg.Runes[0])
				m.limitInputErr = ""
			}
			return m, nil
		}
	}
	switch m.phase {
	case tuiFreeDLPhaseSelect:
		switch msg.String() {
		case "up", "k":
			if m.jobCursor > 0 {
				m.jobCursor--
				m.planLimit = m.jobPlanLimit()
			}
		case "down", "j":
			if m.jobCursor < len(m.jobs)-1 {
				m.jobCursor++
				m.planLimit = m.jobPlanLimit()
			}
		case "l":
			m.limitEditing = true
			m.limitInput = ""
			m.limitInputErr = ""
		case "]":
			if m.planLimit == 0 {
				m.planLimit = tuiDefaultPlanLimit
			} else {
				m.planLimit++
			}
		case "[":
			if m.planLimit == 0 {
				m.planLimit = tuiDefaultPlanLimit
			} else if m.planLimit > tuiMinPlanLimit {
				m.planLimit--
			}
		case "u":
			if m.planLimit == 0 {
				m.planLimit = m.jobPlanLimit()
			} else {
				m.planLimit = 0
			}
		case "e":
			m.openConfigEditor(false)
		case "a":
			m.openConfigEditor(true)
		case "enter":
			return m.startPlan()
		}
	case tuiFreeDLPhasePlanning, tuiFreeDLPhasePlan:
		switch msg.String() {
		case "x", "ctrl+c":
			if m.phase == tuiFreeDLPhasePlanning && m.cancel != nil {
				m.cancel()
			}
		case "up", "k":
			if m.rowCursor > 0 {
				m.rowCursor--
			}
		case "down", "j":
			if m.plan != nil && m.rowCursor < len(m.plan.Rows)-1 {
				m.rowCursor++
			}
		case " ":
			if m.plan != nil && m.rowCursor >= 0 && m.rowCursor < len(m.plan.Rows) && m.plan.Rows[m.rowCursor].Selectable {
				m.plan.Rows[m.rowCursor].Selected = !m.plan.Rows[m.rowCursor].Selected
				if m.selectionOverrides == nil {
					m.selectionOverrides = map[string]bool{}
				}
				m.selectionOverrides[m.plan.Rows[m.rowCursor].RemoteID] = m.plan.Rows[m.rowCursor].Selected
			}
		case "c", "enter":
			if m.phase == tuiFreeDLPhasePlan {
				return m.startCapture()
			}
		case "r":
			return m.startPlan()
		}
	case tuiFreeDLPhaseCapturing, tuiFreeDLPhaseApplying:
		if (msg.String() == "x" || msg.String() == "ctrl+c") && m.cancel != nil {
			m.cancel()
		}
	case tuiFreeDLPhasePromote:
		switch msg.String() {
		case "up", "k":
			if m.promoCursor > 0 {
				m.promoCursor--
			}
		case "down", "j":
			if m.promoPlan != nil && m.promoCursor < len(m.promoPlan.Rows)-1 {
				m.promoCursor++
			}
		case " ":
			if m.promoPlan != nil && m.promoCursor >= 0 && m.promoCursor < len(m.promoPlan.Rows) && m.promoPlan.Rows[m.promoCursor].Action != freedl.PromotionSkip {
				m.promoPlan.Rows[m.promoCursor].Selected = !m.promoPlan.Rows[m.promoCursor].Selected
			}
		case "t":
			m.formatCursor = (m.formatCursor + 1) % len(m.formats)
			return m, m.buildPromotionPlanCmd()
		case "enter":
			m.phase = tuiFreeDLPhaseConfirm
		}
	case tuiFreeDLPhaseConfirm:
		switch msg.String() {
		case "y", "enter":
			return m.startApply()
		case "n", "esc":
			m.phase = tuiFreeDLPhasePromote
		}
	case tuiFreeDLPhaseDone, tuiFreeDLPhaseFailed:
		if msg.String() == "r" {
			return m.startPlan()
		}
	}
	return m, nil
}

func (m tuiFreeDLModel) startPlan() (tuiFreeDLModel, tea.Cmd) {
	if len(m.jobs) == 0 {
		m.err = fmt.Errorf("no enabled SoundCloud Free DL jobs configured")
		m.phase = tuiFreeDLPhaseFailed
		return m, nil
	}
	job := m.jobs[m.jobCursor]
	job.PlanLimit = m.planLimit
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.phase = tuiFreeDLPhasePlanning
	m.err = nil
	m.plan = nil
	m.planningStages = map[string]string{}
	m.selectionOverrides = map[string]bool{}
	events := freedl.Service{}.BuildCapturePlanProgress(ctx, m.mainConfig, job)
	return m, waitFreeDLPlanEvent(events)
}

func waitFreeDLPlanEvent(events <-chan freedl.CapturePlanEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return tuiFreeDLPlanEventMsg{Event: freedl.CapturePlanEvent{Kind: freedl.CapturePlanEventFailed, Err: fmt.Errorf("planning stopped before completion")}}
		}
		return tuiFreeDLPlanEventMsg{Event: event, Events: events}
	}
}

func (m tuiFreeDLModel) updatePlanEvent(msg tuiFreeDLPlanEventMsg) (tuiFreeDLModel, tea.Cmd) {
	next := func() tea.Cmd {
		if msg.Events == nil {
			return nil
		}
		return waitFreeDLPlanEvent(msg.Events)
	}
	switch msg.Event.Kind {
	case freedl.CapturePlanEventStarted:
		plan := msg.Event.Plan
		m.plan = &plan
		return m, next()
	case freedl.CapturePlanEventStage:
		if m.planningStages == nil {
			m.planningStages = map[string]string{}
		}
		label := msg.Event.Status
		if msg.Event.Detail != "" {
			label += ": " + msg.Event.Detail
		}
		m.planningStages[msg.Event.Stage] = label
		return m, next()
	case freedl.CapturePlanEventRow:
		m.mergePlanRow(msg.Event.Row)
		return m, next()
	case freedl.CapturePlanEventDone:
		m.cancel = nil
		plan := msg.Event.Plan
		for idx := range plan.Rows {
			if override, ok := m.selectionOverrides[plan.Rows[idx].RemoteID]; ok {
				plan.Rows[idx].Selected = override && plan.Rows[idx].Selectable
			}
		}
		m.plan = &plan
		m.phase = tuiFreeDLPhasePlan
		m.rowCursor = clampIndex(m.rowCursor, len(plan.Rows))
		return m, nil
	case freedl.CapturePlanEventFailed:
		m.cancel = nil
		m.err = msg.Event.Err
		if m.err == nil {
			m.err = fmt.Errorf("planning failed")
		}
		m.phase = tuiFreeDLPhaseFailed
		return m, nil
	default:
		return m, next()
	}
}

func (m *tuiFreeDLModel) mergePlanRow(row freedl.PlanRow) {
	if m.plan == nil {
		plan := freedl.CapturePlan{Rows: []freedl.PlanRow{}}
		m.plan = &plan
	}
	if override, ok := m.selectionOverrides[row.RemoteID]; ok {
		row.Selected = override && row.Selectable
	}
	for idx := range m.plan.Rows {
		if m.plan.Rows[idx].RemoteID == row.RemoteID {
			m.plan.Rows[idx] = row
			m.rowCursor = clampIndex(m.rowCursor, len(m.plan.Rows))
			return
		}
	}
	m.plan.Rows = append(m.plan.Rows, row)
	m.rowCursor = clampIndex(m.rowCursor, len(m.plan.Rows))
}

func clampIndex(current, length int) int {
	if length <= 0 {
		return 0
	}
	if current < 0 {
		return 0
	}
	if current >= length {
		return length - 1
	}
	return current
}

func (m tuiFreeDLModel) startCapture() (tuiFreeDLModel, tea.Cmd) {
	if m.plan == nil {
		return m, nil
	}
	selected := map[string]struct{}{}
	for _, row := range m.plan.Rows {
		if row.Selected && row.Selectable {
			selected[row.RemoteID] = struct{}{}
		}
	}
	if len(selected) == 0 {
		m.err = fmt.Errorf("select at least one Free DL row before capture")
		return m, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.phase = tuiFreeDLPhaseCapturing
	return m, func() tea.Msg {
		result, err := runFreeDLCapture(ctx, m.app, m.mainConfig, *m.plan, selected)
		return tuiFreeDLCaptureMsg{Result: result, Err: err}
	}
}

func (m tuiFreeDLModel) buildPromotionPlanCmd() tea.Cmd {
	if m.plan == nil {
		return nil
	}
	format := m.formats[m.formatCursor]
	job := m.plan.Job
	return func() tea.Msg {
		plan, err := freedl.Service{}.BuildPromotionPlan(context.Background(), job, m.plan.RunID, format)
		return tuiFreeDLPromotionPlanMsg{Plan: plan, Err: err}
	}
}

func (m tuiFreeDLModel) startApply() (tuiFreeDLModel, tea.Cmd) {
	if m.promoPlan == nil {
		return m, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.phase = tuiFreeDLPhaseApplying
	plan := *m.promoPlan
	return m, func() tea.Msg {
		result := freedl.Service{}.ApplyPromotionPlan(ctx, plan)
		return tuiFreeDLApplyMsg{Result: result}
	}
}

func (m tuiFreeDLModel) View() string {
	return "SoundCloud Free DL"
}

func (m tuiFreeDLModel) allowBack() bool {
	return !m.limitEditing && m.phase != tuiFreeDLPhasePlanning && m.phase != tuiFreeDLPhaseCapturing && m.phase != tuiFreeDLPhaseApplying && m.phase != tuiFreeDLPhaseConfirm && m.phase != tuiFreeDLPhaseConfig
}

func (m tuiFreeDLModel) jobPlanLimit() int {
	if len(m.jobs) == 0 || m.jobCursor < 0 || m.jobCursor >= len(m.jobs) {
		return freedl.DefaultPlanLimit
	}
	if m.jobs[m.jobCursor].PlanLimit > 0 {
		return m.jobs[m.jobCursor].PlanLimit
	}
	return freedl.DefaultPlanLimit
}

func freeDLDownloadOrder(value string) engine.DownloadOrder {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(engine.DownloadOrderNewestFirst):
		return engine.DownloadOrderNewestFirst
	default:
		return engine.DownloadOrderOldestFirst
	}
}

func runFreeDLCapture(ctx context.Context, app *AppContext, main config.Config, plan freedl.CapturePlan, selected map[string]struct{}) (engine.SyncResult, error) {
	downloadsDir := filepath.Join(plan.BufferRoot, "downloads")
	if err := osMkdirAll(downloadsDir, 0o755); err != nil {
		return engine.SyncResult{}, err
	}
	runCfg := main
	runCfg.Defaults.StateDir = plan.LogDir
	runCfg.Defaults.ArchiveFile = "archive.txt"
	runCfg.Sources = []config.Source{{
		ID:        plan.Job.ID,
		Type:      config.SourceTypeSoundCloud,
		Enabled:   true,
		TargetDir: downloadsDir,
		URL:       plan.Job.SourceURL,
		StateFile: "capture.sync.scdl",
		Adapter:   config.AdapterSpec{Kind: "scdl-freedl"},
		Sync: config.SyncPolicy{
			BreakOnExisting: boolPtr(true),
			AskOnExisting:   boolPtr(false),
			LocalIndexCache: boolPtr(false),
		},
	}}
	emitter := output.NewHumanEmitter(io.Discard, io.Discard, true, false)
	useCase := workflows.SyncUseCase{
		Registry: map[string]engine.Adapter{
			"deemix":      deemix.New(),
			"spotdl":      spotdl.New(),
			"scdl":        scdl.New(),
			"scdl-freedl": scdlfreedl.New(),
		},
		Runner:  engine.NewSubprocessRunner(app.IO.In, io.Discard, io.Discard),
		Emitter: emitter,
	}
	interaction := freeDLCaptureInteraction{selected: selected, order: freeDLDownloadOrder(plan.Job.DownloadOrder)}
	result, err := useCase.Run(ctx, runCfg, workflows.SyncRequest{
		SourceIDs:   []string{plan.Job.ID},
		Plan:        true,
		PlanLimit:   plan.Job.PlanLimit,
		AllowPrompt: false,
		TrackStatus: engine.TrackStatusNone,
	}, interaction)
	if err == nil {
		_ = freedl.WriteJSON(filepath.Join(plan.LogDir, "capture-result.json"), result)
	}
	return result, err
}

type freeDLCaptureInteraction struct {
	selected map[string]struct{}
	order    engine.DownloadOrder
}

func (i freeDLCaptureInteraction) Confirm(prompt string, defaultYes bool) (bool, error) {
	return defaultYes, nil
}

func (i freeDLCaptureInteraction) Input(prompt string) (string, error) {
	return "", nil
}

func (i freeDLCaptureInteraction) SelectRows(sourceID string, rows []engine.PlanRow) (engine.PlanSelectionResult, error) {
	indices := []int{}
	for _, row := range rows {
		if _, ok := i.selected[row.RemoteID]; ok && row.Toggleable {
			indices = append(indices, row.Index)
		}
	}
	order := i.order
	if order == "" {
		order = engine.DownloadOrderOldestFirst
	}
	manifest, err := engine.BuildExecutionManifest(sourceID, rows, indices, order)
	if err != nil {
		return engine.PlanSelectionResult{}, err
	}
	return engine.PlanSelectionResult{Manifest: manifest}, nil
}

var osMkdirAll = func(path string, perm uint32) error {
	return os.MkdirAll(path, os.FileMode(perm))
}

func boolPtr(v bool) *bool { return &v }

func qualityLabel(q freedl.Quality) string {
	if q.Error != "" {
		return "unknown"
	}
	codec := strings.TrimSpace(q.Codec)
	if codec == "" {
		codec = "audio"
	}
	if q.Lossless {
		return codec + " lossless"
	}
	if q.EffectiveBitrate > 0 {
		return fmt.Sprintf("%s %dk", codec, q.EffectiveBitrate/1000)
	}
	return codec
}

func freeDLStatusLabel(row freedl.PlanRow) string {
	if row.FreeDLProbe.Status == "" {
		return "not checked"
	}
	if row.FreeDLProbe.Host != "" {
		return string(row.FreeDLProbe.Status) + " " + row.FreeDLProbe.Host
	}
	return string(row.FreeDLProbe.Status)
}

func selectedCaptureCount(plan *freedl.CapturePlan) int {
	if plan == nil {
		return 0
	}
	count := 0
	for _, row := range plan.Rows {
		if row.Selected && row.Selectable {
			count++
		}
	}
	return count
}

func selectableCaptureCount(plan *freedl.CapturePlan) int {
	if plan == nil {
		return 0
	}
	count := 0
	for _, row := range plan.Rows {
		if row.Selectable {
			count++
		}
	}
	return count
}

func selectedPromotionCount(plan *freedl.PromotionPlan) int {
	if plan == nil {
		return 0
	}
	count := 0
	for _, row := range plan.Rows {
		if row.Selected && row.Action != freedl.PromotionSkip {
			count++
		}
	}
	return count
}
