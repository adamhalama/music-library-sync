package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	workflows "github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/rekordbox/bridge"
	"github.com/jaa/update-downloads/internal/rekordbox/music"
	"github.com/jaa/update-downloads/internal/rekordbox/playlistsync"
	"github.com/jaa/update-downloads/internal/rekordbox/pyruntime"
	"github.com/jaa/update-downloads/internal/rekordbox/syncconfig"
)

type tuiRekordboxPhase string

const (
	tuiRekordboxPhaseLoading          tuiRekordboxPhase = "loading"
	tuiRekordboxPhaseDeps             tuiRekordboxPhase = "deps"
	tuiRekordboxPhaseRepairing        tuiRekordboxPhase = "repairing"
	tuiRekordboxPhaseReady            tuiRekordboxPhase = "ready"
	tuiRekordboxPhaseSetupDiscovering tuiRekordboxPhase = "setup-discovering"
	tuiRekordboxPhaseSetupList        tuiRekordboxPhase = "setup-list"
	tuiRekordboxPhaseSetupSource      tuiRekordboxPhase = "setup-source"
	tuiRekordboxPhaseSetupTarget      tuiRekordboxPhase = "setup-target"
	tuiRekordboxPhaseSetupReview      tuiRekordboxPhase = "setup-review"
	tuiRekordboxPhaseSetupSaving      tuiRekordboxPhase = "setup-saving"
	tuiRekordboxPhasePlanning         tuiRekordboxPhase = "planning"
	tuiRekordboxPhaseReview           tuiRekordboxPhase = "review"
	tuiRekordboxPhaseConfirm          tuiRekordboxPhase = "confirm"
	tuiRekordboxPhaseApplying         tuiRekordboxPhase = "applying"
	tuiRekordboxPhaseDone             tuiRekordboxPhase = "done"
	tuiRekordboxPhaseFailed           tuiRekordboxPhase = "failed"
)

type tuiRekordboxJobState struct {
	Label   string
	Options playlistsync.Options
}

type tuiRekordboxSetupState struct {
	ConfigPath    string
	ConfigKind    string
	ConfigExists  bool
	MusicItems    []music.Playlist
	RBInspect     bridge.InspectResponse
	DiscoverErr   error
	SaveErr       error
	Saved         bool
	EditIndex     int
	Cursor        int
	SourceCursor  int
	TargetCursor  int
	Mapping       syncconfig.FolderMapping
	DeleteConfirm bool
	Input         *tuiConfigEditorInlineEditState
	InputField    string
}

type tuiRekordboxModel struct {
	app            *AppContext
	phase          tuiRekordboxPhase
	cfg            config.Config
	rbCfg          syncconfig.Config
	cfgErr         error
	jobs           []tuiRekordboxJobState
	jobCursor      int
	dryRun         bool
	scroll         int
	plan           *playlistsync.Plan
	planPath       string
	resolved       playlistsync.ResolvedOptions
	err            error
	backupPath     string
	applyResp      bridge.ApplyResponse
	applyBatchResp bridge.ApplyBatchResponse
	lastDryRun     bool
	runtimeStatus  pyruntime.Status
	setup          tuiRekordboxSetupState
	runCancel      context.CancelFunc
	width          int
	height         int
}

type tuiRekordboxConfigLoadedMsg struct {
	Config        config.Config
	SyncConfig    syncconfig.Config
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

type tuiRekordboxSetupDiscoveredMsg struct {
	ConfigPath   string
	ConfigKind   string
	ConfigExists bool
	MusicItems   []music.Playlist
	RBInspect    bridge.InspectResponse
	Err          error
}

type tuiRekordboxSetupSavedMsg struct {
	Config       syncconfig.Config
	ConfigPath   string
	ConfigExists bool
	Err          error
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
		rbCfg := syncconfig.Config{}
		if err == nil {
			rbCfg, err = loadRekordboxSyncConfig(m.app, cfg)
			if err == nil {
				cfg = configWithRekordboxSyncDefaults(cfg, rbCfg)
			}
		}
		status := pyruntime.Status{}
		if err == nil {
			status = (pyruntime.Resolver{}).Status(context.Background(), pyruntime.Request{Config: cfg})
		}
		return tuiRekordboxConfigLoadedMsg{Config: cfg, SyncConfig: rbCfg, RuntimeStatus: status, Err: err}
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
		m.rbCfg = typed.SyncConfig
		m.cfgErr = typed.Err
		m.runtimeStatus = typed.RuntimeStatus
		m.jobs = tuiRekordboxJobsForConfig(typed.Config, typed.SyncConfig)
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
		m.applyBatchResp = typed.Result.BatchResponse
		m.lastDryRun = typed.Result.DryRun
		m.phase = tuiRekordboxPhaseDone
		return m, nil
	case tuiRekordboxSetupDiscoveredMsg:
		m.runCancel = nil
		m.setup.ConfigPath = typed.ConfigPath
		m.setup.ConfigKind = typed.ConfigKind
		m.setup.ConfigExists = typed.ConfigExists
		m.setup.MusicItems = typed.MusicItems
		m.setup.RBInspect = typed.RBInspect
		m.setup.DiscoverErr = typed.Err
		m.phase = tuiRekordboxPhaseSetupList
		return m, nil
	case tuiRekordboxSetupSavedMsg:
		m.runCancel = nil
		if typed.Err != nil {
			m.setup.SaveErr = typed.Err
			m.phase = tuiRekordboxPhaseSetupReview
			return m, nil
		}
		m.rbCfg = typed.Config
		m.cfg = configWithRekordboxSyncDefaults(m.cfg, typed.Config)
		m.jobs = tuiRekordboxJobsForConfig(m.cfg, m.rbCfg)
		m.jobCursor = clampInt(m.setup.EditIndex, 0, len(m.jobs)-1)
		m.resolveSelectedJob()
		m.setup.ConfigPath = typed.ConfigPath
		m.setup.ConfigExists = typed.ConfigExists
		m.setup.Saved = true
		m.setup.SaveErr = nil
		m.phase = tuiRekordboxPhaseReady
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
		case "s", "n":
			return m.startSetup(-1)
		case "e":
			if mapping, ok := m.selectedFolderMapping(); ok {
				return m.startSetupWithMapping(m.jobCursor, mapping)
			}
			return m.startSetup(-1)
		case "x":
			if _, ok := m.selectedFolderMapping(); ok {
				m.setup.DeleteConfirm = true
				return m, nil
			}
			return m, nil
		case "enter":
			return m.startPlan()
		}
	case tuiRekordboxPhaseSetupList:
		return m.updateSetupListKey(key)
	case tuiRekordboxPhaseSetupSource:
		return m.updateSetupSourceKey(msg)
	case tuiRekordboxPhaseSetupTarget:
		return m.updateSetupTargetKey(msg)
	case tuiRekordboxPhaseSetupReview:
		return m.updateSetupReviewKey(key)
	case tuiRekordboxPhaseReview:
		switch key {
		case "up", "k":
			if m.scroll > 0 {
				m.scroll--
			}
			return m, nil
		case "down", "j":
			if m.plan != nil && m.scroll < m.planScrollMax() {
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
	if m.setup.DeleteConfirm {
		switch key {
		case "y":
			return m.deleteSelectedMapping()
		case "n", "enter", "esc":
			m.setup.DeleteConfirm = false
			return m, nil
		}
	}
	return m, nil
}

func (m tuiRekordboxModel) planScrollMax() int {
	if m.plan == nil {
		return 0
	}
	if m.plan.Version == playlistsync.PlanVersionFolder {
		return len(m.plan.Operations) - 1
	}
	return len(m.plan.Rows) - 1
}

func (m tuiRekordboxModel) startPlan() (tuiRekordboxModel, tea.Cmd) {
	options := m.selectedJobOptions()
	ctx, cancel := context.WithCancel(context.Background())
	m.runCancel = cancel
	m.phase = tuiRekordboxPhasePlanning
	m.err = nil
	m.backupPath = ""
	m.applyResp = bridge.ApplyResponse{}
	m.applyBatchResp = bridge.ApplyBatchResponse{}
	m.lastDryRun = false
	return m, func() tea.Msg {
		result, err := (workflows.RekordboxPlaylistSyncUseCase{}).Plan(ctx, workflows.RekordboxPlaylistSyncPlanRequest{
			Config:     m.cfg,
			SyncConfig: &m.rbCfg,
			MappingID:  options.MappingID,
			Options:    options,
		})
		if errors.Is(err, context.Canceled) {
			err = fmt.Errorf("Rekordbox playlist sync canceled")
		}
		return tuiRekordboxPlanDoneMsg{Result: result, Err: err}
	}
}

func (m tuiRekordboxModel) startSetup(editIndex int) (tuiRekordboxModel, tea.Cmd) {
	return m.startSetupWithMapping(editIndex, syncconfig.FolderMapping{})
}

func (m tuiRekordboxModel) startSetupWithMapping(editIndex int, mapping syncconfig.FolderMapping) (tuiRekordboxModel, tea.Cmd) {
	if strings.TrimSpace(mapping.ID) == "" {
		mapping = syncconfig.FolderMapping{
			ID:              tuiRekordboxUniqueMappingID(m.rbCfg.Sync.Folders, "music-folder"),
			OnMissingTracks: syncconfig.DefaultMissingTracks,
		}
		createFolders := true
		createPlaylists := true
		mapping.CreateFolders = &createFolders
		mapping.CreatePlaylists = &createPlaylists
	}
	m.setup = tuiRekordboxSetupState{EditIndex: editIndex, Mapping: mapping}
	ctx, cancel := context.WithCancel(context.Background())
	m.runCancel = cancel
	m.phase = tuiRekordboxPhaseSetupDiscovering
	cfg := m.cfg
	resolved := m.resolved
	app := m.app
	return m, func() tea.Msg {
		wd, _ := os.Getwd()
		explicitConfigPath := ""
		if app != nil {
			explicitConfigPath = strings.TrimSpace(app.Opts.RekordboxConfigPath)
		}
		path, pathErr := syncconfig.ResolveWritePath(syncconfig.WritePathOptions{
			ExplicitPath: explicitConfigPath,
			WorkingDir:   wd,
		})
		items, musicErr := (music.Reader{}).ListPlaylists(ctx)
		dbDir := resolved.RekordboxDBDir
		if strings.TrimSpace(dbDir) == "" {
			setupResolved, setupErr := playlistsync.ResolveOptions(cfg, playlistsync.Options{})
			if setupErr != nil && pathErr == nil {
				pathErr = setupErr
			}
			dbDir = setupResolved.RekordboxDBDir
		}
		rt, runtimeErr := (pyruntime.Resolver{}).Ensure(ctx, pyruntime.Request{Config: cfg})
		var inspect bridge.InspectResponse
		var rbErr error
		if runtimeErr != nil {
			rbErr = runtimeErr
		} else {
			inspect, rbErr = (bridge.Client{PythonBin: rt.PythonBin, PythonPath: rt.PythonPath}).Inspect(ctx, dbDir)
		}
		var err error
		for _, candidate := range []error{pathErr, musicErr, rbErr} {
			if candidate != nil {
				if err == nil {
					err = candidate
				} else {
					err = fmt.Errorf("%v; %w", err, candidate)
				}
			}
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			err = fmt.Errorf("Rekordbox setup discovery canceled")
		}
		return tuiRekordboxSetupDiscoveredMsg{
			ConfigPath:   path.Path,
			ConfigKind:   path.Kind,
			ConfigExists: path.Exists,
			MusicItems:   items,
			RBInspect:    inspect,
			Err:          err,
		}
	}
}

func (m tuiRekordboxModel) updateSetupListKey(key string) (tuiRekordboxModel, tea.Cmd) {
	if m.setup.DeleteConfirm {
		switch key {
		case "y":
			return m.deleteSelectedMapping()
		case "n", "enter", "esc":
			m.setup.DeleteConfirm = false
			return m, nil
		}
	}
	switch key {
	case "up", "k":
		if m.setup.Cursor > 0 {
			m.setup.Cursor--
		}
	case "down", "j":
		if m.setup.Cursor < len(m.rbCfg.Sync.Folders)-1 {
			m.setup.Cursor++
		}
	case "n":
		m.setup.EditIndex = -1
		m.setup.Mapping = syncconfig.FolderMapping{ID: tuiRekordboxUniqueMappingID(m.rbCfg.Sync.Folders, "music-folder"), OnMissingTracks: syncconfig.DefaultMissingTracks}
		createFolders := true
		createPlaylists := true
		m.setup.Mapping.CreateFolders = &createFolders
		m.setup.Mapping.CreatePlaylists = &createPlaylists
		m.setup.SourceCursor = 0
		m.setup.TargetCursor = 0
		m.phase = tuiRekordboxPhaseSetupSource
	case "e", "enter":
		if len(m.rbCfg.Sync.Folders) == 0 {
			m.setup.EditIndex = -1
			m.phase = tuiRekordboxPhaseSetupSource
			return m, nil
		}
		idx := clampInt(m.setup.Cursor, 0, len(m.rbCfg.Sync.Folders)-1)
		m.setup.EditIndex = idx
		m.setup.Mapping = syncconfig.Clone(syncconfig.Config{Version: syncconfig.Version, Sync: syncconfig.Sync{Folders: []syncconfig.FolderMapping{m.rbCfg.Sync.Folders[idx]}}}).Sync.Folders[0]
		m.phase = tuiRekordboxPhaseSetupSource
	case "x":
		if len(m.rbCfg.Sync.Folders) > 0 {
			m.setup.DeleteConfirm = true
		}
	case "esc":
		m.phase = tuiRekordboxPhaseReady
	}
	return m, nil
}

func (m tuiRekordboxModel) updateSetupSourceKey(msg tea.KeyMsg) (tuiRekordboxModel, tea.Cmd) {
	if m.setup.Input != nil {
		return m.updateSetupInput(msg)
	}
	folders := m.musicFolders()
	switch msg.String() {
	case "up", "k":
		if m.setup.SourceCursor > 0 {
			m.setup.SourceCursor--
		}
	case "down", "j":
		if m.setup.SourceCursor < len(folders)-1 {
			m.setup.SourceCursor++
		}
	case "m":
		m.startSetupInput("music_folder", "Music Folder", m.setup.Mapping.MusicFolder)
	case "enter":
		if len(folders) == 0 {
			m.startSetupInput("music_folder", "Music Folder", m.setup.Mapping.MusicFolder)
			return m, nil
		}
		folder := folders[clampInt(m.setup.SourceCursor, 0, len(folders)-1)]
		m.setup.Mapping.MusicFolder = folder.Name
		m.setup.Mapping.MusicFolderID = folder.PersistentID
		if strings.TrimSpace(m.setup.Mapping.ID) == "" || m.setup.Mapping.ID == "music-folder" {
			m.setup.Mapping.ID = tuiRekordboxUniqueMappingID(m.rbCfg.Sync.Folders, tuiSlug(folder.Name))
		}
		if strings.TrimSpace(m.setup.Mapping.RekordboxFolder) == "" {
			m.setup.Mapping.RekordboxFolder = folder.Name
		}
		m.phase = tuiRekordboxPhaseSetupTarget
	case "esc":
		m.phase = tuiRekordboxPhaseSetupList
	}
	return m, nil
}

func (m tuiRekordboxModel) updateSetupTargetKey(msg tea.KeyMsg) (tuiRekordboxModel, tea.Cmd) {
	if m.setup.Input != nil {
		return m.updateSetupInput(msg)
	}
	folders := m.rbFolders()
	switch msg.String() {
	case "up", "k":
		if m.setup.TargetCursor > 0 {
			m.setup.TargetCursor--
		}
	case "down", "j":
		if m.setup.TargetCursor < len(folders)-1 {
			m.setup.TargetCursor++
		}
	case "m":
		m.startSetupInput("rekordbox_folder", "Rekordbox Folder", m.setup.Mapping.RekordboxFolder)
	case "i":
		m.startSetupInput("id", "Mapping ID", m.setup.Mapping.ID)
	case "enter":
		if len(folders) > 0 {
			folder := folders[clampInt(m.setup.TargetCursor, 0, len(folders)-1)]
			m.setup.Mapping.RekordboxFolder = folder.Name
			m.setup.Mapping.RekordboxFolderID = folder.ID
		} else if strings.TrimSpace(m.setup.Mapping.RekordboxFolder) == "" {
			m.startSetupInput("rekordbox_folder", "Rekordbox Folder", m.setup.Mapping.MusicFolder)
			return m, nil
		}
		m.phase = tuiRekordboxPhaseSetupReview
	case "esc":
		m.phase = tuiRekordboxPhaseSetupSource
	}
	return m, nil
}

func (m tuiRekordboxModel) updateSetupReviewKey(key string) (tuiRekordboxModel, tea.Cmd) {
	switch key {
	case "enter", "s":
		return m.saveSetupMapping()
	case "esc":
		m.phase = tuiRekordboxPhaseSetupTarget
		return m, nil
	}
	return m, nil
}

func (m tuiRekordboxModel) saveSetupMapping() (tuiRekordboxModel, tea.Cmd) {
	cfg := syncconfig.Clone(m.rbCfg)
	if cfg.Version == 0 {
		cfg.Version = syncconfig.Version
	}
	mapping := m.setup.Mapping
	mapping.MusicFolder = strings.TrimSpace(mapping.MusicFolder)
	mapping.MusicFolderID = strings.TrimSpace(mapping.MusicFolderID)
	mapping.RekordboxFolder = strings.TrimSpace(mapping.RekordboxFolder)
	mapping.RekordboxFolderID = strings.TrimSpace(mapping.RekordboxFolderID)
	mapping.ID = strings.TrimSpace(mapping.ID)
	if mapping.ID == "" {
		mapping.ID = tuiRekordboxUniqueMappingID(cfg.Sync.Folders, tuiSlug(firstNonEmpty(mapping.MusicFolder, mapping.RekordboxFolder, "mapping")))
	}
	if mapping.OnMissingTracks == "" {
		mapping.OnMissingTracks = syncconfig.DefaultMissingTracks
	}
	if m.setup.EditIndex >= 0 && m.setup.EditIndex < len(cfg.Sync.Folders) {
		cfg.Sync.Folders[m.setup.EditIndex] = mapping
	} else {
		cfg.Sync.Folders = append(cfg.Sync.Folders, mapping)
		m.setup.EditIndex = len(cfg.Sync.Folders) - 1
	}
	path := m.setup.ConfigPath
	m.phase = tuiRekordboxPhaseSetupSaving
	return m, func() tea.Msg {
		err := syncconfig.Save(path, cfg)
		return tuiRekordboxSetupSavedMsg{Config: cfg, ConfigPath: path, ConfigExists: err == nil, Err: err}
	}
}

func (m tuiRekordboxModel) deleteSelectedMapping() (tuiRekordboxModel, tea.Cmd) {
	idx := m.jobCursor
	if m.phase == tuiRekordboxPhaseSetupList {
		idx = m.setup.Cursor
	}
	if idx < 0 || idx >= len(m.rbCfg.Sync.Folders) {
		m.setup.DeleteConfirm = false
		return m, nil
	}
	cfg := syncconfig.Clone(m.rbCfg)
	cfg.Sync.Folders = append(cfg.Sync.Folders[:idx], cfg.Sync.Folders[idx+1:]...)
	path := m.setup.ConfigPath
	if strings.TrimSpace(path) == "" {
		wd, _ := os.Getwd()
		explicitConfigPath := ""
		if m.app != nil {
			explicitConfigPath = strings.TrimSpace(m.app.Opts.RekordboxConfigPath)
		}
		resolved, _ := syncconfig.ResolveWritePath(syncconfig.WritePathOptions{
			ExplicitPath: explicitConfigPath,
			WorkingDir:   wd,
		})
		path = resolved.Path
	}
	m.phase = tuiRekordboxPhaseSetupSaving
	m.setup.DeleteConfirm = false
	return m, func() tea.Msg {
		err := syncconfig.Save(path, cfg)
		return tuiRekordboxSetupSavedMsg{Config: cfg, ConfigPath: path, ConfigExists: err == nil, Err: err}
	}
}

func (m tuiRekordboxModel) updateSetupInput(msg tea.KeyMsg) (tuiRekordboxModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		value := strings.TrimSpace(m.setup.Input.Buffer)
		switch m.setup.InputField {
		case "music_folder":
			m.setup.Mapping.MusicFolder = value
			m.setup.Mapping.MusicFolderID = ""
			if strings.TrimSpace(m.setup.Mapping.ID) == "" || m.setup.Mapping.ID == "music-folder" {
				m.setup.Mapping.ID = tuiRekordboxUniqueMappingID(m.rbCfg.Sync.Folders, tuiSlug(value))
			}
			if strings.TrimSpace(m.setup.Mapping.RekordboxFolder) == "" {
				m.setup.Mapping.RekordboxFolder = value
			}
			m.setup.Input = nil
			m.phase = tuiRekordboxPhaseSetupTarget
		case "rekordbox_folder":
			m.setup.Mapping.RekordboxFolder = value
			m.setup.Mapping.RekordboxFolderID = ""
			m.setup.Input = nil
			m.phase = tuiRekordboxPhaseSetupReview
		case "id":
			m.setup.Mapping.ID = value
			m.setup.Input = nil
		}
	case "esc":
		m.setup.Input = nil
	default:
		m.updateSetupInputBuffer(msg)
	}
	return m, nil
}

func (m *tuiRekordboxModel) startSetupInput(key, title, value string) {
	m.setup.InputField = key
	m.setup.Input = &tuiConfigEditorInlineEditState{
		Key:         key,
		Title:       title,
		Buffer:      value,
		Cursor:      utf8RuneCount(value),
		Placeholder: title,
		Help:        []string{"enter: accept", "esc: cancel"},
	}
}

func (m *tuiRekordboxModel) updateSetupInputBuffer(msg tea.KeyMsg) {
	if m.setup.Input == nil {
		return
	}
	switch msg.String() {
	case "left", "ctrl+b":
		if m.setup.Input.Cursor > 0 {
			m.setup.Input.Cursor--
		}
	case "right", "ctrl+f":
		if m.setup.Input.Cursor < utf8RuneCount(m.setup.Input.Buffer) {
			m.setup.Input.Cursor++
		}
	case "home", "ctrl+a":
		m.setup.Input.Cursor = 0
	case "end", "ctrl+e":
		m.setup.Input.Cursor = utf8RuneCount(m.setup.Input.Buffer)
	case "backspace", "ctrl+h":
		if m.setup.Input.Cursor > 0 {
			m.setup.Input.Buffer = deleteRuneAt(m.setup.Input.Buffer, m.setup.Input.Cursor-1)
			m.setup.Input.Cursor--
		}
	case "delete", "ctrl+d":
		if m.setup.Input.Cursor < utf8RuneCount(m.setup.Input.Buffer) {
			m.setup.Input.Buffer = deleteRuneAt(m.setup.Input.Buffer, m.setup.Input.Cursor)
		}
	default:
		if len(msg.Runes) > 0 {
			m.setup.Input.Buffer = insertRunesAt(m.setup.Input.Buffer, m.setup.Input.Cursor, msg.Runes)
			m.setup.Input.Cursor += len(msg.Runes)
		}
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
			Config:     m.cfg,
			SyncConfig: &m.rbCfg,
			Plan:       plan,
			DryRun:     m.dryRun,
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
	return m.phase == tuiRekordboxPhasePlanning || m.phase == tuiRekordboxPhaseApplying || m.phase == tuiRekordboxPhaseRepairing || m.phase == tuiRekordboxPhaseSetupDiscovering || m.phase == tuiRekordboxPhaseSetupSaving
}

func (m tuiRekordboxModel) allowBack() bool {
	return !m.isRunning() && m.phase != tuiRekordboxPhaseConfirm && m.setup.Input == nil && !m.setup.DeleteConfirm
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

func tuiRekordboxJobsForConfig(cfg config.Config, rbCfg syncconfig.Config) []tuiRekordboxJobState {
	if rbCfg.HasFolderMappings() {
		jobs := make([]tuiRekordboxJobState, 0, len(rbCfg.Sync.Folders))
		for _, mapping := range rbCfg.Sync.Folders {
			label := strings.TrimSpace(mapping.ID)
			if label == "" {
				label = firstNonEmpty(mapping.RekordboxFolder, mapping.MusicFolder, "mapping")
			}
			jobs = append(jobs, tuiRekordboxJobState{
				Label: label,
				Options: playlistsync.Options{
					MappingID: mapping.ID,
				},
			})
		}
		return jobs
	}
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

func (m tuiRekordboxModel) musicFolders() []music.Playlist {
	folders := []music.Playlist{}
	for _, item := range m.setup.MusicItems {
		if item.Folder {
			folders = append(folders, item)
		}
	}
	return folders
}

func (m tuiRekordboxModel) rbFolders() []bridge.Playlist {
	folders := []bridge.Playlist{}
	for _, item := range m.setup.RBInspect.Playlists {
		if item.Attribute == 1 {
			folders = append(folders, item)
		}
	}
	return folders
}

func tuiRekordboxUniqueMappingID(existing []syncconfig.FolderMapping, base string) string {
	base = tuiSlug(base)
	if base == "" {
		base = "mapping"
	}
	used := map[string]struct{}{}
	for _, mapping := range existing {
		used[mapping.ID] = struct{}{}
	}
	if _, ok := used[base]; !ok {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if _, ok := used[candidate]; !ok {
			return candidate
		}
	}
}

var tuiSlugPattern = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func tuiSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = tuiSlugPattern.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-._")
	return value
}

func clampInt(value, min, max int) int {
	if max < min {
		return min
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
