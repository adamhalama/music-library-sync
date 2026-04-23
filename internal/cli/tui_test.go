package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jaa/update-downloads/internal/auth"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/doctor"
	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/output"
)

func TestTUICommandHelp(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: stdout, ErrOut: stderr},
	}
	root := newRootCommand(app)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs([]string{"tui", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("tui --help failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "Launch the main TUI setup and sync flow") {
		t.Fatalf("expected tui help output, got: %s", stdout.String())
	}
}

func newMenuRootModelForTest() tuiRootModel {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenMenu
	root.startupAttention = nil
	return root
}

func TestTUIRootMenuShowsGetStartedFirst(t *testing.T) {
	root := newMenuRootModelForTest()

	if len(root.menuItems) < 2 {
		t.Fatalf("expected at least two menu items, got %v", root.menuItems)
	}
	if root.menuItems[0] != "Get Started" {
		t.Fatalf("expected Get Started first, got %v", root.menuItems)
	}
	if root.menuItems[1] != "Credentials" {
		t.Fatalf("expected Credentials second, got %v", root.menuItems)
	}
	if root.menuItems[2] != "Check System" {
		t.Fatalf("expected Check System third, got %v", root.menuItems)
	}
	view := root.View()
	if !strings.Contains(view, "UDL · HOME") {
		t.Fatalf("expected home shell title, got: %s", view)
	}
	if !strings.Contains(view, "Get Started") {
		t.Fatalf("expected Get Started in landing navigation, got: %s", view)
	}
	if !strings.Contains(view, "Create a starter setup") {
		t.Fatalf("expected landing body summary, got: %s", view)
	}
}

func TestTUIRootViewUsesFullShellAtWidth110(t *testing.T) {
	root := newMenuRootModelForTest()
	root.width = 110

	view := root.View()
	if !strings.Contains(view, "WORKFLOWS") {
		t.Fatalf("expected full shell sidebar section at width 110, got: %s", view)
	}
}

func TestTUIRootViewUsesCompactShellBelowBreakpoint(t *testing.T) {
	root := newMenuRootModelForTest()
	root.width = 109

	view := root.View()
	if strings.Contains(view, "WORKFLOWS") {
		t.Fatalf("expected compact shell without sidebar section label, got: %s", view)
	}
}

func TestTUIRootEnterOpensGetStartedWorkflow(t *testing.T) {
	root := newMenuRootModelForTest()

	nextModel, _ := root.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next, ok := nextModel.(tuiRootModel)
	if !ok {
		t.Fatalf("unexpected model type %T", nextModel)
	}
	if next.screen != tuiScreenGetStarted {
		t.Fatalf("expected get started screen, got %v", next.screen)
	}
	if next.onboardingModel.phase != tuiOnboardingPhaseIntro {
		t.Fatalf("expected onboarding intro phase, got %q", next.onboardingModel.phase)
	}
}

func TestTUIRootEnterOpensCheckSystemWorkflow(t *testing.T) {
	root := newMenuRootModelForTest()
	root.menuCursor = 2

	nextModel, _ := root.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next, ok := nextModel.(tuiRootModel)
	if !ok {
		t.Fatalf("unexpected model type %T", nextModel)
	}
	if next.screen != tuiScreenDoctor {
		t.Fatalf("expected doctor screen, got %v", next.screen)
	}
	if next.doctorModel.phase != tuiDoctorPhaseRunning {
		t.Fatalf("expected running doctor phase, got %q", next.doctorModel.phase)
	}
}

func TestTUIRootMenuIncludesAdvancedConfigWorkflow(t *testing.T) {
	root := newMenuRootModelForTest()

	found := false
	for _, item := range root.menuItems {
		if item == "Advanced Config" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected advanced config workflow in menu, got %v", root.menuItems)
	}
}

func TestTUIRootEnterOpensAdvancedConfigWorkflow(t *testing.T) {
	root := newMenuRootModelForTest()
	root.menuCursor = 4

	nextModel, _ := root.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next, ok := nextModel.(tuiRootModel)
	if !ok {
		t.Fatalf("unexpected model type %T", nextModel)
	}
	if next.screen != tuiScreenConfigEditor {
		t.Fatalf("expected config editor screen, got %v", next.screen)
	}
	if next.configModel.phase != tuiConfigEditorPhaseTarget {
		t.Fatalf("expected config editor target phase, got %v", next.configModel.phase)
	}
}

func TestTUIRootEnterOpensCredentialsWorkflow(t *testing.T) {
	root := newMenuRootModelForTest()
	root.menuCursor = 1

	nextModel, _ := root.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next, ok := nextModel.(tuiRootModel)
	if !ok {
		t.Fatalf("unexpected model type %T", nextModel)
	}
	if next.screen != tuiScreenCredentials {
		t.Fatalf("expected credentials screen, got %v", next.screen)
	}
}

func TestTUIRootHomeShowsStartupAttention(t *testing.T) {
	root := newMenuRootModelForTest()
	root.startupAttention = &tuiStartupAttentionState{
		Severity:           tuiStartupAttentionSeverityBlocked,
		PrimaryKind:        auth.CredentialKindSoundCloudClientID,
		PrimarySourceID:    "soundcloud-likes",
		AffectedSourceIDs:  []string{"soundcloud-likes"},
		IssueCount:         1,
		PrimaryActionLabel: "Press `c` to open Credentials",
		Headline:           "Startup Blocked",
		SummaryText:        "soundcloud-likes is blocked by a stale soundcloud client id.",
	}

	view := root.View()
	if !strings.Contains(view, "BLOCKED") {
		t.Fatalf("expected blocked badge, got: %s", view)
	}
	if !strings.Contains(view, "Startup Attention") {
		t.Fatalf("expected startup attention section, got: %s", view)
	}
	if !strings.Contains(view, "Press `c` to open Credentials") {
		t.Fatalf("expected credential repair action, got: %s", view)
	}
	if !root.canOpenCredentialsShortcut() {
		t.Fatalf("expected home credential shortcut to be active")
	}
	if root.recommendedCredentialFocus() != auth.CredentialKindSoundCloudClientID {
		t.Fatalf("unexpected recommended credential focus: %q", root.recommendedCredentialFocus())
	}
}

func TestTUIStartupAttentionScopesToEnabledSourceCredentialBlockers(t *testing.T) {
	origSoundCloud := tuiInspectSoundCloudClientIDStatusFn
	origDeemix := tuiInspectDeemixARLStatusFn
	origSpotify := tuiInspectSpotifyCredentialsStatusFn
	defer func() {
		tuiInspectSoundCloudClientIDStatusFn = origSoundCloud
		tuiInspectDeemixARLStatusFn = origDeemix
		tuiInspectSpotifyCredentialsStatusFn = origSpotify
	}()

	tuiInspectSoundCloudClientIDStatusFn = func(stateDir string) auth.CredentialStatus {
		return auth.CredentialStatus{
			Kind:    auth.CredentialKindSoundCloudClientID,
			Title:   "SoundCloud client ID",
			Health:  auth.CredentialHealthNeedsRefresh,
			Summary: "needs refresh",
		}
	}
	tuiInspectDeemixARLStatusFn = func(stateDir string) auth.CredentialStatus {
		return auth.CredentialStatus{
			Kind:   auth.CredentialKindDeemixARL,
			Title:  "Deezer ARL",
			Health: auth.CredentialHealthAvailable,
		}
	}
	tuiInspectSpotifyCredentialsStatusFn = func(stateDir string) auth.CredentialStatus {
		return auth.CredentialStatus{
			Kind:   auth.CredentialKindSpotifyApp,
			Title:  "Spotify app credentials",
			Health: auth.CredentialHealthMissing,
		}
	}

	cfg := config.Config{
		Version: 1,
		Defaults: config.Defaults{
			StateDir: "/tmp/state",
		},
		Sources: []config.Source{
			{ID: "soundcloud-likes", Type: config.SourceTypeSoundCloud, Enabled: true, Adapter: config.AdapterSpec{Kind: "scdl"}},
			{ID: "spotify-disabled", Type: config.SourceTypeSpotify, Enabled: false, Adapter: config.AdapterSpec{Kind: "deemix"}},
			{ID: "spotify-legacy", Type: config.SourceTypeSpotify, Enabled: true, Adapter: config.AdapterSpec{Kind: "spotdl"}},
		},
	}

	attention := tuiDetectStartupAttentionForConfig(cfg)
	if attention == nil {
		t.Fatalf("expected startup attention")
	}
	if attention.PrimaryKind != auth.CredentialKindSoundCloudClientID {
		t.Fatalf("unexpected primary kind: %q", attention.PrimaryKind)
	}
	if attention.IssueCount != 1 {
		t.Fatalf("expected one issue, got %d", attention.IssueCount)
	}
	if len(attention.AffectedSourceIDs) != 1 || attention.AffectedSourceIDs[0] != "soundcloud-likes" {
		t.Fatalf("unexpected affected sources: %+v", attention.AffectedSourceIDs)
	}
}

func TestTUIRootRefreshesStartupAttentionWhenReturningHome(t *testing.T) {
	origDetect := tuiDetectStartupAttentionFn
	defer func() {
		tuiDetectStartupAttentionFn = origDetect
	}()

	tuiDetectStartupAttentionFn = func(app *AppContext) *tuiStartupAttentionState {
		return &tuiStartupAttentionState{
			Severity:           tuiStartupAttentionSeverityBlocked,
			PrimaryKind:        auth.CredentialKindSoundCloudClientID,
			PrimarySourceID:    "soundcloud-likes",
			AffectedSourceIDs:  []string{"soundcloud-likes"},
			IssueCount:         1,
			PrimaryActionLabel: "Press `c` to open Credentials",
			Headline:           "Startup Blocked",
			SummaryText:        "soundcloud-likes is blocked by a stale soundcloud client id.",
		}
	}

	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenCredentials
	root.startupAttention = nil
	root.credentialsModel = newTUICredentialsModel(&AppContext{}, "")

	nextModel, _ := root.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next := nextModel.(tuiRootModel)
	if next.screen != tuiScreenMenu {
		t.Fatalf("expected return to menu, got %v", next.screen)
	}
	if next.startupAttention == nil {
		t.Fatalf("expected startup attention to refresh on return home")
	}
}

func TestTUIRootAutoStartsGetStartedWhenNoSourcesConfigured(t *testing.T) {
	tmp := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}
	defer func() {
		_ = os.Chdir(wd)
	}()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg-config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "xdg-state"))

	root := newTUIRootModel(&AppContext{}, false)
	if root.screen != tuiScreenGetStarted {
		t.Fatalf("expected get started auto-start screen, got %v", root.screen)
	}
	if root.onboardingModel.startup.Reason != tuiOnboardingReasonNoSources {
		t.Fatalf("expected no-sources onboarding reason, got %q", root.onboardingModel.startup.Reason)
	}
}

func TestTUIOnboardingSaveKeepsSecretsOutOfConfig(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	model := newTUIOnboardingModel(&AppContext{}, tuiOnboardingStartupState{
		Reason:             tuiOnboardingReasonFirstRun,
		ConfigPath:         configPath,
		ConfigContextLabel: configPath,
		Defaults:           config.DefaultConfig().Defaults,
	})
	model.libraryRoot = "~/Music/downloaded"
	model.stateDir = "~/Library/Application Support/udl-state"
	model.sourceType = config.SourceTypeSpotify
	model.sourceID = "spotify-playlist"
	model.sourceURL = "https://open.spotify.com/playlist/replace-me"

	cfg := model.buildConfig()
	payload, err := config.MarshalCanonical(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	text := string(payload)
	if strings.Contains(text, "DeezerARL") || strings.Contains(text, "deezer") || strings.Contains(text, "spotify_client_secret") {
		t.Fatalf("expected secrets to stay out of config payload, got: %s", text)
	}
	if !strings.Contains(text, "spotify-playlist") {
		t.Fatalf("expected source data in config payload, got: %s", text)
	}
}

func TestTUIConfigEditorUsesExplicitConfigPath(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "custom.yaml")
	payload := strings.Join([]string{
		"version: 1",
		"defaults:",
		"  state_dir: /tmp/udl-state",
		"  archive_file: archive.txt",
		"  threads: 1",
		"  continue_on_error: true",
		"  command_timeout_seconds: 900",
		"sources:",
		"  - id: sc",
		"    type: soundcloud",
		"    enabled: true",
		"    target_dir: /tmp/music",
		"    url: https://soundcloud.com/user",
		"    adapter:",
		"      kind: scdl",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(payload), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	model := newTUIConfigEditorModel(&AppContext{Opts: GlobalOptions{ConfigPath: configPath}})
	if model.targetKind != tuiConfigEditorTargetExplicit {
		t.Fatalf("expected explicit target kind, got %q", model.targetKind)
	}
	if model.targetPath != configPath {
		t.Fatalf("expected explicit target path %q, got %q", configPath, model.targetPath)
	}
	if !model.fileExists {
		t.Fatalf("expected explicit config to exist")
	}
}

func TestTUIConfigEditorDefaultsToUserConfigPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg"))

	model := newTUIConfigEditorModel(&AppContext{})
	userPath, err := config.UserConfigPath()
	if err != nil {
		t.Fatalf("user config path: %v", err)
	}
	if model.targetKind != tuiConfigEditorTargetNew {
		t.Fatalf("expected missing default target to be marked new, got %q", model.targetKind)
	}
	if model.targetPath != userPath {
		t.Fatalf("expected user config path %q, got %q", userPath, model.targetPath)
	}
}

func TestTUIConfigEditorParseErrorRecoveryResetsToDefaults(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "broken.yaml")
	if err := os.WriteFile(configPath, []byte("version: [oops\n"), 0o644); err != nil {
		t.Fatalf("write broken config: %v", err)
	}

	model := newTUIConfigEditorModel(&AppContext{Opts: GlobalOptions{ConfigPath: configPath}})
	if model.parseErr == nil {
		t.Fatalf("expected parse error")
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if next.modal == nil || next.modal.Kind != tuiConfigEditorModalReset {
		t.Fatalf("expected reset modal, got %+v", next.modal)
	}
	next, cmd := next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd != nil {
		t.Fatalf("expected reset confirmation not to trigger exit")
	}
	if next.phase != tuiConfigEditorPhaseDefaults {
		t.Fatalf("expected defaults phase after reset, got %v", next.phase)
	}
	if next.parseErr != nil {
		t.Fatalf("expected parse error to clear after reset, got %v", next.parseErr)
	}
}

func TestTUIConfigEditorRejectsDirectoryConfigPath(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config-dir")
	if err := os.Mkdir(configPath, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	model := newTUIConfigEditorModel(&AppContext{Opts: GlobalOptions{ConfigPath: configPath}})
	if model.prepareErr == nil {
		t.Fatalf("expected prepare error for directory target")
	}
	if !strings.Contains(model.prepareErr.Error(), "directory") {
		t.Fatalf("expected directory prepare error, got %v", model.prepareErr)
	}
	if model.parseErr != nil {
		t.Fatalf("expected parse error to remain nil, got %v", model.parseErr)
	}
	if model.phase != tuiConfigEditorPhaseTarget {
		t.Fatalf("expected target phase, got %v", model.phase)
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if next.modal != nil {
		t.Fatalf("expected no reset modal, got %+v", next.modal)
	}
	if next.phase != tuiConfigEditorPhaseTarget {
		t.Fatalf("expected to remain in target phase, got %v", next.phase)
	}
}

func TestTUIConfigEditorSourceIDUpdatesLinkedDefaults(t *testing.T) {
	model := newTUIConfigEditorModel(&AppContext{})
	model.phase = tuiConfigEditorPhaseSources
	model.addSource()

	state := model.currentSourceState()
	if state == nil {
		t.Fatalf("expected source state")
	}
	originalTarget := state.Source.TargetDir
	model.startEdit("source.id", "Edit Source ID", state.Source.ID)
	model.edit.Buffer = "spotify-groove"
	if err := model.applyEdit(); err != nil {
		t.Fatalf("applyEdit: %v", err)
	}

	if state.Source.TargetDir == originalTarget {
		t.Fatalf("expected linked target_dir to update after id change")
	}
	if state.Source.TargetDir != filepath.Join("~", "Music", "downloaded", "spotify-groove") {
		t.Fatalf("unexpected linked target_dir: %q", state.Source.TargetDir)
	}
	if state.Source.StateFile != "spotify-groove.sync.scdl" {
		t.Fatalf("unexpected linked state_file: %q", state.Source.StateFile)
	}

	state.ManualTargetDir = true
	state.Source.TargetDir = "/custom/music"
	model.startEdit("source.id", "Edit Source ID", state.Source.ID)
	model.edit.Buffer = "spotify-groove-2"
	if err := model.applyEdit(); err != nil {
		t.Fatalf("applyEdit second id: %v", err)
	}
	if state.Source.TargetDir != "/custom/music" {
		t.Fatalf("expected manual target_dir to remain unchanged, got %q", state.Source.TargetDir)
	}
}

func TestTUIConfigEditorDefaultsPhaseROpensReviewAndEscReturns(t *testing.T) {
	model := newTUIConfigEditorModel(&AppContext{})
	model.phase = tuiConfigEditorPhaseDefaults

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if next.phase != tuiConfigEditorPhaseReview {
		t.Fatalf("expected review phase, got %q", next.phase)
	}
	if next.reviewReturnPhase != tuiConfigEditorPhaseDefaults {
		t.Fatalf("expected defaults review return phase, got %q", next.reviewReturnPhase)
	}

	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if next.phase != tuiConfigEditorPhaseDefaults {
		t.Fatalf("expected esc to return to defaults, got %q", next.phase)
	}
}

func TestTUIConfigEditorSourcesPhaseROpensReviewAndEscReturns(t *testing.T) {
	model := newTUIConfigEditorModel(&AppContext{})
	model.phase = tuiConfigEditorPhaseSources
	model.addSource()

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if next.phase != tuiConfigEditorPhaseReview {
		t.Fatalf("expected review phase, got %q", next.phase)
	}
	if next.reviewReturnPhase != tuiConfigEditorPhaseSources {
		t.Fatalf("expected sources review return phase, got %q", next.reviewReturnPhase)
	}

	next, _ = next.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if next.phase != tuiConfigEditorPhaseSources {
		t.Fatalf("expected esc to return to sources, got %q", next.phase)
	}
}

func TestTUIConfigEditorDirectSaveFromDefaultsWritesConfig(t *testing.T) {
	tmp := t.TempDir()
	model := newTUIConfigEditorModel(&AppContext{Opts: GlobalOptions{ConfigPath: filepath.Join(tmp, "config.yaml")}})
	model.phase = tuiConfigEditorPhaseDefaults
	model.addSource()

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if next.phase != tuiConfigEditorPhaseDefaults {
		t.Fatalf("expected direct save to stay on defaults, got %q", next.phase)
	}
	if next.saveResult == nil {
		t.Fatalf("expected direct save result")
	}
	if next.saveResult.Path != filepath.Join(tmp, "config.yaml") {
		t.Fatalf("unexpected save path: %+v", next.saveResult)
	}
	if _, err := os.Stat(filepath.Join(tmp, "config.yaml")); err != nil {
		t.Fatalf("expected saved config file: %v", err)
	}
}

func TestTUIConfigEditorDirectSaveFromSourcesWritesConfig(t *testing.T) {
	tmp := t.TempDir()
	model := newTUIConfigEditorModel(&AppContext{Opts: GlobalOptions{ConfigPath: filepath.Join(tmp, "config.yaml")}})
	model.phase = tuiConfigEditorPhaseSources
	model.addSource()

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if next.phase != tuiConfigEditorPhaseSources {
		t.Fatalf("expected direct save to stay on sources, got %q", next.phase)
	}
	if next.saveResult == nil {
		t.Fatalf("expected direct save result from sources")
	}
	if _, err := os.Stat(filepath.Join(tmp, "config.yaml")); err != nil {
		t.Fatalf("expected saved config file from sources: %v", err)
	}
}

func TestTUIConfigEditorDirectSaveFromInvalidStateStaysInPlace(t *testing.T) {
	tmp := t.TempDir()
	model := newTUIConfigEditorModel(&AppContext{Opts: GlobalOptions{ConfigPath: filepath.Join(tmp, "invalid.yaml")}})
	model.phase = tuiConfigEditorPhaseDefaults

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if next.phase != tuiConfigEditorPhaseDefaults {
		t.Fatalf("expected invalid direct save to stay on defaults, got %q", next.phase)
	}
	if next.saveResult != nil {
		t.Fatalf("expected no save result on invalid config")
	}
	if next.validationErr == "" {
		t.Fatalf("expected validation error when saving invalid config")
	}
}

func TestTUIConfigEditorModalEditRendersCursorAndHelp(t *testing.T) {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenConfigEditor
	root.width = 140
	root.height = 50
	root.configModel = newTUIConfigEditorModel(&AppContext{})
	root.configModel.phase = tuiConfigEditorPhaseSources
	root.configModel.addSource()
	root.configModel.startEdit("source.adapter.min_version", "Edit Adapter Min Version", "")

	view := root.View()
	if !strings.Contains(view, "Edit Field") {
		t.Fatalf("expected edit modal title, got: %s", view)
	}
	if !strings.Contains(view, "▌") {
		t.Fatalf("expected visible input cursor, got: %s", view)
	}
	if !strings.Contains(view, "Doctor compatibility hint.") {
		t.Fatalf("expected edit controls in modal help, got: %s", view)
	}
}

func TestTUIConfigEditorModalEditSupportsCursorAndDeletionKeys(t *testing.T) {
	model := newTUIConfigEditorModel(&AppContext{})
	model.phase = tuiConfigEditorPhaseSources
	model.addSource()
	model.startEdit("source.id", "Edit Source ID", "abcd")

	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyHome})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyDelete})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnd})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("!")})

	if model.edit == nil {
		t.Fatalf("expected active edit state")
	}
	if model.edit.Buffer != "aXd!" {
		t.Fatalf("unexpected edit buffer after cursor operations: %q", model.edit.Buffer)
	}

	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	state := model.currentSourceState()
	if state == nil || state.Source.ID != "aXd!" {
		t.Fatalf("expected applied edit to update source id, got %+v", state)
	}
}

func TestTUIConfigEditorSCDLArgsStateParsesLegacyYTArgs(t *testing.T) {
	state := newTUIConfigEditorSourceState(config.Source{
		ID:      "soundcloud-likes",
		Type:    config.SourceTypeSoundCloud,
		Enabled: true,
		URL:     "https://soundcloud.com/user",
		Adapter: config.AdapterSpec{
			Kind: "scdl",
			ExtraArgs: []string{
				"-f",
				"--yt-dlp-args",
				"--embed-thumbnail --embed-metadata --download-archive scdl-archive.txt --no-overwrites --break-on-existing",
			},
		},
	})

	if !state.SCDLArgs.RecommendedEnabled("no_overwrites") {
		t.Fatalf("expected no-overwrites to be detected from legacy yt-dlp args")
	}
	if state.SCDLArgs.AdvancedRaw != "" {
		t.Fatalf("expected managed legacy yt-dlp args to be stripped, got %q", state.SCDLArgs.AdvancedRaw)
	}
	if state.SCDLArgs.DirectRaw != "" {
		t.Fatalf("expected managed direct args to be stripped, got %q", state.SCDLArgs.DirectRaw)
	}

	state.syncAdapterExtraArgs()
	if !reflect.DeepEqual(state.Source.Adapter.ExtraArgs, []string{"--yt-dlp-args", "--no-overwrites"}) {
		t.Fatalf("unexpected serialized extra args: %#v", state.Source.Adapter.ExtraArgs)
	}
}

func TestTUIConfigEditorReviewRendersSeparatedSections(t *testing.T) {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenConfigEditor
	root.width = 150
	root.configModel = newTUIConfigEditorModel(&AppContext{})
	root.configModel.phase = tuiConfigEditorPhaseReview
	root.configModel.reviewReturnPhase = tuiConfigEditorPhaseSources
	root.configModel.addSource()

	view := root.View()
	for _, needle := range []string{"Summary", "Validation", "Preview Scope", "Preview · All sources"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("expected %q in review view, got: %s", needle, view)
		}
	}
	if !strings.Contains(view, "No blocking issues.") || !strings.Contains(view, "save") || !strings.Contains(view, "return") {
		t.Fatalf("expected action-oriented validation text, got: %s", view)
	}
}

func TestTUIConfigEditorReviewSelectorSwitchesPreviewScope(t *testing.T) {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenConfigEditor
	root.width = 150
	root.configModel = newTUIConfigEditorModel(&AppContext{})
	root.configModel.phase = tuiConfigEditorPhaseReview
	root.configModel.reviewReturnPhase = tuiConfigEditorPhaseSources
	root.configModel.addSource()
	root.configModel.addSource()
	root.configModel.sources[0].Source.ID = "source-a"
	root.configModel.sources[1].Source.ID = "source-b"

	view := root.View()
	if !strings.Contains(view, "Preview · All sources") {
		t.Fatalf("expected all-sources preview by default, got: %s", view)
	}

	nextModel, _ := root.Update(tea.KeyMsg{Type: tea.KeyDown})
	next := nextModel.(tuiRootModel)
	view = next.View()
	if !strings.Contains(view, "Preview · source-a") {
		t.Fatalf("expected focused source preview after moving review scope, got: %s", view)
	}
}

func TestTUIConfigEditorPreservesInvalidSpotifyAdapterKindFromDisk(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	payload := strings.Join([]string{
		"version: 1",
		"defaults:",
		"  state_dir: /tmp/udl-state",
		"  archive_file: archive.txt",
		"  threads: 1",
		"  continue_on_error: true",
		"  command_timeout_seconds: 900",
		"sources:",
		"  - id: spotify-list",
		"    type: spotify",
		"    enabled: true",
		"    target_dir: /tmp/music",
		"    state_file: spotify-list.sync.spotify",
		"    url: https://open.spotify.com/playlist/abc123",
		"    adapter:",
		"      kind: future-backend",
		"    sync:",
		"      break_on_existing: true",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(payload), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	model := newTUIConfigEditorModel(&AppContext{Opts: GlobalOptions{ConfigPath: configPath}})
	if len(model.sources) != 1 {
		t.Fatalf("expected one source, got %d", len(model.sources))
	}
	if got := model.sources[0].Source.Adapter.Kind; got != "future-backend" {
		t.Fatalf("expected invalid adapter kind to be preserved, got %q", got)
	}
	if model.sources[0].Source.Sync.BreakOnExisting == nil || !*model.sources[0].Source.Sync.BreakOnExisting {
		t.Fatalf("expected unsupported sync policy to remain visible for validation")
	}

	cfg := model.buildConfig()
	if got := cfg.Sources[0].Adapter.Kind; got != "future-backend" {
		t.Fatalf("expected buildConfig to preserve adapter kind, got %q", got)
	}
	if cfg.Sources[0].Sync.BreakOnExisting == nil || !*cfg.Sources[0].Sync.BreakOnExisting {
		t.Fatalf("expected buildConfig to preserve unsupported sync policy")
	}

	problems := strings.Join(model.validationProblems, "\n")
	for _, needle := range []string{
		`source "spotify-list" has unsupported adapter.kind "future-backend"`,
		`source "spotify-list" spotify type requires spotdl or deemix adapter`,
		`source "spotify-list" sync.break_on_existing is only supported for soundcloud or spotify+deemix`,
	} {
		if !strings.Contains(problems, needle) {
			t.Fatalf("expected validation to include %q, got: %s", needle, problems)
		}
	}
}

func TestTUIConfigEditorInteractiveAdapterChangeClearsUnsupportedSpotifySyncPolicy(t *testing.T) {
	model := newTUIConfigEditorModel(&AppContext{})
	model.phase = tuiConfigEditorPhaseSources
	model.sources = []tuiConfigEditorSourceState{
		newTUIConfigEditorSourceState(config.Source{
			ID:        "spotify-list",
			Type:      config.SourceTypeSpotify,
			Enabled:   true,
			TargetDir: "/tmp/music",
			StateFile: "spotify-list.sync.spotify",
			URL:       "https://open.spotify.com/playlist/abc123",
			Adapter: config.AdapterSpec{
				Kind: "deemix",
			},
			Sync: config.SyncPolicy{
				BreakOnExisting: tuiBoolPtr(true),
				AskOnExisting:   tuiBoolPtr(true),
			},
		}),
	}
	model.sourcePane = tuiConfigEditorPaneForm
	model.sourceFieldCursor = 6

	nextModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next := nextModel
	state := next.currentSourceState()
	if state == nil {
		t.Fatalf("expected current source state")
	}
	if got := state.Source.Adapter.Kind; got != "spotdl" {
		t.Fatalf("expected adapter toggle to switch to spotdl, got %q", got)
	}
	if state.Source.Sync.BreakOnExisting != nil {
		t.Fatalf("expected break_on_existing to be cleared for spotdl")
	}
	if state.Source.Sync.AskOnExisting != nil {
		t.Fatalf("expected ask_on_existing to be cleared for spotdl")
	}
}

func TestTUIRootConfigEditorEscFromReviewReturnsToEditing(t *testing.T) {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenConfigEditor
	root.configModel = newTUIConfigEditorModel(&AppContext{})
	root.configModel.phase = tuiConfigEditorPhaseReview
	root.configModel.reviewReturnPhase = tuiConfigEditorPhaseDefaults

	nextModel, _ := root.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next := nextModel.(tuiRootModel)
	if next.screen != tuiScreenConfigEditor {
		t.Fatalf("expected esc from review to stay in config editor, got screen %v", next.screen)
	}
	if next.configModel.phase != tuiConfigEditorPhaseDefaults {
		t.Fatalf("expected esc from review to return to defaults, got %q", next.configModel.phase)
	}
}

func TestTUISyncModelBuildSyncRequestUsesWorkflowMode(t *testing.T) {
	interactive := newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	interactive.planLimit = 25
	interactive.askOnExisting = true
	interactive.askOnExistingSet = true
	interactive.scanGaps = true
	interactive.noPreflight = true

	interactiveReq := interactive.buildSyncRequest([]string{"source-a"})
	if !interactiveReq.Plan {
		t.Fatalf("expected interactive sync to force plan mode")
	}
	if interactiveReq.PlanLimit != 25 {
		t.Fatalf("expected interactive sync to keep plan limit, got %d", interactiveReq.PlanLimit)
	}
	if interactiveReq.AskOnExisting || interactiveReq.AskOnExistingSet || interactiveReq.ScanGaps || interactiveReq.NoPreflight {
		t.Fatalf("expected interactive sync to omit standard-only flags: %+v", interactiveReq)
	}

	standard := newTUISyncModel(&AppContext{}, tuiSyncWorkflowStandard)
	standard.askOnExisting = true
	standard.askOnExistingSet = true
	standard.scanGaps = true
	standard.noPreflight = true

	standardReq := standard.buildSyncRequest([]string{"source-b"})
	if standardReq.Plan {
		t.Fatalf("expected standard sync to keep plan mode disabled")
	}
	if standardReq.PlanLimit != 0 {
		t.Fatalf("expected standard sync to omit plan limit, got %d", standardReq.PlanLimit)
	}
	if !standardReq.AskOnExisting || !standardReq.AskOnExistingSet || !standardReq.ScanGaps || !standardReq.NoPreflight {
		t.Fatalf("expected standard sync flags to pass through: %+v", standardReq)
	}
}

func TestTUISyncModelViewIsModeSpecific(t *testing.T) {
	interactive := newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	interactive.cfgLoaded = true
	interactive.sources = []config.Source{
		{ID: "soundcloud-likes", Type: config.SourceTypeSoundCloud, TargetDir: "/tmp/music", URL: "https://soundcloud.com/janxadam", StateFile: "/tmp/soundcloud.sync.scdl", Adapter: config.AdapterSpec{Kind: "scdl"}},
	}
	interactive.selected["soundcloud-likes"] = true
	interactiveView := interactive.View()
	if !strings.Contains(interactiveView, "plan_limit=") {
		t.Fatalf("expected interactive sync plan limit controls, got: %s", interactiveView)
	}
	if strings.Contains(interactiveView, "download_order=") || strings.Contains(interactiveView, "ORDER:") {
		t.Fatalf("expected interactive sync to hide global download order summary, got: %s", interactiveView)
	}
	if strings.Contains(interactiveView, "ask_on_existing=") || strings.Contains(interactiveView, "scan_gaps=") || strings.Contains(interactiveView, "no_preflight=") {
		t.Fatalf("expected interactive sync to hide standard-only options, got: %s", interactiveView)
	}

	standard := newTUISyncModel(&AppContext{}, tuiSyncWorkflowStandard)
	standard.cfgLoaded = true
	standardView := standard.View()
	if strings.Contains(standardView, "plan_limit=") || strings.Contains(standardView, "type limit") {
		t.Fatalf("expected standard sync to hide plan controls, got: %s", standardView)
	}
	if !strings.Contains(standardView, "ask_on_existing=") || !strings.Contains(standardView, "scan_gaps=") || !strings.Contains(standardView, "no_preflight=") {
		t.Fatalf("expected standard sync options to remain visible, got: %s", standardView)
	}
}

func TestTUIRootStandardSyncShellShowsSectionedBodyAndShortcuts(t *testing.T) {
	root := renderStandardShellFixture(150, 30)

	view := root.View()
	if !strings.Contains(view, "Selection") || !strings.Contains(view, "Run") || !strings.Contains(view, "Activity") {
		t.Fatalf("expected sectioned standard sync shell body, got: %s", view)
	}
	if !strings.Contains(view, "p") || !strings.Contains(view, "activity") {
		t.Fatalf("expected standard sync shortcut bar to include activity toggle, got: %s", view)
	}
	if !strings.Contains(view, "ask_on_existing=") || !strings.Contains(view, "scan_gaps=") || !strings.Contains(view, "no_preflight=") {
		t.Fatalf("expected standard sync command summary to include standard-only flags, got: %s", view)
	}
	if strings.Contains(view, "plan_limit=") {
		t.Fatalf("expected standard sync shell to keep plan controls hidden, got: %s", view)
	}
}

func TestTUIRootStandardSyncActivityPanelDefaultsCollapsedInCompactAndPToggles(t *testing.T) {
	root := renderStandardShellFixture(100, 40)

	view := root.View()
	if !strings.Contains(view, "collapsed") {
		t.Fatalf("expected compact standard-sync activity panel to default collapsed, got: %s", view)
	}

	nextModel, _ := root.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	next := nextModel.(tuiRootModel)
	view = next.View()
	if !strings.Contains(view, "no activity yet") {
		t.Fatalf("expected p to expand standard-sync activity panel, got: %s", view)
	}
}

func TestTUIRootStandardSyncRunningShowsProgressAndSourceSummary(t *testing.T) {
	root := renderStandardShellFixture(160, 30)
	root.syncModel.running = true
	root.syncModel.runStartedAt = time.Now().Add(-5 * time.Second)

	events := []output.Event{
		{
			Event:    output.EventSourcePreflight,
			SourceID: "soundcloud-likes",
			Details:  map[string]any{"planned_download_count": 3},
		},
		{
			Event:    output.EventSourceStarted,
			SourceID: "soundcloud-likes",
		},
		{
			Event:    output.EventTrackStarted,
			SourceID: "soundcloud-likes",
			Details: map[string]any{
				"track_name": "Structured Song",
				"index":      1,
				"total":      3,
			},
		},
		{
			Event:    output.EventTrackProgress,
			SourceID: "soundcloud-likes",
			Details: map[string]any{
				"track_name": "Structured Song",
				"index":      1,
				"total":      3,
				"percent":    67.5,
			},
		},
	}
	for _, event := range events {
		var cmd tea.Cmd
		root.syncModel, cmd = root.syncModel.Update(tuiSyncEventMsg{Event: event})
		if cmd != nil {
			t.Fatalf("expected no wait command without event channel")
		}
	}

	view := root.View()
	if !strings.Contains(view, "Current Source: soundcloud-likes (running)") {
		t.Fatalf("expected current source headline in run section, got: %s", view)
	}
	if !strings.Contains(view, "Structured Song") || !strings.Contains(view, "[overall]") {
		t.Fatalf("expected structured progress lines in run section, got: %s", view)
	}
	if !strings.Contains(view, "SOURCE") || !strings.Contains(view, "OUTCOME") {
		t.Fatalf("expected standard sync source summary table, got: %s", view)
	}
	if !strings.Contains(view, "soundcloud-likes") {
		t.Fatalf("expected source summary row to render source id, got: %s", view)
	}
}

func TestTUIStandardSyncSummariesAndFooterUseRuntimeCounts(t *testing.T) {
	root := renderStandardShellFixture(160, 30)
	root.syncModel.running = true
	root.syncModel.runStartedAt = time.Now().Add(-11 * time.Second)

	events := []output.Event{
		{Event: output.EventSourcePreflight, SourceID: "source-a", Details: map[string]any{"planned_download_count": 3}},
		{Event: output.EventSourceStarted, SourceID: "source-a"},
		{Event: output.EventTrackDone, SourceID: "source-a", Details: map[string]any{"track_name": "Done Song", "index": 1, "total": 3}},
		{Event: output.EventTrackSkip, SourceID: "source-a", Details: map[string]any{"track_name": "Skip Song", "index": 2, "total": 3, "reason": "duplicate"}},
		{Event: output.EventTrackFail, SourceID: "source-a", Details: map[string]any{"track_name": "Fail Song", "index": 3, "total": 3, "reason": "network"}},
		{Event: output.EventSourceFinished, SourceID: "source-a"},
		{Event: output.EventSourcePreflight, SourceID: "source-b", Details: map[string]any{"planned_download_count": 1}},
		{Event: output.EventSourceStarted, SourceID: "source-b"},
		{Event: output.EventSourceFailed, Level: output.LevelError, SourceID: "source-b", Message: "[source-b] failed"},
	}
	for _, event := range events {
		var cmd tea.Cmd
		root.syncModel, cmd = root.syncModel.Update(tuiSyncEventMsg{Event: event})
		if cmd != nil {
			t.Fatalf("expected no wait command without event channel")
		}
	}
	root.syncModel.running = false
	root.syncModel.done = true
	root.syncModel.runFinishedAt = time.Now()

	sourceA := root.syncModel.standardSummaries["source-a"]
	if sourceA == nil || sourceA.Planned != 3 || sourceA.Done != 1 || sourceA.Skipped != 1 || sourceA.Failed != 1 || sourceA.Lifecycle != tuiStandardSyncSourceFinished {
		t.Fatalf("expected source-a summary to reflect structured runtime updates, got: %+v", sourceA)
	}
	sourceB := root.syncModel.standardSummaries["source-b"]
	if sourceB == nil || sourceB.Planned != 1 || sourceB.Lifecycle != tuiStandardSyncSourceFailed {
		t.Fatalf("expected source-b summary to reflect source failure, got: %+v", sourceB)
	}

	view := root.View()
	if !strings.Contains(view, "done: 1") || !strings.Contains(view, "skipped: 1") || !strings.Contains(view, "failed: 1") {
		t.Fatalf("expected footer to use runtime summary counts, got: %s", view)
	}
	if !strings.Contains(view, "elapsed:") {
		t.Fatalf("expected footer to include elapsed time after runtime, got: %s", view)
	}
	if strings.Contains(view, "attempted:") {
		t.Fatalf("expected footer not to fall back to old attempted-source stats, got: %s", view)
	}
}

func TestTUIRootStandardSyncPromptModalAndFailureDiagnostics(t *testing.T) {
	root := renderStandardShellFixture(150, 40)
	root.syncModel.interactionPrompt = &tuiInteractionPromptState{
		kind:       tuiPromptKindConfirm,
		sourceID:   "spotify-source",
		prompt:     "Retry login?",
		defaultYes: true,
	}
	root.syncModel.lastFailure = &tuiSyncFailureState{
		SourceID:       "spotify-source",
		Message:        "[spotify-source] command failed with exit code 1",
		TimedOut:       true,
		StdoutTail:     "line one",
		StderrTail:     "fatal line",
		FailureLogPath: "/tmp/udl-state/sync-failures.jsonl",
	}

	view := root.View()
	if !strings.Contains(view, "Prompt") || !strings.Contains(view, "Retry login?") {
		t.Fatalf("expected standard sync shell prompt modal, got: %s", view)
	}
	if !strings.Contains(view, "last failure:") || !strings.Contains(view, "stdout_tail:") || !strings.Contains(view, "fatal line") {
		t.Fatalf("expected failure diagnostics to render in activity section, got: %s", view)
	}
}

func TestTUIRootShellShowsSourcesInSidebarForInteractiveSync(t *testing.T) {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenInteractiveSync
	root.syncModel = newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	root.syncModel.cfgLoaded = true
	root.syncModel.sources = []config.Source{
		{ID: "soundcloud-likes", Type: config.SourceTypeSoundCloud, Adapter: config.AdapterSpec{Kind: "scdl"}},
	}
	root.syncModel.selected["soundcloud-likes"] = true

	view := root.View()
	if !strings.Contains(view, "SOURCES") {
		t.Fatalf("expected sources section in sidebar, got: %s", view)
	}
	if !strings.Contains(view, "soundcloud-likes") {
		t.Fatalf("expected source id in sidebar, got: %s", view)
	}
	if strings.Contains(view, "\nSources:\n") {
		t.Fatalf("expected source list to be removed from body, got: %s", view)
	}
}

func TestTUIRootInteractiveSyncIdleShowsStructuredPlaceholder(t *testing.T) {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenInteractiveSync
	root.width = 150
	root.height = 30
	root.syncModel = newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	root.syncModel.cfgLoaded = true
	root.syncModel.sources = []config.Source{
		{ID: "soundcloud-likes", Type: config.SourceTypeSoundCloud, TargetDir: "/tmp/music", URL: "https://soundcloud.com/janxadam", StateFile: "/tmp/soundcloud.sync.scdl", Adapter: config.AdapterSpec{Kind: "scdl"}},
	}
	root.syncModel.selected["soundcloud-likes"] = true

	view := root.View()
	if !strings.Contains(view, "Plan Selection") || !strings.Contains(view, "source=soundcloud-likes") {
		t.Fatalf("expected idle selection to show focused source header, got: %s", view)
	}
	if !strings.Contains(view, "order=oldest_first") {
		t.Fatalf("expected idle selection to show download order, got: %s", view)
	}
	if !strings.Contains(view, "target /tmp/music") || !strings.Contains(view, "url https://soundcloud.com/janxadam") {
		t.Fatalf("expected idle selection to show focused source details, got: %s", view)
	}
	if !strings.Contains(view, "source controls") || !strings.Contains(view, "space  toggle source") || !strings.Contains(view, "o  order") || !strings.Contains(view, "enter  run") {
		t.Fatalf("expected idle controls to render inside selection, got: %s", view)
	}
	if strings.Contains(view, "dry_run=false  timeout=default  plan_limit=10") || strings.Contains(view, "Rows appear here after interactive preflight starts.") || strings.Contains(view, "Press enter to run `udl sync --plan` for the selected sources.") {
		t.Fatalf("expected old duplicate selection summary to be removed, got: %s", view)
	}
	if strings.Contains(view, "[j/k] move") || strings.Contains(view, "[enter] run") {
		t.Fatalf("expected global shortcut bar to be removed for interactive sync, got: %s", view)
	}
	if !strings.Contains(view, "SEL  #  STATUS  TRACK  ID") {
		t.Fatalf("expected empty table shell, got: %s", view)
	}
	if !strings.Contains(view, "start preflight") {
		t.Fatalf("expected tracks section to own the launch hint, got: %s", view)
	}
	if !strings.Contains(view, "no activity yet") {
		t.Fatalf("expected empty activity panel, got: %s", view)
	}
	if !strings.Contains(view, "state: ready") || !strings.Contains(view, "will sync: 0") || !strings.Contains(view, "new: 0") || !strings.Contains(view, "known gap: 0") || !strings.Contains(view, "already have: 0") || !strings.Contains(view, "progress: ░░░░░░░░░░   0%") {
		t.Fatalf("expected idle interactive footer counts, got: %s", view)
	}
}

func TestTUIRootShellRendersSyncPlanPromptInline(t *testing.T) {
	root := renderPlanPromptFixture([]engine.PlanRow{{Index: 1, Toggleable: true, SelectedByDefault: true}})

	view := root.View()
	if !strings.Contains(view, "INTERACTIVE SYNC WORKFLOW") {
		t.Fatalf("expected interactive sync shell title, got: %s", view)
	}
	if !strings.Contains(view, "Plan Selection") {
		t.Fatalf("expected inline plan selection body title, got: %s", view)
	}
	if !strings.Contains(view, "source=soundcloud-likes") {
		t.Fatalf("expected plan prompt source details, got: %s", view)
	}
	if strings.Contains(view, "Prompt") {
		t.Fatalf("expected plan selector not to render as prompt modal, got: %s", view)
	}
}

func TestTUIRootShellHidesGlobalShortcutsDuringPlanSelection(t *testing.T) {
	root := renderPlanPromptFixture([]engine.PlanRow{{Index: 1, Toggleable: true, SelectedByDefault: true}})

	view := root.View()
	if strings.Contains(view, "[tab] filters") || strings.Contains(view, "[enter] run") {
		t.Fatalf("expected global shortcut bar to stay hidden during interactive sync, got: %s", view)
	}
	if !strings.Contains(view, "tab  switch") {
		t.Fatalf("expected inline selection controls to remain visible, got: %s", view)
	}
}

func TestTUIRootShellRunningInteractiveSyncKeepsInlineBrowseControls(t *testing.T) {
	root := renderPlanPromptFixture([]engine.PlanRow{{Index: 1, Title: "track", RemoteID: "a", Status: engine.PlanRowMissingKnownGap, Toggleable: true, SelectedByDefault: true}})
	root.syncModel.running = true
	root.syncModel.planPrompt = nil
	root.syncModel.interactivePhase = tuiInteractivePhaseSyncing
	root.syncModel.interactiveTracker.MarkRuntimeStarted(time.Now().Add(-5 * time.Second))

	view := root.View()
	if strings.Contains(view, "[p] activity") || strings.Contains(view, "[x] cancel active run") {
		t.Fatalf("expected global shortcut bar to stay hidden once inline selection controls exist, got: %s", view)
	}
	if !strings.Contains(view, "tab  switch") || !strings.Contains(view, "j/k  move") || !strings.Contains(view, "p  activity") || !strings.Contains(view, "x  cancel run") {
		t.Fatalf("expected inline runtime browse controls, got: %s", view)
	}
	if strings.Contains(view, "space  toggle") || strings.Contains(view, "a  all visible") || strings.Contains(view, "enter  confirm") {
		t.Fatalf("expected inline controls to drop selection-only actions while running, got: %s", view)
	}
	if strings.Contains(view, "selected=") || strings.Contains(view, "pending=") || strings.Contains(view, "skipped=") || strings.Contains(view, "Running sync... press x or ctrl+c to cancel.") {
		t.Fatalf("expected selection panel to avoid duplicating runtime status, got: %s", view)
	}
}

func TestTUIRootInteractiveSyncIdleSelectionFollowsFocusedSource(t *testing.T) {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenInteractiveSync
	root.width = 150
	root.height = 30
	root.syncModel = newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	root.syncModel.cfgLoaded = true
	root.syncModel.sources = []config.Source{
		{ID: "soundcloud-likes", Type: config.SourceTypeSoundCloud, TargetDir: "/tmp/sc", URL: "https://soundcloud.com/janxadam", StateFile: "/tmp/sc.sync.scdl", Adapter: config.AdapterSpec{Kind: "scdl"}},
		{ID: "spotify-list", Type: config.SourceTypeSpotify, TargetDir: "/tmp/sp", URL: "https://open.spotify.com/playlist/abc", StateFile: "/tmp/sp.sync.spotify", Adapter: config.AdapterSpec{Kind: "spotdl"}},
	}
	root.syncModel.selected["soundcloud-likes"] = true
	root.syncModel.selected["spotify-list"] = true
	root.syncModel.cursor = 1

	view := root.View()
	if !strings.Contains(view, "source=spotify-list") || !strings.Contains(view, "type=spotify/spotdl") {
		t.Fatalf("expected selection to reflect focused source, got: %s", view)
	}
	if !strings.Contains(view, "target /tmp/sp") || !strings.Contains(view, "url https://open.spotify.com/playlist/abc") {
		t.Fatalf("expected focused source details in idle selection, got: %s", view)
	}
	if strings.Contains(view, "source=soundcloud-likes") {
		t.Fatalf("expected unfocused source details to be absent from idle selection, got: %s", view)
	}
}

func TestTUIRootShellKeepsChromeVisibleForLongPlanSelection(t *testing.T) {
	rows := make([]engine.PlanRow, 0, 30)
	for i := 1; i <= 30; i++ {
		rows = append(rows, engine.PlanRow{
			Index:             i,
			Title:             fmt.Sprintf("row-%02d", i),
			RemoteID:          fmt.Sprintf("id-%02d", i),
			Status:            engine.PlanRowAlreadyDownloaded,
			Toggleable:        i%3 == 0,
			SelectedByDefault: i%3 == 0,
		})
	}
	root := renderPlanPromptFixture(rows)
	root.width = 140
	root.height = 20

	view := root.View()
	if !strings.Contains(view, "STATE: review") {
		t.Fatalf("expected shell top bar to remain visible, got: %s", view)
	}
	if !strings.Contains(view, "row-01") {
		t.Fatalf("expected early selector rows in view, got: %s", view)
	}
	if strings.Contains(view, "row-20") {
		t.Fatalf("expected long selector rows to be windowed out, got: %s", view)
	}
	if got := lipgloss.Height(view); got > root.height {
		t.Fatalf("expected rendered shell height <= %d, got %d", root.height, got)
	}
}

func TestTUIRootShellRendersPlanRowsWithStatusAndLockedMarker(t *testing.T) {
	root := renderPlanPromptFixture([]engine.PlanRow{
		{Index: 1, Title: "have", RemoteID: "id-have", Status: engine.PlanRowAlreadyDownloaded},
		{Index: 2, Title: "new", RemoteID: "id-new", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
		{Index: 3, Title: "gap", RemoteID: "id-gap", Status: engine.PlanRowMissingKnownGap, Toggleable: true},
	})

	view := root.View()
	if !strings.Contains(view, "have-it") || !strings.Contains(view, "known-gap") {
		t.Fatalf("expected status chips in table, got: %s", view)
	}
	if !strings.Contains(view, "id-have") || !strings.Contains(view, "id-new") {
		t.Fatalf("expected remote ids in table, got: %s", view)
	}
	if !strings.Contains(view, ">[-]") {
		t.Fatalf("expected locked row selection marker, got: %s", view)
	}
}

func TestTUIRootShellRendersPlanFiltersInSidebar(t *testing.T) {
	root := renderPlanPromptFixture([]engine.PlanRow{
		{Index: 1, Title: "have", RemoteID: "a", Status: engine.PlanRowAlreadyDownloaded},
		{Index: 2, Title: "new", RemoteID: "b", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
		{Index: 3, Title: "gap", RemoteID: "c", Status: engine.PlanRowMissingKnownGap, Toggleable: true},
	})

	view := root.View()
	if !strings.Contains(view, "FILTERS") {
		t.Fatalf("expected filters sidebar section, got: %s", view)
	}
	if !strings.Contains(view, "Will Sync (1)") || !strings.Contains(view, "New (1)") || !strings.Contains(view, "Known Gap (1)") || !strings.Contains(view, "Already Have (1)") {
		t.Fatalf("expected filter counts in sidebar, got: %s", view)
	}
	if !strings.Contains(view, "SEL") || !strings.Contains(view, "STATUS") || !strings.Contains(view, "TRACK") || !strings.Contains(view, "ID") {
		t.Fatalf("expected table header columns, got: %s", view)
	}
}

func TestTUISyncPlanPromptTabFocusesFiltersAndAppliesFilter(t *testing.T) {
	m := newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	m.cfgLoaded = true
	reply := make(chan tuiPlanSelectResult, 1)
	m.planPrompt = newTUIPlanPromptState(tuiPlanSelectRequestMsg{
		SourceID: "source-a",
		Rows: []engine.PlanRow{
			{Index: 1, Title: "have", RemoteID: "a", Status: engine.PlanRowAlreadyDownloaded},
			{Index: 2, Title: "new", RemoteID: "b", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
			{Index: 3, Title: "gap", RemoteID: "c", Status: engine.PlanRowMissingKnownGap, Toggleable: true},
		},
		Details: planSourceDetails{SourceID: "source-a"},
		Reply:   reply,
	})

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if !m.planPrompt.focusFilters {
		t.Fatalf("expected tab to move focus to filters")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.planPrompt.focusFilters {
		t.Fatalf("expected enter to apply filter and return focus to tracks")
	}
	if m.planPrompt.filter != tuiTrackFilterMissingNew {
		t.Fatalf("expected missing_new filter, got %q", m.planPrompt.filter)
	}
}

func TestTUIRootShellInteractiveFooterShowsSelectionCounts(t *testing.T) {
	root := renderPlanPromptFixture([]engine.PlanRow{
		{Index: 1, Title: "have", RemoteID: "a", Status: engine.PlanRowAlreadyDownloaded},
		{Index: 2, Title: "new", RemoteID: "b", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
		{Index: 3, Title: "gap", RemoteID: "c", Status: engine.PlanRowMissingKnownGap, Toggleable: true},
	})

	view := root.View()
	if !strings.Contains(view, "state: review") {
		t.Fatalf("expected interactive review state before actual sync starts, got: %s", view)
	}
	if !strings.Contains(view, "will sync: 1") || !strings.Contains(view, "new: 1") || !strings.Contains(view, "known gap: 1") || !strings.Contains(view, "already have: 1") || !strings.Contains(view, "progress: ░░░░░░░░░░   0%") {
		t.Fatalf("expected interactive footer selection counts, got: %s", view)
	}
	if strings.Contains(view, "elapsed:") {
		t.Fatalf("expected no elapsed timer during review phase, got: %s", view)
	}
}

func TestTUISyncInteractiveRowsTransitionToDownloadedAndFilterCountsUpdate(t *testing.T) {
	m := newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	m.cfgLoaded = true
	reply := make(chan tuiPlanSelectResult, 1)
	m.planPrompt = newTUIPlanPromptState(tuiPlanSelectRequestMsg{
		SourceID: "soundcloud-likes",
		Rows: []engine.PlanRow{
			{Index: 1, Title: "Piano Jazz", RemoteID: "1", Status: engine.PlanRowMissingKnownGap, Toggleable: true, SelectedByDefault: true},
			{Index: 2, Title: "Existing", RemoteID: "2", Status: engine.PlanRowAlreadyDownloaded},
		},
		Details: planSourceDetails{SourceID: "soundcloud-likes"},
		Reply:   reply,
	})
	m.confirmInteractiveSelection(m.planPrompt.sourceID)
	m.planPrompt = nil
	m.running = true
	m.interactivePhase = tuiInteractivePhaseSyncing

	m, _ = m.Update(tuiSyncEventMsg{Event: output.Event{
		Event:    output.EventTrackDone,
		SourceID: "soundcloud-likes",
		Details: map[string]any{
			"track_name": "Piano Jazz",
			"index":      1,
			"total":      1,
		},
	}})

	selection := m.currentInteractiveSelection()
	if selection == nil {
		t.Fatalf("expected interactive selection state")
	}
	displayState := m.currentInteractiveDisplayState()
	if displayState == nil {
		t.Fatalf("expected interactive display state")
	}
	if got := selection.filterCount(displayState.rows, tuiTrackFilterKnownGap, tuiInteractivePhaseSyncing); got != 0 {
		t.Fatalf("expected runtime-only filter family to hide review-only known-gap counts, got %d", got)
	}
	if got := selection.filterCount(displayState.rows, tuiTrackFilterDownloaded, tuiInteractivePhaseSyncing); got != 1 {
		t.Fatalf("expected downloaded filter count to exclude already-have rows, got %d", got)
	}
	if got := selection.filterCount(displayState.rows, tuiTrackFilterAlreadyHave, tuiInteractivePhaseSyncing); got != 1 {
		t.Fatalf("expected already-have filter count to keep locked rows separate, got %d", got)
	}

	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenInteractiveSync
	root.width = 160
	root.height = 30
	root.syncModel = m

	view := root.View()
	if !strings.Contains(view, "downloaded") || !strings.Contains(view, "· gap") || !strings.Contains(view, "run #1") {
		t.Fatalf("expected completed row to render downloaded status, got: %s", view)
	}
	if !strings.Contains(view, "Downloaded (1)") || !strings.Contains(view, "Already Have (1)") || !strings.Contains(view, "In Run (1)") {
		t.Fatalf("expected sidebar filters to reflect runtime state, got: %s", view)
	}
}

func TestTUIInteractiveRuntimeAllFilterShowsDeselectedRowsAsNotRun(t *testing.T) {
	root := renderPlanPromptFixture([]engine.PlanRow{
		{Index: 1, Title: "Selected", RemoteID: "a", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
		{Index: 2, Title: "Deselected", RemoteID: "b", Status: engine.PlanRowMissingKnownGap, Toggleable: true, SelectedByDefault: true},
		{Index: 3, Title: "Have", RemoteID: "c", Status: engine.PlanRowAlreadyDownloaded},
	})
	root.syncModel.planPrompt = nil
	root.syncModel.running = true
	root.syncModel.interactivePhase = tuiInteractivePhaseSyncing

	selection := root.syncModel.currentInteractiveSelection()
	if selection == nil {
		t.Fatalf("expected interactive selection state")
	}
	selection.setSelected(2, false)
	root.syncModel.interactiveTracker.ConfirmSelection(selection)

	view := root.View()
	if !strings.Contains(view, "not-run") || !strings.Contains(view, "· gap") {
		t.Fatalf("expected deselected runtime row to render as not-run, got: %s", view)
	}
	if strings.Contains(view, "In Run (2)") {
		t.Fatalf("expected deselected row to be excluded from runtime in-run count, got: %s", view)
	}
}

func TestTUIInteractiveRuntimeRemapsReviewFilterToInRun(t *testing.T) {
	root := renderPlanPromptFixture([]engine.PlanRow{
		{Index: 1, Title: "New", RemoteID: "a", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
		{Index: 2, Title: "Have", RemoteID: "b", Status: engine.PlanRowAlreadyDownloaded},
	})
	root.syncModel.planPrompt = nil
	root.syncModel.running = true
	root.syncModel.interactivePhase = tuiInteractivePhaseSyncing

	selection := root.syncModel.currentInteractiveSelection()
	if selection == nil {
		t.Fatalf("expected interactive selection state")
	}
	selection.filter = tuiTrackFilterMissingNew
	selection.focusFilters = true
	selection.filterCursor = 2
	root.syncModel.interactiveTracker.ConfirmSelection(selection)

	view := root.View()
	if selection.filter != tuiTrackFilterInRun {
		t.Fatalf("expected runtime phase to remap review filter to in-run, got %q", selection.filter)
	}
	if !strings.Contains(view, "In Run (1)") {
		t.Fatalf("expected remapped runtime filter to render in sidebar, got: %s", view)
	}
}

func TestTUISyncInteractiveProgressRendersGraphicalBars(t *testing.T) {
	m := newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	m.cfgLoaded = true
	reply := make(chan tuiPlanSelectResult, 1)
	m.planPrompt = newTUIPlanPromptState(tuiPlanSelectRequestMsg{
		SourceID: "soundcloud-likes",
		Rows: []engine.PlanRow{
			{Index: 1, Title: "Piano Jazz", RemoteID: "1", Status: engine.PlanRowMissingKnownGap, Toggleable: true, SelectedByDefault: true},
		},
		Details: planSourceDetails{SourceID: "soundcloud-likes"},
		Reply:   reply,
	})
	m.confirmInteractiveSelection(m.planPrompt.sourceID)
	m.planPrompt = nil
	m.running = true
	m.interactivePhase = tuiInteractivePhaseSyncing
	m.interactiveTracker.MarkRuntimeStarted(time.Now().Add(-5 * time.Second))
	m.progress.ObserveEvent(output.Event{
		Event:    output.EventSourcePreflight,
		SourceID: "soundcloud-likes",
		Details:  map[string]any{"planned_download_count": 1},
	})

	m, _ = m.Update(tuiSyncEventMsg{Event: output.Event{
		Event:    output.EventTrackProgress,
		SourceID: "soundcloud-likes",
		Details: map[string]any{
			"track_name": "Piano Jazz",
			"index":      1,
			"total":      1,
			"percent":    50.0,
		},
	}})

	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenInteractiveSync
	root.width = 160
	root.height = 30
	root.syncModel = m

	view := root.View()
	if !strings.Contains(view, "dl  50%") {
		t.Fatalf("expected per-track graphical status to show progress, got: %s", view)
	}
	if !strings.Contains(view, "completed: 0") {
		t.Fatalf("expected footer to use completed track label, got: %s", view)
	}
	if !strings.Contains(view, "50%") || !strings.Contains(view, "█████") {
		t.Fatalf("expected footer progress bar to render graphically, got: %s", view)
	}
}

func TestTUIRootShellActivityPanelDefaultsCollapsedInCompactAndPToggles(t *testing.T) {
	root := renderPlanPromptFixture([]engine.PlanRow{{Index: 1, Title: "new", RemoteID: "b", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true}})
	root.width = 100
	root.height = 40

	view := root.View()
	if !strings.Contains(view, "collapsed") {
		t.Fatalf("expected compact activity panel to default collapsed, got: %s", view)
	}

	nextModel, _ := root.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	next := nextModel.(tuiRootModel)
	view = next.View()
	if !strings.Contains(view, "no activity yet") {
		t.Fatalf("expected p to expand activity panel, got: %s", view)
	}
}

func TestTUIRootInteractiveSyncRunningWithoutRowsShowsPreflightOnlyInTracks(t *testing.T) {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenInteractiveSync
	root.width = 150
	root.height = 30
	root.syncModel = newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	root.syncModel.cfgLoaded = true
	root.syncModel.running = true
	root.syncModel.interactivePhase = tuiInteractivePhasePreflight
	root.syncModel.sources = []config.Source{
		{ID: "soundcloud-likes", Type: config.SourceTypeSoundCloud, TargetDir: "/tmp/music", URL: "https://soundcloud.com/janxadam", StateFile: "/tmp/soundcloud.sync.scdl", Adapter: config.AdapterSpec{Kind: "scdl"}},
	}
	root.syncModel.selected["soundcloud-likes"] = true

	view := root.View()
	if !strings.Contains(view, "Preflight running... loading tracks for selection.") {
		t.Fatalf("expected tracks to show preflight loading state, got: %s", view)
	}
	if !strings.Contains(view, "state: preflight") {
		t.Fatalf("expected footer to show preflight state, got: %s", view)
	}
	if strings.Contains(view, "elapsed:") {
		t.Fatalf("expected no elapsed timer during preflight, got: %s", view)
	}
	if strings.Contains(view, "Running sync... press x or ctrl+c to cancel.") {
		t.Fatalf("expected selection to avoid duplicate running status before rows exist, got: %s", view)
	}
}

func TestTUIRootInteractiveSyncFooterShowsElapsedFromTracker(t *testing.T) {
	root := renderPlanPromptFixture([]engine.PlanRow{
		{Index: 1, Title: "new", RemoteID: "a", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
	})
	root.syncModel.confirmInteractiveSelection("soundcloud-likes")
	root.syncModel.planPrompt = nil
	root.syncModel.running = false
	root.syncModel.done = true
	root.syncModel.interactivePhase = tuiInteractivePhaseDone
	root.syncModel.runStartedAt = time.Time{}
	root.syncModel.interactiveTracker.MarkRuntimeStarted(time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC))
	root.syncModel.interactiveTracker.MarkRunFinished(time.Date(2026, 3, 21, 12, 1, 7, 0, time.UTC))

	view := root.View()
	if !strings.Contains(view, "elapsed: 1:07") {
		t.Fatalf("expected footer to render elapsed time from interactive tracker, got: %s", view)
	}
}

func TestTUIRootInteractiveSyncDoneWithoutRowsShowsTerminalTrackState(t *testing.T) {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenInteractiveSync
	root.width = 150
	root.height = 30
	root.syncModel = newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	root.syncModel.cfgLoaded = true
	root.syncModel.done = true
	root.syncModel.interactivePhase = tuiInteractivePhaseDone
	root.syncModel.result = engine.SyncResult{Attempted: 1, Succeeded: 1}
	root.syncModel.sources = []config.Source{
		{ID: "soundcloud-likes", Type: config.SourceTypeSoundCloud, TargetDir: "/tmp/music", URL: "https://soundcloud.com/janxadam", StateFile: "/tmp/soundcloud.sync.scdl", Adapter: config.AdapterSpec{Kind: "scdl"}},
	}
	root.syncModel.selected["soundcloud-likes"] = true

	view := root.View()
	if !strings.Contains(view, "No track rows were returned. Source is up to date or no downloads were planned.") {
		t.Fatalf("expected terminal no-row state to render in tracks, got: %s", view)
	}
	if !strings.Contains(view, "completed: 0") {
		t.Fatalf("expected terminal footer to stay track-based, got: %s", view)
	}
	if strings.Contains(view, "Run finished: attempted=1 succeeded=1 failed=0 skipped=0") {
		t.Fatalf("expected selection to avoid duplicating terminal no-row status, got: %s", view)
	}
}

func TestTUIInteractiveFooterUsesTrackCountsAfterDone(t *testing.T) {
	root := renderPlanPromptFixture([]engine.PlanRow{
		{Index: 1, Title: "downloaded-a", RemoteID: "a", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
		{Index: 2, Title: "downloaded-b", RemoteID: "b", Status: engine.PlanRowMissingKnownGap, Toggleable: true, SelectedByDefault: true},
		{Index: 3, Title: "existing", RemoteID: "c", Status: engine.PlanRowAlreadyDownloaded},
		{Index: 4, Title: "skipped", RemoteID: "d", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
		{Index: 5, Title: "failed", RemoteID: "e", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
	})
	root.syncModel.planPrompt = nil
	root.syncModel.done = true
	root.syncModel.running = false
	root.syncModel.interactivePhase = tuiInteractivePhaseDone
	root.syncModel.result = engine.SyncResult{Attempted: 1, Succeeded: 1, Failed: 0, Skipped: 0}
	root.syncModel.interactiveTracker.MarkRuntimeStarted(time.Now().Add(-11 * time.Second))
	root.syncModel.interactiveTracker.MarkRunFinished(time.Now())
	selection := root.syncModel.currentInteractiveSelection()
	if selection == nil {
		t.Fatalf("expected interactive selection state")
	}
	root.syncModel.interactiveTracker.ConfirmSelection(selection)
	root.syncModel.interactiveTracker.ObserveEvent(output.Event{Event: output.EventTrackDone, SourceID: "soundcloud-likes", Details: map[string]any{"index": 1}}, nil, "", false)
	root.syncModel.interactiveTracker.ObserveEvent(output.Event{Event: output.EventTrackDone, SourceID: "soundcloud-likes", Details: map[string]any{"index": 2}}, nil, "", false)
	root.syncModel.interactiveTracker.ObserveEvent(output.Event{Event: output.EventTrackSkip, SourceID: "soundcloud-likes", Details: map[string]any{"index": 3, "reason": "ignored"}}, nil, "", false)
	root.syncModel.interactiveTracker.ObserveEvent(output.Event{Event: output.EventTrackFail, SourceID: "soundcloud-likes", Details: map[string]any{"index": 4, "reason": "failed"}}, nil, "", false)

	view := root.View()
	if !strings.Contains(view, "completed: 2") || !strings.Contains(view, "skipped: 1") || !strings.Contains(view, "failed: 1") {
		t.Fatalf("expected terminal footer to use track totals, got: %s", view)
	}
	if strings.Contains(view, "completed: 1") || strings.Contains(view, "done:") {
		t.Fatalf("expected footer not to fall back to source result counts, got: %s", view)
	}
}

func TestTUIInteractiveFooterUsesTrackCountsWhileRunning(t *testing.T) {
	root := renderPlanPromptFixture([]engine.PlanRow{
		{Index: 1, Title: "downloaded-a", RemoteID: "a", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
		{Index: 2, Title: "skipped", RemoteID: "b", Status: engine.PlanRowMissingKnownGap, Toggleable: true, SelectedByDefault: true},
		{Index: 3, Title: "failed", RemoteID: "c", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
		{Index: 4, Title: "existing", RemoteID: "d", Status: engine.PlanRowAlreadyDownloaded},
	})
	root.syncModel.planPrompt = nil
	root.syncModel.running = true
	root.syncModel.interactivePhase = tuiInteractivePhaseSyncing
	root.syncModel.interactiveTracker.MarkRuntimeStarted(time.Now().Add(-6 * time.Second))
	selection := root.syncModel.currentInteractiveSelection()
	if selection == nil {
		t.Fatalf("expected interactive selection state")
	}
	root.syncModel.interactiveTracker.ConfirmSelection(selection)
	root.syncModel.interactiveTracker.ObserveEvent(output.Event{Event: output.EventTrackDone, SourceID: "soundcloud-likes", Details: map[string]any{"index": 1}}, nil, "", false)
	root.syncModel.interactiveTracker.ObserveEvent(output.Event{Event: output.EventTrackSkip, SourceID: "soundcloud-likes", Details: map[string]any{"index": 2, "reason": "ignored"}}, nil, "", false)
	root.syncModel.interactiveTracker.ObserveEvent(output.Event{Event: output.EventTrackFail, SourceID: "soundcloud-likes", Details: map[string]any{"index": 3, "reason": "failed"}}, nil, "", false)

	view := root.View()
	if !strings.Contains(view, "in run: 3") {
		t.Fatalf("expected runtime footer to include in-run count, got: %s", view)
	}
	if !strings.Contains(view, "completed: 1") || !strings.Contains(view, "skipped: 1") || !strings.Contains(view, "failed: 1") {
		t.Fatalf("expected runtime footer to use track totals, got: %s", view)
	}
	if strings.Contains(view, "done:") {
		t.Fatalf("expected footer label to be renamed to completed, got: %s", view)
	}
}

func TestTUIInteractiveSidebarSourceLifecycleTones(t *testing.T) {
	m := newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	m.cfgLoaded = true
	m.sources = []config.Source{
		{ID: "active", Type: config.SourceTypeSoundCloud, Adapter: config.AdapterSpec{Kind: "scdl"}},
		{ID: "running", Type: config.SourceTypeSoundCloud, Adapter: config.AdapterSpec{Kind: "scdl"}},
		{ID: "finished", Type: config.SourceTypeSoundCloud, Adapter: config.AdapterSpec{Kind: "scdl"}},
		{ID: "failed", Type: config.SourceTypeSoundCloud, Adapter: config.AdapterSpec{Kind: "scdl"}},
	}
	for _, source := range m.sources {
		m.selected[source.ID] = true
	}
	m.cursor = 0
	m.setInteractiveSourceLifecycle("active", tuiSourceLifecycleFinished)
	m.setInteractiveSourceLifecycle("running", tuiSourceLifecycleRunning)
	m.setInteractiveSourceLifecycle("finished", tuiSourceLifecycleFinished)
	m.setInteractiveSourceLifecycle("failed", tuiSourceLifecycleFailed)

	sections := m.sidebarSections(tuiScreenInteractiveSync)
	var sourceItems []tuiSidebarItem
	for _, section := range sections {
		if section.Title == "sources" {
			sourceItems = section.Items
			break
		}
	}
	if len(sourceItems) != 4 {
		t.Fatalf("expected source sidebar items, got %+v", sourceItems)
	}
	if !sourceItems[0].Active || sourceItems[0].Tone != "success" {
		t.Fatalf("expected active source to preserve active highlight state while retaining success tone metadata, got %+v", sourceItems[0])
	}
	if sourceItems[1].Tone != "warning" {
		t.Fatalf("expected running source tone=warning, got %+v", sourceItems[1])
	}
	if sourceItems[2].Tone != "success" {
		t.Fatalf("expected finished source tone=success, got %+v", sourceItems[2])
	}
	if sourceItems[3].Tone != "danger" {
		t.Fatalf("expected failed source tone=danger, got %+v", sourceItems[3])
	}
}

func TestTUISyncModelPlanPromptLStillOpensTypedLimitInput(t *testing.T) {
	m := newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	m.cfgLoaded = true
	reply := make(chan tuiPlanSelectResult, 1)
	m.planPrompt = newTUIPlanPromptState(tuiPlanSelectRequestMsg{
		SourceID: "source-a",
		Rows:     []engine.PlanRow{{Index: 1, Title: "new", RemoteID: "b", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true}},
		Reply:    reply,
	})

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if !m.planLimitInputActive {
		t.Fatalf("expected l to keep opening typed plan limit input during selection")
	}
}

func TestTUISyncModelPostStartBrowsingKeepsFilteringAndBlocksConfigMutations(t *testing.T) {
	m := newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	m.cfgLoaded = true
	reply := make(chan tuiPlanSelectResult, 1)
	m.planPrompt = newTUIPlanPromptState(tuiPlanSelectRequestMsg{
		SourceID: "source-a",
		Rows: []engine.PlanRow{
			{Index: 1, Title: "have", RemoteID: "a", Status: engine.PlanRowAlreadyDownloaded},
			{Index: 2, Title: "new", RemoteID: "b", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
		},
		Reply: reply,
	})
	m.confirmInteractiveSelection(m.planPrompt.sourceID)
	m.planPrompt = nil
	m.done = true
	m.interactivePhase = tuiInteractivePhaseDone
	m.planLimit = 10

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	selection := m.currentInteractiveSelection()
	if selection == nil {
		t.Fatalf("expected interactive selection state")
	}
	if !selection.focusFilters {
		t.Fatalf("expected tab to move focus to filters after sync start")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	selection = m.currentInteractiveSelection()
	if selection == nil {
		t.Fatalf("expected interactive selection state")
	}
	if selection.focusFilters {
		t.Fatalf("expected enter to apply filter and return focus to tracks after sync start")
	}
	if selection.filter != tuiTrackFilterRemaining {
		t.Fatalf("expected filter browsing to remain available after sync start, got %q", selection.filter)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if m.planLimitInputActive {
		t.Fatalf("expected post-start config mutation keys to be blocked")
	}
}

func TestTUIRootShellPostStartFilterFocusShowsVisibleSidebarCursor(t *testing.T) {
	root := renderPlanPromptFixture([]engine.PlanRow{
		{Index: 1, Title: "have", RemoteID: "a", Status: engine.PlanRowAlreadyDownloaded},
		{Index: 2, Title: "new", RemoteID: "b", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
	})
	root.syncModel.planPrompt = nil
	root.syncModel.done = true
	root.syncModel.interactivePhase = tuiInteractivePhaseDone
	selection := root.syncModel.currentInteractiveSelection()
	if selection == nil {
		t.Fatalf("expected interactive selection state")
	}
	selection.focusFilters = true
	selection.filterCursor = 6
	selection.filter = tuiTrackFilterAlreadyHave

	view := root.View()
	if !strings.Contains(view, "> Already Have (1)") {
		t.Fatalf("expected focused filter cursor to remain visible after sync start, got: %s", view)
	}
}

func TestTUIInteractiveFooterAggregatesAcrossConfirmedSources(t *testing.T) {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenInteractiveSync
	root.width = 160
	root.height = 30
	root.syncModel = newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	root.syncModel.cfgLoaded = true
	root.syncModel.sources = []config.Source{
		{ID: "source-a", Type: config.SourceTypeSoundCloud, Adapter: config.AdapterSpec{Kind: "scdl"}},
		{ID: "source-b", Type: config.SourceTypeSoundCloud, Adapter: config.AdapterSpec{Kind: "scdl"}},
	}
	root.syncModel.selected["source-a"] = true
	root.syncModel.selected["source-b"] = true

	replyA := make(chan tuiPlanSelectResult, 1)
	root.syncModel, _ = root.syncModel.Update(tuiPlanSelectRequestMsg{
		SourceID: "source-a",
		Rows: []engine.PlanRow{
			{Index: 1, Title: "A done", RemoteID: "a-1", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
			{Index: 2, Title: "A skipped", RemoteID: "a-2", Status: engine.PlanRowMissingKnownGap, Toggleable: true, SelectedByDefault: true},
		},
		Details: planSourceDetails{SourceID: "source-a"},
		Reply:   replyA,
	})
	sourceA := root.syncModel.currentInteractiveSelection()
	if sourceA == nil {
		t.Fatalf("expected source-a selection")
	}
	root.syncModel.interactiveTracker.ConfirmSelection(sourceA)
	root.syncModel.planPrompt = nil

	replyB := make(chan tuiPlanSelectResult, 1)
	root.syncModel, _ = root.syncModel.Update(tuiPlanSelectRequestMsg{
		SourceID: "source-b",
		Rows: []engine.PlanRow{
			{Index: 1, Title: "B downloading", RemoteID: "b-1", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
		},
		Details: planSourceDetails{SourceID: "source-b"},
		Reply:   replyB,
	})
	sourceB := root.syncModel.currentInteractiveSelection()
	if sourceB == nil {
		t.Fatalf("expected source-b selection")
	}
	root.syncModel.interactiveTracker.ConfirmSelection(sourceB)
	root.syncModel.planPrompt = nil
	root.syncModel.running = true
	root.syncModel.interactivePhase = tuiInteractivePhaseSyncing
	root.syncModel.interactiveTracker.MarkRuntimeStarted(time.Now().Add(-5 * time.Second))
	root.syncModel.setInteractiveDisplaySource("source-b")

	root.syncModel.interactiveTracker.ObserveEvent(output.Event{Event: output.EventTrackDone, SourceID: "source-a", Details: map[string]any{"index": 1}}, nil, "", false)
	root.syncModel.interactiveTracker.ObserveEvent(output.Event{Event: output.EventTrackSkip, SourceID: "source-a", Details: map[string]any{"index": 2, "reason": "known gap"}}, nil, "", false)
	root.syncModel.interactiveTracker.ObserveEvent(output.Event{Event: output.EventTrackProgress, SourceID: "source-b", Details: map[string]any{"index": 1, "percent": 50.0}}, nil, "", false)

	view := root.View()
	if !strings.Contains(view, "B downloading") {
		t.Fatalf("expected displayed source body to stay on source-b, got: %s", view)
	}
	if !strings.Contains(view, "completed: 1") || !strings.Contains(view, "skipped: 1") || !strings.Contains(view, "failed: 0") {
		t.Fatalf("expected footer to aggregate counts across confirmed sources, got: %s", view)
	}
	if !strings.Contains(view, " 83%") {
		t.Fatalf("expected aggregate progress to include source-a completion and source-b partial progress, got: %s", view)
	}
}

func TestTUIInteractiveSourceActivityPersistsAcrossSourceSwitches(t *testing.T) {
	m := newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	m.cfgLoaded = true
	m.sources = []config.Source{
		{ID: "source-a", Type: config.SourceTypeSoundCloud, Adapter: config.AdapterSpec{Kind: "scdl"}},
		{ID: "source-b", Type: config.SourceTypeSoundCloud, Adapter: config.AdapterSpec{Kind: "scdl"}},
	}

	m, _ = m.Update(tuiSyncEventMsg{Event: output.Event{
		Event:    output.EventSourcePreflight,
		SourceID: "source-a",
		Details:  map[string]any{"planned_download_count": 2},
	}})
	stateA := m.interactiveSelectionForSource("source-a")
	if stateA == nil || len(m.interactiveTracker.SourceSnapshot("source-a").activity) == 0 {
		t.Fatalf("expected source-a activity to be recorded")
	}

	replyA := make(chan tuiPlanSelectResult, 1)
	m, _ = m.Update(tuiPlanSelectRequestMsg{
		SourceID: "source-a",
		Rows:     []engine.PlanRow{{Index: 1, Title: "A", RemoteID: "a-1", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true}},
		Details:  planSourceDetails{SourceID: "source-a"},
		Reply:    replyA,
	})
	if got := len(m.interactiveTracker.SourceSnapshot("source-a").activity); got == 0 {
		t.Fatalf("expected source-a activity to survive plan prompt replacement")
	}

	replyB := make(chan tuiPlanSelectResult, 1)
	m, _ = m.Update(tuiPlanSelectRequestMsg{
		SourceID: "source-b",
		Rows:     []engine.PlanRow{{Index: 1, Title: "B", RemoteID: "b-1", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true}},
		Details:  planSourceDetails{SourceID: "source-b"},
		Reply:    replyB,
	})
	if m.currentInteractiveDisplaySourceID() != "source-b" {
		t.Fatalf("expected plan prompt to focus the source-b display")
	}
	if got := len(m.interactiveTracker.SourceSnapshot("source-a").activity); got == 0 {
		t.Fatalf("expected source-a activity to remain after source-b prompt")
	}

	m.planPrompt = nil
	m, _ = m.Update(tuiSyncEventMsg{Event: output.Event{
		Event:    output.EventSourceStarted,
		SourceID: "source-b",
	}})
	if m.currentInteractiveDisplaySourceID() != "source-b" {
		t.Fatalf("expected latest active source to control display focus, got %q", m.currentInteractiveDisplaySourceID())
	}
}

func TestTUIInteractiveAggregateExcludesDeselectedRows(t *testing.T) {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenInteractiveSync
	root.width = 160
	root.height = 30
	root.syncModel = newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	root.syncModel.cfgLoaded = true
	root.syncModel.sources = []config.Source{
		{ID: "source-a", Type: config.SourceTypeSoundCloud, Adapter: config.AdapterSpec{Kind: "scdl"}},
	}
	root.syncModel.selected["source-a"] = true

	reply := make(chan tuiPlanSelectResult, 1)
	root.syncModel, _ = root.syncModel.Update(tuiPlanSelectRequestMsg{
		SourceID: "source-a",
		Rows: []engine.PlanRow{
			{Index: 1, Title: "Selected", RemoteID: "a-1", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
			{Index: 2, Title: "Deselected", RemoteID: "a-2", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
		},
		Details: planSourceDetails{SourceID: "source-a"},
		Reply:   reply,
	})
	selection := root.syncModel.currentInteractiveSelection()
	if selection == nil {
		t.Fatalf("expected interactive selection state")
	}
	selection.setSelected(2, false)
	root.syncModel.interactiveTracker.ConfirmSelection(selection)
	root.syncModel.planPrompt = nil
	root.syncModel.running = true
	root.syncModel.interactivePhase = tuiInteractivePhaseSyncing

	root.syncModel.interactiveTracker.ObserveEvent(output.Event{Event: output.EventTrackDone, SourceID: "source-a", Details: map[string]any{"index": 1}}, nil, "", false)
	root.syncModel.interactiveTracker.ObserveEvent(output.Event{Event: output.EventTrackFail, SourceID: "source-a", Details: map[string]any{"index": 2, "reason": "ignored"}}, nil, "", false)

	view := root.View()
	if !strings.Contains(view, "completed: 1") || !strings.Contains(view, "failed: 0") {
		t.Fatalf("expected deselected row to be excluded from aggregate footer counts, got: %s", view)
	}
	if !strings.Contains(view, "100%") {
		t.Fatalf("expected progress to be based only on selected rows, got: %s", view)
	}
}

func TestTUIInteractiveSparseSelectionShowsLaterRowsCompleting(t *testing.T) {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenInteractiveSync
	root.width = 160
	root.height = 30
	root.syncModel = newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	root.syncModel.cfgLoaded = true
	root.syncModel.sources = []config.Source{
		{ID: "source-a", Type: config.SourceTypeSoundCloud, Adapter: config.AdapterSpec{Kind: "scdl"}},
	}
	root.syncModel.selected["source-a"] = true

	reply := make(chan tuiPlanSelectResult, 1)
	rows := make([]engine.PlanRow, 0, 10)
	for i := 1; i <= 10; i++ {
		rows = append(rows, engine.PlanRow{
			Index:             i,
			Title:             fmt.Sprintf("Track %d", i),
			RemoteID:          fmt.Sprintf("track-%d", i),
			Status:            engine.PlanRowMissingNew,
			Toggleable:        true,
			SelectedByDefault: true,
		})
	}
	root.syncModel, _ = root.syncModel.Update(tuiPlanSelectRequestMsg{
		SourceID: "source-a",
		Rows:     rows,
		Details:  planSourceDetails{SourceID: "source-a"},
		Reply:    reply,
	})
	selection := root.syncModel.currentInteractiveSelection()
	if selection == nil {
		t.Fatalf("expected interactive selection state")
	}
	for _, idx := range []int{5, 6, 7, 8} {
		selection.setSelected(idx, false)
	}
	selection.cursor = 8
	root.syncModel.interactiveTracker.ConfirmSelection(selection)
	root.syncModel.planPrompt = nil
	root.syncModel.running = true
	root.syncModel.interactivePhase = tuiInteractivePhaseSyncing

	for executionIndex := 1; executionIndex <= 6; executionIndex++ {
		root.syncModel.interactiveTracker.ObserveEvent(output.Event{
			Event:    output.EventTrackDone,
			SourceID: "source-a",
			Details:  map[string]any{"index": executionIndex},
		}, nil, "", false)
	}

	view := root.View()
	if !strings.Contains(view, "completed: 6") {
		t.Fatalf("expected footer to count all selected later rows, got: %s", view)
	}
	if !strings.Contains(view, "Track 9") || !strings.Contains(view, "Track 10") || !strings.Contains(view, "downloaded") {
		t.Fatalf("expected later selected rows to render as downloaded, got: %s", view)
	}
	if !strings.Contains(view, "Track 5") || !strings.Contains(view, "Track 6") || !strings.Contains(view, "not-run") {
		t.Fatalf("expected deselected middle rows to remain visible as not-run, got: %s", view)
	}
}

func TestTUIInteractiveDisplayStateTracksOldestFirstRuntimeRows(t *testing.T) {
	m := newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	m.cfgLoaded = true
	m.sources = []config.Source{
		{ID: "soundcloud-likes", Type: config.SourceTypeSoundCloud, Adapter: config.AdapterSpec{Kind: "scdl"}},
	}
	m.selected["soundcloud-likes"] = true

	reply := make(chan tuiPlanSelectResult, 1)
	m.planPrompt = newTUIPlanPromptState(tuiPlanSelectRequestMsg{
		SourceID: "soundcloud-likes",
		Rows: []engine.PlanRow{
			{Index: 1, Title: "Desert Overworld (LUKA EDIT)", RemoteID: "2157239994", Status: engine.PlanRowMissingKnownGap, Toggleable: true, SelectedByDefault: true},
			{Index: 2, Title: "Premiere: KiNK & Raredub - Time To Change [MRS001]", RemoteID: "1939503326", Status: engine.PlanRowMissingKnownGap, Toggleable: true, SelectedByDefault: true},
			{Index: 3, Title: "Carte Blanche (DREY Schranz Edit) - Veracocha [FREE DOWNLOAD]", RemoteID: "1935964850", Status: engine.PlanRowMissingKnownGap, Toggleable: true, SelectedByDefault: true},
			{Index: 4, Title: "Showtek - Colours Of The Harder Styles (L4ZARUS Remix)", RemoteID: "2102155746", Status: engine.PlanRowMissingKnownGap, Toggleable: true, SelectedByDefault: true},
		},
		Details:       planSourceDetails{SourceID: "soundcloud-likes"},
		DownloadOrder: engine.DownloadOrderOldestFirst,
		Reply:         reply,
	})
	m.confirmInteractiveSelection(m.planPrompt.sourceID)
	m.planPrompt = nil
	m.running = true
	m.interactivePhase = tuiInteractivePhaseSyncing

	m.interactiveTracker.ObserveEvent(output.Event{
		Event:    output.EventTrackDone,
		SourceID: "soundcloud-likes",
		Details:  map[string]any{"index": 1},
	}, nil, "", false)
	m.interactiveTracker.ObserveEvent(output.Event{
		Event:    output.EventTrackProgress,
		SourceID: "soundcloud-likes",
		Details:  map[string]any{"index": 2, "percent": 14.0},
	}, nil, "", false)

	display := m.currentInteractiveDisplayState()
	if display == nil || len(display.rows) != 4 {
		t.Fatalf("expected interactive display rows, got %+v", display)
	}
	rowsByTitle := map[string]tuiTrackRowState{}
	for _, row := range display.rows {
		rowsByTitle[row.Title] = row
	}
	if got := rowsByTitle["Showtek - Colours Of The Harder Styles (L4ZARUS Remix)"]; got.RuntimeStatus != tuiTrackStatusDownloaded || got.ExecutionSlot != 1 {
		t.Fatalf("expected Showtek row downloaded in execution slot 1, got %+v", got)
	}
	if got := rowsByTitle["Carte Blanche (DREY Schranz Edit) - Veracocha [FREE DOWNLOAD]"]; got.RuntimeStatus != tuiTrackStatusDownloading || got.ExecutionSlot != 2 || !got.ProgressKnown || got.ProgressPercent != 14 {
		t.Fatalf("expected Carte Blanche row downloading in execution slot 2, got %+v", got)
	}
	if got := rowsByTitle["Desert Overworld (LUKA EDIT)"]; got.RuntimeStatus != tuiTrackStatusQueued || got.ExecutionSlot != 4 {
		t.Fatalf("expected Desert row to remain queued in execution slot 4, got %+v", got)
	}
	if got := rowsByTitle["Premiere: KiNK & Raredub - Time To Change [MRS001]"]; got.RuntimeStatus != tuiTrackStatusQueued || got.ExecutionSlot != 3 {
		t.Fatalf("expected KiNK row to remain queued in execution slot 3, got %+v", got)
	}
}

func renderPlanPromptFixture(rows []engine.PlanRow) tuiRootModel {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenInteractiveSync
	root.width = 160
	root.height = 28
	root.syncModel = newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	root.syncModel.cfgLoaded = true
	root.syncModel.sources = []config.Source{
		{ID: "soundcloud-likes", Type: config.SourceTypeSoundCloud, Adapter: config.AdapterSpec{Kind: "scdl"}},
	}
	root.syncModel.selected["soundcloud-likes"] = true
	reply := make(chan tuiPlanSelectResult, 1)
	root.syncModel, _ = root.syncModel.Update(tuiPlanSelectRequestMsg{
		SourceID: "soundcloud-likes",
		Rows:     rows,
		Details: planSourceDetails{
			SourceID:   "soundcloud-likes",
			SourceType: "soundcloud",
			Adapter:    "scdl",
			URL:        "https://soundcloud.com/janxadam",
			TargetDir:  "/tmp/music",
			StateFile:  "/tmp/soundcloud.sync.scdl",
			PlanLimit:  10,
		},
		Reply: reply,
	})
	return root
}

func renderStandardShellFixture(width, height int) tuiRootModel {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenSync
	root.width = width
	root.height = height
	root.syncModel = newTUISyncModel(&AppContext{}, tuiSyncWorkflowStandard)
	root.syncModel.width = width
	root.syncModel.height = height
	root.syncModel.cfgLoaded = true
	root.syncModel.sources = []config.Source{
		{ID: "soundcloud-likes", Type: config.SourceTypeSoundCloud, Adapter: config.AdapterSpec{Kind: "scdl"}},
		{ID: "spotify-source", Type: config.SourceTypeSpotify, Adapter: config.AdapterSpec{Kind: "spotdl"}},
	}
	root.syncModel.selected["soundcloud-likes"] = true
	root.syncModel.selected["spotify-source"] = true
	return root
}

func TestTUIRootShellRendersInitPromptModal(t *testing.T) {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenInit
	root.initModel = newTUIInitModel(&AppContext{})
	root.initModel.prompt = &tuiInteractionPromptState{
		kind:       tuiPromptKindConfirm,
		prompt:     "Overwrite existing config?",
		defaultYes: false,
	}

	view := root.View()
	if !strings.Contains(view, "INIT") {
		t.Fatalf("expected init shell title, got: %s", view)
	}
	if !strings.Contains(view, "Init Prompt") {
		t.Fatalf("expected init modal title, got: %s", view)
	}
	if !strings.Contains(view, "Overwrite existing config?") {
		t.Fatalf("expected init prompt body, got: %s", view)
	}
}

func TestTUIRootDoctorShellShowsSeverityOrderedChecklist(t *testing.T) {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenDoctor
	root.width = 150
	root.height = 36
	root.doctorModel = newTUIDoctorModel(&AppContext{})
	checks := []doctor.Check{
		{Severity: doctor.SeverityInfo, Name: "filesystem", Message: "target_dir is writable"},
		{Severity: doctor.SeverityWarn, Name: "security", Message: "credentials should be treated as sensitive"},
		{Severity: doctor.SeverityError, Name: "dependency", Message: "scdl not found in PATH"},
	}
	root.doctorModel.phase = tuiDoctorPhaseComplete
	root.doctorModel.checks = tuiSortedDoctorChecks(checks)
	root.doctorModel.summary = tuiDoctorSummary(checks)

	view := root.View()
	if !strings.Contains(view, "Summary") || !strings.Contains(view, "Checks") || !strings.Contains(view, "Next Step") {
		t.Fatalf("expected doctor shell sections, got: %s", view)
	}
	if !strings.Contains(view, "Errors: 1") || !strings.Contains(view, "Warnings: 1") || !strings.Contains(view, "Infos: 1") {
		t.Fatalf("expected doctor summary counts, got: %s", view)
	}
	errorIdx := strings.Index(view, "[ERROR]")
	warnIdx := strings.Index(view, "[WARN]")
	infoIdx := strings.Index(view, "[INFO]")
	if errorIdx < 0 || warnIdx < 0 || infoIdx < 0 || !(errorIdx < warnIdx && warnIdx < infoIdx) {
		t.Fatalf("expected severity-first ordering in doctor view, got: %s", view)
	}
}

func TestTUIRootDoctorShellShowsSetupFailure(t *testing.T) {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenDoctor
	root.doctorModel = newTUIDoctorModel(&AppContext{})
	root.doctorModel.phase = tuiDoctorPhaseComplete
	root.doctorModel.setupErr = fmt.Errorf("config file does not exist: /tmp/missing.yaml")

	view := root.View()
	if !strings.Contains(view, "FAILED") || !strings.Contains(view, "setup failed") || !strings.Contains(view, "/tmp/missing.yaml") {
		t.Fatalf("expected setup failure rendering in doctor shell, got: %s", view)
	}
}

func TestTUIRootValidateShellShowsExplicitConfigContext(t *testing.T) {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenValidate
	root.validateModel = newTUIValidateModel(&AppContext{})
	root.validateModel.phase = tuiValidatePhaseComplete
	root.validateModel.result = tuiValidateResultState{
		Valid:              true,
		ConfigLoaded:       true,
		ConfigContextLabel: "/tmp/explicit-config.yaml",
		SourceCount:        2,
		EnabledSourceCount: 1,
		DetailLines:        []string{"Config schema and source definitions passed validation."},
	}

	view := root.View()
	if !strings.Contains(view, "VALID") || !strings.Contains(view, "Config valid") {
		t.Fatalf("expected valid badge and status, got: %s", view)
	}
	if !strings.Contains(view, "/tmp/explicit-config.yaml") || !strings.Contains(view, "Sources: 2 total") || !strings.Contains(view, "Enabled: 1") {
		t.Fatalf("expected explicit config context and counts, got: %s", view)
	}
}

func TestTUIRootValidateShellShowsDefaultSearchContextOnFailure(t *testing.T) {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenValidate
	root.width = 100
	root.validateModel = newTUIValidateModel(&AppContext{})
	root.validateModel.phase = tuiValidatePhaseComplete
	root.validateModel.result = tuiValidateResultState{
		FailureKind:        tuiValidateFailureLoad,
		ConfigContextLabel: "/home/test/.config/udl/config.yaml + /repo/udl.yaml",
		DetailLines:        []string{"parse config file /repo/udl.yaml: yaml: line 4: did not find expected key"},
	}

	view := root.View()
	if !strings.Contains(view, "INVALID") || !strings.Contains(view, "Config load failed") {
		t.Fatalf("expected invalid load failure status, got: %s", view)
	}
	if !strings.Contains(view, "/home/test/.config/udl/config.yaml + /repo/udl.yaml") {
		t.Fatalf("expected default search-path context, got: %s", view)
	}
	if !strings.Contains(view, "did not find expected key") {
		t.Fatalf("expected validation details in shell, got: %s", view)
	}
}

func TestTUIInitWorkflowStartsAtIntroAndEnterStartsRun(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "udl.yaml")
	root := newTUIRootModel(&AppContext{
		Opts: GlobalOptions{ConfigPath: configPath},
	}, false)
	root.screen = tuiScreenInit
	root.initModel = newTUIInitModel(root.app)

	if root.initModel.phase != tuiInitPhaseIntro {
		t.Fatalf("expected init intro phase, got %q", root.initModel.phase)
	}
	if root.initModel.eventCh != nil {
		t.Fatalf("expected intro phase not to start run channel")
	}
	view := root.View()
	if !strings.Contains(view, "Actions") || !strings.Contains(view, "enter: start init") {
		t.Fatalf("expected guided init intro view, got: %s", view)
	}

	nextModel, cmd := root.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next := nextModel.(tuiRootModel)
	if cmd == nil {
		t.Fatalf("expected enter in init intro to start run")
	}
	if next.initModel.phase != tuiInitPhaseRunning {
		t.Fatalf("expected init to enter running phase, got %q", next.initModel.phase)
	}
	if next.canReturnToMenuOnEsc() {
		t.Fatalf("expected esc to be disabled while init is running")
	}
}

func TestTUIRootInitEscBehaviorFollowsPhaseAndPromptState(t *testing.T) {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenInit
	root.initModel = newTUIInitModel(&AppContext{})

	nextModel, _ := root.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next := nextModel.(tuiRootModel)
	if next.screen != tuiScreenMenu {
		t.Fatalf("expected intro esc to return to menu, got screen %v", next.screen)
	}

	root.screen = tuiScreenInit
	root.initModel = newTUIInitModel(&AppContext{})
	root.initModel.phase = tuiInitPhaseRunning
	nextModel, _ = root.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next = nextModel.(tuiRootModel)
	if next.screen != tuiScreenInit {
		t.Fatalf("expected running init to stay on init screen, got %v", next.screen)
	}

	root.initModel.phase = tuiInitPhaseDone
	root.initModel.prompt = &tuiInteractionPromptState{
		kind:   tuiPromptKindConfirm,
		prompt: "Overwrite?",
		reply:  make(chan tuiPromptResult, 1),
	}
	nextModel, _ = root.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next = nextModel.(tuiRootModel)
	if next.screen != tuiScreenInit {
		t.Fatalf("expected active prompt to block esc navigation, got %v", next.screen)
	}

	root.initModel.prompt = nil
	nextModel, _ = root.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next = nextModel.(tuiRootModel)
	if next.screen != tuiScreenMenu {
		t.Fatalf("expected completed init esc to return to menu, got %v", next.screen)
	}
}

func TestTUIRootInitResultStatesRenderExpectedSummaries(t *testing.T) {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenInit
	root.width = 120
	root.initModel = newTUIInitModel(&AppContext{})
	root.initModel.result = tuiInitResultState{
		ConfigPath: "/tmp/udl.yaml",
		StateDir:   "/tmp/.udl-state",
	}

	root.initModel.phase = tuiInitPhaseDone
	root.initModel.result.DetailLines = []string{"Starter config written.", "State directory ensured."}
	view := root.View()
	if !strings.Contains(view, "Initialization complete.") || !strings.Contains(view, "udl validate") {
		t.Fatalf("expected success summary and next step, got: %s", view)
	}

	root.initModel.phase = tuiInitPhaseCanceled
	root.initModel.result.DetailLines = []string{"Initialization canceled before writing a new config."}
	view = root.View()
	if !strings.Contains(view, "Initialization canceled.") {
		t.Fatalf("expected canceled summary, got: %s", view)
	}

	root.initModel.phase = tuiInitPhaseFailed
	root.initModel.result.DetailLines = []string{"write config file: permission denied"}
	view = root.View()
	if !strings.Contains(view, "Initialization failed.") || !strings.Contains(view, "permission denied") {
		t.Fatalf("expected failure summary, got: %s", view)
	}
}

func TestTUISyncModelPlanPromptConfirmFlow(t *testing.T) {
	m := newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	m.cfgLoaded = true
	m.running = true
	m.interactivePhase = tuiInteractivePhaseReview
	m.eventCh = make(chan tea.Msg, 1)
	reply := make(chan tuiPlanSelectResult, 1)
	rows := []engine.PlanRow{
		{Index: 1, Title: "first", RemoteID: "a", Status: engine.PlanRowMissingNew, Toggleable: true, SelectedByDefault: true},
		{Index: 2, Title: "second", RemoteID: "b", Status: engine.PlanRowAlreadyDownloaded, Toggleable: false},
		{Index: 3, Title: "third", RemoteID: "c", Status: engine.PlanRowMissingKnownGap, Toggleable: true, SelectedByDefault: false},
	}

	m, _ = m.Update(tuiPlanSelectRequestMsg{
		SourceID: "source-a",
		Rows:     rows,
		Details:  planSourceDetails{SourceID: "source-a"},
		Reply:    reply,
	})
	if m.planPrompt == nil {
		t.Fatalf("expected plan prompt state")
	}

	// Move to row 3 and include it in the selection.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.planPrompt != nil {
		t.Fatalf("expected plan prompt to close after confirm")
	}
	if m.interactivePhase != tuiInteractivePhaseSyncing {
		t.Fatalf("expected confirm to start actual sync phase, got %q", m.interactivePhase)
	}
	if m.interactiveTracker == nil || m.interactiveTracker.startedAt.IsZero() {
		t.Fatalf("expected confirm to start elapsed timing for actual sync")
	}
	select {
	case got := <-reply:
		if got.Canceled {
			t.Fatalf("expected confirmed selection, got canceled")
		}
		want := []int{1, 3}
		if !reflect.DeepEqual(got.Manifest.SelectedIndices, want) {
			t.Fatalf("selected indices mismatch: got=%v want=%v", got.Manifest.SelectedIndices, want)
		}
	default:
		t.Fatalf("expected selection reply")
	}
}

func TestTUISyncModelInitialEnterStartsPreflightWithoutRuntimeTimer(t *testing.T) {
	m := newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	m.cfgLoaded = true
	m.sources = []config.Source{{ID: "soundcloud-likes", Type: config.SourceTypeSoundCloud, Adapter: config.AdapterSpec{Kind: "scdl"}}}
	m.selected["soundcloud-likes"] = true

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.running {
		t.Fatalf("expected interactive workflow goroutine to start on enter")
	}
	if m.interactivePhase != tuiInteractivePhasePreflight {
		t.Fatalf("expected initial enter to start preflight phase, got %q", m.interactivePhase)
	}
	if m.interactiveTracker != nil && !m.interactiveTracker.startedAt.IsZero() {
		t.Fatalf("expected elapsed timer to remain unset until final confirmation")
	}
}

func TestTUISyncModelInitValidatesConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "udl.yaml")
	configPayload := strings.Join([]string{
		"version: 1",
		"defaults:",
		"  state_dir: ./relative-state",
		"  archive_file: archive.txt",
		"  threads: 1",
		"  command_timeout_seconds: 60",
		"sources:",
		"  - id: sc",
		"    type: soundcloud",
		"    enabled: true",
		"    target_dir: /tmp",
		"    url: https://soundcloud.com/user",
		"    state_file: sc.sync.scdl",
		"    adapter:",
		"      kind: scdl",
	}, "\n") + "\n"
	if err := os.WriteFile(configPath, []byte(configPayload), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	m := newTUISyncModel(&AppContext{
		Opts: GlobalOptions{ConfigPath: configPath},
	}, tuiSyncWorkflowStandard)
	raw := m.Init()()
	msg, ok := raw.(tuiConfigLoadedMsg)
	if !ok {
		t.Fatalf("expected tuiConfigLoadedMsg, got %T", raw)
	}
	if msg.err == nil {
		t.Fatalf("expected config validation error")
	}
	if !strings.Contains(msg.err.Error(), "defaults.state_dir must resolve to an absolute path") {
		t.Fatalf("expected validation error for state_dir, got %v", msg.err)
	}
}

func TestTUIRootEscDuringPlanPromptCancelsPlanInsteadOfLeavingScreen(t *testing.T) {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenInteractiveSync
	root.syncModel = newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	root.syncModel.cfgLoaded = true
	root.syncModel.running = true
	reply := make(chan tuiPlanSelectResult, 1)
	root.syncModel.planPrompt = newTUIPlanPromptState(tuiPlanSelectRequestMsg{
		SourceID: "source-a",
		Rows: []engine.PlanRow{
			{Index: 1, Toggleable: true, SelectedByDefault: true},
		},
		Reply: reply,
	})

	nextModel, _ := root.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next, ok := nextModel.(tuiRootModel)
	if !ok {
		t.Fatalf("unexpected model type %T", nextModel)
	}
	if next.screen != tuiScreenInteractiveSync {
		t.Fatalf("expected to remain on sync screen, got %v", next.screen)
	}
	select {
	case result := <-reply:
		if !result.Canceled {
			t.Fatalf("expected canceled=true when esc is pressed in plan prompt")
		}
	default:
		t.Fatalf("expected plan prompt cancellation reply")
	}
}

func TestTUISyncInteractionSelectRowsUsesTUIHandshake(t *testing.T) {
	ch := make(chan tea.Msg, 1)
	interaction := &tuiSyncInteraction{
		ch: ch,
		sourceByID: map[string]config.Source{
			"s1": {ID: "s1", Type: config.SourceTypeSoundCloud, Adapter: config.AdapterSpec{Kind: "scdl"}},
		},
		planLimit: 10,
	}
	rows := []engine.PlanRow{
		{Index: 1, Toggleable: true, SelectedByDefault: true},
	}

	done := make(chan struct{})
	var result engine.PlanSelectionResult
	var gotErr error
	go func() {
		result, gotErr = interaction.SelectRows("s1", rows)
		close(done)
	}()

	raw := <-ch
	req, ok := raw.(tuiPlanSelectRequestMsg)
	if !ok {
		t.Fatalf("expected tuiPlanSelectRequestMsg, got %T", raw)
	}
	manifest, err := engine.BuildExecutionManifest("s1", rows, []int{1}, engine.DownloadOrderNewestFirst)
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	req.Reply <- tuiPlanSelectResult{Manifest: manifest}
	<-done

	if gotErr != nil {
		t.Fatalf("unexpected select error: %v", gotErr)
	}
	if result.Canceled {
		t.Fatalf("expected canceled=false")
	}
	if !reflect.DeepEqual(result.Manifest.SelectedIndices, []int{1}) {
		t.Fatalf("selected indices mismatch: got=%v", result.Manifest.SelectedIndices)
	}
	if result.Manifest.DownloadOrder != engine.DownloadOrderNewestFirst {
		t.Fatalf("expected default handshake order newest_first, got %q", result.Manifest.DownloadOrder)
	}
}

func TestTUISyncModelPlanLimitControls(t *testing.T) {
	m := newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	m.cfgLoaded = true

	if m.planLimit != tuiDefaultPlanLimit {
		t.Fatalf("expected default plan limit %d, got %d", tuiDefaultPlanLimit, m.planLimit)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	if m.planLimit != tuiDefaultPlanLimit+1 {
		t.Fatalf("expected incremented plan limit, got %d", m.planLimit)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")})
	if m.planLimit != tuiDefaultPlanLimit {
		t.Fatalf("expected decremented plan limit, got %d", m.planLimit)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	if m.planLimit != 0 {
		t.Fatalf("expected unlimited plan limit (0), got %d", m.planLimit)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")})
	if m.planLimit != tuiDefaultPlanLimit {
		t.Fatalf("expected reset to default when decreasing from unlimited, got %d", m.planLimit)
	}
}

func TestTUISyncModelInteractiveDownloadOrderToggle(t *testing.T) {
	m := newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	m.cfgLoaded = true
	m.sources = []config.Source{
		{
			ID:        "soundcloud-likes",
			Type:      config.SourceTypeSoundCloud,
			TargetDir: "/tmp/music",
			URL:       "https://soundcloud.com/janxadam",
			StateFile: "/tmp/soundcloud.sync.scdl",
			Adapter:   config.AdapterSpec{Kind: "scdl"},
		},
	}
	m.selected["soundcloud-likes"] = true
	m.interactiveOrders["soundcloud-likes"] = engine.DownloadOrderOldestFirst

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if got := m.interactiveOrders["soundcloud-likes"]; got != engine.DownloadOrderNewestFirst {
		t.Fatalf("expected download order to toggle to newest_first, got %q", got)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if got := m.interactiveOrders["soundcloud-likes"]; got != engine.DownloadOrderOldestFirst {
		t.Fatalf("expected download order to toggle back to oldest_first, got %q", got)
	}
}

func TestTUISyncModelPlanPromptDownloadOrderToggleRebuildsManifest(t *testing.T) {
	m := newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	m.cfgLoaded = true
	source := config.Source{
		ID:        "soundcloud-likes",
		Type:      config.SourceTypeSoundCloud,
		TargetDir: "/tmp/music",
		URL:       "https://soundcloud.com/janxadam",
		StateFile: "/tmp/soundcloud.sync.scdl",
		Adapter:   config.AdapterSpec{Kind: "scdl"},
	}
	m.sources = []config.Source{source}
	m.selected["soundcloud-likes"] = true
	m.interactiveOrders["soundcloud-likes"] = engine.DownloadOrderOldestFirst
	reply := make(chan tuiPlanSelectResult, 1)
	state := newTUIInteractiveSelectionState(tuiPlanSelectRequestMsg{
		SourceID:      source.ID,
		Rows:          []engine.PlanRow{{Index: 1, Toggleable: true, SelectedByDefault: true}},
		Details:       m.planSourceDetailsForSource(source),
		DownloadOrder: engine.DownloadOrderOldestFirst,
		Reply:         reply,
	})
	m.planPrompt = &tuiPlanPromptState{
		tuiInteractiveSelectionState: state,
		reply:                        reply,
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if got := m.interactiveOrders["soundcloud-likes"]; got != engine.DownloadOrderNewestFirst {
		t.Fatalf("expected source order to toggle to newest_first, got %q", got)
	}
	if got := m.planPrompt.manifest.DownloadOrder; got != engine.DownloadOrderNewestFirst {
		t.Fatalf("expected prompt manifest order to update, got %q", got)
	}
}

func TestTUISyncModelPlanLimitTypedEntry(t *testing.T) {
	m := newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	m.cfgLoaded = true
	m.planLimit = 10

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if !m.planLimitInputActive {
		t.Fatalf("expected typed-input mode to be active")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.planLimit != 25 {
		t.Fatalf("expected typed plan limit 25, got %d", m.planLimit)
	}
	if m.planLimitInputActive {
		t.Fatalf("expected typed-input mode to close after apply")
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("0")})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.planLimit != 0 {
		t.Fatalf("expected typed plan limit 0 (unlimited), got %d", m.planLimit)
	}
}

func TestTUISyncModelPlanLimitTypedEntryEscCancels(t *testing.T) {
	m := newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	m.cfgLoaded = true
	m.planLimit = 17

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if m.planLimit != 17 {
		t.Fatalf("expected plan limit unchanged on cancel, got %d", m.planLimit)
	}
	if m.planLimitInputActive {
		t.Fatalf("expected typed-input mode to close on esc")
	}
}

func TestTUIRootEscDuringPlanLimitInputDoesNotLeaveSync(t *testing.T) {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenInteractiveSync
	root.syncModel = newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	root.syncModel.cfgLoaded = true
	root.syncModel.planLimitInputActive = true
	root.syncModel.planLimit = 10

	nextModel, _ := root.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next, ok := nextModel.(tuiRootModel)
	if !ok {
		t.Fatalf("unexpected model type %T", nextModel)
	}
	if next.screen != tuiScreenInteractiveSync {
		t.Fatalf("expected to remain on sync screen, got %v", next.screen)
	}
	if next.syncModel.planLimitInputActive {
		t.Fatalf("expected esc to cancel typed-input mode")
	}
}

func TestTUISyncInteractionConfirmUsesPromptHandshake(t *testing.T) {
	ch := make(chan tea.Msg, 1)
	interaction := &tuiSyncInteraction{ch: ch}

	done := make(chan struct{})
	var confirmed bool
	var err error
	go func() {
		confirmed, err = interaction.Confirm("[source-a] Continue?", true)
		close(done)
	}()

	raw := <-ch
	req, ok := raw.(tuiPromptRequestMsg)
	if !ok {
		t.Fatalf("expected tuiPromptRequestMsg, got %T", raw)
	}
	if req.Kind != tuiPromptKindConfirm {
		t.Fatalf("expected confirm prompt kind, got %q", req.Kind)
	}
	req.Reply <- tuiPromptResult{Confirmed: true}
	<-done

	if err != nil {
		t.Fatalf("unexpected confirm error: %v", err)
	}
	if !confirmed {
		t.Fatalf("expected confirmed=true")
	}
}

func TestTUISyncInteractionInputMasksARLPrompt(t *testing.T) {
	ch := make(chan tea.Msg, 1)
	interaction := &tuiSyncInteraction{ch: ch}

	done := make(chan struct{})
	var value string
	var err error
	go func() {
		value, err = interaction.Input("[source-a] Enter your Deezer ARL for deemix")
		close(done)
	}()

	raw := <-ch
	req, ok := raw.(tuiPromptRequestMsg)
	if !ok {
		t.Fatalf("expected tuiPromptRequestMsg, got %T", raw)
	}
	if req.Kind != tuiPromptKindInput {
		t.Fatalf("expected input prompt kind, got %q", req.Kind)
	}
	if !req.MaskInput {
		t.Fatalf("expected ARL prompt to be masked")
	}
	req.Reply <- tuiPromptResult{Input: "abc123"}
	<-done

	if err != nil {
		t.Fatalf("unexpected input error: %v", err)
	}
	if value != "abc123" {
		t.Fatalf("unexpected input value %q", value)
	}
}

func TestTUISyncModelEnterValidatesIncompatiblePlanFlags(t *testing.T) {
	m := newTUISyncModel(&AppContext{}, tuiSyncWorkflowInteractive)
	m.cfgLoaded = true
	m.scanGaps = true

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.running {
		t.Fatalf("expected run not to start on invalid options")
	}
	if !strings.Contains(m.validationErr, "scan-gaps") {
		t.Fatalf("expected validation error mentioning scan-gaps, got %q", m.validationErr)
	}
}

func TestTUISyncModelCancelKeyCancelsActiveRunAndPrompt(t *testing.T) {
	cancelCalled := false
	reply := make(chan tuiPromptResult, 1)
	m := newTUISyncModel(&AppContext{}, tuiSyncWorkflowStandard)
	m.cfgLoaded = true
	m.running = true
	m.eventCh = make(chan tea.Msg, 1)
	m.runCancel = func() { cancelCalled = true }
	m.interactionPrompt = &tuiInteractionPromptState{
		kind:  tuiPromptKindConfirm,
		reply: reply,
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if !m.cancelRequested {
		t.Fatalf("expected cancel requested state")
	}
	if !cancelCalled {
		t.Fatalf("expected run cancel callback to be called")
	}
	if m.interactionPrompt != nil {
		t.Fatalf("expected active prompt to be cleared on cancel")
	}
	select {
	case result := <-reply:
		if !result.Canceled {
			t.Fatalf("expected prompt cancellation result")
		}
	default:
		t.Fatalf("expected cancellation reply for active prompt")
	}
}

func TestTUISyncModelRendersCompactProgressAndHistory(t *testing.T) {
	m := newTUISyncModel(&AppContext{}, tuiSyncWorkflowStandard)
	m.cfgLoaded = true
	m.running = true

	events := []output.Event{
		{
			Event:    output.EventSourcePreflight,
			SourceID: "soundcloud-likes",
			Details: map[string]any{
				"planned_download_count": 1,
			},
			Message: "[soundcloud-likes] preflight",
		},
		{
			Event:    output.EventTrackStarted,
			SourceID: "soundcloud-likes",
			Details: map[string]any{
				"track_name": "Structured Song",
				"index":      1,
				"total":      4,
			},
		},
		{
			Event:    output.EventTrackProgress,
			SourceID: "soundcloud-likes",
			Details: map[string]any{
				"track_name": "Structured Song",
				"index":      1,
				"total":      4,
				"percent":    67.5,
			},
		},
	}

	for _, event := range events {
		var cmd tea.Cmd
		m, cmd = m.Update(tuiSyncEventMsg{Event: event})
		if cmd != nil {
			t.Fatalf("expected no wait command without event channel")
		}
	}

	view := m.View()
	if !strings.Contains(view, "Structured Song") {
		t.Fatalf("expected active track in view, got: %s", view)
	}
	if !strings.Contains(view, "67.5%") || !strings.Contains(view, "[overall]") {
		t.Fatalf("expected compact progress lines in view, got: %s", view)
	}
	if !strings.Contains(view, "(1/1)") {
		t.Fatalf("expected overall line to use planned total 1, got: %s", view)
	}

	m, _ = m.Update(tuiSyncEventMsg{Event: output.Event{
		Event:    output.EventTrackDone,
		SourceID: "soundcloud-likes",
		Details: map[string]any{
			"track_name": "Structured Song",
			"index":      1,
			"total":      4,
		},
	}})

	view = m.View()
	if !strings.Contains(view, "[done] Structured Song") {
		t.Fatalf("expected compact outcome history, got: %s", view)
	}
	if !strings.Contains(view, "all planned tracks complete (1/1)") {
		t.Fatalf("expected idle completion line after done, got: %s", view)
	}
}

func TestTUISyncModelSuppressesTrackProgressSpamInActivity(t *testing.T) {
	m := newTUISyncModel(&AppContext{}, tuiSyncWorkflowStandard)
	m.cfgLoaded = true

	progressEvent := output.Event{
		Event:    output.EventTrackProgress,
		SourceID: "soundcloud-likes",
		Message:  "[soundcloud-likes] [track_progress] Structured Song",
		Details: map[string]any{
			"track_name": "Structured Song",
			"index":      1,
			"total":      1,
			"percent":    50.0,
		},
	}
	for i := 0; i < 3; i++ {
		var cmd tea.Cmd
		m, cmd = m.Update(tuiSyncEventMsg{Event: progressEvent})
		if cmd != nil {
			t.Fatalf("expected no wait command without event channel")
		}
	}

	m, _ = m.Update(tuiSyncEventMsg{Event: output.Event{
		Event:    output.EventTrackDone,
		SourceID: "soundcloud-likes",
		Details: map[string]any{
			"track_name": "Structured Song",
			"index":      1,
			"total":      1,
		},
	}})
	m, _ = m.Update(tuiSyncEventMsg{Event: output.Event{
		Event:   output.EventSyncFinished,
		Message: "sync finished: attempted=1 succeeded=1 failed=0 skipped=0",
	}})

	view := m.View()
	if strings.Contains(view, "[track_progress]") {
		t.Fatalf("expected track progress chatter to be suppressed, got: %s", view)
	}
	if !strings.Contains(view, "[done] Structured Song") {
		t.Fatalf("expected compact outcome line in activity, got: %s", view)
	}
	if !strings.Contains(view, "sync finished: attempted=1 succeeded=1 failed=0 skipped=0") {
		t.Fatalf("expected sync summary in activity, got: %s", view)
	}
}

func TestTUISyncModelShowsPinnedLastFailureDiagnostics(t *testing.T) {
	m := newTUISyncModel(&AppContext{}, tuiSyncWorkflowStandard)
	m.cfgLoaded = true
	m.running = true

	m, _ = m.Update(tuiSyncEventMsg{Event: output.Event{
		Event:    output.EventSourceFailed,
		Level:    output.LevelError,
		SourceID: "spotify-source",
		Message:  "[spotify-source] command failed with exit code 1",
		Details: map[string]any{
			"failure_message":  "[spotify-source] command failed with exit code 1",
			"exit_code":        1,
			"timed_out":        true,
			"stdout_tail":      "line one\nline two",
			"stderr_tail":      "fatal line",
			"failure_log_path": "/tmp/udl-state/sync-failures.jsonl",
		},
	}})

	view := m.View()
	if !strings.Contains(view, "Last Failure:") {
		t.Fatalf("expected pinned last failure section, got: %s", view)
	}
	if !strings.Contains(view, "[spotify-source] command failed with exit code 1") {
		t.Fatalf("expected failure source/message, got: %s", view)
	}
	if !strings.Contains(view, "exit_code=1") || !strings.Contains(view, "timed_out=true") {
		t.Fatalf("expected failure status details, got: %s", view)
	}
	if !strings.Contains(view, "stdout_tail:") || !strings.Contains(view, "line one") || !strings.Contains(view, "line two") {
		t.Fatalf("expected stdout tail excerpt, got: %s", view)
	}
	if !strings.Contains(view, "stderr_tail:") || !strings.Contains(view, "fatal line") {
		t.Fatalf("expected stderr tail excerpt, got: %s", view)
	}
	if !strings.Contains(view, "/tmp/udl-state/sync-failures.jsonl") {
		t.Fatalf("expected failure log path, got: %s", view)
	}
}

func TestTUIInitStartRunRequestsOverwriteConfirmAndCanCancel(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "udl.yaml")
	if err := os.WriteFile(configPath, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	model := newTUIInitModel(&AppContext{
		Opts: GlobalOptions{ConfigPath: configPath},
	})
	model.eventCh = make(chan tea.Msg, 8)
	runCmd := model.startRunCmd()

	done := make(chan struct{})
	go func() {
		_ = runCmd()
		close(done)
	}()

	raw := <-model.eventCh
	req, ok := raw.(tuiPromptRequestMsg)
	if !ok {
		t.Fatalf("expected init confirm prompt request, got %T", raw)
	}
	if req.Kind != tuiPromptKindConfirm {
		t.Fatalf("expected confirm kind, got %q", req.Kind)
	}
	req.Reply <- tuiPromptResult{Canceled: true}

	finished := <-model.eventCh
	doneMsg, ok := finished.(tuiInitDoneMsg)
	if !ok {
		t.Fatalf("expected init done message, got %T", finished)
	}
	if !doneMsg.Canceled {
		t.Fatalf("expected canceled init result, got %+v", doneMsg)
	}
	<-done
}

func TestReadmeIncludesTUIAndGuideLink(t *testing.T) {
	readmePath := filepath.Join("..", "..", "readme.md")
	docsPath := filepath.Join("..", "..", "docs", "tui.md")

	content, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read readme: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "\n  tui\n") {
		t.Fatalf("expected command surface to include tui")
	}
	if !strings.Contains(text, "docs/tui.md") {
		t.Fatalf("expected readme to link docs/tui.md")
	}
	if _, err := os.Stat(docsPath); err != nil {
		t.Fatalf("expected docs/tui.md to exist: %v", err)
	}
}
