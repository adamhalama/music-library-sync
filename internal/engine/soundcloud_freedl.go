package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/output"
)

var (
	errSoundCloudNoFreeDownloadLink = errors.New("soundcloud track has no free-download link")
	errBrowserDownloadIdleTimeout   = errors.New("browser download idle timeout")
	errBrowserDownloadMaxTimeout    = errors.New("browser download max timeout")
)

var soundCloudBuyAnchorPattern = regexp.MustCompile(`(?i)<a href="([^"]+)">Buy[^<]*</a>`)

var (
	openURLInBrowserFn            = openURLInBrowser
	detectBrowserDownloadedFileFn = detectBrowserDownloadedFile
	browserDownloadsDirFn         = defaultBrowserDownloadsDir
	moveDownloadedMediaToTargetFn = moveDownloadedMediaToTarget
	runtimeGOOS                   = runtime.GOOS
	runBrowserCommandFn           = runBrowserCommand
	browserDownloadPollInterval   = 1 * time.Second
)

type soundCloudHydrationItem struct {
	Hydratable string          `json:"hydratable"`
	Data       json.RawMessage `json:"data"`
}

type soundCloudHydratedSound struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	Genre        string `json:"genre"`
	ArtworkURL   string `json:"artwork_url"`
	PurchaseURL  string `json:"purchase_url"`
	PermalinkURL string `json:"permalink_url"`
	User         struct {
		Username  string `json:"username"`
		AvatarURL string `json:"avatar_url"`
	} `json:"user"`
}

type soundCloudFreeDownloadMetadata struct {
	ID            string
	Title         string
	Artist        string
	Genre         string
	SoundCloudURL string
	ArtworkURL    string
	PurchaseURL   string
}

func (s *Syncer) runSoundCloudFreeDownloadSource(
	ctx context.Context,
	cfg config.Config,
	source config.Source,
	sourceForExec config.Source,
	sourcePreflight *SoundCloudPreflight,
	plannedTracks []soundCloudRemoteTrack,
	stateSwap soundCloudStateSwap,
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
		Message:   fmt.Sprintf("[%s] running soundcloud free-download flow for %d track(s)", source.ID, len(plannedTracks)),
		Details: map[string]any{
			"planned_download_count": len(plannedTracks),
		},
	})

	skippedNoLink := 0
	skippedUnsupportedHost := 0
	skippedHypedditTimeout := 0
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
	_ = s.Emitter.Emit(output.Event{
		Timestamp: s.Now(),
		Level:     output.LevelInfo,
		Event:     output.EventSourceFinished,
		SourceID:  source.ID,
		Message:   fmt.Sprintf("[%s] completed", source.ID),
		Details: map[string]any{
			"planned_download_count":   len(plannedTracks),
			"skipped_no_free_dl":       skippedNoLink,
			"skipped_unsupported_host": skippedUnsupportedHost,
			"skipped_hypeddit_timeout": skippedHypedditTimeout,
		},
	})
	return outcome, nil
}

func sanitizeSoundCloudFreeDownloadURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func normalizeSoundCloudStatePath(targetDir string, downloadedPath string) string {
	candidate := strings.TrimSpace(downloadedPath)
	if candidate == "" {
		return ""
	}
	abs := candidate
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(targetDir, abs)
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(targetDir, abs)
	if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return filepath.ToSlash(rel)
	}
	return abs
}

func appendSoundCloudSyncStateEntry(statePath string, trackID string, localPath string) error {
	id := strings.TrimSpace(trackID)
	path := strings.TrimSpace(localPath)
	if id == "" {
		return fmt.Errorf("soundcloud state append requires track id")
	}
	if path == "" {
		return fmt.Errorf("soundcloud state append requires local path")
	}
	return appendLine(statePath, fmt.Sprintf("soundcloud %s %s\n", id, path))
}

func appendSoundCloudArchiveID(archivePath string, trackID string) error {
	id := strings.TrimSpace(trackID)
	if id == "" {
		return fmt.Errorf("soundcloud archive append requires track id")
	}
	return appendLine(archivePath, fmt.Sprintf("soundcloud %s\n", id))
}

func appendLine(path string, line string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return fmt.Errorf("append target path must not be empty")
	}
	if err := os.MkdirAll(filepath.Dir(trimmed), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(trimmed, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()
	if _, err := file.WriteString(line); err != nil {
		return err
	}
	return file.Sync()
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

func fetchSoundCloudFreeDownloadMetadata(ctx context.Context, track soundCloudRemoteTrack) (soundCloudFreeDownloadMetadata, error) {
	metadata := soundCloudFreeDownloadMetadata{
		ID:            strings.TrimSpace(track.ID),
		Title:         strings.TrimSpace(track.Title),
		SoundCloudURL: strings.TrimSpace(track.URL),
	}
	trackURL := strings.TrimSpace(track.URL)
	if trackURL == "" {
		return metadata, fmt.Errorf("soundcloud track %q has empty url", track.ID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trackURL, nil)
	if err != nil {
		return metadata, fmt.Errorf("create soundcloud track page request: %w", err)
	}
	req.Header.Set("User-Agent", "udl/soundcloud-freedl")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return metadata, fmt.Errorf("soundcloud track page request failed: %w", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return metadata, fmt.Errorf("read soundcloud track page response: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return metadata, fmt.Errorf("soundcloud track page request failed: status=%d", resp.StatusCode)
	}

	document := string(body)
	if hydrated, hydrationErr := parseSoundCloudHydratedSound(document); hydrationErr == nil {
		if hydrated.ID != 0 && metadata.ID == "" {
			metadata.ID = strconv.FormatInt(hydrated.ID, 10)
		}
		if title := strings.TrimSpace(hydrated.Title); title != "" {
			metadata.Title = title
		}
		if artist := strings.TrimSpace(hydrated.User.Username); artist != "" {
			metadata.Artist = artist
		}
		if genre := strings.TrimSpace(hydrated.Genre); genre != "" {
			metadata.Genre = genre
		}
		if url := strings.TrimSpace(hydrated.PermalinkURL); url != "" {
			metadata.SoundCloudURL = url
		}
		if artworkURL := strings.TrimSpace(hydrated.ArtworkURL); artworkURL != "" {
			metadata.ArtworkURL = resolveRelativeURL(trackURL, resolveSoundCloudArtworkURL(artworkURL))
		}
		if strings.TrimSpace(metadata.ArtworkURL) == "" {
			if avatarURL := strings.TrimSpace(hydrated.User.AvatarURL); avatarURL != "" {
				metadata.ArtworkURL = resolveRelativeURL(trackURL, resolveSoundCloudArtworkURL(avatarURL))
			}
		}
		if purchaseURL := strings.TrimSpace(hydrated.PurchaseURL); purchaseURL != "" {
			metadata.PurchaseURL = resolveRelativeURL(trackURL, purchaseURL)
		}
	}
	if strings.TrimSpace(metadata.PurchaseURL) == "" {
		if fallback := extractSoundCloudBuyURLFallback(document); fallback != "" {
			metadata.PurchaseURL = resolveRelativeURL(trackURL, fallback)
		}
	}
	if strings.TrimSpace(metadata.PurchaseURL) == "" {
		return metadata, errSoundCloudNoFreeDownloadLink
	}
	return metadata, nil
}

func parseSoundCloudHydratedSound(document string) (soundCloudHydratedSound, error) {
	payload, err := extractSoundCloudHydrationPayload(document)
	if err != nil {
		return soundCloudHydratedSound{}, err
	}

	items := []soundCloudHydrationItem{}
	if err := json.Unmarshal([]byte(payload), &items); err != nil {
		return soundCloudHydratedSound{}, fmt.Errorf("decode soundcloud hydration payload: %w", err)
	}
	for _, item := range items {
		if item.Hydratable != "sound" {
			continue
		}
		var sound soundCloudHydratedSound
		if err := json.Unmarshal(item.Data, &sound); err != nil {
			continue
		}
		if sound.ID != 0 || strings.TrimSpace(sound.Title) != "" {
			return sound, nil
		}
	}
	return soundCloudHydratedSound{}, fmt.Errorf("soundcloud hydration payload missing sound entry")
}

func extractSoundCloudHydrationPayload(document string) (string, error) {
	const prefix = "window.__sc_hydration = "
	start := strings.Index(document, prefix)
	if start < 0 {
		return "", fmt.Errorf("soundcloud hydration payload not found")
	}
	start += len(prefix)

	suffixOffset := strings.Index(document[start:], ";</script>")
	if suffixOffset < 0 {
		suffixOffset = strings.Index(document[start:], "</script>")
	}
	if suffixOffset < 0 {
		return "", fmt.Errorf("soundcloud hydration payload terminator not found")
	}
	payload := strings.TrimSpace(document[start : start+suffixOffset])
	payload = strings.TrimSuffix(payload, ";")
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return "", fmt.Errorf("soundcloud hydration payload is empty")
	}
	return payload, nil
}

func extractSoundCloudBuyURLFallback(document string) string {
	match := soundCloudBuyAnchorPattern.FindStringSubmatch(document)
	if len(match) != 2 {
		return ""
	}
	return strings.TrimSpace(html.UnescapeString(match[1]))
}

func resolveRelativeURL(base string, candidate string) string {
	raw := strings.TrimSpace(candidate)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if parsed.IsAbs() {
		return parsed.String()
	}
	baseParsed, baseErr := url.Parse(strings.TrimSpace(base))
	if baseErr != nil {
		return raw
	}
	return baseParsed.ResolveReference(parsed).String()
}

func resolveSoundCloudArtworkURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	// SoundCloud commonly serves `-large` assets by default; prefer a higher-res variant for embedded covers.
	return strings.Replace(trimmed, "-large.", "-t500x500.", 1)
}

func isHypedditPurchaseURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "hypeddit.com" {
		return true
	}
	return strings.HasSuffix(host, ".hypeddit.com")
}

func defaultBrowserDownloadsDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv("UDL_FREEDL_BROWSER_DOWNLOAD_DIR")); override != "" {
		return config.ExpandPath(override)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Downloads"), nil
}

func openURLInBrowser(ctx context.Context, rawURL string) error {
	bin, args, err := browserOpenCommand(rawURL)
	if err != nil {
		return err
	}
	if err := runBrowserCommandFn(ctx, bin, args...); err != nil {
		return fmt.Errorf("browser launch command failed: %w", err)
	}
	return nil
}

func browserOpenCommand(rawURL string) (string, []string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", nil, fmt.Errorf("browser url is empty")
	}

	// macOS app name or bundle path (for example: "Helium" or "/Applications/Helium.app")
	browserApp := strings.TrimSpace(os.Getenv("UDL_FREEDL_BROWSER_APP"))
	switch runtimeGOOS {
	case "darwin":
		if browserApp != "" {
			return "open", []string{"-a", browserApp, trimmed}, nil
		}
		return "open", []string{trimmed}, nil
	case "linux":
		return "xdg-open", []string{trimmed}, nil
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", trimmed}, nil
	default:
		return "", nil, fmt.Errorf("unsupported platform for browser handoff: %s", runtimeGOOS)
	}
}

func runBrowserCommand(ctx context.Context, bin string, args ...string) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, detail)
	}
	return nil
}

func detectBrowserDownloadedFile(
	ctx context.Context,
	downloadsDir string,
	before map[string]mediaFileSnapshot,
	timeout time.Duration,
	metadata soundCloudFreeDownloadMetadata,
) (string, error) {
	dir := strings.TrimSpace(downloadsDir)
	if dir == "" {
		return "", fmt.Errorf("browser download directory is empty")
	}
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	idleTimeout := resolveBrowserDownloadIdleTimeout(timeout)
	if idleTimeout <= 0 {
		idleTimeout = 1 * time.Minute
	}
	startedAt := time.Now()
	absoluteDeadline := startedAt.Add(timeout)
	lastProgressAt := startedAt
	lastCandidate := ""
	lastCandidateSnapshot := mediaFileSnapshot{}
	lastCandidateSnapshotSet := false
	stableSamples := 0
	inProgressBefore, _ := snapshotBrowserInProgressFiles(dir)

	for {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		now := time.Now()
		if now.After(absoluteDeadline) {
			return "", fmt.Errorf("%w in %s (max_wait=%s)", errBrowserDownloadMaxTimeout, dir, timeout)
		}
		if now.Sub(lastProgressAt) >= idleTimeout {
			return "", fmt.Errorf("%w in %s (idle_for=%s)", errBrowserDownloadIdleTimeout, dir, idleTimeout)
		}

		after, err := snapshotMediaFiles(dir)
		if err == nil {
			candidate := selectBrowserDownloadCandidate(before, after, metadata)
			if candidate != "" {
				abs := filepath.Join(dir, filepath.FromSlash(candidate))
				candidateSnapshot := after[candidate]
				if abs == lastCandidate &&
					lastCandidateSnapshotSet &&
					candidateSnapshot.Size == lastCandidateSnapshot.Size &&
					!candidateSnapshot.ModTime.After(lastCandidateSnapshot.ModTime) {
					stableSamples++
				} else {
					lastCandidate = abs
					stableSamples = 1
					lastProgressAt = now
					lastCandidateSnapshotSet = true
				}
				lastCandidateSnapshot = candidateSnapshot
				if stableSamples >= 2 {
					return abs, nil
				}
			}
		}
		inProgressAfter, snapshotErr := snapshotBrowserInProgressFiles(dir)
		if snapshotErr == nil {
			if hasBrowserInProgressActivity(inProgressBefore, inProgressAfter) {
				lastProgressAt = now
			}
			inProgressBefore = inProgressAfter
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(browserDownloadPollInterval):
		}
	}
}

func resolveBrowserDownloadIdleTimeout(maxWait time.Duration) time.Duration {
	idle := 1 * time.Minute
	if override := strings.TrimSpace(os.Getenv("UDL_FREEDL_BROWSER_IDLE_TIMEOUT")); override != "" {
		if parsed, err := time.ParseDuration(override); err == nil && parsed > 0 {
			idle = parsed
		}
	}
	if maxWait > 0 && idle > maxWait {
		return maxWait
	}
	return idle
}

func snapshotBrowserInProgressFiles(dir string) (map[string]mediaFileSnapshot, error) {
	snapshots := map[string]mediaFileSnapshot{}
	root := strings.TrimSpace(dir)
	if root == "" {
		return snapshots, nil
	}
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return snapshots, nil
		}
		return nil, err
	}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		name := strings.ToLower(strings.TrimSpace(d.Name()))
		if d.IsDir() {
			if !isBrowserInProgressName(name) {
				return nil
			}
			info, infoErr := d.Info()
			if infoErr != nil {
				return infoErr
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			snapshots[filepath.ToSlash(rel)] = mediaFileSnapshot{
				Size:    0,
				ModTime: info.ModTime(),
			}
			return filepath.SkipDir
		}
		if !isBrowserInProgressName(name) {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		snapshots[filepath.ToSlash(rel)] = mediaFileSnapshot{
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return snapshots, nil
}

func isBrowserInProgressName(name string) bool {
	switch {
	case strings.HasSuffix(name, ".crdownload"):
		return true
	case strings.HasSuffix(name, ".download"):
		return true
	case strings.HasSuffix(name, ".part"):
		return true
	case strings.HasSuffix(name, ".partial"):
		return true
	case strings.HasSuffix(name, ".tmp"):
		return true
	case strings.HasSuffix(name, ".opdownload"):
		return true
	case strings.HasSuffix(name, ".aria2"):
		return true
	default:
		return false
	}
}

func hasBrowserInProgressActivity(
	before map[string]mediaFileSnapshot,
	after map[string]mediaFileSnapshot,
) bool {
	if len(before) != len(after) {
		return true
	}
	for rel, current := range after {
		previous, exists := before[rel]
		if !exists {
			return true
		}
		if current.Size != previous.Size {
			return true
		}
		if current.ModTime.After(previous.ModTime) {
			return true
		}
	}
	return false
}

func selectBrowserDownloadCandidate(
	before map[string]mediaFileSnapshot,
	after map[string]mediaFileSnapshot,
	metadata soundCloudFreeDownloadMetadata,
) string {
	type candidate struct {
		Rel     string
		ModTime time.Time
		Score   int
	}

	candidates := make([]candidate, 0)
	expectedTitle := normalizeTrackKey(metadata.Title)
	expectedArtist := normalizeTrackKey(metadata.Artist)
	for rel, current := range after {
		previous, existed := before[rel]
		if existed &&
			current.Size == previous.Size &&
			!current.ModTime.After(previous.ModTime) {
			continue
		}
		stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel)))
		key := normalizeTrackKey(stem)
		score := 0
		if expectedTitle != "" && key != "" && strings.Contains(key, expectedTitle) {
			score += 2
		}
		if expectedArtist != "" && key != "" && strings.Contains(key, expectedArtist) {
			score++
		}
		candidates = append(candidates, candidate{
			Rel:     rel,
			ModTime: current.ModTime,
			Score:   score,
		})
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if !candidates[i].ModTime.Equal(candidates[j].ModTime) {
			return candidates[i].ModTime.After(candidates[j].ModTime)
		}
		return candidates[i].Rel < candidates[j].Rel
	})
	return candidates[0].Rel
}

func moveDownloadedMediaToTarget(sourcePath string, targetDir string) (string, error) {
	src := strings.TrimSpace(sourcePath)
	if src == "" {
		return "", fmt.Errorf("source path is empty")
	}
	destRoot := strings.TrimSpace(targetDir)
	if destRoot == "" {
		return "", fmt.Errorf("target_dir is empty")
	}
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		return "", err
	}
	base := filepath.Base(src)
	dest := filepath.Join(destRoot, base)
	dest = nextAvailablePath(dest)
	if err := os.Rename(src, dest); err == nil {
		return dest, nil
	}

	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = in.Close()
	}()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dest)
		return "", err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dest)
		return "", err
	}
	if err := os.Remove(src); err != nil {
		return "", err
	}
	return dest, nil
}

func nextAvailablePath(path string) string {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return path
	}
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
	}
}

func applySoundCloudTrackMetadata(ctx context.Context, filePath string, metadata soundCloudFreeDownloadMetadata) error {
	trimmed := strings.TrimSpace(filePath)
	if trimmed == "" {
		return fmt.Errorf("empty file path")
	}
	info, err := os.Stat(trimmed)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory: %s", trimmed)
	}

	tempFile, err := os.CreateTemp(filepath.Dir(trimmed), ".udl-meta-*"+filepath.Ext(trimmed))
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	_ = tempFile.Close()
	_ = os.Remove(tempPath)

	artworkPath := ""
	artworkEmbedErr := error(nil)
	if artworkURL := strings.TrimSpace(metadata.ArtworkURL); artworkURL != "" {
		downloadedArtworkPath, artworkErr := downloadSoundCloudArtwork(ctx, artworkURL, filepath.Dir(trimmed))
		if artworkErr == nil {
			artworkPath = downloadedArtworkPath
			defer func() {
				_ = os.Remove(downloadedArtworkPath)
			}()
		} else {
			artworkEmbedErr = fmt.Errorf("artwork download failed: %w", artworkErr)
		}
	}

	if artworkPath != "" {
		if err := runSoundCloudMetadataFFmpeg(ctx, trimmed, tempPath, metadata, artworkPath); err == nil {
			return replaceTaggedMediaFile(tempPath, trimmed)
		} else {
			artworkEmbedErr = err
			_ = os.Remove(tempPath)
		}
	}

	if err := runSoundCloudMetadataFFmpeg(ctx, trimmed, tempPath, metadata, ""); err != nil {
		if artworkEmbedErr == nil {
			return err
		}
		return fmt.Errorf("artwork embedding failed (%v) and metadata fallback failed (%w)", artworkEmbedErr, err)
	}
	if err := replaceTaggedMediaFile(tempPath, trimmed); err != nil {
		return err
	}
	if artworkEmbedErr != nil {
		return fmt.Errorf("metadata written without artwork: %v", artworkEmbedErr)
	}
	return nil
}

func runSoundCloudMetadataFFmpeg(
	ctx context.Context,
	inputPath string,
	outputPath string,
	metadata soundCloudFreeDownloadMetadata,
	artworkPath string,
) error {
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", inputPath,
		"-map", "0",
		"-codec", "copy",
	}
	if strings.TrimSpace(artworkPath) != "" {
		args = append(args, "-i", artworkPath)
		args = append(args, "-map", "1:0")
		args = append(args, "-c:v", "mjpeg", "-disposition:v:0", "attached_pic")
	}
	if title := strings.TrimSpace(metadata.Title); title != "" {
		args = append(args, "-metadata", "title="+title)
	}
	if artist := strings.TrimSpace(metadata.Artist); artist != "" {
		args = append(args, "-metadata", "artist="+artist)
		args = append(args, "-metadata", "album_artist="+artist)
	}
	if genre := strings.TrimSpace(metadata.Genre); genre != "" {
		args = append(args, "-metadata", "genre="+genre)
	}
	if sourceURL := strings.TrimSpace(metadata.SoundCloudURL); sourceURL != "" {
		args = append(args, "-metadata", "comment="+sourceURL)
	}
	args = append(args, outputPath)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	output, runErr := cmd.CombinedOutput()
	if runErr != nil {
		_ = os.Remove(outputPath)
		trimmedOutput := strings.TrimSpace(string(output))
		if trimmedOutput == "" {
			return runErr
		}
		return fmt.Errorf("%v: %s", runErr, trimmedOutput)
	}
	return nil
}

func replaceTaggedMediaFile(tempPath string, targetPath string) error {
	if err := os.Rename(tempPath, targetPath); err != nil {
		if removeErr := os.Remove(targetPath); removeErr != nil {
			_ = os.Remove(tempPath)
			return err
		}
		if retryErr := os.Rename(tempPath, targetPath); retryErr != nil {
			_ = os.Remove(tempPath)
			return retryErr
		}
	}
	return nil
}

func downloadSoundCloudArtwork(ctx context.Context, rawURL string, tempDir string) (string, error) {
	resolvedURL := strings.TrimSpace(resolveSoundCloudArtworkURL(rawURL))
	if resolvedURL == "" {
		return "", fmt.Errorf("empty artwork url")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resolvedURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "udl/soundcloud-freedl")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("artwork request failed: status=%d", resp.StatusCode)
	}

	pathHint := ""
	if resp.Request != nil && resp.Request.URL != nil {
		pathHint = resp.Request.URL.Path
	}
	ext := inferArtworkFileExtension(resp.Header.Get("Content-Type"), pathHint)
	tmp, err := os.CreateTemp(tempDir, ".udl-artwork-*"+ext)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

func inferArtworkFileExtension(contentType string, pathHint string) string {
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(pathHint)))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp":
		return ext
	}
	normalized := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch normalized {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/jpg", "image/jpeg":
		return ".jpg"
	default:
		return ".jpg"
	}
}
