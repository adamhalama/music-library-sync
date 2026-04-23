package app

import "github.com/jaa/update-downloads/internal/engine"

type Interaction interface {
	Confirm(prompt string, defaultYes bool) (bool, error)
	Input(prompt string) (string, error)
	SelectRows(sourceID string, rows []engine.PlanRow) (engine.PlanSelectionResult, error)
}

type NoopInteraction struct{}

func (NoopInteraction) Confirm(prompt string, defaultYes bool) (bool, error) {
	return defaultYes, nil
}

func (NoopInteraction) Input(prompt string) (string, error) {
	return "", nil
}

func (NoopInteraction) SelectRows(sourceID string, rows []engine.PlanRow) (engine.PlanSelectionResult, error) {
	manifest, err := engine.BuildExecutionManifest(sourceID, rows, engine.DefaultSelectedPlanIndices(rows), engine.DownloadOrderNewestFirst)
	if err != nil {
		return engine.PlanSelectionResult{}, err
	}
	return engine.PlanSelectionResult{
		Manifest: manifest,
	}, nil
}
