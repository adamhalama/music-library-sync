package app

import "github.com/jaa/update-downloads/internal/engine"

type Interaction interface {
	Confirm(prompt string, defaultYes bool) (bool, error)
	Input(prompt string) (string, error)
	SelectRows(sourceID string, rows []engine.PlanRow) ([]int, bool, error)
}

type NoopInteraction struct{}

func (NoopInteraction) Confirm(prompt string, defaultYes bool) (bool, error) {
	return defaultYes, nil
}

func (NoopInteraction) Input(prompt string) (string, error) {
	return "", nil
}

func (NoopInteraction) SelectRows(sourceID string, rows []engine.PlanRow) ([]int, bool, error) {
	selected := make([]int, 0, len(rows))
	for _, row := range rows {
		if row.Toggleable && row.SelectedByDefault {
			selected = append(selected, row.Index)
		}
	}
	return selected, false, nil
}
