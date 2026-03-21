package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/auth"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/output"
)

type spotifyDeemixExecutionPlan struct {
	Source           config.Source
	Preflight        *SoundCloudPreflight
	PlannedTrackIDs  []string
	ExistingTrackIDs []string
	TrackMetadata    map[string]spotifyTrackMetadata
	State            spotifySyncState
}

func (s *Syncer) runSpotifyDeemix(
	ctx context.Context,
	cfg config.Config,
	source config.Source,
	adapter Adapter,
	sourceForExec config.Source,
	sourcePreflight *SoundCloudPreflight,
	opts SyncOptions,
) sourceRunOutcome {
	outcome := sourceRunOutcome{}
	flow := s.buildSourceFlowContext(source)

	plan, planErr := s.prepareSpotifyDeemixExecutionPlan(ctx, cfg, source, opts)
	if planErr != nil {
		outcome.Failed++
		outcome.Attempted++
		if errors.Is(planErr, auth.ErrSpotifyCredentialsNotFound) ||
			errors.Is(planErr, auth.ErrDeemixARLNotFound) {
			outcome.DependencyFailures++
		}
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelError,
			Event:     output.EventSourceFailed,
			SourceID:  source.ID,
			Message:   fmt.Sprintf("[%s] spotify deemix preflight failed: %v", source.ID, planErr),
		})
		outcome.Stop = !cfg.Defaults.ContinueOnError
		return outcome
	}
	sourceForExec = plan.Source
	sourcePreflight = plan.Preflight
	if plan.Preflight != nil {
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelInfo,
			Event:     output.EventSourcePreflight,
			SourceID:  source.ID,
			Message: fmt.Sprintf(
				"[%s] preflight: remote=%d known=%d gaps=%d known_gaps=%d first_existing=%d planned=%d mode=%s",
				source.ID,
				plan.Preflight.RemoteTotal,
				plan.Preflight.KnownCount,
				plan.Preflight.ArchiveGapCount,
				plan.Preflight.KnownGapCount,
				plan.Preflight.FirstExistingIndex,
				plan.Preflight.PlannedDownloadCount,
				plan.Preflight.Mode,
			),
			Details: map[string]any{
				"remote_total":           plan.Preflight.RemoteTotal,
				"known_count":            plan.Preflight.KnownCount,
				"archive_gap_count":      plan.Preflight.ArchiveGapCount,
				"known_gap_count":        plan.Preflight.KnownGapCount,
				"first_existing_index":   plan.Preflight.FirstExistingIndex,
				"planned_download_count": plan.Preflight.PlannedDownloadCount,
				"mode":                   plan.Preflight.Mode,
			},
		})
	}
	emitSpotifyDeemixExistingTrackStatus(s, source.ID, plan, opts.TrackStatus)

	timeout := time.Duration(cfg.Defaults.CommandTimeoutSeconds) * time.Second
	if opts.TimeoutOverride > 0 {
		timeout = opts.TimeoutOverride
	}

	if opts.DryRun {
		outcome.Attempted++
		outcome.Succeeded++
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelInfo,
			Event:     output.EventSourceFinished,
			SourceID:  source.ID,
			Message:   fmt.Sprintf("[%s] dry-run complete", source.ID),
		})
		return outcome
	}

	if sourcePreflight != nil && sourcePreflight.PlannedDownloadCount == 0 {
		outcome.Attempted++
		outcome.Succeeded++
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelInfo,
			Event:     output.EventSourceFinished,
			SourceID:  source.ID,
			Message:   fmt.Sprintf("[%s] up-to-date (no downloads planned)", source.ID),
			Details: map[string]any{
				"planned_download_count": 0,
				"mode":                   sourcePreflight.Mode,
			},
		})
		return outcome
	}

	plannedTrackIDs := append([]string(nil), plan.PlannedTrackIDs...)
	if len(plannedTrackIDs) == 0 {
		if trackID := extractSpotifyTrackID(sourceForExec.URL); trackID != "" {
			plannedTrackIDs = []string{trackID}
		} else if playlistID, playlistErr := resolveSpotifyPlaylistID(sourceForExec.URL); playlistErr == nil {
			pageTracks, pageErr := enumerateSpotifyViaPageFn(ctx, playlistID)
			if pageErr != nil || len(pageTracks) == 0 {
				plannedTrackIDs = []string{""}
			} else {
				plannedTrackIDs = make([]string, 0, len(pageTracks))
				for _, track := range pageTracks {
					plannedTrackIDs = append(plannedTrackIDs, track.ID)
				}
				metadata := buildSpotifyTrackMetadataIndex(pageTracks)
				if len(metadata) > 0 {
					if plan.TrackMetadata == nil {
						plan.TrackMetadata = metadata
					} else {
						for id, value := range metadata {
							plan.TrackMetadata[id] = value
						}
					}
				}
				_ = s.Emitter.Emit(output.Event{
					Timestamp: s.Now(),
					Level:     output.LevelWarn,
					Event:     output.EventSourcePreflight,
					SourceID:  source.ID,
					Message:   fmt.Sprintf("[%s] no-preflight playlist run using public playlist page track enumeration (%d track(s))", source.ID, len(plannedTrackIDs)),
				})
			}
		} else {
			plannedTrackIDs = []string{""}
		}
	}

	outcome.Attempted++
	_ = s.Emitter.Emit(output.Event{
		Timestamp: s.Now(),
		Level:     output.LevelInfo,
		Event:     output.EventSourceStarted,
		SourceID:  source.ID,
		Message:   fmt.Sprintf("[%s] running deemix for %d track(s)", source.ID, len(plannedTrackIDs)),
	})

	runtimeDir := strings.TrimSpace(sourceForExec.DeemixRuntimeDir)
	sourceFailed := false
	var sourceFailureMessage string
	var sourceFailureDetails map[string]any
	skippedUnavailable := 0
	spotifyTargetDir, targetDirErr := config.ExpandPath(sourceForExec.TargetDir)
	if targetDirErr != nil {
		sourceFailed = true
		sourceFailureMessage = fmt.Sprintf("[%s] resolve target_dir: %v", source.ID, targetDirErr)
	}
	for idx, trackID := range plannedTrackIDs {
		if sourceFailed {
			break
		}
		trackSource := sourceForExec
		if trackID != "" {
			trackSource.URL = spotifyTrackURL(trackID)
		}
		trackLabel := spotifyTrackDisplayNameFromState(trackID, plan.TrackMetadata, plan.State)
		spec, buildErr := adapter.BuildExecSpec(trackSource, cfg.Defaults, timeout)
		if buildErr != nil {
			sourceFailed = true
			sourceFailureMessage = fmt.Sprintf("[%s] cannot build command: %v", source.ID, buildErr)
			break
		}
		spec = s.applyFlowObservers(spec, flow, source)
		if runtimeDir == "" {
			runtimeDir = spec.Dir
			sourceForExec.DeemixRuntimeDir = spec.Dir
		}
		trackSource.DeemixRuntimeDir = runtimeDir
		spec.Dir = runtimeDir

		if trackID != "" && strings.TrimSpace(runtimeDir) != "" {
			metadata, metadataErr := resolveSpotifyTrackMetadataForExecution(ctx, trackID, plan.TrackMetadata)
			if metadataErr != nil {
				_ = s.Emitter.Emit(output.Event{
					Timestamp: s.Now(),
					Level:     output.LevelWarn,
					Event:     output.EventSourcePreflight,
					SourceID:  source.ID,
					Message:   fmt.Sprintf("[%s] spotify metadata lookup failed for %s: %v", source.ID, trackID, metadataErr),
				})
			} else if cacheErr := writeSpotifyTrackMetadataCache(runtimeDir, trackID, metadata); cacheErr != nil {
				_ = s.Emitter.Emit(output.Event{
					Timestamp: s.Now(),
					Level:     output.LevelWarn,
					Event:     output.EventSourcePreflight,
					SourceID:  source.ID,
					Message:   fmt.Sprintf("[%s] unable to prime deemix spotify cache for %s: %v", source.ID, trackID, cacheErr),
				})
			}
		}

		if trackID != "" {
			message := fmt.Sprintf("[%s] deemix track %d/%d %s", source.ID, idx+1, len(plannedTrackIDs), trackID)
			if trackLabel != "" {
				message = fmt.Sprintf("[%s] deemix track %d/%d %s (%s)", source.ID, idx+1, len(plannedTrackIDs), trackID, trackLabel)
			}
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelInfo,
				Event:     output.EventSourcePreflight,
				SourceID:  source.ID,
				Message:   message,
			})
		}

		var mediaBefore map[string]mediaFileSnapshot
		if trackID != "" {
			before, snapshotErr := snapshotMediaFiles(spotifyTargetDir)
			if snapshotErr != nil {
				_ = s.Emitter.Emit(output.Event{
					Timestamp: s.Now(),
					Level:     output.LevelWarn,
					Event:     output.EventSourcePreflight,
					SourceID:  source.ID,
					Message:   fmt.Sprintf("[%s] unable to snapshot target directory before track run: %v", source.ID, snapshotErr),
				})
			} else {
				mediaBefore = before
			}
		}

		execResult := s.Runner.Run(ctx, spec)
		s.flushFlowParser(flow, source)
		if execResult.Interrupted {
			_ = cleanupRuntimeDir(runtimeDir)
			outcome.Interrupted = true
			outcome.Stop = true
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelError,
				Event:     output.EventSourceFailed,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] interrupted", source.ID),
				Details:   buildExecFailureDetails(source, spec, execResult),
			})
			return outcome
		}

		if unavailable, reason := deemixReportedTrackUnavailable(execResult); unavailable {
			skippedUnavailable++
			display := trackLabel
			if display == "" {
				display = deemixTrackDisplayName(execResult)
			}
			if display == "" {
				display = trackID
			}
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelInfo,
				Event:     output.EventSourcePreflight,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] [skip] %s (%s) (%s)", source.ID, trackID, display, reason),
			})
			continue
		}

		if execResult.ExitCode != 0 {
			sourceFailed = true
			sourceFailureMessage = fmt.Sprintf("[%s] command failed with exit code %d", source.ID, execResult.ExitCode)
			sourceFailureDetails = buildExecFailureDetails(source, spec, execResult)
			break
		}
		if failed, reason := deemixReportedFailure(execResult); failed {
			sourceFailed = true
			sourceFailureMessage = fmt.Sprintf(
				"[%s] deemix reported runtime failure despite exit code 0 (%s); check Spotify app credentials/quota and consider fallback to spotdl for this source",
				source.ID,
				reason,
			)
			sourceFailureDetails = buildExecFailureDetails(source, spec, execResult)
			break
		}

		if trackID != "" {
			entryLabel := trackLabel
			if entryLabel == "" {
				entryLabel = deemixTrackDisplayName(execResult)
			}
			localPath := ""
			if mediaBefore != nil {
				after, snapshotErr := snapshotMediaFiles(spotifyTargetDir)
				if snapshotErr == nil {
					localPath = detectUpdatedMediaPath(mediaBefore, after)
				}
			}
			doneMessage := fmt.Sprintf("[%s] [done] %s", source.ID, trackID)
			if entryLabel != "" {
				doneMessage = fmt.Sprintf("[%s] [done] %s (%s)", source.ID, trackID, entryLabel)
			}
			if appendErr := appendSpotifySyncStateEntry(sourceForExec.StateFile, trackID, entryLabel, localPath); appendErr != nil {
				sourceFailed = true
				sourceFailureMessage = fmt.Sprintf("[%s] failed to update spotify state file: %v", source.ID, appendErr)
				break
			}
			plan.State.KnownIDs[trackID] = struct{}{}
			entry := plan.State.Entries[trackID]
			if entryLabel != "" {
				entry.DisplayName = entryLabel
			}
			if localPath != "" {
				entry.LocalPath = localPath
			}
			if entry.DisplayName != "" || entry.LocalPath != "" {
				plan.State.Entries[trackID] = entry
			}
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelInfo,
				Event:     output.EventSourcePreflight,
				SourceID:  source.ID,
				Message:   doneMessage,
			})
		}
	}

	_ = cleanupRuntimeDir(runtimeDir)

	if sourceFailed {
		outcome.Failed++
		if sourceFailureMessage == "" {
			sourceFailureMessage = fmt.Sprintf("[%s] command failed", source.ID)
		}
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelError,
			Event:     output.EventSourceFailed,
			SourceID:  source.ID,
			Message:   sourceFailureMessage,
			Details:   sourceFailureDetails,
		})
		outcome.Stop = !cfg.Defaults.ContinueOnError
		return outcome
	}

	outcome.Succeeded++
	_ = s.Emitter.Emit(output.Event{
		Timestamp: s.Now(),
		Level:     output.LevelInfo,
		Event:     output.EventSourceFinished,
		SourceID:  source.ID,
		Message:   fmt.Sprintf("[%s] completed", source.ID),
		Details: map[string]any{
			"planned_download_count": len(plannedTrackIDs),
			"skipped_unavailable":    skippedUnavailable,
		},
	})

	return outcome
}

func (s *Syncer) prepareSpotifyDeemixExecutionPlan(
	ctx context.Context,
	cfg config.Config,
	source config.Source,
	opts SyncOptions,
) (spotifyDeemixExecutionPlan, error) {
	plan := spotifyDeemixExecutionPlan{
		Source: source,
		State: spotifySyncState{
			KnownIDs: map[string]struct{}{},
			Entries:  map[string]spotifyStateEntry{},
		},
	}

	stateFilePath, err := config.ResolveStateFile(cfg.Defaults.StateDir, source.StateFile)
	if err != nil {
		return plan, fmt.Errorf("resolve state_file: %w", err)
	}
	plan.Source.StateFile = stateFilePath

	spotifyCreds, err := resolveSpotifyCredentialsFn()
	if err != nil {
		return plan, err
	}
	plan.Source.SpotifyClientID = spotifyCreds.ClientID
	plan.Source.SpotifyClientSecret = spotifyCreds.ClientSecret

	arl, err := resolveDeemixARLFn()
	if err != nil && !errors.Is(err, auth.ErrDeemixARLNotFound) {
		return plan, err
	}
	if strings.TrimSpace(arl) == "" && opts.AllowPrompt && opts.PromptOnDeemixARL != nil {
		prompted, promptErr := opts.PromptOnDeemixARL(source.ID)
		if promptErr != nil {
			return plan, promptErr
		}
		arl = strings.TrimSpace(prompted)
		if arl != "" {
			if saveErr := saveDeemixARLFn(arl); saveErr != nil {
				_ = s.Emitter.Emit(output.Event{
					Timestamp: s.Now(),
					Level:     output.LevelWarn,
					Event:     output.EventSourcePreflight,
					SourceID:  source.ID,
					Message:   fmt.Sprintf("[%s] unable to save Deezer ARL to keychain: %v", source.ID, saveErr),
				})
			}
		}
	}
	arl = strings.TrimSpace(arl)
	if arl == "" {
		return plan, auth.ErrDeemixARLNotFound
	}
	plan.Source.DeezerARL = arl

	mode := determineSoundCloudMode(source, opts)
	askOnExisting := resolveAskOnExisting(source, opts)
	breakOnExisting := mode == SoundCloudModeBreak
	plan.Source.Sync.BreakOnExisting = &breakOnExisting

	if opts.NoPreflight {
		if askOnExisting {
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelWarn,
				Event:     output.EventSourcePreflight,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] ask-on-existing ignored because preflight is disabled", source.ID),
			})
		}
		if mode == SoundCloudModeScanGaps {
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelWarn,
				Event:     output.EventSourcePreflight,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] scan-gaps without preflight disables remote diff planning", source.ID),
			})
		}
		return plan, nil
	}

	tracks := []spotifyRemoteTrack{}
	if trackID := extractSpotifyTrackID(source.URL); trackID != "" {
		tracks = append(tracks, spotifyRemoteTrack{
			ID:    trackID,
			URL:   spotifyTrackURL(trackID),
			Title: trackID,
		})
	} else {
		tracks, err = enumerateSpotifyTracksFn(ctx, source, spotifyCreds)
		if err != nil {
			return plan, err
		}
	}
	plan.TrackMetadata = buildSpotifyTrackMetadataIndex(tracks)

	state, err := parseSpotifySyncState(stateFilePath)
	if err != nil {
		return plan, fmt.Errorf("parse spotify sync state file: %w", err)
	}
	plan.State = state

	targetDir, err := config.ExpandPath(source.TargetDir)
	if err != nil {
		return plan, fmt.Errorf("resolve target_dir: %w", err)
	}

	preflight, _, _, plannedTrackIDs, existingTrackIDs := buildSpotifyPreflight(tracks, state, targetDir, mode)
	if askOnExisting &&
		mode == SoundCloudModeBreak &&
		preflight.FirstExistingIndex > 0 &&
		opts.AllowPrompt &&
		opts.PromptOnExisting != nil {
		shouldScanGaps, promptErr := opts.PromptOnExisting(source.ID, preflight)
		if promptErr != nil {
			return plan, promptErr
		}
		if shouldScanGaps {
			mode = SoundCloudModeScanGaps
			preflight, _, _, plannedTrackIDs, existingTrackIDs = buildSpotifyPreflight(tracks, state, targetDir, mode)
		}
	}

	plan.Preflight = &preflight
	plan.PlannedTrackIDs = plannedTrackIDs
	plan.ExistingTrackIDs = existingTrackIDs
	breakOnExisting = mode == SoundCloudModeBreak
	plan.Source.Sync.BreakOnExisting = &breakOnExisting
	return plan, nil
}

func shouldRetrySpotifyWithUserAuth(source config.Source, execResult ExecResult, opts SyncOptions) bool {
	if !opts.AllowPrompt {
		return false
	}
	if source.Type != config.SourceTypeSpotify {
		return false
	}
	if source.Adapter.Kind != "spotdl" {
		return false
	}
	if execResult.ExitCode == 0 || execResult.Interrupted || execResult.TimedOut {
		return false
	}
	if hasSpotDLUserAuthArg(source.Adapter.ExtraArgs) {
		return false
	}
	return isSpotifyUserAuthRequired(source, execResult)
}

func planSpotifyUserAuthRetry(source config.Source, opts SyncOptions) (config.Source, bool, error) {
	retrySource := withSpotDLUserAuth(source)
	if !hasSpotDLHeadlessArg(retrySource.Adapter.ExtraArgs) {
		return retrySource, true, nil
	}
	if opts.PromptOnSpotifyAuth == nil {
		return retrySource, false, nil
	}
	openBrowser, err := opts.PromptOnSpotifyAuth(source.ID)
	if err != nil {
		return retrySource, false, err
	}
	if openBrowser {
		retrySource = withoutSpotDLHeadless(retrySource)
	}
	return retrySource, openBrowser, nil
}

func isSpotifyUserAuthRequired(source config.Source, execResult ExecResult) bool {
	if source.Type != config.SourceTypeSpotify {
		return false
	}
	if source.Adapter.Kind != "spotdl" {
		return false
	}
	if execResult.ExitCode == 0 || execResult.Interrupted {
		return false
	}
	combined := strings.ToLower(execResult.StdoutTail + "\n" + execResult.StderrTail)
	return strings.Contains(combined, "valid user authentication required") ||
		(strings.Contains(combined, "user authentication required") && strings.Contains(combined, "api.spotify.com"))
}

func isSpotifyRateLimited(source config.Source, execResult ExecResult) bool {
	if source.Type != config.SourceTypeSpotify {
		return false
	}
	if source.Adapter.Kind != "spotdl" {
		return false
	}
	if execResult.ExitCode == 0 || execResult.Interrupted {
		return false
	}
	combined := strings.ToLower(execResult.StdoutTail + "\n" + execResult.StderrTail)
	return strings.Contains(combined, "rate/request limit") &&
		strings.Contains(combined, "retry will occur after:")
}

func hasSpotDLHeadlessArg(args []string) bool {
	for _, candidate := range args {
		if isSpotDLHeadlessArg(candidate) {
			return true
		}
	}
	return false
}

func hasSpotDLUserAuthArg(args []string) bool {
	for _, candidate := range args {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "--user-auth" {
			return true
		}
	}
	return false
}

func withSpotDLUserAuth(source config.Source) config.Source {
	if hasSpotDLUserAuthArg(source.Adapter.ExtraArgs) {
		return source
	}
	cloned := append([]string{}, source.Adapter.ExtraArgs...)
	cloned = append(cloned, "--user-auth")
	source.Adapter.ExtraArgs = cloned
	return source
}

func withoutSpotDLHeadless(source config.Source) config.Source {
	if !hasSpotDLHeadlessArg(source.Adapter.ExtraArgs) {
		return source
	}
	filtered := make([]string, 0, len(source.Adapter.ExtraArgs))
	for _, candidate := range source.Adapter.ExtraArgs {
		if isSpotDLHeadlessArg(candidate) {
			continue
		}
		filtered = append(filtered, candidate)
	}
	source.Adapter.ExtraArgs = filtered
	return source
}

func isSpotDLHeadlessArg(candidate string) bool {
	trimmed := strings.TrimSpace(candidate)
	return trimmed == "--headless" || strings.HasPrefix(trimmed, "--headless=")
}

func deemixReportedFailure(execResult ExecResult) (bool, string) {
	combined := strings.ToLower(execResult.StdoutTail + "\n" + execResult.StderrTail)

	switch {
	case strings.Contains(combined, "cannot read properties of undefined") &&
		strings.Contains(combined, "spotifyplugin."):
		return true, "spotify-plugin-exception"
	case strings.Contains(combined, "pluginnotenablederror") ||
		strings.Contains(combined, "populate spotify app credentials to download spotify links"):
		return true, "spotify-credentials-missing"
	case strings.Contains(combined, "not logged in"):
		return true, "deezer-auth-failed"
	case strings.Contains(combined, "typeerror:") && strings.Contains(combined, "spotifyplugin"):
		return true, "spotify-plugin-typeerror"
	default:
		return false, ""
	}
}

func deemixReportedTrackUnavailable(execResult ExecResult) (bool, string) {
	combined := strings.ToLower(execResult.StdoutTail + "\n" + execResult.StderrTail)
	if strings.Contains(combined, "track unavailable on deezer") {
		return true, "unavailable-on-deezer"
	}
	return false, ""
}

func deemixTrackDisplayName(execResult ExecResult) string {
	combined := execResult.StdoutTail + "\n" + execResult.StderrTail
	matches := deemixTitlePattern.FindAllStringSubmatch(combined, -1)
	if len(matches) == 0 {
		return ""
	}
	for i := len(matches) - 1; i >= 0; i-- {
		if len(matches[i]) < 2 {
			continue
		}
		title := strings.TrimSpace(matches[i][1])
		if title != "" {
			return title
		}
	}
	return ""
}

func spotifyTrackDisplayName(trackID string, metadata map[string]spotifyTrackMetadata) string {
	id := extractSpotifyTrackID(trackID)
	if id == "" || metadata == nil {
		return ""
	}
	item, ok := metadata[id]
	if !ok {
		return ""
	}

	title := strings.TrimSpace(item.Title)
	artist := strings.TrimSpace(item.Artist)
	name := title
	if artist != "" && title != "" {
		name = artist + " - " + title
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if extractSpotifyTrackID(name) != "" {
		return ""
	}
	return name
}

func spotifyTrackDisplayNameFromState(trackID string, metadata map[string]spotifyTrackMetadata, state spotifySyncState) string {
	if label := spotifyTrackDisplayName(trackID, metadata); label != "" {
		return label
	}
	id := extractSpotifyTrackID(trackID)
	if id == "" {
		return ""
	}
	entry, ok := state.Entries[id]
	if !ok {
		return ""
	}
	label := strings.TrimSpace(entry.DisplayName)
	if label == "" {
		return ""
	}
	if extractSpotifyTrackID(label) != "" {
		return ""
	}
	return label
}

func normalizeTrackStatusMode(mode TrackStatusMode) TrackStatusMode {
	switch mode {
	case TrackStatusNames, TrackStatusCount, TrackStatusNone:
		return mode
	default:
		return TrackStatusNames
	}
}

func emitSpotifyDeemixExistingTrackStatus(s *Syncer, sourceID string, plan spotifyDeemixExecutionPlan, mode TrackStatusMode) {
	statusMode := normalizeTrackStatusMode(mode)
	if statusMode == TrackStatusNone || len(plan.ExistingTrackIDs) == 0 {
		return
	}

	if statusMode == TrackStatusCount {
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelInfo,
			Event:     output.EventSourcePreflight,
			SourceID:  sourceID,
			Message:   fmt.Sprintf("[%s] already-present locally: %d track(s)", sourceID, len(plan.ExistingTrackIDs)),
		})
		return
	}

	const maxDetailed = 20
	limit := len(plan.ExistingTrackIDs)
	if limit > maxDetailed {
		limit = maxDetailed
	}
	for i := 0; i < limit; i++ {
		id := plan.ExistingTrackIDs[i]
		label := spotifyTrackDisplayNameFromState(id, plan.TrackMetadata, plan.State)
		if strings.TrimSpace(label) == "" {
			label = id
		}
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelInfo,
			Event:     output.EventSourcePreflight,
			SourceID:  sourceID,
			Message:   fmt.Sprintf("[%s] [skip] %s (%s) (already-present)", sourceID, id, label),
		})
	}
	if len(plan.ExistingTrackIDs) > limit {
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelInfo,
			Event:     output.EventSourcePreflight,
			SourceID:  sourceID,
			Message:   fmt.Sprintf("[%s] [skip] ... and %d more already-present track(s)", sourceID, len(plan.ExistingTrackIDs)-limit),
		})
	}
}

func resolveSpotDLOAuthCachePath() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	candidates := []string{
		filepath.Join(home, ".spotdl", ".spotipy"),
		filepath.Join(home, ".spotdl", ".spotify_cache"),
	}
	for _, candidate := range candidates {
		info, statErr := os.Stat(candidate)
		if statErr != nil || info.IsDir() {
			continue
		}
		if strings.HasPrefix(candidate, home) {
			return "~" + strings.TrimPrefix(candidate, home), true
		}
		return candidate, true
	}
	return "", false
}
