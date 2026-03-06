package engine

import (
	"context"

	"github.com/jaa/update-downloads/internal/config"
)

type PlanProvider interface {
	Build(ctx context.Context, cfg config.Config, source config.Source, opts SyncOptions) (SourcePlan, error)
}

type SourcePlan interface {
	Rows() []PlanRow
	ApplySelection(selectedIndices []int, dryRun bool) (sourcePlanExecution, error)
}

type PlanRegistry struct {
	providers map[string]PlanProvider
}

func NewPlanRegistry() *PlanRegistry {
	return &PlanRegistry{providers: map[string]PlanProvider{}}
}

func (r *PlanRegistry) Register(adapterKind string, provider PlanProvider) {
	if r == nil || provider == nil {
		return
	}
	r.providers[adapterKind] = provider
}

func (r *PlanRegistry) ProviderFor(adapterKind string) PlanProvider {
	if r == nil {
		return nil
	}
	return r.providers[adapterKind]
}
