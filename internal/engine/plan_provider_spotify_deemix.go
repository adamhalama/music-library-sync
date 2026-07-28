package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jaa/update-downloads/internal/auth"
	"github.com/jaa/update-downloads/internal/config"
)

type SpotifyDeemixPlanProvider struct{}

func NewSpotifyDeemixPlanProvider() *SpotifyDeemixPlanProvider {
	return &SpotifyDeemixPlanProvider{}
}

type spotifyDeemixSourcePlan struct {
	rows []PlanRow
	plan spotifyDeemixExecutionPlan
}

func (p *SpotifyDeemixPlanProvider) Build(
	ctx context.Context,
	cfg config.Config,
	source config.Source,
	opts SyncOptions,
) (SourcePlan, error) {
	if source.Type != config.SourceTypeSpotify {
		return nil, fmt.Errorf("spotify deemix plan provider only supports spotify sources")
	}
	if source.Adapter.Kind != "deemix" {
		return nil, fmt.Errorf("spotify deemix plan provider only supports adapter.kind=deemix")
	}
	if opts.NoPreflight {
		return nil, fmt.Errorf("spotify deemix plan provider requires preflight")
	}

	plan, tracks, archiveGapIDs, knownGapIDs, plannedIDs, err := buildSpotifyDeemixPlan(ctx, cfg, source, opts)
	if err != nil {
		return nil, err
	}

	plannedSet := stringSliceSet(plannedIDs)
	rows := make([]PlanRow, 0, len(tracks))
	for i, track := range tracks {
		status := PlanRowAlreadyDownloaded
		if _, ok := archiveGapIDs[track.ID]; ok {
			status = PlanRowMissingNew
		} else if _, ok := knownGapIDs[track.ID]; ok {
			status = PlanRowMissingKnownGap
		}
		toggleable := status != PlanRowAlreadyDownloaded
		_, selected := plannedSet[track.ID]
		rows = append(rows, PlanRow{
			Index:             i + 1,
			RemoteID:          track.ID,
			Title:             spotifyTrackRowTitle(track),
			Status:            status,
			Toggleable:        toggleable,
			SelectedByDefault: toggleable && selected,
		})
	}

	return &spotifyDeemixSourcePlan{
		rows: rows,
		plan: plan,
	}, nil
}

func (p *spotifyDeemixSourcePlan) Rows() []PlanRow {
	return append([]PlanRow{}, p.rows...)
}

func (p *spotifyDeemixSourcePlan) ApplySelection(manifest ExecutionManifest, opts PlanApplyOptions) (sourcePlanExecution, error) {
	manifest, err := CanonicalizeExecutionManifest(p.plan.Source.ID, p.rows, manifest)
	if err != nil {
		return sourcePlanExecution{}, err
	}

	selectedIDs := make([]string, 0, len(manifest.Execution))
	for _, entry := range manifest.Execution {
		selectedIDs = append(selectedIDs, entry.RemoteID)
	}

	plan := p.plan
	plan.DownloadOrder = NormalizeDownloadOrder(manifest.DownloadOrder)
	plan.PlannedTrackIDs = selectedIDs
	if plan.Preflight != nil {
		preflight := *plan.Preflight
		preflight.PlannedDownloadCount = len(selectedIDs)
		plan.Preflight = &preflight
	}

	return sourcePlanExecution{
		SourceForExec:     plan.Source,
		SourcePreflight:   plan.Preflight,
		SpotifyDeemixPlan: &plan,
		DownloadOrder:     plan.DownloadOrder,
	}, nil
}

func buildSpotifyDeemixPlan(
	ctx context.Context,
	cfg config.Config,
	source config.Source,
	opts SyncOptions,
) (spotifyDeemixExecutionPlan, []spotifyRemoteTrack, map[string]struct{}, map[string]struct{}, []string, error) {
	plan := spotifyDeemixExecutionPlan{
		Source: source,
		State: spotifySyncState{
			KnownIDs: map[string]struct{}{},
			Entries:  map[string]spotifyStateEntry{},
		},
		DownloadOrder: DownloadOrderNewestFirst,
	}

	stateFilePath, err := config.ResolveStateFile(cfg.Defaults.StateDir, source.StateFile)
	if err != nil {
		return plan, nil, nil, nil, nil, fmt.Errorf("resolve state_file: %w", err)
	}
	stateStore := resolveSpotifyStateStore(stateFilePath)
	plan.Source.StateFile = stateStore.WritePath
	plan.StateWritePath = stateStore.WritePath

	spotifyCreds, err := resolveSpotifyCredentialsFn()
	if err != nil {
		return plan, nil, nil, nil, nil, err
	}
	plan.Source.SpotifyClientID = spotifyCreds.ClientID
	plan.Source.SpotifyClientSecret = spotifyCreds.ClientSecret

	arl, err := resolveDeemixARLFn()
	if err != nil && !errors.Is(err, auth.ErrDeemixARLNotFound) {
		return plan, nil, nil, nil, nil, err
	}
	if strings.TrimSpace(arl) == "" && opts.AllowPrompt && opts.PromptOnDeemixARL != nil {
		prompted, promptErr := opts.PromptOnDeemixARL(source.ID)
		if promptErr != nil {
			return plan, nil, nil, nil, nil, promptErr
		}
		arl = strings.TrimSpace(prompted)
		if arl != "" {
			_ = saveDeemixARLFn(arl)
		}
	}
	arl = strings.TrimSpace(arl)
	if arl == "" {
		return plan, nil, nil, nil, nil, auth.ErrDeemixARLNotFound
	}
	plan.Source.DeezerARL = arl

	mode := determineSoundCloudMode(source, opts)
	breakOnExisting := mode == SoundCloudModeBreak
	plan.Source.Sync.BreakOnExisting = &breakOnExisting

	tracks := []spotifyRemoteTrack{}
	if trackID := extractSpotifyTrackID(source.URL); trackID != "" {
		tracks = append(tracks, spotifyRemoteTrack{
			ID:       trackID,
			URL:      spotifyTrackURL(trackID),
			Title:    trackID,
			Position: 1,
		})
	} else {
		tracks, err = enumerateSpotifyTracksFn(ctx, source, spotifyCreds)
		if err != nil {
			return plan, nil, nil, nil, nil, err
		}
	}
	tracks = applySpotifyPlanWindow(tracks, opts.PlanLimit, EffectivePlanWindow(source, opts))
	tracks = enrichSpotifyRemoteTrackMetadata(ctx, tracks)

	plan.TrackMetadata = buildSpotifyTrackMetadataIndex(tracks)

	state, stateStore, err := loadSpotifySyncState(stateFilePath)
	if err != nil {
		return plan, nil, nil, nil, nil, fmt.Errorf("parse spotify sync state file: %w", err)
	}
	plan.State = state
	plan.Source.StateFile = stateStore.WritePath
	plan.StateWritePath = stateStore.WritePath

	targetDir, err := config.ExpandPath(source.TargetDir)
	if err != nil {
		return plan, nil, nil, nil, nil, fmt.Errorf("resolve target_dir: %w", err)
	}

	preflight, archiveGapIDs, knownGapIDs, plannedTrackIDs, existingTrackIDs, backfills := buildSpotifyPreflight(tracks, state, targetDir, mode)
	if resolveAskOnExisting(source, opts) &&
		mode == SoundCloudModeBreak &&
		preflight.FirstExistingIndex > 0 &&
		opts.AllowPrompt &&
		opts.PromptOnExisting != nil {
		shouldScanGaps, promptErr := opts.PromptOnExisting(source.ID, preflight)
		if promptErr != nil {
			return plan, nil, nil, nil, nil, promptErr
		}
		if shouldScanGaps {
			mode = SoundCloudModeScanGaps
			preflight, archiveGapIDs, knownGapIDs, plannedTrackIDs, existingTrackIDs, backfills = buildSpotifyPreflight(tracks, state, targetDir, mode)
		}
	}

	plan.Preflight = &preflight
	plan.PlannedTrackIDs = orderForExecution(plannedTrackIDs, plan.DownloadOrder)
	plan.ExistingTrackIDs = existingTrackIDs
	plan.BackfillEntries = backfills
	breakOnExisting = mode == SoundCloudModeBreak
	plan.Source.Sync.BreakOnExisting = &breakOnExisting
	return plan, tracks, archiveGapIDs, knownGapIDs, plannedTrackIDs, nil
}

func applySpotifyPlanWindow(tracks []spotifyRemoteTrack, limit int, window PlanWindow) []spotifyRemoteTrack {
	selected := append([]spotifyRemoteTrack(nil), tracks...)
	if NormalizePlanWindow(window) == PlanWindowLatest {
		if spotifyTracksHaveAddedAt(selected) {
			slices.SortStableFunc(selected, func(a, b spotifyRemoteTrack) int {
				switch {
				case !a.AddedAt.IsZero() && !b.AddedAt.IsZero() && !a.AddedAt.Equal(b.AddedAt):
					if a.AddedAt.After(b.AddedAt) {
						return -1
					}
					return 1
				case !a.AddedAt.IsZero() && b.AddedAt.IsZero():
					return -1
				case a.AddedAt.IsZero() && !b.AddedAt.IsZero():
					return 1
				case a.Position > 0 && b.Position > 0 && a.Position != b.Position:
					return a.Position - b.Position
				default:
					return 0
				}
			})
		} else {
			for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
				selected[i], selected[j] = selected[j], selected[i]
			}
		}
	}
	if limit > 0 && len(selected) > limit {
		selected = selected[:limit]
	}
	return selected
}

func spotifyTracksHaveAddedAt(tracks []spotifyRemoteTrack) bool {
	for _, track := range tracks {
		if !track.AddedAt.IsZero() {
			return true
		}
	}
	return false
}

func spotifyTrackRowTitle(track spotifyRemoteTrack) string {
	title := spotifyTrackLocalTitle(track)
	if strings.TrimSpace(title) != "" {
		return title
	}
	return track.ID
}

func stringSliceSet(values []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		out[value] = struct{}{}
	}
	return out
}
