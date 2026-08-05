package app

import (
	"context"
	"fmt"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/navidrome"
	"github.com/jaa/update-downloads/internal/output"
)

type SyncRequest struct {
	SourceIDs          []string
	DryRun             bool
	TimeoutOverride    time.Duration
	Plan               bool
	PlanLimit          int
	PlanWindow         engine.PlanWindow
	PlanWindowBySource map[string]engine.PlanWindow
	AskOnExisting      bool
	AskOnExistingSet   bool
	ScanGaps           bool
	NoPreflight        bool
	AllowPrompt        bool
	TrackStatus        engine.TrackStatusMode
}

type SyncUseCase struct {
	Registry map[string]engine.Adapter
	Runner   engine.ExecRunner
	Emitter  output.EventEmitter
	// NavidromeOptions selects the phone-library config the post-sync scan
	// hook reads. The zero value uses normal discovery.
	NavidromeOptions navidrome.ManagerOptions
	// NavidromeScanner overrides the post-sync scan request in tests.
	NavidromeScanner NavidromeScanner
}

func (u SyncUseCase) Run(ctx context.Context, cfg config.Config, req SyncRequest, interaction Interaction) (engine.SyncResult, error) {
	result, err := u.run(ctx, cfg, req, interaction)
	// The optional phone server never changes the download result: the hook
	// returns warnings, not errors, and runs after the result is already final.
	RequestNavidromeScan(ctx, req, result, err, u.NavidromeOptions, u.NavidromeScanner, u.Emitter)
	return result, err
}

func (u SyncUseCase) run(ctx context.Context, cfg config.Config, req SyncRequest, interaction Interaction) (engine.SyncResult, error) {
	if interaction == nil {
		interaction = NoopInteraction{}
	}
	syncer := engine.NewSyncer(u.Registry, u.Runner, u.Emitter)
	return syncer.Sync(ctx, cfg, engine.SyncOptions{
		SourceIDs:          req.SourceIDs,
		DryRun:             req.DryRun,
		TimeoutOverride:    req.TimeoutOverride,
		Plan:               req.Plan,
		PlanLimit:          req.PlanLimit,
		PlanWindow:         req.PlanWindow,
		PlanWindowBySource: req.PlanWindowBySource,
		AskOnExisting:      req.AskOnExisting,
		AskOnExistingSet:   req.AskOnExistingSet,
		ScanGaps:           req.ScanGaps,
		NoPreflight:        req.NoPreflight,
		AllowPrompt:        req.AllowPrompt,
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
