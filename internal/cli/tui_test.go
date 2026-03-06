package cli

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
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
