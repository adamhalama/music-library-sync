package app

import (
	"context"
	"fmt"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/output"
)

type SyncRequest struct {
	SourceIDs        []string
	DryRun           bool
	TimeoutOverride  time.Duration
	Plan             bool
	PlanLimit        int
	AskOnExisting    bool
	AskOnExistingSet bool
	ScanGaps         bool
	NoPreflight      bool
	AllowPrompt      bool
	TrackStatus      engine.TrackStatusMode
}

type SyncUseCase struct {
	Registry map[string]engine.Adapter
	Runner   engine.ExecRunner
	Emitter  output.EventEmitter
}

func (u SyncUseCase) Run(ctx context.Context, cfg config.Config, req SyncRequest, interaction Interaction) (engine.SyncResult, error) {
	if interaction == nil {
		interaction = NoopInteraction{}
	}
	syncer := engine.NewSyncer(u.Registry, u.Runner, u.Emitter)
	return syncer.Sync(ctx, cfg, engine.SyncOptions{
		SourceIDs:        req.SourceIDs,
		DryRun:           req.DryRun,
		TimeoutOverride:  req.TimeoutOverride,
		Plan:             req.Plan,
		PlanLimit:        req.PlanLimit,
		AskOnExisting:    req.AskOnExisting,
		AskOnExistingSet: req.AskOnExistingSet,
		ScanGaps:         req.ScanGaps,
		NoPreflight:      req.NoPreflight,
		AllowPrompt:      req.AllowPrompt,
		SelectPlanRows: func(sourceID string, rows []engine.PlanRow) (engine.PlanSelectionResult, error) {
			return interaction.SelectRows(sourceID, rows)
		},
		PromptOnExisting: func(sourceID string, preflight engine.SoundCloudPreflight) (bool, error) {
			prompt := fmt.Sprintf("[%s] Existing track found at position %d of %d. Continue scanning for gaps?", sourceID, preflight.FirstExistingIndex, preflight.RemoteTotal)
			return interaction.Confirm(prompt, false)
		},
		PromptOnSpotifyAuth: func(sourceID string) (bool, error) {
			return interaction.Confirm(fmt.Sprintf("[%s] Spotify login required. Open browser now?", sourceID), true)
		},
		PromptOnDeemixARL: func(sourceID string) (string, error) {
			return interaction.Input(fmt.Sprintf("[%s] Enter your Deezer ARL for deemix", sourceID))
		},
		TrackStatus: req.TrackStatus,
	})
}
