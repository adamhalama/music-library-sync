package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/playlists"
)

type tuiPlaylistPhase string

const (
	tuiPlaylistPhaseLoading          tuiPlaylistPhase = "loading"
	tuiPlaylistPhaseSetupDiscovering tuiPlaylistPhase = "setup-discovering"
	tuiPlaylistPhaseSetup            tuiPlaylistPhase = "setup"
	tuiPlaylistPhaseList             tuiPlaylistPhase = "list"
	tuiPlaylistPhaseDetail           tuiPlaylistPhase = "detail"
	tuiPlaylistPhaseRefreshing       tuiPlaylistPhase = "refreshing"
	tuiPlaylistPhaseFailed           tuiPlaylistPhase = "failed"
)

type tuiPlaylistModel struct {
	app            *AppContext
	phase          tuiPlaylistPhase
	mainConfig     config.Config
	cfg            playlists.Config
	snapshots      map[string]playlists.Snapshot
	snapshotErrs   map[string]string
	cursor         int
	trackCursor    int
	setupItems     []playlists.ProviderPlaylist
	setupCursor    int
	err            error
	refreshErr     error
	lastChanges    *playlists.Changes
	refreshCancel  context.CancelFunc
	discoverCancel context.CancelFunc
	width          int
	height         int
}

type tuiPlaylistLoadedMsg struct {
	Main         config.Config
	Config       playlists.Config
	Snapshots    map[string]playlists.Snapshot
	SnapshotErrs map[string]string
	Err          error
}

type tuiPlaylistDiscoveredMsg struct {
	Items []playlists.ProviderPlaylist
	Err   error
}

type tuiPlaylistSavedMsg struct {
	Config playlists.Config
	Err    error
}

type tuiPlaylistRefreshedMsg struct {
	Result playlists.RefreshResult
	Err    error
}

type tuiPlaylistOpenFreeDLMsg struct {
	Definition playlists.Definition
	Snapshot   playlists.Snapshot
}

type tuiPlaylistOpenRekordboxMsg struct {
	Definition playlists.Definition
	Snapshot   playlists.Snapshot
}

func newTUIPlaylistModel(app *AppContext) tuiPlaylistModel {
	return tuiPlaylistModel{
		app:          app,
		phase:        tuiPlaylistPhaseLoading,
		snapshots:    map[string]playlists.Snapshot{},
		snapshotErrs: map[string]string{},
	}
}

func (m tuiPlaylistModel) Init() tea.Cmd {
	return m.loadCmd()
}

func (m tuiPlaylistModel) loadCmd() tea.Cmd {
	return func() tea.Msg {
		main, cfg, err := loadPlaylistConfigs(m.app)
		if err != nil {
			return tuiPlaylistLoadedMsg{Err: err}
		}
		snapshots := map[string]playlists.Snapshot{}
		snapshotErrs := map[string]string{}
		for _, definition := range cfg.Playlists {
			snapshot, loadErr := playlists.LoadSnapshot(main.Defaults.StateDir, definition.ID)
			if loadErr == nil {
				snapshots[definition.ID] = snapshot
			} else if !errors.Is(loadErr, os.ErrNotExist) {
				snapshotErrs[definition.ID] = loadErr.Error()
			}
		}
		return tuiPlaylistLoadedMsg{Main: main, Config: cfg, Snapshots: snapshots, SnapshotErrs: snapshotErrs}
	}
}

func (m tuiPlaylistModel) Update(msg tea.Msg) (tuiPlaylistModel, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = typed.Width
		m.height = typed.Height
	case tuiPlaylistLoadedMsg:
		if typed.Err != nil {
			m.phase = tuiPlaylistPhaseFailed
			m.err = typed.Err
			return m, nil
		}
		m.mainConfig = typed.Main
		m.cfg = typed.Config
		m.snapshots = typed.Snapshots
		m.snapshotErrs = typed.SnapshotErrs
		m.err = nil
		if len(m.cfg.Playlists) == 0 {
			return m.startDiscovery()
		}
		m.phase = tuiPlaylistPhaseList
	case tuiPlaylistDiscoveredMsg:
		m.discoverCancel = nil
		if typed.Err != nil {
			m.phase = tuiPlaylistPhaseFailed
			m.err = typed.Err
			return m, nil
		}
		m.setupItems = typed.Items
		m.setupCursor = preferredMusicPlaylistIndex(typed.Items)
		m.phase = tuiPlaylistPhaseSetup
	case tuiPlaylistSavedMsg:
		if typed.Err != nil {
			m.err = typed.Err
			m.phase = tuiPlaylistPhaseFailed
			return m, nil
		}
		m.cfg = typed.Config
		m.cursor = len(m.cfg.Playlists) - 1
		m.phase = tuiPlaylistPhaseList
		m.err = nil
	case tuiPlaylistRefreshedMsg:
		m.refreshCancel = nil
		m.phase = tuiPlaylistPhaseDetail
		if typed.Err != nil {
			m.refreshErr = typed.Err
			return m, nil
		}
		m.snapshots[typed.Result.Snapshot.PlaylistID] = typed.Result.Snapshot
		delete(m.snapshotErrs, typed.Result.Snapshot.PlaylistID)
		changes := typed.Result.Changes
		m.lastChanges = &changes
		m.refreshErr = nil
	case tea.KeyMsg:
		return m.updateKey(typed)
	}
	return m, nil
}

func (m tuiPlaylistModel) updateKey(msg tea.KeyMsg) (tuiPlaylistModel, tea.Cmd) {
	switch m.phase {
	case tuiPlaylistPhaseSetup:
		switch msg.String() {
		case "up", "k":
			if m.setupCursor > 0 {
				m.setupCursor--
			}
		case "down", "j":
			if m.setupCursor < len(m.setupItems)-1 {
				m.setupCursor++
			}
		case "enter":
			return m.saveSelectedProviderPlaylist()
		case "r":
			return m.startDiscovery()
		}
	case tuiPlaylistPhaseList:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.cfg.Playlists)-1 {
				m.cursor++
			}
		case "enter":
			if len(m.cfg.Playlists) > 0 {
				m.trackCursor = 0
				m.lastChanges = nil
				m.refreshErr = nil
				m.phase = tuiPlaylistPhaseDetail
			}
		case "n":
			return m.startDiscovery()
		}
	case tuiPlaylistPhaseDetail:
		switch msg.String() {
		case "up", "k":
			if m.trackCursor > 0 {
				m.trackCursor--
			}
		case "down", "j":
			if snapshot, ok := m.currentSnapshot(); ok && m.trackCursor < len(snapshot.Tracks)-1 {
				m.trackCursor++
			}
		case "r":
			return m.startRefresh()
		case "f":
			if definition, snapshot, ok := m.currentDefinitionAndSnapshot(); ok {
				return m, func() tea.Msg {
					return tuiPlaylistOpenFreeDLMsg{Definition: definition, Snapshot: snapshot}
				}
			}
		case "b":
			if definition, snapshot, ok := m.currentDefinitionAndSnapshot(); ok {
				return m, func() tea.Msg {
					return tuiPlaylistOpenRekordboxMsg{Definition: definition, Snapshot: snapshot}
				}
			}
		case "esc":
			m.phase = tuiPlaylistPhaseList
		}
	case tuiPlaylistPhaseRefreshing:
		if (msg.String() == "x" || msg.String() == "ctrl+c") && m.refreshCancel != nil {
			m.refreshCancel()
		}
	case tuiPlaylistPhaseSetupDiscovering:
		if (msg.String() == "x" || msg.String() == "ctrl+c") && m.discoverCancel != nil {
			m.discoverCancel()
		}
	case tuiPlaylistPhaseFailed:
		if msg.String() == "r" {
			m.phase = tuiPlaylistPhaseLoading
			return m, m.loadCmd()
		}
	}
	return m, nil
}

func (m tuiPlaylistModel) startDiscovery() (tuiPlaylistModel, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	m.discoverCancel = cancel
	m.phase = tuiPlaylistPhaseSetupDiscovering
	return m, func() tea.Msg {
		items, err := (playlists.Service{}).ListProviderPlaylists(ctx, playlists.ProviderAppleMusic)
		if errors.Is(err, context.Canceled) {
			err = fmt.Errorf("Apple Music playlist discovery canceled")
		}
		return tuiPlaylistDiscoveredMsg{Items: items, Err: err}
	}
}

func (m tuiPlaylistModel) saveSelectedProviderPlaylist() (tuiPlaylistModel, tea.Cmd) {
	if len(m.setupItems) == 0 || m.setupCursor < 0 || m.setupCursor >= len(m.setupItems) {
		m.err = fmt.Errorf("no Apple Music playlist selected")
		m.phase = tuiPlaylistPhaseFailed
		return m, nil
	}
	selected := m.setupItems[m.setupCursor]
	cfg := m.cfg
	id := uniquePlaylistID(cfg, tuiSlug(firstNonEmpty(selected.Name, "playlist")))
	name := selected.Name
	if strings.EqualFold(selected.Name, "Favourites") || strings.EqualFold(selected.Name, "Favorites") {
		id = uniquePlaylistID(cfg, "favorites")
		name = "Favorites"
	}
	cfg.Playlists = append(cfg.Playlists, playlists.Definition{
		ID:                     id,
		Name:                   name,
		Provider:               playlists.ProviderAppleMusic,
		ProviderPlaylist:       selected.Name,
		ProviderPlaylistID:     selected.ID,
		DefaultFreeDLJob:       "soundcloud-free-dl",
		DefaultRekordboxTarget: "fav_imports",
	})
	app := m.app
	return m, func() tea.Msg {
		path, err := playlists.ResolveWritePath(app.Opts.PlaylistsConfigPath, "")
		if err == nil {
			err = playlists.Save(path, cfg)
		}
		return tuiPlaylistSavedMsg{Config: cfg, Err: err}
	}
}

func (m tuiPlaylistModel) startRefresh() (tuiPlaylistModel, tea.Cmd) {
	definition, ok := m.currentDefinition()
	if !ok {
		return m, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.refreshCancel = cancel
	m.phase = tuiPlaylistPhaseRefreshing
	m.refreshErr = nil
	m.lastChanges = nil
	main := m.mainConfig
	return m, func() tea.Msg {
		result, err := (playlists.Service{}).Refresh(ctx, main, definition)
		if errors.Is(err, context.Canceled) {
			err = fmt.Errorf("playlist refresh canceled")
		}
		return tuiPlaylistRefreshedMsg{Result: result, Err: err}
	}
}

func (m tuiPlaylistModel) currentDefinition() (playlists.Definition, bool) {
	if len(m.cfg.Playlists) == 0 || m.cursor < 0 || m.cursor >= len(m.cfg.Playlists) {
		return playlists.Definition{}, false
	}
	return m.cfg.Playlists[m.cursor], true
}

func (m tuiPlaylistModel) currentSnapshot() (playlists.Snapshot, bool) {
	definition, ok := m.currentDefinition()
	if !ok {
		return playlists.Snapshot{}, false
	}
	snapshot, ok := m.snapshots[definition.ID]
	return snapshot, ok
}

func (m tuiPlaylistModel) currentDefinitionAndSnapshot() (playlists.Definition, playlists.Snapshot, bool) {
	definition, ok := m.currentDefinition()
	if !ok {
		return playlists.Definition{}, playlists.Snapshot{}, false
	}
	snapshot, ok := m.snapshots[definition.ID]
	return definition, snapshot, ok
}

func (m tuiPlaylistModel) allowBack() bool {
	return m.phase == tuiPlaylistPhaseList || m.phase == tuiPlaylistPhaseSetup || m.phase == tuiPlaylistPhaseFailed
}

func preferredMusicPlaylistIndex(items []playlists.ProviderPlaylist) int {
	for idx, item := range items {
		if strings.EqualFold(item.Name, "Favourites") || strings.EqualFold(item.Name, "Favorites") {
			return idx
		}
	}
	return 0
}

func uniquePlaylistID(cfg playlists.Config, base string) string {
	if strings.TrimSpace(base) == "" {
		base = "playlist"
	}
	used := map[string]struct{}{}
	for _, definition := range cfg.Playlists {
		used[definition.ID] = struct{}{}
	}
	if _, exists := used[base]; !exists {
		return base
	}
	for idx := 2; ; idx++ {
		candidate := fmt.Sprintf("%s-%d", base, idx)
		if _, exists := used[candidate]; !exists {
			return candidate
		}
	}
}
