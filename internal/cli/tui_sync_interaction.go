package cli

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/output"
)

type tuiChannelEmitter struct {
	ch chan tea.Msg
}

func (e *tuiChannelEmitter) Emit(event output.Event) error {
	if e == nil || e.ch == nil {
		return nil
	}
	e.ch <- tuiSyncEventMsg{Event: event}
	return nil
}

type tuiSyncInteraction struct {
	ch         chan tea.Msg
	defaults   config.Defaults
	sourceByID map[string]config.Source
	orderByID  map[string]engine.DownloadOrder
	windowByID map[string]engine.PlanWindow
	planLimit  int
	dryRun     bool
}

func (i *tuiSyncInteraction) Confirm(prompt string, defaultYes bool) (bool, error) {
	if i == nil || i.ch == nil {
		return defaultYes, nil
	}
	reply := make(chan tuiPromptResult, 1)
	i.ch <- tuiPromptRequestMsg{
		Kind:       tuiPromptKindConfirm,
		SourceID:   sourceIDFromPrompt(prompt),
		Prompt:     prompt,
		DefaultYes: defaultYes,
		Reply:      reply,
	}
	result := <-reply
	if result.Err != nil {
		return false, result.Err
	}
	if result.Canceled {
		return false, engine.ErrInterrupted
	}
	return result.Confirmed, nil
}

func (i *tuiSyncInteraction) Input(prompt string) (string, error) {
	if i == nil || i.ch == nil {
		return "", nil
	}
	reply := make(chan tuiPromptResult, 1)
	i.ch <- tuiPromptRequestMsg{
		Kind:      tuiPromptKindInput,
		SourceID:  sourceIDFromPrompt(prompt),
		Prompt:    prompt,
		MaskInput: shouldMaskPromptInput(prompt),
		Reply:     reply,
	}
	result := <-reply
	if result.Err != nil {
		return "", result.Err
	}
	if result.Canceled {
		return "", engine.ErrInterrupted
	}
	return result.Input, nil
}

func (i *tuiSyncInteraction) SelectRows(sourceID string, rows []engine.PlanRow) (engine.PlanSelectionResult, error) {
	if i == nil || i.ch == nil {
		manifest, err := engine.BuildExecutionManifest(sourceID, rows, engine.DefaultSelectedPlanIndices(rows), engine.DefaultDownloadOrder)
		if err != nil {
			return engine.PlanSelectionResult{}, err
		}
		return engine.PlanSelectionResult{
			Manifest: manifest,
		}, nil
	}
	source, ok := i.sourceByID[sourceID]
	if !ok {
		source.ID = sourceID
	}
	planWindow := engine.DefaultPlanWindowForSource(source)
	if window, ok := i.windowByID[sourceID]; ok {
		planWindow = engine.NormalizePlanWindow(window)
	}
	details := buildPlanSourceDetails(source, i.defaults, i.planLimit, planWindow, i.dryRun)
	downloadOrder := engine.DefaultDownloadOrder
	if order, ok := i.orderByID[sourceID]; ok && engine.SupportsDownloadOrder(source) {
		downloadOrder = engine.NormalizeDownloadOrder(order)
	}
	reply := make(chan tuiPlanSelectResult, 1)
	i.ch <- tuiPlanSelectRequestMsg{
		SourceID:      sourceID,
		Rows:          append([]engine.PlanRow{}, rows...),
		Details:       details,
		DownloadOrder: downloadOrder,
		PlanWindow:    planWindow,
		Reply:         reply,
	}
	result := <-reply
	if result.Err != nil {
		return engine.PlanSelectionResult{}, result.Err
	}
	if result.Rebuild {
		if i.windowByID == nil {
			i.windowByID = map[string]engine.PlanWindow{}
		}
		i.windowByID[sourceID] = engine.NormalizePlanWindow(result.Window)
	}
	return engine.PlanSelectionResult{
		Manifest: result.Manifest,
		Canceled: result.Canceled,
		Rebuild:  result.Rebuild,
		Window:   result.Window,
	}, nil
}

func sourceIDFromPrompt(prompt string) string {
	trimmed := strings.TrimSpace(prompt)
	if strings.HasPrefix(trimmed, "[") {
		if end := strings.Index(trimmed, "]"); end > 1 {
			return strings.TrimSpace(trimmed[1:end])
		}
	}
	return "sync"
}

func shouldMaskPromptInput(prompt string) bool {
	lower := strings.ToLower(prompt)
	return strings.Contains(lower, "arl") ||
		strings.Contains(lower, "token") ||
		strings.Contains(lower, "secret") ||
		strings.Contains(lower, "password")
}

type tuiInitInteraction struct {
	ch chan tea.Msg
}

func (i *tuiInitInteraction) Confirm(prompt string, defaultYes bool) (bool, error) {
	if i == nil || i.ch == nil {
		return defaultYes, nil
	}
	reply := make(chan tuiPromptResult, 1)
	i.ch <- tuiPromptRequestMsg{
		Kind:       tuiPromptKindConfirm,
		SourceID:   "init",
		Prompt:     prompt,
		DefaultYes: defaultYes,
		Reply:      reply,
	}
	result := <-reply
	if result.Err != nil {
		return false, result.Err
	}
	if result.Canceled {
		return false, nil
	}
	return result.Confirmed, nil
}

func (i *tuiInitInteraction) Input(prompt string) (string, error) {
	if i == nil || i.ch == nil {
		return "", nil
	}
	reply := make(chan tuiPromptResult, 1)
	i.ch <- tuiPromptRequestMsg{
		Kind:      tuiPromptKindInput,
		SourceID:  "init",
		Prompt:    prompt,
		MaskInput: shouldMaskPromptInput(prompt),
		Reply:     reply,
	}
	result := <-reply
	if result.Err != nil {
		return "", result.Err
	}
	if result.Canceled {
		return "", nil
	}
	return result.Input, nil
}

func (i *tuiInitInteraction) SelectRows(sourceID string, rows []engine.PlanRow) (engine.PlanSelectionResult, error) {
	manifest, err := engine.BuildExecutionManifest(sourceID, rows, engine.DefaultSelectedPlanIndices(rows), engine.DefaultDownloadOrder)
	if err != nil {
		return engine.PlanSelectionResult{}, err
	}
	return engine.PlanSelectionResult{
		Manifest: manifest,
	}, nil
}
