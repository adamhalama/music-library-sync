package cli

import (
	"context"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jaa/update-downloads/internal/auth"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/doctor"
)

type tuiOnboardingPhase string

const (
	tuiOnboardingPhaseIntro     tuiOnboardingPhase = "intro"
	tuiOnboardingPhaseLocations tuiOnboardingPhase = "locations"
	tuiOnboardingPhaseSource    tuiOnboardingPhase = "source"
	tuiOnboardingPhaseCredentials tuiOnboardingPhase = "credentials"
	tuiOnboardingPhaseReview    tuiOnboardingPhase = "review"
	tuiOnboardingPhaseSaving    tuiOnboardingPhase = "saving"
	tuiOnboardingPhaseDone      tuiOnboardingPhase = "done"
)

type tuiOnboardingReason string

const (
	tuiOnboardingReasonFirstRun      tuiOnboardingReason = "first_run"
	tuiOnboardingReasonNoSources     tuiOnboardingReason = "no_sources"
	tuiOnboardingReasonInvalidConfig tuiOnboardingReason = "invalid_config"
)

type tuiOnboardingStartupState struct {
	Reason             tuiOnboardingReason
	AutoStarted        bool
	ConfigPath         string
	ConfigContextLabel string
	DetailLines        []string
	Defaults           config.Defaults
}

type tuiOnboardingInlineEditState struct {
	Field       string
	Title       string
	Buffer      string
	Cursor      int
	Placeholder string
	Help        []string
}

type tuiOnboardingSaveState struct {
	Path      string
	StateDir  string
	TargetDir string
}

type tuiOnboardingDoneMsg struct {
	SaveState tuiOnboardingSaveState
	Report    doctor.Report
	Err       error
}

type tuiOnboardingModel struct {
	app                *AppContext
	phase              tuiOnboardingPhase
	startup            tuiOnboardingStartupState
	locationsCursor    int
	sourceCursor       int
	credentialsCursor  int
	libraryRoot        string
	stateDir           string
	sourceType         config.SourceType
	sourceID           string
	sourceURL          string
	soundCloudClientID string
	deemixARL          string
	spotifyClientID    string
	spotifyClientSecret string
	edit               *tuiOnboardingInlineEditState
	saveResult         *tuiOnboardingSaveState
	saveErr            error
	doctorReport       doctor.Report
	doctorChecks       []doctor.Check
	doctorSummary      tuiDoctorSummaryState
	validationProblems []string
}

func newTUIOnboardingModel(app *AppContext, startup tuiOnboardingStartupState) tuiOnboardingModel {
	model := tuiOnboardingModel{
		app:         app,
		phase:       tuiOnboardingPhaseIntro,
		startup:     startup,
		libraryRoot: filepath.Join("~", "Music", "downloaded"),
		stateDir:    firstNonEmpty(startup.Defaults.StateDir, config.DefaultConfig().Defaults.StateDir),
		sourceType:  config.SourceTypeSoundCloud,
		sourceID:    "soundcloud-likes",
		sourceURL:   "https://soundcloud.com/your-user",
	}
	if value, _, err := auth.ResolveSoundCloudClientIDWithSource(); err == nil {
		model.soundCloudClientID = value
	}
	if value, _, err := auth.ResolveDeemixARLWithSource(); err == nil {
		model.deemixARL = value
	}
	if creds, _, err := auth.ResolveSpotifyCredentialsWithSource(); err == nil {
		model.spotifyClientID = creds.ClientID
		model.spotifyClientSecret = creds.ClientSecret
	}
	model.revalidate()
	return model
}

func (m tuiOnboardingModel) Init() tea.Cmd {
	return nil
}

func (m tuiOnboardingModel) Update(msg tea.Msg) (tuiOnboardingModel, tea.Cmd) {
	switch typed := msg.(type) {
	case tuiOnboardingDoneMsg:
		m.phase = tuiOnboardingPhaseDone
		m.saveErr = typed.Err
		if typed.Err == nil {
			m.saveResult = &typed.SaveState
			m.doctorReport = typed.Report
			m.doctorChecks = tuiSortedDoctorChecks(typed.Report.Checks)
			m.doctorSummary = tuiDoctorSummary(typed.Report.Checks)
		}
		return m, nil
	case tea.KeyMsg:
		if m.edit != nil {
			return m.updateEdit(typed)
		}
		switch typed.String() {
		case "esc":
			switch m.phase {
			case tuiOnboardingPhaseLocations:
				m.phase = tuiOnboardingPhaseIntro
				return m, nil
			case tuiOnboardingPhaseSource:
				m.phase = tuiOnboardingPhaseLocations
				return m, nil
			case tuiOnboardingPhaseCredentials:
				m.phase = tuiOnboardingPhaseSource
				return m, nil
			case tuiOnboardingPhaseReview:
				m.phase = tuiOnboardingPhaseCredentials
				return m, nil
			}
		}
		switch m.phase {
		case tuiOnboardingPhaseIntro:
			return m.updateIntro(typed)
		case tuiOnboardingPhaseLocations:
			return m.updateLocations(typed)
		case tuiOnboardingPhaseSource:
			return m.updateSource(typed)
		case tuiOnboardingPhaseCredentials:
			return m.updateCredentials(typed)
		case tuiOnboardingPhaseReview:
			return m.updateReview(typed)
		}
	}
	return m, nil
}

func (m tuiOnboardingModel) View() string {
	return "Get Started"
}

func (m tuiOnboardingModel) allowBack() bool {
	return m.phase != tuiOnboardingPhaseSaving && m.edit == nil
}

func (m tuiOnboardingModel) updateIntro(msg tea.KeyMsg) (tuiOnboardingModel, tea.Cmd) {
	if msg.String() == "enter" {
		m.phase = tuiOnboardingPhaseLocations
	}
	return m, nil
}

func (m tuiOnboardingModel) updateLocations(msg tea.KeyMsg) (tuiOnboardingModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.locationsCursor > 0 {
			m.locationsCursor--
		}
	case "down", "j":
		if m.locationsCursor < 2 {
			m.locationsCursor++
		}
	case "enter":
		switch m.locationsCursor {
		case 0:
			m.startEdit("library_root", "Music Folder", m.libraryRoot, []string{
				"Choose the parent folder where UDL will create source folders.",
				"Example: ~/Music/downloaded",
			})
		case 1:
			m.startEdit("state_dir", "State Folder", m.stateDir, []string{
				"UDL stores sync state and archive data here.",
				"The default is fine for most users.",
			})
		default:
			m.phase = tuiOnboardingPhaseSource
		}
	}
	return m, nil
}

func (m tuiOnboardingModel) updateSource(msg tea.KeyMsg) (tuiOnboardingModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.sourceCursor > 0 {
			m.sourceCursor--
		}
	case "down", "j":
		if m.sourceCursor < 3 {
			m.sourceCursor++
		}
	case "space":
		if m.sourceCursor == 0 {
			m.toggleSourceType()
		}
	case "enter":
		switch m.sourceCursor {
		case 0:
			m.toggleSourceType()
		case 1:
			m.startEdit("source_id", "Source Name", m.sourceID, []string{
				"Use a short folder-friendly label. UDL uses it for the target folder and state file.",
				"Example: soundcloud-likes",
			})
		case 2:
			m.startEdit("source_url", "Source URL", m.sourceURL, m.sourceURLHelp())
		default:
			m.phase = tuiOnboardingPhaseCredentials
			m.revalidate()
		}
	}
	return m, nil
}

func (m tuiOnboardingModel) updateCredentials(msg tea.KeyMsg) (tuiOnboardingModel, tea.Cmd) {
	limit := 1
	if m.sourceType == config.SourceTypeSpotify {
		limit = 3
	}
	switch msg.String() {
	case "up", "k":
		if m.credentialsCursor > 0 {
			m.credentialsCursor--
		}
	case "down", "j":
		if m.credentialsCursor < limit {
			m.credentialsCursor++
		}
	case "enter":
		if m.sourceType == config.SourceTypeSpotify {
			switch m.credentialsCursor {
			case 0:
				m.startEdit("deemix_arl", "Deezer ARL", m.deemixARL, []string{
					"Paste the Deezer ARL for Spotify deemix conversion.",
					"UDL stores it in macOS Keychain, not in YAML.",
				})
			case 1:
				m.startEdit("spotify_client_id", "Spotify Client ID", m.spotifyClientID, []string{
					"Paste your Spotify app client ID.",
					"UDL stores it in macOS Keychain, not in YAML.",
				})
			case 2:
				m.startEdit("spotify_client_secret", "Spotify Client Secret", m.spotifyClientSecret, []string{
					"Paste your Spotify app client secret.",
					"UDL stores it in macOS Keychain, not in YAML.",
				})
			default:
				m.phase = tuiOnboardingPhaseReview
			}
			return m, nil
		}
		switch m.credentialsCursor {
		case 0:
			m.startEdit("soundcloud_client_id", "SoundCloud Client ID", m.soundCloudClientID, []string{
				"Paste the SoundCloud client ID used by scdl.",
				"UDL stores it in macOS Keychain, not in YAML.",
			})
		default:
			m.phase = tuiOnboardingPhaseReview
		}
	}
	return m, nil
}

func (m tuiOnboardingModel) updateReview(msg tea.KeyMsg) (tuiOnboardingModel, tea.Cmd) {
	switch msg.String() {
	case "s", "enter":
		m.revalidate()
		if len(m.validationProblems) > 0 {
			return m, nil
		}
		m.phase = tuiOnboardingPhaseSaving
		m.saveErr = nil
		return m, m.saveAndCheckCmd()
	}
	return m, nil
}

func (m tuiOnboardingModel) updateEdit(msg tea.KeyMsg) (tuiOnboardingModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.applyEdit()
		m.edit = nil
		m.revalidate()
	case "esc", "q":
		m.edit = nil
	case "left", "ctrl+b":
		if m.edit.Cursor > 0 {
			m.edit.Cursor--
		}
	case "right", "ctrl+f":
		if m.edit.Cursor < utf8RuneCount(m.edit.Buffer) {
			m.edit.Cursor++
		}
	case "home", "ctrl+a":
		m.edit.Cursor = 0
	case "end", "ctrl+e":
		m.edit.Cursor = utf8RuneCount(m.edit.Buffer)
	case "backspace", "ctrl+h":
		if m.edit.Cursor > 0 {
			m.edit.Buffer = deleteRuneAt(m.edit.Buffer, m.edit.Cursor-1)
			m.edit.Cursor--
		}
	case "delete", "ctrl+d":
		if m.edit.Cursor < utf8RuneCount(m.edit.Buffer) {
			m.edit.Buffer = deleteRuneAt(m.edit.Buffer, m.edit.Cursor)
		}
	default:
		if len(msg.Runes) > 0 {
			m.edit.Buffer = insertRunesAt(m.edit.Buffer, m.edit.Cursor, msg.Runes)
			m.edit.Cursor += len(msg.Runes)
		}
	}
	return m, nil
}

func (m *tuiOnboardingModel) startEdit(field string, title string, value string, help []string) {
	m.edit = &tuiOnboardingInlineEditState{
		Field:  field,
		Title:  title,
		Buffer: value,
		Cursor: utf8RuneCount(value),
		Help:   help,
	}
}

func (m *tuiOnboardingModel) applyEdit() {
	if m.edit == nil {
		return
	}
	value := strings.TrimSpace(m.edit.Buffer)
	switch m.edit.Field {
	case "library_root":
		m.libraryRoot = value
	case "state_dir":
		m.stateDir = value
	case "source_id":
		m.sourceID = value
	case "source_url":
		m.sourceURL = value
	case "soundcloud_client_id":
		m.soundCloudClientID = value
	case "deemix_arl":
		m.deemixARL = value
	case "spotify_client_id":
		m.spotifyClientID = value
	case "spotify_client_secret":
		m.spotifyClientSecret = value
	}
}

func (m *tuiOnboardingModel) toggleSourceType() {
	if m.sourceType == config.SourceTypeSoundCloud {
		m.sourceType = config.SourceTypeSpotify
		if m.sourceID == "" || m.sourceID == "soundcloud-likes" {
			m.sourceID = "spotify-playlist"
		}
		if m.sourceURL == "" || m.sourceURL == "https://soundcloud.com/your-user" {
			m.sourceURL = "https://open.spotify.com/playlist/replace-me"
		}
	} else {
		m.sourceType = config.SourceTypeSoundCloud
		if m.sourceID == "" || m.sourceID == "spotify-playlist" {
			m.sourceID = "soundcloud-likes"
		}
		if m.sourceURL == "" || m.sourceURL == "https://open.spotify.com/playlist/replace-me" {
			m.sourceURL = "https://soundcloud.com/your-user"
		}
	}
	m.revalidate()
}

func (m tuiOnboardingModel) buildConfig() config.Config {
	cfg := config.Config{
		Version: 1,
		Defaults: config.Defaults{
			StateDir:              strings.TrimSpace(m.stateDir),
			ArchiveFile:           m.startup.Defaults.ArchiveFile,
			Threads:               m.startup.Defaults.Threads,
			ContinueOnError:       m.startup.Defaults.ContinueOnError,
			CommandTimeoutSeconds: m.startup.Defaults.CommandTimeoutSeconds,
		},
		Sources: []config.Source{
			{
				ID:        strings.TrimSpace(m.sourceID),
				Type:      m.sourceType,
				Enabled:   true,
				TargetDir: filepath.Join(strings.TrimSpace(m.libraryRoot), strings.TrimSpace(m.sourceID)),
				URL:       strings.TrimSpace(m.sourceURL),
				StateFile: tuiConfigEditorSuggestedStateFile(strings.TrimSpace(m.sourceID), m.sourceType),
				Adapter: config.AdapterSpec{
					Kind: tuiConfigEditorDefaultAdapterKind(m.sourceType),
				},
			},
		},
	}
	return cfg
}

func (m *tuiOnboardingModel) revalidate() {
	problems := []string{}
	cfg := m.buildConfig()
	if strings.TrimSpace(m.libraryRoot) == "" {
		problems = append(problems, "Choose a music folder before saving.")
	}
	if strings.TrimSpace(m.stateDir) == "" {
		problems = append(problems, "Choose a state folder before saving.")
	}
	if err := config.Validate(cfg); err != nil {
		if validationErr, ok := err.(*config.ValidationError); ok {
			for _, problem := range validationErr.Problems {
				if strings.Contains(problem, "defaults.archive_file") || strings.Contains(problem, "defaults.threads") || strings.Contains(problem, "defaults.command_timeout_seconds") {
					continue
				}
				problems = append(problems, problem)
			}
		} else {
			problems = append(problems, err.Error())
		}
	}
	m.validationProblems = dedupeStrings(problems)
}

func (m tuiOnboardingModel) saveAndCheckCmd() tea.Cmd {
	cfg := m.buildConfig()
	targetPath := m.startup.ConfigPath
	targetDir, _ := config.ExpandPath(cfg.Sources[0].TargetDir)
	return func() tea.Msg {
		switch m.sourceType {
		case config.SourceTypeSoundCloud:
			if strings.TrimSpace(m.soundCloudClientID) != "" {
				if err := auth.SaveSoundCloudClientID(m.soundCloudClientID); err != nil {
					return tuiOnboardingDoneMsg{Err: err}
				}
				_ = auth.ClearCredentialFailure(cfg.Defaults.StateDir, auth.CredentialKindSoundCloudClientID)
			}
		case config.SourceTypeSpotify:
			if strings.TrimSpace(m.deemixARL) != "" {
				if err := auth.SaveDeemixARL(m.deemixARL); err != nil {
					return tuiOnboardingDoneMsg{Err: err}
				}
				_ = auth.ClearCredentialFailure(cfg.Defaults.StateDir, auth.CredentialKindDeemixARL)
			}
			if strings.TrimSpace(m.spotifyClientID) != "" || strings.TrimSpace(m.spotifyClientSecret) != "" {
				if err := auth.SaveSpotifyCredentials(auth.SpotifyCredentials{
					ClientID:     m.spotifyClientID,
					ClientSecret: m.spotifyClientSecret,
				}); err != nil {
					return tuiOnboardingDoneMsg{Err: err}
				}
				_ = auth.ClearCredentialFailure(cfg.Defaults.StateDir, auth.CredentialKindSpotifyApp)
			}
		}
		stateDir, err := config.SaveSingleFile(targetPath, cfg)
		if err != nil {
			return tuiOnboardingDoneMsg{Err: err}
		}
		report := doctor.NewChecker().Check(context.Background(), cfg)
		return tuiOnboardingDoneMsg{
			SaveState: tuiOnboardingSaveState{
				Path:      targetPath,
				StateDir:  stateDir,
				TargetDir: targetDir,
			},
			Report: report,
		}
	}
}

func (m tuiOnboardingModel) sourceURLHelp() []string {
	if m.sourceType == config.SourceTypeSpotify {
		return []string{
			"Spotify is available, but this v1 release treats it as beta/advanced.",
			"Paste a playlist URL. The next step can save Deezer ARL and Spotify app credentials into macOS Keychain.",
		}
	}
	return []string{
		"Paste the SoundCloud profile, likes, or playlist URL you want to sync.",
		"The Homebrew install prefers bundled SoundCloud tools when available.",
	}
}

func (m tuiOnboardingModel) sourceTypeLabel() string {
	if m.sourceType == config.SourceTypeSpotify {
		return "Spotify (beta/advanced)"
	}
	return "SoundCloud (recommended)"
}

func (m tuiOnboardingModel) startupHeadline() string {
	switch m.startup.Reason {
	case tuiOnboardingReasonInvalidConfig:
		return "UDL found a config problem and is switching to guided recovery."
	case tuiOnboardingReasonNoSources:
		return "UDL is installed, but no music sources are configured yet."
	default:
		return "UDL is ready for first-time setup."
	}
}

func (m tuiOnboardingModel) currentTargetDir() string {
	return filepath.Join(strings.TrimSpace(m.libraryRoot), strings.TrimSpace(m.sourceID))
}

func (m tuiOnboardingModel) nextStepLines() []string {
	if m.saveErr != nil {
		return []string{"Fix the save problem above, then press esc to return and try again."}
	}
	if reportHasCheckContaining(m.doctorReport, "scdl not found") || reportHasCheckContaining(m.doctorReport, "yt-dlp not found") {
		return []string{"UDL could not find its SoundCloud tools. Reinstall with Homebrew, then reopen `udl tui` and choose Check System."}
	}
	if reportHasCheckContaining(m.doctorReport, "SoundCloud client ID is missing") {
		return []string{"Open `udl tui`, choose Credentials, and save the SoundCloud client ID to Keychain before your first sync."}
	}
	if reportHasCheckContaining(m.doctorReport, "needs refresh") {
		return []string{"One credential needs refresh. Open `udl tui`, choose Credentials, update it, then rerun Check System."}
	}
	if reportHasCheckContaining(m.doctorReport, "Deezer ARL") {
		return []string{"Open `udl tui`, choose Credentials, and save Deezer ARL to Keychain before syncing Spotify with deemix."}
	}
	if reportHasCheckContaining(m.doctorReport, "Spotify app credentials") {
		return []string{"Open `udl tui`, choose Credentials, and save Spotify app credentials to Keychain before syncing Spotify with deemix."}
	}
	if m.doctorReport.HasErrors() {
		return []string{"Fix the blocking issue above, then choose Check System before your first sync."}
	}
	if m.doctorSummary.WarnCount > 0 {
		return []string{"Your setup is close. Choose Run Sync and start with a dry-run before downloading anything."}
	}
	return []string{"Setup looks ready. Choose Run Sync, start with a dry-run, then run a real sync when the plan looks right."}
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func reportHasCheckContaining(report doctor.Report, fragment string) bool {
	needle := strings.ToLower(strings.TrimSpace(fragment))
	if needle == "" {
		return false
	}
	for _, check := range report.Checks {
		if strings.Contains(strings.ToLower(check.Message), needle) {
			return true
		}
	}
	return false
}
