package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/output"
)

func validateSoundCloudAppendOnlyMode(source config.Source, opts SyncOptions) error {
	if !source.Sync.AppendOnly {
		return nil
	}
	if err := config.ValidateAppendOnlySource(source); err != nil {
		return err
	}
	if opts.NoPreflight {
		return fmt.Errorf("sync.append_only requires preflight planning; remove --no-preflight")
	}
	return nil
}

// The downloader sees only private files. Even scdl's destructive --sync and its
// failure cleanup cannot access existing library files through the sync state.
func (s *Syncer) runSoundCloudAppendOnly(
	ctx context.Context, cfg config.Config, source config.Source, adapter Adapter,
	sourceForExec config.Source, preflight *SoundCloudPreflight,
	tracks []soundCloudRemoteTrack, order DownloadOrder, opts SyncOptions,
) sourceRunOutcome {
	fail := func(err error) sourceRunOutcome {
		_ = s.Emitter.Emit(output.Event{Timestamp: s.Now(), Level: output.LevelError, Event: output.EventSourceFailed, SourceID: source.ID, Message: fmt.Sprintf("[%s] append-only: %v", source.ID, err)})
		return sourceRunOutcome{Attempted: 1, Failed: 1, Stop: !cfg.Defaults.ContinueOnError}
	}
	if err := validateSoundCloudAppendOnlyMode(source, opts); err != nil {
		return fail(err)
	}
	if preflight == nil {
		return fail(fmt.Errorf("a preflight plan is required"))
	}
	if len(tracks) == 0 || opts.DryRun {
		_ = s.Emitter.Emit(output.Event{Timestamp: s.Now(), Level: output.LevelInfo, Event: output.EventSourceFinished, SourceID: source.ID, Message: fmt.Sprintf("[%s] append-only: %d new track(s) planned (download_order=%s)", source.ID, len(tracks), order)})
		return sourceRunOutcome{Attempted: 1, Succeeded: 1}
	}
	target, err := config.ExpandPath(source.TargetDir)
	if err != nil {
		return fail(err)
	}
	// Resolve symlinks once: scdl writes absolute paths, including /private/tmp on macOS.
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return fail(err)
	}
	stage, err := os.MkdirTemp(target, ".udl-append-*")
	if err != nil {
		return fail(err)
	}
	defer os.RemoveAll(stage)
	stageTarget := filepath.Join(stage, "downloads")
	if err := os.Mkdir(stageTarget, 0o700); err != nil {
		return fail(err)
	}
	statePath, err := config.ResolveStateFile(cfg.Defaults.StateDir, source.StateFile)
	if err != nil {
		return fail(err)
	}
	lockPath := statePath + ".append.lock"
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fail(fmt.Errorf("cannot lock source state %s (another append-only run or a stale lock): %w", lockPath, err))
	}
	defer os.Remove(lockPath)
	if err := lock.Close(); err != nil {
		return fail(err)
	}
	archivePath, err := resolveSoundCloudArchivePath(source, cfg.Defaults)
	if err != nil {
		return fail(err)
	}
	state, err := parseSoundCloudSyncState(statePath)
	if err != nil {
		return fail(err)
	}
	for _, entry := range state.Entries {
		fields := strings.Fields(entry.RawLine)
		if len(fields) > 0 && fields[0] == "soundcloud" && entry.ID == "" {
			return fail(fmt.Errorf("cannot identify an existing sync-state entry; refusing to re-download an unknown ID"))
		}
	}
	known, err := parseSoundCloudArchive(archivePath)
	if err != nil {
		return fail(err)
	}
	for id := range state.ByID {
		known[id] = struct{}{}
	}
	// Recheck after interactive selection; a known ID must never be re-downloaded.
	for _, track := range tracks {
		if _, exists := known[track.ID]; exists {
			return fail(fmt.Errorf("track %s is already recorded; rebuild the plan", track.ID))
		}
	}
	staged := sourceForExec
	staged.TargetDir = stageTarget
	staged.StateFile = filepath.Join(stage, "sync.scdl")
	staged.DownloadArchivePath = filepath.Join(stage, "archive.txt")
	knownIDs := make([]string, 0, len(known))
	for id := range known {
		knownIDs = append(knownIDs, id)
	}
	sort.Strings(knownIDs)
	var seededArchive strings.Builder
	for _, id := range knownIDs {
		fmt.Fprintf(&seededArchive, "soundcloud %s\n", id)
	}
	if err := os.WriteFile(staged.DownloadArchivePath, []byte(seededArchive.String()), 0o600); err != nil {
		return fail(err)
	}
	staged.AppendOnlyStaging = true
	staged.DisableSyncMode = false
	// Each URL is a single planned track, so changing remote playlist positions
	// between preflight and execution cannot download an existing or wrong ID.
	staged.SelectedPlaylistIDs = nil
	staged.Sync.DownloadOrder = ""
	noBreak := false
	staged.Sync.BreakOnExisting = &noBreak
	for _, track := range tracks {
		if strings.TrimSpace(track.URL) == "" || track.URL == "NA" {
			return fail(fmt.Errorf("track %s has no resolved URL; refusing an unsafe playlist download", track.ID))
		}
		staged.URL = track.URL
		// Fresh sync state prevents scdl deleting earlier staged downloads.
		if err := os.WriteFile(staged.StateFile, nil, 0o600); err != nil {
			return fail(err)
		}
		one := *preflight
		one.PlannedDownloadCount = 1
		originalEmitter := s.Emitter
		s.Emitter = soundCloudAppendTrackEmitter{originalEmitter}
		outcome := s.runGenericAdapter(ctx, cfg, source, adapter, staged, &one, soundCloudStateSwap{}, order, opts)
		s.Emitter = originalEmitter
		if outcome.Succeeded != 1 {
			return outcome
		}
		stagedState, err := parseSoundCloudSyncState(staged.StateFile)
		if err != nil {
			return fail(err)
		}
		if len(stagedState.ByID) != 1 {
			return fail(fmt.Errorf("expected one downloaded state entry for %s", track.ID))
		}
		entry, ok := stagedState.ByID[track.ID]
		if !ok {
			return fail(fmt.Errorf("downloaded ID differs from planned ID %s", track.ID))
		}
		if err := publishAppendOnlySoundCloudTrack(stageTarget, target, statePath, archivePath, entry); err != nil {
			return fail(err)
		}
	}
	_ = s.Emitter.Emit(output.Event{Timestamp: s.Now(), Level: output.LevelInfo, Event: output.EventSourceFinished, SourceID: source.ID, Message: fmt.Sprintf("[%s] appended %d new track(s) (download_order=%s)", source.ID, len(tracks), order)})
	return sourceRunOutcome{Attempted: 1, Succeeded: 1}
}

type soundCloudAppendTrackEmitter struct{ output.EventEmitter }

func (e soundCloudAppendTrackEmitter) Emit(event output.Event) error {
	if event.Event == output.EventSourceFinished {
		return nil
	}
	return e.EventEmitter.Emit(event)
}

func publishAppendOnlySoundCloudTrack(stage, target, statePath, archivePath string, entry soundCloudSyncEntry) error {
	path := entry.FilePath
	if !filepath.IsAbs(path) {
		path = filepath.Join(stage, path)
	}
	// Only a flat, regular staged file is publishable. No symlink traversal or
	// adapter-supplied paths outside staging are accepted.
	if filepath.Clean(filepath.Dir(path)) != filepath.Clean(stage) {
		return fmt.Errorf("downloaded path is outside the staging directory")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("downloaded path is not a regular file")
	}
	name := filepath.Base(path)
	if strings.ContainsAny(name, "\r\n") {
		return fmt.Errorf("downloaded filename contains a newline")
	}
	// Link is atomic and fails if any file or symlink occupies the destination.
	// The stage is on the same filesystem; removing it only unlinks its private name.
	publishedPath, err := filepath.Abs(filepath.Join(target, name))
	if err != nil {
		return err
	}
	if strings.ContainsAny(publishedPath, "\r\n") {
		return fmt.Errorf("published path contains a newline")
	}
	if err := os.Link(path, publishedPath); err != nil {
		return fmt.Errorf("cannot add %s without overwriting: %w", name, err)
	}
	if err := appendSoundCloudPreservingLines(statePath, fmt.Sprintf("soundcloud %s %s\n", entry.ID, publishedPath)); err != nil {
		return err
	}
	return appendSoundCloudPreservingLines(archivePath, fmt.Sprintf("soundcloud %s\n", entry.ID))
}

func appendSoundCloudPreservingLines(path, line string) error {
	payload, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(payload) > 0 && payload[len(payload)-1] != '\n' {
		line = "\n" + line
	}
	return appendLine(path, line)
}
