package engine

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/output"
)

func (s *Syncer) runSoundCloudFreeDownloadSource(
	ctx context.Context,
	cfg config.Config,
	source config.Source,
	sourceForExec config.Source,
	sourcePreflight *SoundCloudPreflight,
	plannedTracks []soundCloudRemoteTrack,
	stateSwap soundCloudStateSwap,
	downloadOrder DownloadOrder,
	opts SyncOptions,
) (soundCloudFreeDownloadOutcome, error) {
	outcome := soundCloudFreeDownloadOutcome{Attempted: true}

	timeout := time.Duration(cfg.Defaults.CommandTimeoutSeconds) * time.Second
	if opts.TimeoutOverride > 0 {
		timeout = opts.TimeoutOverride
	}

	if opts.DryRun {
		if err := cleanupTempStateFiles(stateSwap); err != nil {
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelWarn,
				Event:     output.EventSourceFinished,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] unable to clean temporary state file: %v", source.ID, err),
			})
		}
		outcome.Succeeded = true
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelInfo,
			Event:     output.EventSourceFinished,
			SourceID:  source.ID,
			Message:   fmt.Sprintf("[%s] dry-run complete", source.ID),
		})
		return outcome, nil
	}

	if sourcePreflight != nil && sourcePreflight.PlannedDownloadCount == 0 {
		if err := cleanupTempStateFiles(stateSwap); err != nil {
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelWarn,
				Event:     output.EventSourceFinished,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] unable to clean temporary state file: %v", source.ID, err),
			})
		}
		outcome.Succeeded = true
		finishedMessage := fmt.Sprintf("[%s] up-to-date (no downloads planned)", source.ID)
		if sourcePreflight.KnownGapCount > 0 || sourcePreflight.ArchiveGapCount > 0 {
			finishedMessage = fmt.Sprintf(
				"[%s] no new downloads planned in %s mode (known_gaps=%d archive_gaps=%d)",
				source.ID,
				sourcePreflight.Mode,
				sourcePreflight.KnownGapCount,
				sourcePreflight.ArchiveGapCount,
			)
		}
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelInfo,
			Event:     output.EventSourceFinished,
			SourceID:  source.ID,
			Message:   finishedMessage,
			Details: map[string]any{
				"planned_download_count": 0,
				"mode":                   sourcePreflight.Mode,
				"known_gap_count":        sourcePreflight.KnownGapCount,
				"archive_gap_count":      sourcePreflight.ArchiveGapCount,
			},
		})
		return outcome, nil
	}

	targetDir, err := config.ExpandPath(sourceForExec.TargetDir)
	if err != nil {
		if cleanupErr := cleanupTempStateFiles(stateSwap); cleanupErr != nil {
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelWarn,
				Event:     output.EventSourceFailed,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] unable to clean temporary state file: %v", source.ID, cleanupErr),
			})
		}
		return outcome, fmt.Errorf("[%s] resolve target_dir: %w", source.ID, err)
	}

	archivePath := strings.TrimSpace(sourceForExec.DownloadArchivePath)
	if archivePath == "" {
		archivePath, err = resolveSoundCloudArchivePath(sourceForExec, cfg.Defaults)
		if err != nil {
			if cleanupErr := cleanupTempStateFiles(stateSwap); cleanupErr != nil {
				_ = s.Emitter.Emit(output.Event{
					Timestamp: s.Now(),
					Level:     output.LevelWarn,
					Event:     output.EventSourceFailed,
					SourceID:  source.ID,
					Message:   fmt.Sprintf("[%s] unable to clean temporary state file: %v", source.ID, cleanupErr),
				})
			}
			return outcome, fmt.Errorf("[%s] resolve archive path: %w", source.ID, err)
		}
	}
	knownArchiveIDs, err := parseSoundCloudArchive(archivePath)
	if err != nil {
		if cleanupErr := cleanupTempStateFiles(stateSwap); cleanupErr != nil {
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelWarn,
				Event:     output.EventSourceFailed,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] unable to clean temporary state file: %v", source.ID, cleanupErr),
			})
		}
		return outcome, fmt.Errorf("[%s] parse archive file: %w", source.ID, err)
	}

	stuckLogPath, stuckPathErr := resolveSoundCloudFreeDLStuckLogPath(cfg.Defaults.StateDir, source.ID)
	if stuckPathErr != nil {
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelWarn,
			Event:     output.EventSourceStarted,
			SourceID:  source.ID,
			Message:   fmt.Sprintf("[%s] unable to resolve free-dl stuck log path: %v", source.ID, stuckPathErr),
		})
		stuckLogPath = ""
	}

	cleanupSuffixes := artifactSuffixesForAdapter("scdl")
	preArtifacts := map[string]struct{}{}
	if len(cleanupSuffixes) > 0 {
		preArtifacts, err = snapshotArtifacts(targetDir, cleanupSuffixes)
		if err != nil {
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelWarn,
				Event:     output.EventSourceStarted,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] unable to snapshot artifacts before run: %v", source.ID, err),
			})
			preArtifacts = map[string]struct{}{}
		}
	}

	_ = s.Emitter.Emit(output.Event{
		Timestamp: s.Now(),
		Level:     output.LevelInfo,
		Event:     output.EventSourcePreflight,
		SourceID:  source.ID,
		Message:   fmt.Sprintf("[%s] downloading to %s", source.ID, targetDir),
		Details: map[string]any{
			"target_dir": targetDir,
		},
	})

	_ = s.Emitter.Emit(output.Event{
		Timestamp: s.Now(),
		Level:     output.LevelInfo,
		Event:     output.EventSourceStarted,
		SourceID:  source.ID,
		Message:   fmt.Sprintf("[%s] running soundcloud free-download flow for %d track(s) (download_order=%s)", source.ID, len(plannedTracks), downloadOrder),
		Details: map[string]any{
			"planned_download_count": len(plannedTracks),
			"download_order":         string(downloadOrder),
		},
	})

	skippedNoLink := 0
	skippedUnsupportedHost := 0
	skippedHypedditTimeout := 0
	stuckLogCount := 0
	var failureDetails map[string]any
	failureMessage := ""
	for idx, track := range plannedTracks {
		displayName := strings.TrimSpace(track.Title)
		if displayName == "" {
			displayName = track.ID
		}
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelInfo,
			Event:     output.EventSourcePreflight,
			SourceID:  source.ID,
			Message:   fmt.Sprintf("[%s] free-dl track %d/%d %s (%s)", source.ID, idx+1, len(plannedTracks), track.ID, displayName),
		})

		metadata, metadataErr := fetchSoundCloudFreeDownloadMetadataFn(ctx, track)
		if errors.Is(metadataErr, errSoundCloudNoFreeDownloadLink) {
			skippedNoLink++
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelInfo,
				Event:     output.EventSourcePreflight,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] [skip] %s (%s) (no-free-download-link)", source.ID, track.ID, displayName),
			})
			continue
		}
		if metadataErr != nil {
			failureMessage = fmt.Sprintf("[%s] free-download metadata lookup failed for %s: %v", source.ID, track.ID, metadataErr)
			break
		}

		if !isHypedditPurchaseURL(metadata.PurchaseURL) {
			skippedUnsupportedHost++
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelInfo,
				Event:     output.EventSourcePreflight,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] [skip] %s (%s) (unsupported-free-download-host)", source.ID, track.ID, displayName),
				Details: map[string]any{
					"purchase_url": sanitizeSoundCloudFreeDownloadURL(metadata.PurchaseURL),
				},
			})
			continue
		}

		downloadsDir, dirErr := browserDownloadsDirFn()
		if dirErr != nil {
			failureMessage = fmt.Sprintf("[%s] browser download setup failed for %s: %v", source.ID, track.ID, dirErr)
			break
		}
		downloadsBefore, snapshotErr := snapshotMediaFiles(downloadsDir)
		if snapshotErr != nil {
			failureMessage = fmt.Sprintf("[%s] browser download setup failed for %s: %v", source.ID, track.ID, snapshotErr)
			break
		}
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelInfo,
			Event:     output.EventSourcePreflight,
			SourceID:  source.ID,
			Message:   fmt.Sprintf("[%s] [free-dl] hypeddit gate detected for %s; opening browser", source.ID, track.ID),
		})
		if openErr := openURLInBrowserFn(ctx, metadata.PurchaseURL); openErr != nil {
			if errors.Is(openErr, exec.ErrNotFound) {
				outcome.DependencyFailure = true
			}
			stuckRecord := soundCloudFreeDLStuckRecord{
				Timestamp:     s.Now().UTC().Format(time.RFC3339Nano),
				SourceID:      source.ID,
				TrackID:       track.ID,
				Title:         strings.TrimSpace(metadata.Title),
				Artist:        strings.TrimSpace(metadata.Artist),
				SoundCloudURL: strings.TrimSpace(metadata.SoundCloudURL),
				PurchaseURL:   sanitizeSoundCloudFreeDownloadURL(metadata.PurchaseURL),
				DownloadDir:   downloadsDir,
				Stage:         "browser-launch",
				Error:         openErr.Error(),
				Strategy:      "browser-handoff",
			}
			if appendErr := appendSoundCloudFreeDLStuckRecord(stuckLogPath, stuckRecord); appendErr == nil {
				stuckLogCount++
			}
			failureMessage = fmt.Sprintf("[%s] browser launch failed for %s: %v", source.ID, track.ID, openErr)
			failureDetails = map[string]any{
				"purchase_url": sanitizeSoundCloudFreeDownloadURL(metadata.PurchaseURL),
				"strategy":     "browser-handoff",
			}
			break
		}
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelInfo,
			Event:     output.EventSourcePreflight,
			SourceID:  source.ID,
			Message:   fmt.Sprintf("[%s] [free-dl] waiting for completed browser download for %s in %s", source.ID, track.ID, downloadsDir),
		})
		detectedPath, detectErr := detectBrowserDownloadedFileFn(ctx, downloadsDir, downloadsBefore, timeout, metadata)
		if detectErr != nil {
			if errors.Is(detectErr, context.Canceled) || errors.Is(detectErr, context.DeadlineExceeded) {
				s.cleanupArtifactsOnFailure(source.ID, targetDir, preArtifacts, cleanupSuffixes)
				if cleanupErr := cleanupTempStateFiles(stateSwap); cleanupErr != nil {
					_ = s.Emitter.Emit(output.Event{
						Timestamp: s.Now(),
						Level:     output.LevelWarn,
						Event:     output.EventSourceFailed,
						SourceID:  source.ID,
						Message:   fmt.Sprintf("[%s] unable to clean temporary state file: %v", source.ID, cleanupErr),
					})
				}
				outcome.Interrupted = true
				_ = s.Emitter.Emit(output.Event{
					Timestamp: s.Now(),
					Level:     output.LevelError,
					Event:     output.EventSourceFailed,
					SourceID:  source.ID,
					Message:   fmt.Sprintf("[%s] interrupted", source.ID),
				})
				return outcome, nil
			}
			if errors.Is(detectErr, errBrowserDownloadIdleTimeout) || errors.Is(detectErr, errBrowserDownloadMaxTimeout) {
				skippedHypedditTimeout++
				stuckRecord := soundCloudFreeDLStuckRecord{
					Timestamp:     s.Now().UTC().Format(time.RFC3339Nano),
					SourceID:      source.ID,
					TrackID:       track.ID,
					Title:         strings.TrimSpace(metadata.Title),
					Artist:        strings.TrimSpace(metadata.Artist),
					SoundCloudURL: strings.TrimSpace(metadata.SoundCloudURL),
					PurchaseURL:   sanitizeSoundCloudFreeDownloadURL(metadata.PurchaseURL),
					DownloadDir:   downloadsDir,
					Stage:         "browser-wait-timeout",
					Error:         detectErr.Error(),
					Strategy:      "browser-handoff",
				}
				if appendErr := appendSoundCloudFreeDLStuckRecord(stuckLogPath, stuckRecord); appendErr == nil {
					stuckLogCount++
				}
				_ = s.Emitter.Emit(output.Event{
					Timestamp: s.Now(),
					Level:     output.LevelWarn,
					Event:     output.EventSourcePreflight,
					SourceID:  source.ID,
					Message: fmt.Sprintf(
						"[%s] [skip] %s (%s) (hypeddit-timeout) %s",
						source.ID,
						track.ID,
						displayName,
						sanitizeSoundCloudFreeDownloadURL(metadata.PurchaseURL),
					),
					Details: map[string]any{
						"track_id":       track.ID,
						"title":          metadata.Title,
						"artist":         metadata.Artist,
						"soundcloud_url": metadata.SoundCloudURL,
						"purchase_url":   sanitizeSoundCloudFreeDownloadURL(metadata.PurchaseURL),
						"download_dir":   downloadsDir,
						"strategy":       "browser-handoff",
						"error":          detectErr.Error(),
					},
				})
				continue
			}
			stuckRecord := soundCloudFreeDLStuckRecord{
				Timestamp:     s.Now().UTC().Format(time.RFC3339Nano),
				SourceID:      source.ID,
				TrackID:       track.ID,
				Title:         strings.TrimSpace(metadata.Title),
				Artist:        strings.TrimSpace(metadata.Artist),
				SoundCloudURL: strings.TrimSpace(metadata.SoundCloudURL),
				PurchaseURL:   sanitizeSoundCloudFreeDownloadURL(metadata.PurchaseURL),
				DownloadDir:   downloadsDir,
				Stage:         "browser-detect",
				Error:         detectErr.Error(),
				Strategy:      "browser-handoff",
			}
			if appendErr := appendSoundCloudFreeDLStuckRecord(stuckLogPath, stuckRecord); appendErr == nil {
				stuckLogCount++
			}
			failureMessage = fmt.Sprintf("[%s] browser download failed for %s: %v", source.ID, track.ID, detectErr)
			failureDetails = map[string]any{
				"track_id":       track.ID,
				"title":          metadata.Title,
				"artist":         metadata.Artist,
				"soundcloud_url": metadata.SoundCloudURL,
				"purchase_url":   sanitizeSoundCloudFreeDownloadURL(metadata.PurchaseURL),
				"strategy":       "browser-handoff",
				"download_dir":   downloadsDir,
			}
			break
		}
		downloadedPath, moveErr := moveDownloadedMediaToTargetFn(detectedPath, targetDir)
		if moveErr != nil {
			stuckRecord := soundCloudFreeDLStuckRecord{
				Timestamp:     s.Now().UTC().Format(time.RFC3339Nano),
				SourceID:      source.ID,
				TrackID:       track.ID,
				Title:         strings.TrimSpace(metadata.Title),
				Artist:        strings.TrimSpace(metadata.Artist),
				SoundCloudURL: strings.TrimSpace(metadata.SoundCloudURL),
				PurchaseURL:   sanitizeSoundCloudFreeDownloadURL(metadata.PurchaseURL),
				DownloadDir:   downloadsDir,
				Stage:         "post-process-move",
				Error:         moveErr.Error(),
				Strategy:      "browser-handoff",
			}
			if appendErr := appendSoundCloudFreeDLStuckRecord(stuckLogPath, stuckRecord); appendErr == nil {
				stuckLogCount++
			}
			failureMessage = fmt.Sprintf("[%s] browser download post-processing failed for %s: %v", source.ID, track.ID, moveErr)
			failureDetails = map[string]any{
				"purchase_url": sanitizeSoundCloudFreeDownloadURL(metadata.PurchaseURL),
				"strategy":     "browser-handoff",
				"download_dir": downloadsDir,
				"source_path":  detectedPath,
			}
			break
		}

		if tagErr := applySoundCloudTrackMetadataFn(ctx, downloadedPath, metadata); tagErr != nil {
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelWarn,
				Event:     output.EventSourcePreflight,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] metadata tagging warning for %s: %v", source.ID, track.ID, tagErr),
			})
		}

		statePath := normalizeSoundCloudStatePath(targetDir, downloadedPath)
		if appendErr := appendSoundCloudSyncStateEntry(sourceForExec.StateFile, track.ID, statePath); appendErr != nil {
			failureMessage = fmt.Sprintf("[%s] failed to update soundcloud state file: %v", source.ID, appendErr)
			break
		}
		if _, exists := knownArchiveIDs[track.ID]; !exists {
			if appendErr := appendSoundCloudArchiveID(archivePath, track.ID); appendErr != nil {
				failureMessage = fmt.Sprintf("[%s] failed to update soundcloud archive file: %v", source.ID, appendErr)
				break
			}
			knownArchiveIDs[track.ID] = struct{}{}
		}

		doneLabel := soundCloudTrackDisplayName(metadata)
		if doneLabel == "" {
			doneLabel = displayName
		}
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelInfo,
			Event:     output.EventSourcePreflight,
			SourceID:  source.ID,
			Message:   fmt.Sprintf("[%s] [done] %s (%s)", source.ID, track.ID, doneLabel),
		})
	}

	if failureMessage != "" {
		s.cleanupArtifactsOnFailure(source.ID, targetDir, preArtifacts, cleanupSuffixes)
		if cleanupErr := cleanupTempStateFiles(stateSwap); cleanupErr != nil {
			_ = s.Emitter.Emit(output.Event{
				Timestamp: s.Now(),
				Level:     output.LevelWarn,
				Event:     output.EventSourceFailed,
				SourceID:  source.ID,
				Message:   fmt.Sprintf("[%s] unable to clean temporary state file: %v", source.ID, cleanupErr),
			})
		}
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelError,
			Event:     output.EventSourceFailed,
			SourceID:  source.ID,
			Message:   failureMessage,
			Details:   failureDetails,
		})
		return outcome, errors.New(failureMessage)
	}

	if err := commitTempStateFiles(stateSwap); err != nil {
		_ = s.Emitter.Emit(output.Event{
			Timestamp: s.Now(),
			Level:     output.LevelError,
			Event:     output.EventSourceFailed,
			SourceID:  source.ID,
			Message:   fmt.Sprintf("[%s] failed to finalize sync state file: %v", source.ID, err),
		})
		return outcome, err
	}

	outcome.Succeeded = true
	finishedDetails := map[string]any{
		"planned_download_count":   len(plannedTracks),
		"skipped_no_free_dl":       skippedNoLink,
		"skipped_unsupported_host": skippedUnsupportedHost,
		"skipped_hypeddit_timeout": skippedHypedditTimeout,
		"stuck_log_count":          stuckLogCount,
	}
	if strings.TrimSpace(stuckLogPath) != "" {
		finishedDetails["stuck_log_path"] = stuckLogPath
	}
	finishedMessage := fmt.Sprintf("[%s] completed", source.ID)
	if stuckLogCount > 0 && strings.TrimSpace(stuckLogPath) != "" {
		finishedMessage = fmt.Sprintf("[%s] completed (stuck=%d log=%s)", source.ID, stuckLogCount, stuckLogPath)
	}
	_ = s.Emitter.Emit(output.Event{
		Timestamp: s.Now(),
		Level:     output.LevelInfo,
		Event:     output.EventSourceFinished,
		SourceID:  source.ID,
		Message:   finishedMessage,
		Details:   finishedDetails,
	})
	return outcome, nil
}

func soundCloudTrackDisplayName(metadata soundCloudFreeDownloadMetadata) string {
	title := strings.TrimSpace(metadata.Title)
	artist := strings.TrimSpace(metadata.Artist)
	switch {
	case artist != "" && title != "":
		return artist + " - " + title
	case title != "":
		return title
	default:
		return strings.TrimSpace(metadata.ID)
	}
}
