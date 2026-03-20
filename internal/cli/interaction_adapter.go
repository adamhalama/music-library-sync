package cli

import (
	"github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
)

type cliInteraction struct {
	app        *AppContext
	defaults   config.Defaults
	sourceByID map[string]config.Source
	planLimit  int
	dryRun     bool
}

func (i cliInteraction) Confirm(prompt string, defaultYes bool) (bool, error) {
	return promptYesNoDefault(i.app, prompt, defaultYes)
}

func (i cliInteraction) Input(prompt string) (string, error) {
	return promptLine(i.app, prompt)
}

func (i cliInteraction) SelectRows(sourceID string, rows []engine.PlanRow) ([]int, bool, error) {
	source, ok := i.sourceByID[sourceID]
	if !ok {
		source.ID = sourceID
	}
	details := buildPlanSourceDetails(source, i.defaults, i.planLimit, i.dryRun)
	return runPlanSelector(i.app, details, rows)
}

func buildCLIInteraction(appCtx *AppContext, cfg config.Config, planLimit int, dryRun bool) app.Interaction {
	sourceByID := map[string]config.Source{}
	for _, source := range cfg.Sources {
		sourceByID[source.ID] = source
	}
	return cliInteraction{
		app:        appCtx,
		defaults:   cfg.Defaults,
		sourceByID: sourceByID,
		planLimit:  planLimit,
		dryRun:     dryRun,
	}
}
