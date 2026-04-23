package cli

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jaa/update-downloads/internal/auth"
	"github.com/jaa/update-downloads/internal/config"
)

type tuiCredentialsCard struct {
	Kind              auth.CredentialKind
	Title             string
	StatusLabel       string
	StorageLabel      string
	AffectedWorkflows string
	Summary           string
	ActionLabel       string
	Tone              string
	LastCheckedLabel  string
	Clearable         bool
	External          bool
}

type tuiCredentialsEditState struct {
	Kind            auth.CredentialKind
	Field           string
	Title           string
	Buffer          string
	Cursor          int
	MaskInput       bool
	Help            []string
	NextSpotifyID   string
	ExternalSource  auth.CredentialStorageSource
}

type tuiCredentialsLoadMsg struct {
	StateDir string
	Cards    []tuiCredentialsCard
	Err      error
}

type tuiCredentialsSavedMsg struct {
	Flash string
	Err   error
}

type tuiCredentialsModel struct {
	app       *AppContext
	stateDir  string
	cursor    int
	cards     []tuiCredentialsCard
	edit      *tuiCredentialsEditState
	loading   bool
	err       error
	flash     string
	focusKind auth.CredentialKind
}

func newTUICredentialsModel(app *AppContext, focusKind auth.CredentialKind) tuiCredentialsModel {
	return tuiCredentialsModel{
		app:       app,
		loading:   true,
		focusKind: focusKind,
	}
}

func (m tuiCredentialsModel) Init() tea.Cmd {
	return m.reloadCmd()
}

func (m tuiCredentialsModel) Update(msg tea.Msg) (tuiCredentialsModel, tea.Cmd) {
	switch typed := msg.(type) {
	case tuiCredentialsLoadMsg:
		m.loading = false
		m.err = typed.Err
		m.stateDir = typed.StateDir
		m.cards = typed.Cards
		m.cursor = m.clampCursor()
		if m.focusKind != "" {
			for idx, card := range m.cards {
				if card.Kind == m.focusKind {
					m.cursor = idx
					break
				}
			}
			m.focusKind = ""
		}
		return m, nil
	case tuiCredentialsSavedMsg:
		m.edit = nil
		m.flash = typed.Flash
		m.err = typed.Err
		return m, m.reloadCmd()
	case tea.KeyMsg:
		if m.edit != nil {
			return m.updateEdit(typed)
		}
		switch typed.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.cards)-1 {
				m.cursor++
			}
		case "r":
			m.loading = true
			m.err = nil
			m.flash = ""
			return m, m.reloadCmd()
		case "x":
			card := m.selectedCard()
			if card != nil && card.Clearable {
				return m, m.clearCredentialCmd(card.Kind)
			}
		case "enter":
			card := m.selectedCard()
			if card != nil {
				m.startEditForCard(*card)
			}
		}
	}
	return m, nil
}

func (m tuiCredentialsModel) View() string {
	return "Credentials"
}

func (m tuiCredentialsModel) allowBack() bool {
	return m.edit == nil
}

func (m tuiCredentialsModel) reloadCmd() tea.Cmd {
	return func() tea.Msg {
		stateDir, cards, err := tuiLoadCredentialCards(m.app)
		return tuiCredentialsLoadMsg{
			StateDir: stateDir,
			Cards:    cards,
			Err:      err,
		}
	}
}

func (m tuiCredentialsModel) selectedCard() *tuiCredentialsCard {
	if len(m.cards) == 0 {
		return nil
	}
	idx := m.clampCursor()
	return &m.cards[idx]
}

func (m tuiCredentialsModel) clampCursor() int {
	if len(m.cards) == 0 {
		return 0
	}
	if m.cursor < 0 {
		return 0
	}
	if m.cursor >= len(m.cards) {
		return len(m.cards) - 1
	}
	return m.cursor
}

func (m *tuiCredentialsModel) startEditForCard(card tuiCredentialsCard) {
	switch card.Kind {
	case auth.CredentialKindSoundCloudClientID:
		value, source, _ := auth.ResolveSoundCloudClientIDWithSource()
		m.edit = &tuiCredentialsEditState{
			Kind:           card.Kind,
			Field:          "soundcloud_client_id",
			Title:          "SoundCloud Client ID",
			Buffer:         value,
			Cursor:         utf8RuneCount(value),
			ExternalSource: source,
			Help: []string{
				"Paste the current SoundCloud client ID used by scdl.",
				"UDL saves it to macOS Keychain, not to YAML.",
			},
		}
	case auth.CredentialKindDeemixARL:
		value, source, _ := auth.ResolveDeemixARLWithSource()
		m.edit = &tuiCredentialsEditState{
			Kind:           card.Kind,
			Field:          "deemix_arl",
			Title:          "Deezer ARL",
			Buffer:         value,
			Cursor:         utf8RuneCount(value),
			MaskInput:      true,
			ExternalSource: source,
			Help: []string{
				"Paste the Deezer ARL used for Spotify-to-Deezer conversion.",
				"UDL saves it to macOS Keychain, not to YAML.",
			},
		}
	case auth.CredentialKindSpotifyApp:
		creds, source, _ := auth.ResolveSpotifyCredentialsWithSource()
		m.edit = &tuiCredentialsEditState{
			Kind:           card.Kind,
			Field:          "spotify_client_id",
			Title:          "Spotify Client ID",
			Buffer:         creds.ClientID,
			Cursor:         utf8RuneCount(creds.ClientID),
			ExternalSource: source,
			Help: []string{
				"Paste your Spotify app client ID first.",
				"Press enter to continue to the client secret.",
			},
		}
	}
}

func (m tuiCredentialsModel) updateEdit(msg tea.KeyMsg) (tuiCredentialsModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		switch m.edit.Field {
		case "spotify_client_id":
			nextID := strings.TrimSpace(m.edit.Buffer)
			creds, _, _ := auth.ResolveSpotifyCredentialsWithSource()
			m.edit = &tuiCredentialsEditState{
				Kind:           auth.CredentialKindSpotifyApp,
				Field:          "spotify_client_secret",
				Title:          "Spotify Client Secret",
				Buffer:         creds.ClientSecret,
				Cursor:         utf8RuneCount(creds.ClientSecret),
				MaskInput:      true,
				NextSpotifyID:  nextID,
				Help: []string{
					"Paste your Spotify app client secret.",
					"UDL saves both values to macOS Keychain.",
				},
			}
			return m, nil
		default:
			return m, m.saveEditCmd()
		}
	case "esc", "q":
		m.edit = nil
		return m, nil
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

func (m tuiCredentialsModel) saveEditCmd() tea.Cmd {
	if m.edit == nil {
		return nil
	}
	edit := *m.edit
	stateDir := m.stateDir
	return func() tea.Msg {
		switch edit.Kind {
		case auth.CredentialKindSoundCloudClientID:
			if err := auth.SaveSoundCloudClientID(strings.TrimSpace(edit.Buffer)); err != nil {
				return tuiCredentialsSavedMsg{Err: err}
			}
			_ = auth.ClearCredentialFailure(stateDir, auth.CredentialKindSoundCloudClientID)
			flash := "Saved SoundCloud client ID to macOS Keychain."
			if edit.ExternalSource == auth.CredentialStorageSourceEnv {
				flash += " The environment override still wins until you remove SCDL_CLIENT_ID."
			}
			return tuiCredentialsSavedMsg{Flash: flash}
		case auth.CredentialKindDeemixARL:
			if err := auth.SaveDeemixARL(strings.TrimSpace(edit.Buffer)); err != nil {
				return tuiCredentialsSavedMsg{Err: err}
			}
			_ = auth.ClearCredentialFailure(stateDir, auth.CredentialKindDeemixARL)
			flash := "Saved Deezer ARL to macOS Keychain."
			if edit.ExternalSource == auth.CredentialStorageSourceEnv {
				flash += " The environment override still wins until you remove UDL_DEEMIX_ARL."
			}
			return tuiCredentialsSavedMsg{Flash: flash}
		case auth.CredentialKindSpotifyApp:
			creds := auth.SpotifyCredentials{
				ClientID:     strings.TrimSpace(edit.NextSpotifyID),
				ClientSecret: strings.TrimSpace(edit.Buffer),
			}
			if err := auth.SaveSpotifyCredentials(creds); err != nil {
				return tuiCredentialsSavedMsg{Err: err}
			}
			_ = auth.ClearCredentialFailure(stateDir, auth.CredentialKindSpotifyApp)
			flash := "Saved Spotify app credentials to macOS Keychain."
			if edit.ExternalSource == auth.CredentialStorageSourceEnv {
				flash += " Environment overrides still win until you remove UDL_SPOTIFY_CLIENT_ID and UDL_SPOTIFY_CLIENT_SECRET."
			}
			return tuiCredentialsSavedMsg{Flash: flash}
		default:
			return tuiCredentialsSavedMsg{Err: fmt.Errorf("unknown credential kind %q", edit.Kind)}
		}
	}
}

func (m tuiCredentialsModel) clearCredentialCmd(kind auth.CredentialKind) tea.Cmd {
	stateDir := m.stateDir
	return func() tea.Msg {
		switch kind {
		case auth.CredentialKindSoundCloudClientID:
			if err := auth.RemoveSoundCloudClientID(); err != nil {
				return tuiCredentialsSavedMsg{Err: err}
			}
		case auth.CredentialKindDeemixARL:
			if err := auth.RemoveDeemixARL(); err != nil {
				return tuiCredentialsSavedMsg{Err: err}
			}
		case auth.CredentialKindSpotifyApp:
			if err := auth.RemoveSpotifyCredentials(); err != nil {
				return tuiCredentialsSavedMsg{Err: err}
			}
		default:
			return tuiCredentialsSavedMsg{Err: fmt.Errorf("unknown credential kind %q", kind)}
		}
		_ = auth.ClearCredentialFailure(stateDir, kind)
		return tuiCredentialsSavedMsg{Flash: "Removed managed Keychain credential."}
	}
}

func tuiLoadCredentialCards(app *AppContext) (string, []tuiCredentialsCard, error) {
	stateDir := config.DefaultConfig().Defaults.StateDir
	cfg, err := loadConfig(app)
	if err == nil && strings.TrimSpace(cfg.Defaults.StateDir) != "" {
		stateDir = cfg.Defaults.StateDir
	}
	cards := []tuiCredentialsCard{
		tuiCredentialCardFromStatus(auth.InspectSoundCloudClientID(stateDir), "SoundCloud sync"),
		tuiCredentialCardFromStatus(auth.InspectDeemixARL(stateDir), "Spotify via deemix"),
		tuiCredentialCardFromStatus(auth.InspectSpotifyCredentials(stateDir), "Spotify via deemix"),
	}
	return stateDir, cards, nil
}

func tuiCredentialCardFromStatus(status auth.CredentialStatus, workflows string) tuiCredentialsCard {
	statusLabel := string(status.Health)
	storageLabel := "not set"
	tone := "warning"
	clearable := false
	external := false
	action := "Save to Keychain"
	switch status.StorageSource {
	case auth.CredentialStorageSourceEnv:
		storageLabel = "environment override"
		external = true
	case auth.CredentialStorageSourceKeychain:
		storageLabel = "macOS Keychain"
		clearable = true
	case auth.CredentialStorageSourceSpotDL:
		storageLabel = "~/.spotdl/config.json"
		external = true
	}
	switch status.Health {
	case auth.CredentialHealthAvailable:
		statusLabel = "available"
		tone = "success"
		action = "Update"
	case auth.CredentialHealthExternalOverride:
		statusLabel = "external override"
		tone = "info"
		action = "Save to Keychain"
	case auth.CredentialHealthNeedsRefresh:
		statusLabel = "needs refresh"
		tone = "danger"
		action = "Refresh"
	default:
		statusLabel = "missing"
		tone = "warning"
	}
	lastChecked := "not checked yet"
	if !status.LastCheckedAt.IsZero() {
		lastChecked = status.LastCheckedAt.Local().Format("2006-01-02 15:04")
	}
	return tuiCredentialsCard{
		Kind:              status.Kind,
		Title:             status.Title,
		StatusLabel:       statusLabel,
		StorageLabel:      storageLabel,
		AffectedWorkflows: workflows,
		Summary:           status.Summary,
		ActionLabel:       action,
		Tone:              tone,
		LastCheckedLabel:  lastChecked,
		Clearable:         clearable,
		External:          external,
	}
}
