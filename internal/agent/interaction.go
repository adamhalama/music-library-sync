package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/runstate"
)

type Interaction struct {
	Conn            *Conn
	Runs            *RunRegistry
	RunID           string
	SourceDetails   func(string) app.PlanSourceDetails
	DownloadOrder   func(string) engine.DownloadOrder
	PlanWindow      func(string) engine.PlanWindow
	SetPlanWindow   func(string, engine.PlanWindow)
	Tracker         *runstate.Tracker
	nextUIRequestID atomic.Uint64
}

type confirmParams struct {
	RunID      string `json:"run_id"`
	SourceID   string `json:"source_id,omitempty"`
	Prompt     string `json:"prompt"`
	DefaultYes bool   `json:"default_yes"`
}

type confirmResult struct {
	Confirmed bool `json:"confirmed"`
	Canceled  bool `json:"canceled"`
}

type inputParams struct {
	RunID    string `json:"run_id"`
	SourceID string `json:"source_id,omitempty"`
	Prompt   string `json:"prompt"`
	Mask     bool   `json:"mask"`
}

type inputResult struct {
	Value    string `json:"value"`
	Canceled bool   `json:"canceled"`
}

type selectRowsParams struct {
	RunID         string                `json:"run_id"`
	SourceID      string                `json:"source_id"`
	Rows          []engine.PlanRow      `json:"rows"`
	Details       app.PlanSourceDetails `json:"details"`
	DownloadOrder engine.DownloadOrder  `json:"download_order"`
	PlanWindow    engine.PlanWindow     `json:"plan_window"`
}

type selectRowsResult struct {
	SelectedIndices []int                `json:"selected_indices"`
	DownloadOrder   engine.DownloadOrder `json:"download_order"`
	Canceled        bool                 `json:"canceled"`
	Rebuild         bool                 `json:"rebuild"`
	PlanWindow      engine.PlanWindow    `json:"plan_window"`
}

func (i *Interaction) Confirm(prompt string, defaultYes bool) (bool, error) {
	var result confirmResult
	err := i.callUI("ui.confirm", confirmParams{
		RunID: i.RunID, SourceID: sourceIDFromPrompt(prompt),
		Prompt: prompt, DefaultYes: defaultYes,
	}, &result)
	if err != nil {
		return false, err
	}
	if result.Canceled {
		return false, context.Canceled
	}
	return result.Confirmed, nil
}

func (i *Interaction) Input(prompt string) (string, error) {
	var result inputResult
	err := i.callUI("ui.input", inputParams{
		RunID: i.RunID, SourceID: sourceIDFromPrompt(prompt),
		Prompt: prompt, Mask: true,
	}, &result)
	if err != nil {
		return "", err
	}
	if result.Canceled {
		return "", context.Canceled
	}
	return result.Value, nil
}

func (i *Interaction) SelectRows(sourceID string, rows []engine.PlanRow) (engine.PlanSelectionResult, error) {
	order := engine.DefaultDownloadOrder
	if i.DownloadOrder != nil {
		order = engine.NormalizeDownloadOrder(i.DownloadOrder(sourceID))
	}
	window := engine.PlanWindowFirst
	if i.PlanWindow != nil {
		window = engine.NormalizePlanWindow(i.PlanWindow(sourceID))
	}
	details := app.PlanSourceDetails{SourceID: sourceID}
	if i.SourceDetails != nil {
		details = i.SourceDetails(sourceID)
	}
	var result selectRowsResult
	err := i.callUI("ui.selectRows", selectRowsParams{
		RunID: i.RunID, SourceID: sourceID, Rows: rows, Details: details,
		DownloadOrder: order, PlanWindow: window,
	}, &result)
	if err != nil {
		return engine.PlanSelectionResult{}, err
	}
	if result.Canceled {
		return engine.PlanSelectionResult{Canceled: true}, nil
	}
	resultOrder := engine.NormalizeDownloadOrder(result.DownloadOrder)
	if result.DownloadOrder == "" {
		resultOrder = order
	}
	resultWindow := engine.NormalizePlanWindow(result.PlanWindow)
	if result.PlanWindow == "" {
		resultWindow = window
	}
	if result.Rebuild {
		if i.SetPlanWindow != nil {
			i.SetPlanWindow(sourceID, resultWindow)
		}
		return engine.PlanSelectionResult{Rebuild: true, Window: resultWindow}, nil
	}
	manifest, buildErr := engine.BuildExecutionManifest(sourceID, rows, result.SelectedIndices, resultOrder)
	if buildErr != nil {
		return engine.PlanSelectionResult{}, fmt.Errorf("validate ui.selectRows result: %w", buildErr)
	}
	if i.Tracker != nil {
		selected := make(map[int]bool, len(manifest.SelectedIndices))
		for _, index := range manifest.SelectedIndices {
			selected[index] = true
		}
		trackedRows := make([]runstate.PlanTrackRow, 0, len(rows))
		for _, row := range rows {
			trackedRows = append(trackedRows, runstate.PlanTrackRow{
				SourceID: sourceID, SourceLabel: sourceID,
				RemoteID: row.RemoteID, Title: row.Title, Index: row.Index,
				Toggleable: row.Toggleable, PlanStatus: row.Status,
				PlanClass:         runstate.TrackPlanClassFromPlanStatus(row.Status),
				SelectedByDefault: row.SelectedByDefault,
			})
		}
		i.Tracker.ConfirmSelection(sourceID, trackedRows, manifest, func(index int) bool { return selected[index] })
	}
	return engine.PlanSelectionResult{Manifest: manifest, Window: resultWindow}, nil
}

func (i *Interaction) callUI(method string, params, result any) error {
	if i == nil || i.Conn == nil || i.Runs == nil || strings.TrimSpace(i.RunID) == "" {
		return fmt.Errorf("agent interaction is not initialized")
	}
	if _, ok := i.Runs.Context(i.RunID); !ok {
		return NewRPCError(CodeRunNotFound, "run not found", map[string]string{"run_id": i.RunID})
	}
	requestID := fmt.Sprintf("%s-ui-%d", i.RunID, i.nextUIRequestID.Add(1))
	callCtx, cancelCall := context.WithCancel(context.Background())
	if err := i.Runs.SetPendingUI(i.RunID, requestID, cancelCall); err != nil {
		cancelCall()
		return err
	}
	defer func() {
		i.Runs.ClearPendingUI(i.RunID, requestID)
		cancelCall()
	}()
	err := i.Conn.Call(callCtx, method, params, result)
	if err == context.Canceled {
		return context.Canceled
	}
	return err
}

func sourceIDFromPrompt(prompt string) string {
	trimmed := strings.TrimSpace(prompt)
	if !strings.HasPrefix(trimmed, "[") {
		return ""
	}
	end := strings.IndexByte(trimmed, ']')
	if end <= 1 {
		return ""
	}
	return strings.TrimSpace(trimmed[1:end])
}

// Assert the transport adapter continues to satisfy the application contract.
var _ app.Interaction = (*Interaction)(nil)

func decodeParams[T any](params json.RawMessage) (T, error) {
	var value T
	err := json.Unmarshal(params, &value)
	return value, err
}
