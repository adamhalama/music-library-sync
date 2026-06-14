package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	workflows "github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/rekordbox/bridge"
	"github.com/jaa/update-downloads/internal/rekordbox/playlistsync"
	"github.com/jaa/update-downloads/internal/rekordbox/pyruntime"
)

type tuiRekordboxPhase string

const (
	tuiRekordboxPhaseLoading   tuiRekordboxPhase = "loading"
	tuiRekordboxPhaseDeps      tuiRekordboxPhase = "deps"
	tuiRekordboxPhaseRepairing tuiRekordboxPhase = "repairing"
	tuiRekordboxPhaseReady     tuiRekordboxPhase = "ready"
	tuiRekordboxPhasePlanning  tuiRekordboxPhase = "planning"
	tuiRekordboxPhaseReview    tuiRekordboxPhase = "review"
	tuiRekordboxPhaseConfirm   tuiRekordboxPhase = "confirm"
	tuiRekordboxPhaseApplying  tuiRekordboxPhase = "applying"
	tuiRekordboxPhaseDone      tuiRekordboxPhase = "done"
	tuiRekordboxPhaseFailed    tuiRekordboxPhase = "failed"
)

type tuiRekordboxJobState struct {
	Label   string
	Options playlistsync.Options
}

type tuiRekordboxModel struct {
	app           *AppContext
	phase         tuiRekordboxPhase
	cfg           config.Config
	cfgErr        error
	jobs          []tuiRekordboxJobState
	jobCursor     int
	dryRun        bool
	scroll        int
	plan          *playlistsync.Plan
	planPath      string
	resolved      playlistsync.ResolvedOptions
	err           error
	backupPath    string
	applyResp     bridge.ApplyResponse
	lastDryRun    bool
	runtimeStatus pyruntime.Status
	runCancel     context.CancelFunc
	width         int
	height        int
}

type tuiRekordboxConfigLoadedMsg struct {
	Config        config.Config
	RuntimeStatus pyruntime.Status
	Err           error
}

type tuiRekordboxDepsDoneMsg struct {
	Runtime pyruntime.Runtime
	Status  pyruntime.Status
	Err     error
}

type tuiRekordboxPlanDoneMsg struct {
	Result workflows.RekordboxPlaylistSyncPlanResult
	Err    error
}

type tuiRekordboxApplyDoneMsg struct {
	Result workflows.RekordboxPlaylistSyncApplyResult
	Err    error
}

func newTUIRekordboxModel(app *AppContext) tuiRekordboxModel {
	dryRun := false
	if app != nil {
		dryRun = app.Opts.DryRun
	}
	return tuiRekordboxModel{app: app, phase: tuiRekordboxPhaseLoading, dryRun: dryRun}
}

func (m tuiRekordboxModel) Init() tea.Cmd {
	return func() tea.Msg {
		cfg, err := loadConfig(m.app)
		if err == nil {
			err = config.ValidateRekordbox(cfg)
		}
		status := pyruntime.Status{}
		if err == nil {
			status = (pyruntime.Resolver{}).Status(context.Background(), pyruntime.Request{Config: cfg})
		}
		return tuiRekordboxConfigLoadedMsg{Config: cfg, RuntimeStatus: status, Err: err}
	}
}

func (m tuiRekordboxModel) Update(msg tea.Msg) (tuiRekordboxModel, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = typed.Width
		m.height = typed.Height
		return m, nil
	case tuiRekordboxConfigLoadedMsg:
		m.cfg = typed.Config
		m.cfgErr = typed.Err
		m.runtimeStatus = typed.RuntimeStatus
		m.jobs = tuiRekordboxJobsForConfig(typed.Config)
		if len(m.jobs) == 0 {
			m.jobs = []tuiRekordboxJobState{tuiDefaultRekordboxJobState()}
		}
		m.phase = tuiRekordboxPhaseReady
		if typed.Err != nil {
			m.phase = tuiRekordboxPhaseFailed
			m.err = typed.Err
		} else if !typed.RuntimeStatus.Healthy {
			m.phase = tuiRekordboxPhaseDeps
		}
		m.resolveSelectedJob()
		if m.err != nil {
			m.phase = tuiRekordboxPhaseFailed
		}
		return m, nil
	case tuiRekordboxDepsDoneMsg:
		m.runCancel = nil
		m.runtimeStatus = typed.Status
		if typed.Err != nil {
			m.phase = tuiRekordboxPhaseFailed
			m.err = typed.Err
			return m, nil
		}
		if !typed.Status.Healthy {
			m.phase = tuiRekordboxPhaseDeps
			m.err = errors.New(typed.Status.Message)
			return m, nil
		}
		m.err = nil
		m.phase = tuiRekordboxPhaseReady
		m.resolveSelectedJob()
		return m, nil
	case tuiRekordboxPlanDoneMsg:
		m.runCancel = nil
		if typed.Err != nil {
			m.phase = tuiRekordboxPhaseFailed
			m.err = typed.Err
			return m, nil
		}
		m.err = nil
		m.plan = &typed.Result.Plan
		m.planPath = typed.Result.PlanPath
		m.resolved = typed.Result.Resolved
		m.scroll = 0
		m.phase = tuiRekordboxPhaseReview
		return m, nil
	case tuiRekordboxApplyDoneMsg:
		m.runCancel = nil
		if typed.Err != nil {
			m.phase = tuiRekordboxPhaseFailed
			m.err = typed.Err
			return m, nil
		}
		m.err = nil
		m.backupPath = typed.Result.BackupPath
		m.applyResp = typed.Result.Response
		m.lastDryRun = typed.Result.DryRun
		m.phase = tuiRekordboxPhaseDone
		return m, nil
	case tea.KeyMsg:
		return m.updateKey(typed)
	default:
		return m, nil
	}
}

func (m tuiRekordboxModel) updateKey(msg tea.KeyMsg) (tuiRekordboxModel, tea.Cmd) {
	key := msg.String()
	if m.isRunning() && (key == "x" || key == "ctrl+c") {
		if m.runCancel != nil {
			m.runCancel()
		}
		return m, nil
	}
	switch m.phase {
	case tuiRekordboxPhaseDeps:
		switch key {
		case "enter":
			return m.startDepsEnsure()
		case "r":
			return m.refreshDepsStatus()
		}
	case tuiRekordboxPhaseReady:
		switch key {
		case "up", "k":
			if m.jobCursor > 0 {
				m.jobCursor--
				m.resolveSelectedJob()
			}
			return m, nil
		case "down", "j":
			if m.jobCursor < len(m.jobs)-1 {
				m.jobCursor++
				m.resolveSelectedJob()
			}
			return m, nil
		case "d":
			m.dryRun = !m.dryRun
			return m, nil
		case "enter":
			return m.startPlan()
		}
	case tuiRekordboxPhaseReview:
		switch key {
		case "up", "k":
			if m.scroll > 0 {
				m.scroll--
			}
			return m, nil
		case "down", "j":
			if m.plan != nil && m.scroll < len(m.plan.Rows)-1 {
				m.scroll++
			}
			return m, nil
		case "d":
			m.dryRun = !m.dryRun
			return m, nil
		case "r":
			return m.startPlan()
		case "enter":
			if m.canApplyPlan() {
				m.phase = tuiRekordboxPhaseConfirm
			}
			return m, nil
		}
	case tuiRekordboxPhaseConfirm:
		switch key {
		case "y":
			return m.startApply()
		case "n", "esc", "enter":
			m.phase = tuiRekordboxPhaseReview
			return m, nil
		}
	case tuiRekordboxPhaseDone, tuiRekordboxPhaseFailed:
		switch key {
		case "r":
			return m.startPlan()
		case "d":
			m.dryRun = !m.dryRun
			return m, nil
		}
	}
	return m, nil
}

func (m tuiRekordboxModel) startPlan() (tuiRekordboxModel, tea.Cmd) {
	options := m.selectedJobOptions()
	ctx, cancel := context.WithCancel(context.Background())
	m.runCancel = cancel
	m.phase = tuiRekordboxPhasePlanning
	m.err = nil
	m.backupPath = ""
	m.applyResp = bridge.ApplyResponse{}
	m.lastDryRun = false
	return m, func() tea.Msg {
		result, err := (workflows.RekordboxPlaylistSyncUseCase{}).Plan(ctx, workflows.RekordboxPlaylistSyncPlanRequest{
			Config:  m.cfg,
			Options: options,
		})
		if errors.Is(err, context.Canceled) {
			err = fmt.Errorf("Rekordbox playlist sync canceled")
		}
		return tuiRekordboxPlanDoneMsg{Result: result, Err: err}
	}
}

func (m tuiRekordboxModel) startDepsEnsure() (tuiRekordboxModel, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	m.runCancel = cancel
	m.phase = tuiRekordboxPhaseRepairing
	m.err = nil
	cfg := m.cfg
	return m, func() tea.Msg {
		rt, err := (pyruntime.Resolver{}).Ensure(ctx, pyruntime.Request{Config: cfg})
		status := (pyruntime.Resolver{}).Status(context.Background(), pyruntime.Request{Config: cfg})
		if errors.Is(err, context.Canceled) {
			err = fmt.Errorf("Rekordbox dependency repair canceled")
		}
		return tuiRekordboxDepsDoneMsg{Runtime: rt, Status: status, Err: err}
	}
}

func (m tuiRekordboxModel) refreshDepsStatus() (tuiRekordboxModel, tea.Cmd) {
	cfg := m.cfg
	return m, func() tea.Msg {
		status := (pyruntime.Resolver{}).Status(context.Background(), pyruntime.Request{Config: cfg})
		return tuiRekordboxDepsDoneMsg{Status: status}
	}
}

func (m tuiRekordboxModel) startApply() (tuiRekordboxModel, tea.Cmd) {
	if m.plan == nil {
		m.phase = tuiRekordboxPhaseFailed
		m.err = fmt.Errorf("no plan available to apply")
		return m, nil
	}
	plan := *m.plan
	ctx, cancel := context.WithCancel(context.Background())
	m.runCancel = cancel
	m.phase = tuiRekordboxPhaseApplying
	m.err = nil
	return m, func() tea.Msg {
		result, err := (workflows.RekordboxPlaylistSyncUseCase{}).Apply(ctx, workflows.RekordboxPlaylistSyncApplyRequest{
			Config: m.cfg,
			Plan:   plan,
			DryRun: m.dryRun,
		})
		if errors.Is(err, context.Canceled) {
			err = fmt.Errorf("Rekordbox playlist sync apply canceled")
		}
		return tuiRekordboxApplyDoneMsg{Result: result, Err: err}
	}
}

func (m *tuiRekordboxModel) resolveSelectedJob() {
	if len(m.jobs) == 0 || m.jobCursor < 0 || m.jobCursor >= len(m.jobs) {
		return
	}
	resolved, err := playlistsync.ResolveOptions(m.cfg, m.jobs[m.jobCursor].Options)
	if err != nil {
		m.err = err
		return
	}
	m.resolved = resolved
	m.err = nil
}

func (m tuiRekordboxModel) selectedJobOptions() playlistsync.Options {
	if len(m.jobs) == 0 || m.jobCursor < 0 || m.jobCursor >= len(m.jobs) {
		return playlistsync.Options{}
	}
	return m.jobs[m.jobCursor].Options
}

func (m tuiRekordboxModel) selectedJobLabel() string {
	if len(m.jobs) == 0 || m.jobCursor < 0 || m.jobCursor >= len(m.jobs) {
		return "default"
	}
	return m.jobs[m.jobCursor].Label
}

func (m tuiRekordboxModel) isRunning() bool {
	return m.phase == tuiRekordboxPhasePlanning || m.phase == tuiRekordboxPhaseApplying || m.phase == tuiRekordboxPhaseRepairing
}

func (m tuiRekordboxModel) allowBack() bool {
	return !m.isRunning() && m.phase != tuiRekordboxPhaseConfirm
}

func (m tuiRekordboxModel) canApplyPlan() bool {
	if m.plan == nil {
		return false
	}
	return playlistsync.ValidatePlanForApply(*m.plan) == nil
}

func (m tuiRekordboxModel) applyBlocker() string {
	if m.plan == nil {
		return "no plan has been generated"
	}
	if err := playlistsync.ValidatePlanForApply(*m.plan); err != nil {
		return err.Error()
	}
	return ""
}

func tuiRekordboxJobsForConfig(cfg config.Config) []tuiRekordboxJobState {
	if cfg.Rekordbox == nil || len(cfg.Rekordbox.PlaylistSync.Jobs) == 0 {
		return []tuiRekordboxJobState{tuiDefaultRekordboxJobState()}
	}
	jobs := make([]tuiRekordboxJobState, 0, len(cfg.Rekordbox.PlaylistSync.Jobs))
	for _, job := range cfg.Rekordbox.PlaylistSync.Jobs {
		label := strings.TrimSpace(job.ID)
		if label == "" {
			label = firstNonEmpty(job.RekordboxPlaylist, playlistsync.DefaultRekordboxPlaylist)
		}
		jobs = append(jobs, tuiRekordboxJobState{
			Label: label,
			Options: playlistsync.Options{
				JobID: job.ID,
			},
		})
	}
	return jobs
}

func tuiDefaultRekordboxJobState() tuiRekordboxJobState {
	return tuiRekordboxJobState{
		Label: "default",
		Options: playlistsync.Options{
			MusicPlaylist:     playlistsync.DefaultMusicPlaylist,
			RekordboxPlaylist: playlistsync.DefaultRekordboxPlaylist,
		},
	}
}
