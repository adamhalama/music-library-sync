package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jaa/update-downloads/internal/config"
)

type tuiConfigEditorPhase string

const (
	tuiConfigEditorPhaseTarget   tuiConfigEditorPhase = "target"
	tuiConfigEditorPhaseDefaults tuiConfigEditorPhase = "defaults"
	tuiConfigEditorPhaseSources  tuiConfigEditorPhase = "sources"
	tuiConfigEditorPhaseReview   tuiConfigEditorPhase = "review"
	tuiConfigEditorPhaseSave     tuiConfigEditorPhase = "save"
)

type tuiConfigEditorPane string

const (
	tuiConfigEditorPaneList tuiConfigEditorPane = "list"
	tuiConfigEditorPaneForm tuiConfigEditorPane = "form"
)

type tuiConfigEditorTargetKind string

const (
	tuiConfigEditorTargetExplicit tuiConfigEditorTargetKind = "explicit"
	tuiConfigEditorTargetUser     tuiConfigEditorTargetKind = "user"
	tuiConfigEditorTargetNew      tuiConfigEditorTargetKind = "new"
)

type tuiConfigEditorModalKind string

const (
	tuiConfigEditorModalDelete  tuiConfigEditorModalKind = "delete"
	tuiConfigEditorModalDiscard tuiConfigEditorModalKind = "discard"
	tuiConfigEditorModalReset   tuiConfigEditorModalKind = "reset"
)

type tuiConfigEditorExitAcceptedMsg struct{}

type tuiConfigEditorSourceState struct {
	Source          config.Source
	ManualTargetDir bool
	ManualStateFile bool
	SCDLArgs        tuiConfigEditorSCDLArgsState
}

type tuiConfigEditorInlineEditState struct {
	Key         string
	Title       string
	Buffer      string
	Cursor      int
	Placeholder string
	Help        []string
}

type tuiConfigEditorModalState struct {
	Kind        tuiConfigEditorModalKind
	Title       string
	Lines       []string
	SourceIndex int
}

type tuiConfigEditorSaveState struct {
	Path     string
	StateDir string
}

type tuiConfigEditorSCDLArgsState struct {
	Recommended map[string]bool
	AdvancedRaw string
	DirectRaw   string
}

type tuiConfigEditorSCDLArgSpec struct {
	Key         string
	Flag        string
	Label       string
	Description string
}

type tuiConfigEditorModel struct {
	app                *AppContext
	phase              tuiConfigEditorPhase
	reviewReturnPhase  tuiConfigEditorPhase
	reviewSourceCursor int
	targetPath         string
	targetKind         tuiConfigEditorTargetKind
	fileExists         bool
	prepareErr         error
	parseErr           error
	defaults           config.Defaults
	sources            []tuiConfigEditorSourceState
	dirty              bool
	previewVisible     bool
	defaultsCursor     int
	sourceListCursor   int
	sourceFieldCursor  int
	sourcePane         tuiConfigEditorPane
	validationProblems []string
	validationErr      string
	saveErr            error
	saveResult         *tuiConfigEditorSaveState
	modal              *tuiConfigEditorModalState
	edit               *tuiConfigEditorInlineEditState
}

type tuiConfigEditorFormField struct {
	Key      string
	Label    string
	Value    string
	Kind     string
	Options  []string
	ReadOnly bool
	Action   bool
}

var tuiConfigEditorSCDLRecommendedArgSpecs = []tuiConfigEditorSCDLArgSpec{
	{
		Key:         "no_overwrites",
		Flag:        "--no-overwrites",
		Label:       "yt-dlp · no-overwrites",
		Description: "Skip writing over existing files on disk.",
	},
	{
		Key:         "ignore_errors",
		Flag:        "--ignore-errors",
		Label:       "yt-dlp · ignore-errors",
		Description: "Keep going when yt-dlp hits a per-track failure.",
	},
	{
		Key:         "write_info_json",
		Flag:        "--write-info-json",
		Label:       "yt-dlp · write-info-json",
		Description: "Write per-track metadata JSON next to the downloaded files.",
	},
	{
		Key:         "write_thumbnail",
		Flag:        "--write-thumbnail",
		Label:       "yt-dlp · write-thumbnail",
		Description: "Save thumbnail files alongside the media files.",
	},
	{
		Key:         "write_description",
		Flag:        "--write-description",
		Label:       "yt-dlp · write-description",
		Description: "Write descriptions to sidecar text files.",
	},
}

func newTUIConfigEditorModel(app *AppContext) tuiConfigEditorModel {
	model := tuiConfigEditorModel{
		app:            app,
		phase:          tuiConfigEditorPhaseTarget,
		previewVisible: true,
		sourcePane:     tuiConfigEditorPaneList,
	}
	model.loadTarget()
	return model
}

func (m tuiConfigEditorModel) Init() tea.Cmd {
	return nil
}

func (m tuiConfigEditorModel) View() string {
	return "Config editor"
}

func (m *tuiConfigEditorModel) loadTarget() {
	m.prepareErr = nil
	m.parseErr = nil
	path, kind, err := tuiResolveConfigEditorTargetPath(m.app)
	if err != nil {
		m.prepareErr = err
		return
	}
	m.targetPath = path
	m.targetKind = kind
	base := config.DefaultConfig()
	m.defaults = base.Defaults
	m.sources = nil
	info, statErr := os.Stat(path)
	switch {
	case statErr == nil:
		if info.IsDir() {
			m.fileExists = false
			m.prepareErr = fmt.Errorf("config path %s is a directory; expected a file", path)
			m.revalidate()
			return
		}
		m.fileExists = true
		cfg, loadErr := config.LoadSingleFile(path)
		if loadErr != nil {
			m.parseErr = loadErr
			m.revalidate()
			return
		}
		m.applyConfig(cfg, false)
	case os.IsNotExist(statErr):
		m.fileExists = false
		m.targetKind = tuiConfigEditorTargetNew
		m.applyConfig(base, false)
	default:
		m.prepareErr = fmt.Errorf("inspect config path %s: %w", path, statErr)
	}
	m.revalidate()
}

func (m *tuiConfigEditorModel) applyConfig(cfg config.Config, dirty bool) {
	m.defaults = cfg.Defaults
	m.sources = make([]tuiConfigEditorSourceState, 0, len(cfg.Sources))
	for _, source := range cfg.Sources {
		state := newTUIConfigEditorSourceState(source)
		m.sources = append(m.sources, state)
	}
	m.dirty = dirty
	m.validationErr = ""
	m.saveErr = nil
	m.saveResult = nil
	m.ensureSourceCursor()
	m.revalidate()
}

func newTUIConfigEditorSourceState(source config.Source) tuiConfigEditorSourceState {
	suggestedTarget := tuiConfigEditorSuggestedTargetDir(source.ID)
	suggestedState := tuiConfigEditorSuggestedStateFile(source.ID, source.Type)
	state := tuiConfigEditorSourceState{
		Source:          source,
		ManualTargetDir: strings.TrimSpace(source.TargetDir) != "" && strings.TrimSpace(source.TargetDir) != suggestedTarget,
		ManualStateFile: strings.TrimSpace(source.StateFile) != "" && strings.TrimSpace(source.StateFile) != suggestedState,
	}
	state.applyCompatibilityDefaults()
	return state
}

func (s *tuiConfigEditorSourceState) applyCompatibilityDefaults() {
	if strings.TrimSpace(s.Source.ID) == "" {
		s.Source.ID = "source"
	}
	switch s.Source.Type {
	case config.SourceTypeSpotify:
	default:
		s.Source.Type = config.SourceTypeSoundCloud
	}
	if strings.TrimSpace(s.Source.Adapter.Kind) == "" {
		s.Source.Adapter.Kind = tuiConfigEditorDefaultAdapterKind(s.Source.Type)
	}
	if !s.ManualTargetDir || strings.TrimSpace(s.Source.TargetDir) == "" {
		s.Source.TargetDir = tuiConfigEditorSuggestedTargetDir(s.Source.ID)
	}
	if !s.ManualStateFile || strings.TrimSpace(s.Source.StateFile) == "" {
		s.Source.StateFile = tuiConfigEditorSuggestedStateFile(s.Source.ID, s.Source.Type)
	}
	supportsBreakAsk, supportsLocalIndex := tuiConfigEditorSyncSupport(s.Source)
	if supportsBreakAsk {
		if s.Source.Sync.BreakOnExisting == nil {
			s.Source.Sync.BreakOnExisting = tuiBoolPtr(true)
		}
		if s.Source.Sync.AskOnExisting == nil {
			s.Source.Sync.AskOnExisting = tuiBoolPtr(false)
		}
	}
	if supportsLocalIndex {
		if s.Source.Sync.LocalIndexCache == nil {
			s.Source.Sync.LocalIndexCache = tuiBoolPtr(false)
		}
	}
	s.syncAdapterEditorState()
}

func (s *tuiConfigEditorSourceState) normalizeInteractiveCompatibility() {
	if !tuiConfigEditorAdapterSupportedByType(s.Source.Type, s.Source.Adapter.Kind) {
		s.Source.Adapter.Kind = tuiConfigEditorDefaultAdapterKind(s.Source.Type)
	}
	supportsBreakAsk, supportsLocalIndex := tuiConfigEditorSyncSupport(s.Source)
	if !supportsBreakAsk {
		s.Source.Sync.BreakOnExisting = nil
		s.Source.Sync.AskOnExisting = nil
	}
	if !supportsLocalIndex {
		s.Source.Sync.LocalIndexCache = nil
	}
	s.applyCompatibilityDefaults()
}

func (s *tuiConfigEditorSourceState) syncAdapterEditorState() {
	if s.Source.Adapter.Kind != "scdl" {
		s.SCDLArgs = tuiConfigEditorSCDLArgsState{}
		return
	}
	s.SCDLArgs = tuiConfigEditorParseSCDLArgs(s.Source.Adapter.ExtraArgs)
}

func (s *tuiConfigEditorSourceState) syncAdapterExtraArgs() {
	if s.Source.Adapter.Kind != "scdl" {
		return
	}
	s.Source.Adapter.ExtraArgs = s.SCDLArgs.Build()
}

func tuiResolveConfigEditorTargetPath(app *AppContext) (string, tuiConfigEditorTargetKind, error) {
	if app != nil && strings.TrimSpace(app.Opts.ConfigPath) != "" {
		path, err := config.ExpandPath(strings.TrimSpace(app.Opts.ConfigPath))
		return path, tuiConfigEditorTargetExplicit, err
	}
	path, err := config.UserConfigPath()
	return path, tuiConfigEditorTargetUser, err
}

func (m *tuiConfigEditorModel) revalidate() {
	cfg := m.buildConfig()
	err := config.Validate(cfg)
	if err == nil {
		m.validationProblems = nil
		return
	}
	if validationErr, ok := err.(*config.ValidationError); ok {
		m.validationProblems = append([]string{}, validationErr.Problems...)
		return
	}
	m.validationProblems = []string{err.Error()}
}

func (m tuiConfigEditorModel) buildConfig() config.Config {
	cfg := config.Config{
		Version:  1,
		Defaults: m.defaults,
		Sources:  make([]config.Source, 0, len(m.sources)),
	}
	for _, source := range m.sources {
		item := source
		item.syncAdapterExtraArgs()
		item.applyCompatibilityDefaults()
		cfg.Sources = append(cfg.Sources, item.Source)
	}
	return cfg
}

func (m tuiConfigEditorModel) allowBack() bool {
	if m.modal != nil || m.edit != nil {
		return false
	}
	if m.phase == tuiConfigEditorPhaseReview || m.phase == tuiConfigEditorPhaseSave {
		return false
	}
	return !m.dirty
}

func (m tuiConfigEditorModel) Update(msg tea.Msg) (tuiConfigEditorModel, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		if m.modal != nil {
			return m.updateModal(typed)
		}
		if m.edit != nil {
			return m.updateEdit(typed)
		}
		switch typed.String() {
		case "esc":
			if m.phase == tuiConfigEditorPhaseReview || m.phase == tuiConfigEditorPhaseSave {
				return m.returnFromReview(), nil
			}
			if m.dirty {
				m.modal = &tuiConfigEditorModalState{
					Kind:  tuiConfigEditorModalDiscard,
					Title: "Discard Changes",
					Lines: []string{"Discard unsaved config editor changes and return to the home screen?", "y: discard  n/esc: stay"},
				}
				return m, nil
			}
		case "tab":
			return m.updateTab(false), nil
		case "shift+tab":
			return m.updateTab(true), nil
		}
		switch m.phase {
		case tuiConfigEditorPhaseTarget:
			return m.updateTargetPhase(typed)
		case tuiConfigEditorPhaseDefaults:
			return m.updateDefaultsPhase(typed)
		case tuiConfigEditorPhaseSources:
			return m.updateSourcesPhase(typed)
		case tuiConfigEditorPhaseReview:
			return m.updateReviewPhase(typed)
		case tuiConfigEditorPhaseSave:
			return m.updateSavePhase(typed)
		}
	}
	return m, nil
}

func (m tuiConfigEditorModel) updateModal(msg tea.KeyMsg) (tuiConfigEditorModel, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		switch m.modal.Kind {
		case tuiConfigEditorModalDelete:
			if m.modal.SourceIndex >= 0 && m.modal.SourceIndex < len(m.sources) {
				m.sources = append(m.sources[:m.modal.SourceIndex], m.sources[m.modal.SourceIndex+1:]...)
				m.ensureSourceCursor()
				m.dirty = true
				m.revalidate()
			}
			m.modal = nil
			return m, nil
		case tuiConfigEditorModalReset:
			m.parseErr = nil
			m.applyConfig(config.DefaultConfig(), true)
			m.phase = tuiConfigEditorPhaseDefaults
			m.modal = nil
			return m, nil
		case tuiConfigEditorModalDiscard:
			m.modal = nil
			m.dirty = false
			return m, func() tea.Msg { return tuiConfigEditorExitAcceptedMsg{} }
		}
	case "n", "esc", "q":
		m.modal = nil
		return m, nil
	}
	return m, nil
}

func (m tuiConfigEditorModel) updateEdit(msg tea.KeyMsg) (tuiConfigEditorModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if err := m.applyEdit(); err != nil {
			m.validationErr = err.Error()
			return m, nil
		}
		m.edit = nil
		m.dirty = true
		m.validationErr = ""
		m.saveErr = nil
		m.saveResult = nil
		m.revalidate()
		return m, nil
	case "esc", "q":
		m.edit = nil
		return m, nil
	case "left", "ctrl+b":
		if m.edit.Cursor > 0 {
			m.edit.Cursor--
		}
		return m, nil
	case "right", "ctrl+f":
		if m.edit.Cursor < utf8RuneCount(m.edit.Buffer) {
			m.edit.Cursor++
		}
		return m, nil
	case "home", "ctrl+a":
		m.edit.Cursor = 0
		return m, nil
	case "end", "ctrl+e":
		m.edit.Cursor = utf8RuneCount(m.edit.Buffer)
		return m, nil
	case "backspace", "ctrl+h":
		if m.edit.Cursor > 0 {
			m.edit.Buffer = deleteRuneAt(m.edit.Buffer, m.edit.Cursor-1)
			m.edit.Cursor--
		}
		return m, nil
	case "delete", "ctrl+d":
		if m.edit.Cursor < utf8RuneCount(m.edit.Buffer) {
			m.edit.Buffer = deleteRuneAt(m.edit.Buffer, m.edit.Cursor)
		}
		return m, nil
	default:
		if len(msg.Runes) > 0 {
			m.edit.Buffer = insertRunesAt(m.edit.Buffer, m.edit.Cursor, msg.Runes)
			m.edit.Cursor += len(msg.Runes)
		}
		return m, nil
	}
}

func (m *tuiConfigEditorModel) applyEdit() error {
	if m.edit == nil {
		return nil
	}
	raw := strings.TrimSpace(m.edit.Buffer)
	switch m.edit.Key {
	case "defaults.state_dir":
		m.defaults.StateDir = raw
	case "defaults.archive_file":
		m.defaults.ArchiveFile = raw
	case "defaults.threads":
		value, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("threads must be a number")
		}
		m.defaults.Threads = value
	case "defaults.command_timeout_seconds":
		value, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("command timeout must be a number")
		}
		m.defaults.CommandTimeoutSeconds = value
	case "source.id":
		state := m.currentSourceState()
		if state == nil {
			return nil
		}
		state.Source.ID = raw
		state.applyCompatibilityDefaults()
	case "source.url":
		if state := m.currentSourceState(); state != nil {
			state.Source.URL = raw
		}
	case "source.target_dir":
		if state := m.currentSourceState(); state != nil {
			state.Source.TargetDir = raw
			state.ManualTargetDir = true
		}
	case "source.state_file":
		if state := m.currentSourceState(); state != nil {
			state.Source.StateFile = raw
			state.ManualStateFile = true
		}
	case "source.adapter.extra_args":
		if state := m.currentSourceState(); state != nil {
			state.Source.Adapter.ExtraArgs = tuiConfigEditorParseList(raw)
			state.syncAdapterEditorState()
		}
	case "source.adapter.scdl.advanced_raw":
		if state := m.currentSourceState(); state != nil {
			state.SCDLArgs.AdvancedRaw = raw
			state.syncAdapterExtraArgs()
		}
	case "source.adapter.scdl.direct_raw":
		if state := m.currentSourceState(); state != nil {
			state.SCDLArgs.DirectRaw = raw
			state.syncAdapterExtraArgs()
		}
	case "source.adapter.min_version":
		if state := m.currentSourceState(); state != nil {
			state.Source.Adapter.MinVersion = raw
		}
	default:
		return fmt.Errorf("unknown edit field %q", m.edit.Key)
	}
	return nil
}

func (m tuiConfigEditorModel) updateTab(reverse bool) tuiConfigEditorModel {
	if m.phase == tuiConfigEditorPhaseSources {
		if len(m.sources) == 0 {
			m.sourcePane = tuiConfigEditorPaneList
			return m
		}
		if m.sourcePane == tuiConfigEditorPaneList {
			m.sourcePane = tuiConfigEditorPaneForm
		} else {
			m.sourcePane = tuiConfigEditorPaneList
		}
		return m
	}
	if m.phase == tuiConfigEditorPhaseReview || m.phase == tuiConfigEditorPhaseSave {
		m.previewVisible = !m.previewVisible
	}
	return m
}

func (m tuiConfigEditorModel) updateTargetPhase(msg tea.KeyMsg) (tuiConfigEditorModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if m.prepareErr != nil {
			return m, nil
		}
		if m.parseErr != nil {
			m.modal = &tuiConfigEditorModalState{
				Kind:  tuiConfigEditorModalReset,
				Title: "Reset Invalid Config",
				Lines: []string{"The target file could not be parsed.", "Reset the in-memory document from defaults and overwrite on save?", "y: reset  n/esc: cancel"},
			}
			return m, nil
		}
		if !m.fileExists {
			m.applyConfig(config.DefaultConfig(), true)
		}
		m.phase = tuiConfigEditorPhaseDefaults
		return m, nil
	}
	return m, nil
}

func (m tuiConfigEditorModel) updateDefaultsPhase(msg tea.KeyMsg) (tuiConfigEditorModel, tea.Cmd) {
	maxCursor := 6
	switch msg.String() {
	case "up", "k":
		if m.defaultsCursor > 0 {
			m.defaultsCursor--
		}
	case "down", "j":
		if m.defaultsCursor < maxCursor {
			m.defaultsCursor++
		}
	case " ":
		if m.defaultsCursor == 4 {
			m.defaults.ContinueOnError = !m.defaults.ContinueOnError
			m.dirty = true
			m.revalidate()
		}
	case "r":
		m.openReview(tuiConfigEditorPhaseDefaults)
		return m, nil
	case "s":
		return m.performSave()
	case "enter":
		switch m.defaultsCursor {
		case 1:
			m.startEdit("defaults.state_dir", "Edit State Dir", m.defaults.StateDir)
		case 2:
			m.startEdit("defaults.archive_file", "Edit Archive File", m.defaults.ArchiveFile)
		case 3:
			m.startEdit("defaults.threads", "Edit Threads", strconv.Itoa(m.defaults.Threads))
		case 4:
			m.defaults.ContinueOnError = !m.defaults.ContinueOnError
			m.dirty = true
			m.revalidate()
		case 5:
			m.startEdit("defaults.command_timeout_seconds", "Edit Command Timeout Seconds", strconv.Itoa(m.defaults.CommandTimeoutSeconds))
		case 6:
			if len(m.defaultsValidationProblems()) > 0 {
				m.validationErr = "fix defaults errors before continuing"
				return m, nil
			}
			m.phase = tuiConfigEditorPhaseSources
			m.validationErr = ""
			return m, nil
		}
	}
	return m, nil
}

func (m tuiConfigEditorModel) updateSourcesPhase(msg tea.KeyMsg) (tuiConfigEditorModel, tea.Cmd) {
	switch msg.String() {
	case "r":
		m.openReview(tuiConfigEditorPhaseSources)
		return m, nil
	case "s":
		return m.performSave()
	}
	if m.sourcePane == tuiConfigEditorPaneList {
		switch msg.String() {
		case "up", "k":
			if m.sourceListCursor > 0 {
				m.sourceListCursor--
			}
		case "down", "j":
			if m.sourceListCursor < len(m.sources)-1 {
				m.sourceListCursor++
			}
		case "space":
			if state := m.currentSourceState(); state != nil {
				state.Source.Enabled = !state.Source.Enabled
				m.dirty = true
				m.revalidate()
			}
		case "a":
			m.addSource()
		case "d":
			m.duplicateSource()
		case "D":
			if len(m.sources) > 0 {
				m.modal = &tuiConfigEditorModalState{
					Kind:        tuiConfigEditorModalDelete,
					Title:       "Delete Source",
					SourceIndex: m.sourceListCursor,
					Lines: []string{
						fmt.Sprintf("Delete source %q from the in-memory config?", m.sources[m.sourceListCursor].Source.ID),
						"y: delete  n/esc: cancel",
					},
				}
			}
		case "[":
			m.moveSource(-1)
		case "]":
			m.moveSource(1)
		case "enter":
			if len(m.sources) == 0 {
				m.addSource()
			}
			if len(m.sources) > 0 {
				m.sourcePane = tuiConfigEditorPaneForm
			}
		}
		return m, nil
	}

	fields := m.currentSourceFields()
	maxCursor := len(fields) - 1
	if maxCursor < 0 {
		maxCursor = 0
	}
	switch msg.String() {
	case "up", "k":
		if m.sourceFieldCursor > 0 {
			m.sourceFieldCursor--
		}
	case "down", "j":
		if m.sourceFieldCursor < maxCursor {
			m.sourceFieldCursor++
		}
	case "space":
		m.applySourceFieldAction(fields, true)
	case "enter":
		if m.applySourceFieldAction(fields, false) {
			return m, nil
		}
	}
	return m, nil
}

func (m *tuiConfigEditorModel) applySourceFieldAction(fields []tuiConfigEditorFormField, fromSpace bool) bool {
	if len(fields) == 0 || m.sourceFieldCursor < 0 || m.sourceFieldCursor >= len(fields) {
		return false
	}
	field := fields[m.sourceFieldCursor]
	state := m.currentSourceState()
	if state == nil {
		return false
	}
	switch field.Key {
	case "source.id":
		if !fromSpace {
			m.startEdit(field.Key, "Edit Source ID", state.Source.ID)
			return true
		}
	case "source.enabled":
		state.Source.Enabled = !state.Source.Enabled
	case "source.type":
		if state.Source.Type == config.SourceTypeSoundCloud {
			state.Source.Type = config.SourceTypeSpotify
		} else {
			state.Source.Type = config.SourceTypeSoundCloud
		}
		state.normalizeInteractiveCompatibility()
	case "source.url":
		if !fromSpace {
			m.startEdit(field.Key, "Edit Source URL", state.Source.URL)
			return true
		}
	case "source.target_dir":
		if !fromSpace {
			m.startEdit(field.Key, "Edit Target Dir", state.Source.TargetDir)
			return true
		}
	case "source.state_file":
		if !fromSpace {
			m.startEdit(field.Key, "Edit State File", state.Source.StateFile)
			return true
		}
	case "source.adapter.kind":
		state.Source.Adapter.Kind = tuiConfigEditorNextAdapter(state.Source)
		state.normalizeInteractiveCompatibility()
	case "source.adapter.extra_args":
		if !fromSpace {
			m.startEdit(field.Key, "Edit Adapter Extra Args", strings.Join(state.Source.Adapter.ExtraArgs, ", "))
			return true
		}
	case "source.adapter.scdl.toggle.no_overwrites":
		state.SCDLArgs.ToggleRecommended("no_overwrites")
		state.syncAdapterExtraArgs()
	case "source.adapter.scdl.toggle.ignore_errors":
		state.SCDLArgs.ToggleRecommended("ignore_errors")
		state.syncAdapterExtraArgs()
	case "source.adapter.scdl.toggle.write_info_json":
		state.SCDLArgs.ToggleRecommended("write_info_json")
		state.syncAdapterExtraArgs()
	case "source.adapter.scdl.toggle.write_thumbnail":
		state.SCDLArgs.ToggleRecommended("write_thumbnail")
		state.syncAdapterExtraArgs()
	case "source.adapter.scdl.toggle.write_description":
		state.SCDLArgs.ToggleRecommended("write_description")
		state.syncAdapterExtraArgs()
	case "source.adapter.scdl.advanced_raw":
		if !fromSpace {
			m.startEdit(field.Key, "Edit Advanced yt-dlp Args", state.SCDLArgs.AdvancedRaw)
			return true
		}
	case "source.adapter.scdl.direct_raw":
		if !fromSpace {
			m.startEdit(field.Key, "Edit Additional scdl Args", state.SCDLArgs.DirectRaw)
			return true
		}
	case "source.adapter.min_version":
		if !fromSpace {
			m.startEdit(field.Key, "Edit Adapter Min Version", state.Source.Adapter.MinVersion)
			return true
		}
	case "source.sync.break_on_existing":
		if state.Source.Sync.BreakOnExisting == nil || !*state.Source.Sync.BreakOnExisting {
			state.Source.Sync.BreakOnExisting = tuiBoolPtr(true)
		} else {
			state.Source.Sync.BreakOnExisting = tuiBoolPtr(false)
		}
	case "source.sync.ask_on_existing":
		if state.Source.Sync.AskOnExisting == nil || !*state.Source.Sync.AskOnExisting {
			state.Source.Sync.AskOnExisting = tuiBoolPtr(true)
		} else {
			state.Source.Sync.AskOnExisting = tuiBoolPtr(false)
		}
	case "source.sync.local_index_cache":
		if state.Source.Sync.LocalIndexCache == nil || !*state.Source.Sync.LocalIndexCache {
			state.Source.Sync.LocalIndexCache = tuiBoolPtr(true)
		} else {
			state.Source.Sync.LocalIndexCache = tuiBoolPtr(false)
		}
	case "action.review":
		m.openReview(tuiConfigEditorPhaseSources)
		return true
	}
	m.dirty = true
	m.revalidate()
	m.saveErr = nil
	m.saveResult = nil
	return true
}

func (m tuiConfigEditorModel) updateReviewPhase(msg tea.KeyMsg) (tuiConfigEditorModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.reviewSourceCursor > 0 {
			m.reviewSourceCursor--
		}
	case "down", "j":
		if m.reviewSourceCursor < len(m.sources) {
			m.reviewSourceCursor++
		}
	case "p":
		m.previewVisible = !m.previewVisible
	case "enter":
		if len(m.validationProblems) > 0 {
			m.validationErr = "fix validation issues before opening the save step"
			return m, nil
		}
		m.phase = tuiConfigEditorPhaseSave
	case "s":
		return m.performSave()
	}
	return m, nil
}

func (m tuiConfigEditorModel) updateSavePhase(msg tea.KeyMsg) (tuiConfigEditorModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.reviewSourceCursor > 0 {
			m.reviewSourceCursor--
		}
	case "down", "j":
		if m.reviewSourceCursor < len(m.sources) {
			m.reviewSourceCursor++
		}
	case "p":
		m.previewVisible = !m.previewVisible
	case "s", "enter":
		return m.performSave()
	}
	return m, nil
}

func (m tuiConfigEditorModel) performSave() (tuiConfigEditorModel, tea.Cmd) {
	m.saveErr = nil
	m.validationErr = ""
	if len(m.validationProblems) > 0 {
		m.validationErr = "fix validation issues before saving"
		return m, nil
	}
	stateDir, err := config.SaveSingleFile(m.targetPath, m.buildConfig())
	if err != nil {
		m.saveErr = err
		return m, nil
	}
	m.saveResult = &tuiConfigEditorSaveState{
		Path:     m.targetPath,
		StateDir: stateDir,
	}
	m.fileExists = true
	m.dirty = false
	m.saveErr = nil
	return m, nil
}

func (m *tuiConfigEditorModel) startEdit(key, title, value string) {
	m.edit = &tuiConfigEditorInlineEditState{
		Key:         key,
		Title:       title,
		Buffer:      value,
		Cursor:      utf8RuneCount(value),
		Placeholder: tuiConfigEditorEditPlaceholder(key),
		Help:        tuiConfigEditorEditHelp(key),
	}
}

func (m *tuiConfigEditorModel) openReview(from tuiConfigEditorPhase) {
	m.reviewReturnPhase = from
	m.phase = tuiConfigEditorPhaseReview
	m.validationErr = ""
	if m.reviewSourceCursor > len(m.sources) {
		m.reviewSourceCursor = len(m.sources)
	}
}

func (m tuiConfigEditorModel) returnFromReview() tuiConfigEditorModel {
	switch m.reviewReturnPhase {
	case tuiConfigEditorPhaseDefaults, tuiConfigEditorPhaseSources:
		m.phase = m.reviewReturnPhase
	default:
		m.phase = tuiConfigEditorPhaseSources
		if len(m.sources) == 0 {
			m.phase = tuiConfigEditorPhaseDefaults
		}
	}
	return m
}

func (m *tuiConfigEditorModel) ensureSourceCursor() {
	if len(m.sources) == 0 {
		m.sourceListCursor = 0
		m.sourceFieldCursor = 0
		m.sourcePane = tuiConfigEditorPaneList
		return
	}
	if m.sourceListCursor < 0 {
		m.sourceListCursor = 0
	}
	if m.sourceListCursor >= len(m.sources) {
		m.sourceListCursor = len(m.sources) - 1
	}
	fields := m.currentSourceFields()
	if m.sourceFieldCursor >= len(fields) {
		m.sourceFieldCursor = len(fields) - 1
	}
	if m.sourceFieldCursor < 0 {
		m.sourceFieldCursor = 0
	}
}

func (m *tuiConfigEditorModel) addSource() {
	index := len(m.sources) + 1
	sourceType := config.SourceTypeSoundCloud
	id := tuiConfigEditorUniqueSourceID(m.sources, fmt.Sprintf("%s-%d", sourceType, index))
	state := newTUIConfigEditorSourceState(config.Source{
		ID:      id,
		Type:    sourceType,
		Enabled: true,
		Adapter: config.AdapterSpec{Kind: "scdl"},
		URL:     "https://soundcloud.com/your-user",
	})
	m.sources = append(m.sources, state)
	m.sourceListCursor = len(m.sources) - 1
	m.sourcePane = tuiConfigEditorPaneForm
	m.sourceFieldCursor = 0
	m.dirty = true
	m.revalidate()
}

func (m *tuiConfigEditorModel) duplicateSource() {
	state := m.currentSourceState()
	if state == nil {
		return
	}
	copyState := *state
	if state.SCDLArgs.Recommended != nil {
		copyState.SCDLArgs.Recommended = map[string]bool{}
		for key, enabled := range state.SCDLArgs.Recommended {
			copyState.SCDLArgs.Recommended[key] = enabled
		}
	}
	copyState.Source.ID = tuiConfigEditorUniqueSourceID(m.sources, state.Source.ID+"-copy")
	copyState.ManualTargetDir = false
	copyState.ManualStateFile = false
	copyState.syncAdapterExtraArgs()
	copyState.applyCompatibilityDefaults()
	insertAt := m.sourceListCursor + 1
	m.sources = append(m.sources[:insertAt], append([]tuiConfigEditorSourceState{copyState}, m.sources[insertAt:]...)...)
	m.sourceListCursor = insertAt
	m.sourcePane = tuiConfigEditorPaneForm
	m.sourceFieldCursor = 0
	m.dirty = true
	m.revalidate()
}

func (m *tuiConfigEditorModel) moveSource(direction int) {
	if len(m.sources) == 0 {
		return
	}
	next := m.sourceListCursor + direction
	if next < 0 || next >= len(m.sources) {
		return
	}
	m.sources[m.sourceListCursor], m.sources[next] = m.sources[next], m.sources[m.sourceListCursor]
	m.sourceListCursor = next
	m.dirty = true
}

func (m *tuiConfigEditorModel) currentSourceState() *tuiConfigEditorSourceState {
	if len(m.sources) == 0 || m.sourceListCursor < 0 || m.sourceListCursor >= len(m.sources) {
		return nil
	}
	return &m.sources[m.sourceListCursor]
}

func (m tuiConfigEditorModel) currentSourceFields() []tuiConfigEditorFormField {
	state := m.currentSourceState()
	if state == nil {
		return []tuiConfigEditorFormField{{Key: "action.review", Label: "Open Review", Kind: "action", Action: true}}
	}
	fields := []tuiConfigEditorFormField{
		{Key: "source.id", Label: "Identity · id", Value: state.Source.ID, Kind: "text"},
		{Key: "source.enabled", Label: "Identity · enabled", Value: tuiBoolLabel(state.Source.Enabled), Kind: "bool"},
		{Key: "source.type", Label: "Identity · type", Value: string(state.Source.Type), Kind: "select"},
		{Key: "source.url", Label: "Remote · url", Value: state.Source.URL, Kind: "text"},
		{Key: "source.target_dir", Label: "Location · target_dir", Value: state.Source.TargetDir, Kind: "text"},
		{Key: "source.state_file", Label: "Location · state_file", Value: state.Source.StateFile, Kind: "text"},
		{Key: "source.adapter.kind", Label: "Adapter · kind", Value: state.Source.Adapter.Kind, Kind: "select"},
	}
	if state.Source.Adapter.Kind == "scdl" {
		fields = append(fields,
			tuiConfigEditorFormField{Key: "source.adapter.scdl.managed", Label: "Adapter · managed defaults", Value: "UDL-managed yt-dlp/scdl flags", Kind: "info", ReadOnly: true},
		)
		for _, spec := range tuiConfigEditorSCDLRecommendedArgSpecs {
			fields = append(fields, tuiConfigEditorFormField{
				Key:   "source.adapter.scdl.toggle." + spec.Key,
				Label: "Adapter · " + spec.Label,
				Value: tuiBoolLabel(state.SCDLArgs.RecommendedEnabled(spec.Key)),
				Kind:  "bool",
			})
		}
		fields = append(fields,
			tuiConfigEditorFormField{Key: "source.adapter.scdl.advanced_raw", Label: "Adapter · yt-dlp advanced", Value: state.SCDLArgs.AdvancedRaw, Kind: "text"},
			tuiConfigEditorFormField{Key: "source.adapter.scdl.direct_raw", Label: "Adapter · scdl extra args", Value: state.SCDLArgs.DirectRaw, Kind: "text"},
		)
	} else {
		fields = append(fields, tuiConfigEditorFormField{Key: "source.adapter.extra_args", Label: "Adapter · extra_args", Value: strings.Join(state.Source.Adapter.ExtraArgs, ", "), Kind: "text"})
	}
	fields = append(fields, tuiConfigEditorFormField{Key: "source.adapter.min_version", Label: "Adapter · min_version (doctor)", Value: state.Source.Adapter.MinVersion, Kind: "text"})
	supportsBreakAsk, supportsLocalIndex := tuiConfigEditorSyncSupport(state.Source)
	if supportsBreakAsk {
		fields = append(fields,
			tuiConfigEditorFormField{Key: "source.sync.break_on_existing", Label: "Sync · break_on_existing", Value: tuiBoolPtrLabel(state.Source.Sync.BreakOnExisting), Kind: "bool"},
			tuiConfigEditorFormField{Key: "source.sync.ask_on_existing", Label: "Sync · ask_on_existing", Value: tuiBoolPtrLabel(state.Source.Sync.AskOnExisting), Kind: "bool"},
		)
	}
	if supportsLocalIndex {
		fields = append(fields, tuiConfigEditorFormField{Key: "source.sync.local_index_cache", Label: "Sync · local_index_cache", Value: tuiBoolPtrLabel(state.Source.Sync.LocalIndexCache), Kind: "bool"})
	}
	fields = append(fields, tuiConfigEditorFormField{Key: "action.review", Label: "Open Review", Value: "enter", Kind: "action", Action: true})
	return fields
}

func tuiConfigEditorUniqueSourceID(existing []tuiConfigEditorSourceState, base string) string {
	candidate := strings.TrimSpace(base)
	if candidate == "" {
		candidate = "source"
	}
	seen := map[string]struct{}{}
	for _, source := range existing {
		seen[source.Source.ID] = struct{}{}
	}
	if _, ok := seen[candidate]; !ok {
		return candidate
	}
	for i := 2; ; i++ {
		next := fmt.Sprintf("%s-%d", candidate, i)
		if _, ok := seen[next]; !ok {
			return next
		}
	}
}

func tuiConfigEditorSuggestedTargetDir(sourceID string) string {
	cleanID := strings.TrimSpace(sourceID)
	if cleanID == "" {
		cleanID = "source"
	}
	return filepath.Join("~", "Music", "downloaded", cleanID)
}

func tuiConfigEditorSuggestedStateFile(sourceID string, sourceType config.SourceType) string {
	cleanID := strings.TrimSpace(sourceID)
	if cleanID == "" {
		cleanID = "source"
	}
	if sourceType == config.SourceTypeSpotify {
		return cleanID + ".sync.spotify"
	}
	return cleanID + ".sync.scdl"
}

func tuiConfigEditorDefaultAdapterKind(sourceType config.SourceType) string {
	if sourceType == config.SourceTypeSpotify {
		return "deemix"
	}
	return "scdl"
}

func tuiConfigEditorAdapterSupportedByType(sourceType config.SourceType, adapterKind string) bool {
	switch sourceType {
	case config.SourceTypeSpotify:
		return adapterKind == "deemix" || adapterKind == "spotdl"
	case config.SourceTypeSoundCloud:
		return adapterKind == "scdl" || adapterKind == "scdl-freedl"
	default:
		return false
	}
}

func tuiConfigEditorSyncSupport(source config.Source) (bool, bool) {
	switch source.Type {
	case config.SourceTypeSoundCloud:
		return true, true
	case config.SourceTypeSpotify:
		return source.Adapter.Kind == "deemix", false
	default:
		return false, false
	}
}

func tuiConfigEditorNextAdapter(source config.Source) string {
	if source.Type == config.SourceTypeSpotify {
		if source.Adapter.Kind == "deemix" {
			return "spotdl"
		}
		return "deemix"
	}
	if source.Adapter.Kind == "scdl" {
		return "scdl-freedl"
	}
	return "scdl"
}

func tuiConfigEditorParseList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		result = append(result, part)
	}
	return result
}

func tuiBoolPtr(v bool) *bool {
	return &v
}

func (s tuiConfigEditorSCDLArgsState) RecommendedEnabled(key string) bool {
	return s.Recommended[key]
}

func (s *tuiConfigEditorSCDLArgsState) ToggleRecommended(key string) {
	if s.Recommended == nil {
		s.Recommended = map[string]bool{}
	}
	s.Recommended[key] = !s.Recommended[key]
}

func tuiBoolLabel(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func tuiBoolPtrLabel(v *bool) string {
	if v == nil {
		return "unset"
	}
	return tuiBoolLabel(*v)
}

func tuiConfigEditorParseSCDLArgs(extraArgs []string) tuiConfigEditorSCDLArgsState {
	state := tuiConfigEditorSCDLArgsState{Recommended: map[string]bool{}}
	directArgs := make([]string, 0, len(extraArgs))
	ytdlpRaw, foundYTDLP := extractInlineArg(extraArgs, "--yt-dlp-args")
	for i := 0; i < len(extraArgs); i++ {
		token := strings.TrimSpace(extraArgs[i])
		if token == "" || token == "-f" {
			continue
		}
		if token == "--yt-dlp-args" || token == "--sync" {
			i++
			continue
		}
		if strings.HasPrefix(token, "--yt-dlp-args=") || strings.HasPrefix(token, "--sync=") {
			continue
		}
		directArgs = append(directArgs, extraArgs[i])
	}
	state.DirectRaw = strings.TrimSpace(strings.Join(directArgs, " "))
	if !foundYTDLP {
		return state
	}

	tokens := strings.Fields(ytdlpRaw)
	filtered := make([]string, 0, len(tokens))
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		switch token {
		case "--embed-thumbnail", "--embed-metadata", "--break-on-existing", "--no-break-on-existing":
			continue
		case "--download-archive", "--playlist-items":
			i++
			continue
		}
		if strings.HasPrefix(token, "--download-archive=") || strings.HasPrefix(token, "--playlist-items=") {
			continue
		}
		if spec, ok := tuiConfigEditorSCDLRecommendedArgSpecByFlag(token); ok {
			state.Recommended[spec.Key] = true
			continue
		}
		filtered = append(filtered, token)
	}
	state.AdvancedRaw = strings.TrimSpace(strings.Join(filtered, " "))
	return state
}

func (s tuiConfigEditorSCDLArgsState) Build() []string {
	args := []string{}
	if strings.TrimSpace(s.DirectRaw) != "" {
		args = append(args, strings.Fields(s.DirectRaw)...)
	}
	ytdlpTokens := []string{}
	for _, spec := range tuiConfigEditorSCDLRecommendedArgSpecs {
		if s.RecommendedEnabled(spec.Key) {
			ytdlpTokens = append(ytdlpTokens, spec.Flag)
		}
	}
	if strings.TrimSpace(s.AdvancedRaw) != "" {
		ytdlpTokens = append(ytdlpTokens, strings.Fields(s.AdvancedRaw)...)
	}
	if len(ytdlpTokens) > 0 {
		args = append(args, "--yt-dlp-args", strings.Join(ytdlpTokens, " "))
	}
	if len(args) == 0 {
		return nil
	}
	return args
}

func extractInlineArg(args []string, flag string) (string, bool) {
	for i := 0; i < len(args); i++ {
		token := strings.TrimSpace(args[i])
		if token == flag {
			if i+1 >= len(args) {
				return "", false
			}
			return strings.TrimSpace(args[i+1]), true
		}
		prefix := flag + "="
		if strings.HasPrefix(token, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(token, prefix)), true
		}
	}
	return "", false
}

func tuiConfigEditorSCDLRecommendedArgSpecByFlag(flag string) (tuiConfigEditorSCDLArgSpec, bool) {
	for _, spec := range tuiConfigEditorSCDLRecommendedArgSpecs {
		if spec.Flag == flag {
			return spec, true
		}
	}
	return tuiConfigEditorSCDLArgSpec{}, false
}

func tuiConfigEditorEditPlaceholder(key string) string {
	switch key {
	case "source.adapter.scdl.advanced_raw", "source.adapter.scdl.direct_raw", "source.adapter.extra_args", "source.adapter.min_version":
		return "(empty)"
	default:
		return ""
	}
}

func tuiConfigEditorEditHelp(key string) []string {
	switch key {
	case "source.adapter.scdl.advanced_raw":
		return []string{
			"Pass additional yt-dlp flags here. UDL still manages embed metadata, archives, and break behavior.",
			"Examples: --concurrent-fragments 4 --write-subs",
		}
	case "source.adapter.scdl.direct_raw":
		return []string{
			"Pass extra direct scdl flags here. Keep yt-dlp flags in the advanced yt-dlp field instead.",
			"Examples: --hide-progress --error",
		}
	case "source.adapter.min_version":
		return []string{
			"Doctor compatibility hint. For scdl, UDL expects scdl >= 3.0.0.",
			"scdl v3 is a yt-dlp wrapper and exposes --yt-dlp-args. Run `udl doctor` to verify compatibility.",
		}
	default:
		return []string{"type to edit  left/right move cursor  backspace/delete remove  enter apply  esc cancel"}
	}
}

func utf8RuneCount(value string) int {
	return len([]rune(value))
}

func deleteRuneAt(value string, idx int) string {
	runes := []rune(value)
	if idx < 0 || idx >= len(runes) {
		return value
	}
	return string(append(runes[:idx], runes[idx+1:]...))
}

func insertRunesAt(value string, idx int, extra []rune) string {
	runes := []rune(value)
	if idx < 0 {
		idx = 0
	}
	if idx > len(runes) {
		idx = len(runes)
	}
	out := append([]rune{}, runes[:idx]...)
	out = append(out, extra...)
	out = append(out, runes[idx:]...)
	return string(out)
}

func (m tuiConfigEditorModel) defaultsValidationProblems() []string {
	lines := []string{}
	stateDir, err := config.ExpandPath(m.defaults.StateDir)
	switch {
	case err != nil || strings.TrimSpace(stateDir) == "":
		lines = append(lines, "defaults.state_dir must be a valid path")
	case !filepath.IsAbs(stateDir):
		lines = append(lines, "defaults.state_dir must resolve to an absolute path")
	}
	if strings.TrimSpace(m.defaults.ArchiveFile) == "" {
		lines = append(lines, "defaults.archive_file must be set")
	}
	if m.defaults.Threads <= 0 {
		lines = append(lines, "defaults.threads must be > 0")
	}
	if m.defaults.CommandTimeoutSeconds <= 0 {
		lines = append(lines, "defaults.command_timeout_seconds must be > 0")
	}
	return lines
}

func (m tuiConfigEditorModel) sourceValidationProblems() []string {
	all := m.validationProblems
	out := make([]string, 0, len(all))
	for _, problem := range all {
		if strings.HasPrefix(problem, "defaults.") {
			continue
		}
		out = append(out, problem)
	}
	return out
}
