package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/freedl"
)

func (m *tuiFreeDLModel) openConfigEditor(addJob bool) {
	m.phase = tuiFreeDLPhaseConfig
	m.configStep = tuiFreeDLConfigStepEdit
	m.configPane = tuiFreeDLConfigPaneList
	m.configErr = nil
	m.configSaveErr = nil
	m.configSaved = false
	m.configDirty = false
	m.configEdit = nil
	m.configDeleteConfirm = false
	m.configDiscardConfirm = false
	m.configPath = m.resolveFreeDLConfigPath()
	m.configFileExists = false
	m.configProjectWarning = m.detectFreeDLProjectOverrideWarning()

	cfg := m.cfg
	if strings.TrimSpace(m.configPath) != "" {
		if info, err := os.Stat(m.configPath); err == nil && !info.IsDir() {
			m.configFileExists = true
			if loaded, loadErr := freedl.LoadSingleFile(m.configPath, m.mainConfig); loadErr == nil {
				cfg = loaded
			} else {
				m.configErr = loadErr
			}
		}
	}
	if cfg.Version == 0 {
		cfg = freedl.DefaultConfig(m.mainConfig)
	}
	if addJob || len(cfg.Jobs) == 0 {
		cfg.Jobs = append(cfg.Jobs, m.defaultFreeDLJob(cfg))
		m.configJobCursor = len(cfg.Jobs) - 1
		m.configPane = tuiFreeDLConfigPaneForm
		m.configDirty = true
	} else if m.configJobCursor >= len(cfg.Jobs) {
		m.configJobCursor = len(cfg.Jobs) - 1
	}
	m.configCfg = cfg
	m.ensureConfigCursor()
	m.revalidateConfig()
}

func (m tuiFreeDLModel) resolveFreeDLConfigPath() string {
	if m.app != nil && strings.TrimSpace(m.app.Opts.FreeDLConfigPath) != "" {
		path, err := config.ExpandPath(m.app.Opts.FreeDLConfigPath)
		if err == nil {
			return path
		}
		return strings.TrimSpace(m.app.Opts.FreeDLConfigPath)
	}
	path, err := freedl.UserConfigPath()
	if err != nil {
		return ""
	}
	return path
}

func (m tuiFreeDLModel) detectFreeDLProjectOverrideWarning() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	projectPath := freedl.ProjectConfigPath(cwd)
	if info, err := os.Stat(projectPath); err == nil && !info.IsDir() {
		target := filepath.Clean(m.resolveFreeDLConfigPath())
		if filepath.Clean(projectPath) != target {
			return "Project override exists: " + projectPath
		}
	}
	return ""
}

func (m tuiFreeDLModel) defaultFreeDLJob(cfg freedl.Config) freedl.Job {
	id := uniqueFreeDLJobID(cfg.Jobs, "soundcloud-free-dl")
	stateDir := firstNonEmpty(strings.TrimSpace(m.mainConfig.Defaults.StateDir), config.DefaultStateDir())
	base := filepath.Join(stateDir, "freedl", id)
	return freedl.Job{
		ID:              id,
		Enabled:         true,
		SourceURL:       "",
		LibraryDir:      "~/Music/downloaded/sc-likes",
		BufferDir:       filepath.Join(base, "buffer"),
		BackupDir:       filepath.Join(base, "backups"),
		LogDir:          filepath.Join(base, "logs"),
		StateFile:       id + ".sync.scdl",
		PlanLimit:       freeDLFirstPositive(cfg.Defaults.PlanLimit, freedl.DefaultPlanLimit),
		DownloadOrder:   firstNonEmpty(cfg.Defaults.DownloadOrder, freedl.DefaultDownloadOrder),
		TargetFormat:    firstNonEmpty(cfg.Defaults.TargetFormat, freedl.DefaultTargetFormat),
		MinMatchScore:   freeDLFirstPositive(cfg.Defaults.MinMatchScore, freedl.DefaultMinMatchScore),
		AmbiguityGap:    freeDLFirstPositive(cfg.Defaults.AmbiguityGap, freedl.DefaultAmbiguityGap),
		ReplaceLimit:    cfg.Defaults.ReplaceLimit,
		ApplyPromotions: false,
	}
}

func uniqueFreeDLJobID(jobs []freedl.Job, base string) string {
	candidate := strings.TrimSpace(base)
	if candidate == "" {
		candidate = "soundcloud-free-dl"
	}
	seen := map[string]struct{}{}
	for _, job := range jobs {
		seen[job.ID] = struct{}{}
	}
	if _, ok := seen[candidate]; !ok {
		return candidate
	}
	for idx := 2; ; idx++ {
		next := fmt.Sprintf("%s-%d", candidate, idx)
		if _, ok := seen[next]; !ok {
			return next
		}
	}
}

func (m tuiFreeDLModel) updateConfigKey(msg tea.KeyMsg) (tuiFreeDLModel, tea.Cmd) {
	if m.configEdit != nil {
		return m.updateConfigEdit(msg), nil
	}
	if m.configDeleteConfirm {
		switch msg.String() {
		case "y", "enter":
			m.deleteConfigJob()
		case "n", "esc":
			m.configDeleteConfirm = false
		}
		return m, nil
	}
	if m.configDiscardConfirm {
		switch msg.String() {
		case "y", "enter":
			m.configDiscardConfirm = false
			m.phase = tuiFreeDLPhaseSelect
		case "n", "esc":
			m.configDiscardConfirm = false
		}
		return m, nil
	}
	switch m.configStep {
	case tuiFreeDLConfigStepReview, tuiFreeDLConfigStepSave:
		return m.updateConfigReviewKey(msg), nil
	default:
		return m.updateConfigEditKey(msg), nil
	}
}

func (m tuiFreeDLModel) updateConfigEditKey(msg tea.KeyMsg) tuiFreeDLModel {
	switch msg.String() {
	case "esc":
		if m.configDirty {
			m.configDiscardConfirm = true
		} else {
			m.phase = tuiFreeDLPhaseSelect
		}
	case "tab", "shift+tab":
		if m.configPane == tuiFreeDLConfigPaneList {
			m.configPane = tuiFreeDLConfigPaneForm
		} else {
			m.configPane = tuiFreeDLConfigPaneList
		}
	case "r":
		m.configStep = tuiFreeDLConfigStepReview
	case "s":
		return m.saveConfig()
	}
	if m.configPane == tuiFreeDLConfigPaneList {
		switch msg.String() {
		case "up", "k":
			if m.configJobCursor > 0 {
				m.configJobCursor--
				m.ensureConfigCursor()
			}
		case "down", "j":
			if m.configJobCursor < len(m.configCfg.Jobs)-1 {
				m.configJobCursor++
				m.ensureConfigCursor()
			}
		case "a":
			m.configCfg.Jobs = append(m.configCfg.Jobs, m.defaultFreeDLJob(m.configCfg))
			m.configJobCursor = len(m.configCfg.Jobs) - 1
			m.configPane = tuiFreeDLConfigPaneForm
			m.configDirty = true
			m.configSaved = false
			m.revalidateConfig()
		case "d":
			if job := m.currentConfigJob(); job != nil {
				copyJob := *job
				copyJob.ID = uniqueFreeDLJobID(m.configCfg.Jobs, job.ID+"-copy")
				m.configCfg.Jobs = append(m.configCfg.Jobs[:m.configJobCursor+1], append([]freedl.Job{copyJob}, m.configCfg.Jobs[m.configJobCursor+1:]...)...)
				m.configJobCursor++
				m.configPane = tuiFreeDLConfigPaneForm
				m.configDirty = true
				m.configSaved = false
				m.revalidateConfig()
			}
		case "D":
			if len(m.configCfg.Jobs) > 0 {
				m.configDeleteConfirm = true
			}
		case "space":
			if job := m.currentConfigJob(); job != nil {
				job.Enabled = !job.Enabled
				m.configDirty = true
				m.configSaved = false
				m.revalidateConfig()
			}
		case "enter":
			if len(m.configCfg.Jobs) == 0 {
				m.configCfg.Jobs = append(m.configCfg.Jobs, m.defaultFreeDLJob(m.configCfg))
				m.configJobCursor = 0
				m.configDirty = true
				m.configSaved = false
			}
			m.configPane = tuiFreeDLConfigPaneForm
		}
		return m
	}
	fields := m.configFields()
	switch msg.String() {
	case "up", "k":
		if m.configFieldCursor > 0 {
			m.configFieldCursor--
		}
	case "down", "j":
		if m.configFieldCursor < len(fields)-1 {
			m.configFieldCursor++
		}
	case "space", "enter":
		m.applyConfigField(fields, msg.String() == "space")
	}
	return m
}

func (m tuiFreeDLModel) updateConfigReviewKey(msg tea.KeyMsg) tuiFreeDLModel {
	switch msg.String() {
	case "esc":
		if m.configStep == tuiFreeDLConfigStepSave && !m.configDirty {
			m.phase = tuiFreeDLPhaseSelect
			return m
		}
		m.configStep = tuiFreeDLConfigStepEdit
	case "up", "k":
		if m.configReviewCursor > 0 {
			m.configReviewCursor--
		}
	case "down", "j":
		if m.configReviewCursor < len(m.configCfg.Jobs) {
			m.configReviewCursor++
		}
	case "s", "enter":
		return m.saveConfig()
	}
	return m
}

func (m tuiFreeDLModel) updateConfigEdit(msg tea.KeyMsg) tuiFreeDLModel {
	switch msg.String() {
	case "enter":
		if err := m.applyConfigEdit(); err != nil {
			m.configErr = err
			return m
		}
		m.configEdit = nil
		m.configDirty = true
		m.configSaved = false
		m.configErr = nil
		m.configSaveErr = nil
		m.revalidateConfig()
	case "esc", "q":
		m.configEdit = nil
	case "left", "ctrl+b":
		if m.configEdit.Cursor > 0 {
			m.configEdit.Cursor--
		}
	case "right", "ctrl+f":
		if m.configEdit.Cursor < utf8RuneCount(m.configEdit.Buffer) {
			m.configEdit.Cursor++
		}
	case "home", "ctrl+a":
		m.configEdit.Cursor = 0
	case "end", "ctrl+e":
		m.configEdit.Cursor = utf8RuneCount(m.configEdit.Buffer)
	case "backspace", "ctrl+h":
		if m.configEdit.Cursor > 0 {
			m.configEdit.Buffer = deleteRuneAt(m.configEdit.Buffer, m.configEdit.Cursor-1)
			m.configEdit.Cursor--
		}
	case "delete", "ctrl+d":
		if m.configEdit.Cursor < utf8RuneCount(m.configEdit.Buffer) {
			m.configEdit.Buffer = deleteRuneAt(m.configEdit.Buffer, m.configEdit.Cursor)
		}
	default:
		if len(msg.Runes) > 0 {
			m.configEdit.Buffer = insertRunesAt(m.configEdit.Buffer, m.configEdit.Cursor, msg.Runes)
			m.configEdit.Cursor += len(msg.Runes)
		}
	}
	return m
}

func (m *tuiFreeDLModel) applyConfigEdit() error {
	if m.configEdit == nil {
		return nil
	}
	raw := strings.TrimSpace(m.configEdit.Buffer)
	job := m.currentConfigJob()
	switch m.configEdit.Key {
	case "job.id":
		if job != nil {
			oldID := job.ID
			job.ID = raw
			if !strings.Contains(job.StateFile, oldID) || strings.TrimSpace(job.StateFile) == "" {
				job.StateFile = raw + ".sync.scdl"
			}
		}
	case "job.source_url":
		if job != nil {
			job.SourceURL = raw
		}
	case "job.library_dir":
		if job != nil {
			job.LibraryDir = raw
		}
	case "job.buffer_dir":
		if job != nil {
			job.BufferDir = raw
		}
	case "job.backup_dir":
		if job != nil {
			job.BackupDir = raw
		}
	case "job.log_dir":
		if job != nil {
			job.LogDir = raw
		}
	case "job.state_file":
		if job != nil {
			job.StateFile = raw
		}
	case "job.plan_limit":
		value, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("plan_limit must be a number")
		}
		if job != nil {
			job.PlanLimit = value
		}
	case "job.min_match_score":
		value, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("min_match_score must be a number")
		}
		if job != nil {
			job.MinMatchScore = value
		}
	case "job.ambiguity_gap":
		value, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("ambiguity_gap must be a number")
		}
		if job != nil {
			job.AmbiguityGap = value
		}
	case "job.replace_limit":
		value, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("replace_limit must be a number")
		}
		if job != nil {
			job.ReplaceLimit = value
		}
	default:
		return fmt.Errorf("unknown Free DL field %q", m.configEdit.Key)
	}
	return nil
}

func (m *tuiFreeDLModel) applyConfigField(fields []tuiConfigEditorFormField, fromSpace bool) {
	if len(fields) == 0 || m.configFieldCursor < 0 || m.configFieldCursor >= len(fields) {
		return
	}
	field := fields[m.configFieldCursor]
	job := m.currentConfigJob()
	if field.Action {
		m.configStep = tuiFreeDLConfigStepReview
		return
	}
	if job == nil {
		return
	}
	switch field.Key {
	case "job.enabled":
		job.Enabled = !job.Enabled
	case "job.download_order":
		if job.DownloadOrder == "newest_first" {
			job.DownloadOrder = freedl.DefaultDownloadOrder
		} else {
			job.DownloadOrder = "newest_first"
		}
	case "job.target_format":
		job.TargetFormat = nextFreeDLTargetFormat(job.TargetFormat)
	default:
		if !fromSpace {
			m.startConfigEdit(field.Key, "Edit "+field.Label, field.Value)
			return
		}
		return
	}
	m.configDirty = true
	m.configSaved = false
	m.revalidateConfig()
}

func nextFreeDLTargetFormat(current string) string {
	formats := []string{freedl.TargetAuto, freedl.TargetMP3320, freedl.TargetAAC256, freedl.TargetWAV}
	for idx, format := range formats {
		if current == format {
			return formats[(idx+1)%len(formats)]
		}
	}
	return freedl.TargetAuto
}

func (m *tuiFreeDLModel) startConfigEdit(key, title, value string) {
	m.configEdit = &tuiConfigEditorInlineEditState{
		Key:         key,
		Title:       title,
		Buffer:      value,
		Cursor:      utf8RuneCount(value),
		Placeholder: "",
		Help:        []string{"type to edit  left/right move cursor  backspace/delete remove  enter apply  esc cancel"},
	}
}

func (m tuiFreeDLModel) saveConfig() tuiFreeDLModel {
	m.configSaveErr = nil
	m.configErr = nil
	m.revalidateConfig()
	if len(m.configValidation) > 0 {
		m.configErr = fmt.Errorf("fix validation issues before saving")
		return m
	}
	if strings.TrimSpace(m.configPath) == "" {
		m.configSaveErr = fmt.Errorf("freedl config target path could not be resolved")
		return m
	}
	if err := freedl.SaveSingleFile(m.configPath, m.configCfg, m.mainConfig); err != nil {
		m.configSaveErr = err
		return m
	}
	m.configFileExists = true
	m.configDirty = false
	m.configSaved = true
	m.configStep = tuiFreeDLConfigStepSave
	m.cfg = m.configCfg
	m.jobs = freedl.EnabledJobs(m.cfg)
	if m.jobCursor >= len(m.jobs) {
		m.jobCursor = len(m.jobs) - 1
	}
	if m.jobCursor < 0 {
		m.jobCursor = 0
	}
	m.planLimit = m.jobPlanLimit()
	return m
}

func (m *tuiFreeDLModel) revalidateConfig() {
	cfg := m.configCfg
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	err := freedl.Validate(cfg)
	if err == nil {
		m.configValidation = nil
		return
	}
	m.configValidation = tuiSplitDetailLines(err.Error())
}

func (m *tuiFreeDLModel) ensureConfigCursor() {
	if m.configJobCursor < 0 {
		m.configJobCursor = 0
	}
	if m.configJobCursor >= len(m.configCfg.Jobs) {
		m.configJobCursor = len(m.configCfg.Jobs) - 1
	}
	if m.configJobCursor < 0 {
		m.configJobCursor = 0
	}
	fields := m.configFields()
	if m.configFieldCursor >= len(fields) {
		m.configFieldCursor = len(fields) - 1
	}
	if m.configFieldCursor < 0 {
		m.configFieldCursor = 0
	}
}

func (m *tuiFreeDLModel) deleteConfigJob() {
	if len(m.configCfg.Jobs) == 0 || m.configJobCursor < 0 || m.configJobCursor >= len(m.configCfg.Jobs) {
		m.configDeleteConfirm = false
		return
	}
	m.configCfg.Jobs = append(m.configCfg.Jobs[:m.configJobCursor], m.configCfg.Jobs[m.configJobCursor+1:]...)
	m.configDeleteConfirm = false
	m.configDirty = true
	m.configSaved = false
	m.ensureConfigCursor()
	m.revalidateConfig()
}

func (m *tuiFreeDLModel) currentConfigJob() *freedl.Job {
	if len(m.configCfg.Jobs) == 0 || m.configJobCursor < 0 || m.configJobCursor >= len(m.configCfg.Jobs) {
		return nil
	}
	return &m.configCfg.Jobs[m.configJobCursor]
}

func (m *tuiFreeDLModel) configFields() []tuiConfigEditorFormField {
	job := m.currentConfigJob()
	if job == nil {
		return []tuiConfigEditorFormField{{Key: "action.review", Label: "Open Review", Value: "enter", Kind: "action", Action: true}}
	}
	return []tuiConfigEditorFormField{
		{Key: "job.id", Label: "Identity · id", Value: job.ID, Kind: "text"},
		{Key: "job.enabled", Label: "Identity · enabled", Value: tuiBoolLabel(job.Enabled), Kind: "bool"},
		{Key: "job.source_url", Label: "Remote · source_url", Value: job.SourceURL, Kind: "text"},
		{Key: "job.library_dir", Label: "Location · library_dir", Value: job.LibraryDir, Kind: "text"},
		{Key: "job.buffer_dir", Label: "Location · buffer_dir", Value: job.BufferDir, Kind: "text"},
		{Key: "job.backup_dir", Label: "Location · backup_dir", Value: job.BackupDir, Kind: "text"},
		{Key: "job.log_dir", Label: "Location · log_dir", Value: job.LogDir, Kind: "text"},
		{Key: "job.state_file", Label: "Location · state_file", Value: job.StateFile, Kind: "text"},
		{Key: "job.plan_limit", Label: "Planning · plan_limit", Value: strconv.Itoa(job.PlanLimit), Kind: "number"},
		{Key: "job.download_order", Label: "Planning · download_order", Value: job.DownloadOrder, Kind: "select"},
		{Key: "job.target_format", Label: "Promotion · target_format", Value: job.TargetFormat, Kind: "select"},
		{Key: "job.min_match_score", Label: "Matching · min_match_score", Value: strconv.Itoa(job.MinMatchScore), Kind: "number"},
		{Key: "job.ambiguity_gap", Label: "Matching · ambiguity_gap", Value: strconv.Itoa(job.AmbiguityGap), Kind: "number"},
		{Key: "job.replace_limit", Label: "Safety · replace_limit", Value: strconv.Itoa(job.ReplaceLimit), Kind: "number"},
		{Key: "action.review", Label: "Open Review", Value: "enter", Kind: "action", Action: true},
	}
}

func freeDLFirstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
