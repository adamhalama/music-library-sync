package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jaa/update-downloads/internal/config"
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
	if !strings.Contains(stdout.String(), "Launch the full-screen TUI shell") {
		t.Fatalf("expected tui help output, got: %s", stdout.String())
	}
}

func TestTUISyncModelPlanPromptConfirmFlow(t *testing.T) {
	m := newTUISyncModel(&AppContext{})
	m.cfgLoaded = true
	m.running = true
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
	select {
	case got := <-reply:
		if got.Canceled {
			t.Fatalf("expected confirmed selection, got canceled")
		}
		want := []int{1, 3}
		if !reflect.DeepEqual(got.SelectedIndices, want) {
			t.Fatalf("selected indices mismatch: got=%v want=%v", got.SelectedIndices, want)
		}
	default:
		t.Fatalf("expected selection reply")
	}
}

func TestTUIRootEscDuringPlanPromptCancelsPlanInsteadOfLeavingScreen(t *testing.T) {
	root := newTUIRootModel(&AppContext{}, false)
	root.screen = tuiScreenSync
	root.syncModel.cfgLoaded = true
	root.syncModel.running = true
	reply := make(chan tuiPlanSelectResult, 1)
	root.syncModel.planPrompt = &tuiPlanPromptState{
		sourceID: "source-a",
		rows: []engine.PlanRow{
			{Index: 1, Toggleable: true, SelectedByDefault: true},
		},
		reply:    reply,
		selected: map[int]bool{1: true},
	}

	nextModel, _ := root.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next, ok := nextModel.(tuiRootModel)
	if !ok {
		t.Fatalf("unexpected model type %T", nextModel)
	}
	if next.screen != tuiScreenSync {
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
	var gotSelected []int
	var gotCanceled bool
	var gotErr error
	go func() {
		gotSelected, gotCanceled, gotErr = interaction.SelectRows("s1", rows)
		close(done)
	}()

	raw := <-ch
	req, ok := raw.(tuiPlanSelectRequestMsg)
	if !ok {
		t.Fatalf("expected tuiPlanSelectRequestMsg, got %T", raw)
	}
	req.Reply <- tuiPlanSelectResult{SelectedIndices: []int{1}}
	<-done

	if gotErr != nil {
		t.Fatalf("unexpected select error: %v", gotErr)
	}
	if gotCanceled {
		t.Fatalf("expected canceled=false")
	}
	if !reflect.DeepEqual(gotSelected, []int{1}) {
		t.Fatalf("selected indices mismatch: got=%v", gotSelected)
	}
}

func TestTUISyncModelPlanLimitControls(t *testing.T) {
	m := newTUISyncModel(&AppContext{})
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

func TestTUISyncModelPlanLimitTypedEntry(t *testing.T) {
	m := newTUISyncModel(&AppContext{})
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
	m := newTUISyncModel(&AppContext{})
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
	root.screen = tuiScreenSync
	root.syncModel.cfgLoaded = true
	root.syncModel.planLimitInputActive = true
	root.syncModel.planLimit = 10

	nextModel, _ := root.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next, ok := nextModel.(tuiRootModel)
	if !ok {
		t.Fatalf("unexpected model type %T", nextModel)
	}
	if next.screen != tuiScreenSync {
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
	m := newTUISyncModel(&AppContext{})
	m.cfgLoaded = true
	m.planEnabled = true
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
	m := newTUISyncModel(&AppContext{})
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
	m := newTUISyncModel(&AppContext{})
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
	m := newTUISyncModel(&AppContext{})
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
	m := newTUISyncModel(&AppContext{})
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
	if !strings.Contains(strings.ToLower(doneMsg.Result), "canceled") {
		t.Fatalf("expected canceled init result, got %q", doneMsg.Result)
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
