package engine

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/jaa/update-downloads/internal/config"
)

type soundCloudRemoteTrack struct {
	ID    string
	Title string
	URL   string
}

type soundCloudSyncEntry struct {
	RawLine  string
	ID       string
	FilePath string
}

type soundCloudSyncState struct {
	Entries []soundCloudSyncEntry
	ByID    map[string]soundCloudSyncEntry
}

type idSet map[string]struct{}

func effectiveSoundCloudListURL(source config.Source) string {
	base := strings.TrimSpace(source.URL)
	mode := detectSoundCloudMode(source.Adapter.ExtraArgs)
	switch mode {
	case "-t":
		return appendURLPath(base, "tracks")
	case "-f":
		return appendURLPath(base, "likes")
	case "-C":
		return appendURLPath(base, "comments")
	case "-p":
		return appendURLPath(base, "sets")
	case "-r":
		return appendURLPath(base, "reposts")
	default:
		return base
	}
}

func detectSoundCloudMode(args []string) string {
	if hasFlagArg(args, "-a") {
		return "-a"
	}
	if hasFlagArg(args, "-t") {
		return "-t"
	}
	if hasFlagArg(args, "-f") {
		return "-f"
	}
	if hasFlagArg(args, "-C") {
		return "-C"
	}
	if hasFlagArg(args, "-p") {
		return "-p"
	}
	if hasFlagArg(args, "-r") {
		return "-r"
	}
	// Match scdl default behavior: when no mode is specified, download likes.
	return "-f"
}

func appendURLPath(base string, segment string) string {
	trimmedBase := strings.TrimSuffix(strings.TrimSpace(base), "/")
	if trimmedBase == "" {
		return base
	}
	if strings.HasSuffix(trimmedBase, "/"+segment) {
		return trimmedBase
	}
	return trimmedBase + "/" + segment
}

func hasFlagArg(args []string, candidate string) bool {
	for _, arg := range args {
		if strings.TrimSpace(arg) == candidate {
			return true
		}
	}
	return false
}

func enumerateSoundCloudTracks(ctx context.Context, source config.Source) ([]soundCloudRemoteTrack, error) {
	listURL := effectiveSoundCloudListURL(source)
	cmd := exec.CommandContext(
		ctx,
		"yt-dlp",
		"--flat-playlist",
		"--print",
		"%(id)s\t%(title)s\t%(webpage_url)s",
		listURL,
	)
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("yt-dlp preflight failed for %s: %s", listURL, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("yt-dlp preflight failed for %s: %w", listURL, err)
	}
	return parseSoundCloudTrackList(output), nil
}

func parseSoundCloudTrackList(payload []byte) []soundCloudRemoteTrack {
	scanner := bufio.NewScanner(bytes.NewReader(payload))
	tracks := make([]soundCloudRemoteTrack, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) == 0 {
			continue
		}
		id := strings.TrimSpace(parts[0])
		if id == "" || id == "NA" {
			continue
		}
		title := ""
		url := ""
		if len(parts) > 1 {
			title = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 {
			url = strings.TrimSpace(parts[2])
		}
		tracks = append(tracks, soundCloudRemoteTrack{
			ID:    id,
			Title: title,
			URL:   url,
		})
	}
	return tracks
}

func parseSoundCloudSyncState(path string) (soundCloudSyncState, error) {
	state := soundCloudSyncState{
		Entries: make([]soundCloudSyncEntry, 0),
		ByID:    map[string]soundCloudSyncEntry{},
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state, nil
		}
		return state, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(payload))
	for scanner.Scan() {
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}

		entry := soundCloudSyncEntry{RawLine: raw}
		parts := strings.SplitN(raw, " ", 3)
		if len(parts) == 3 && strings.TrimSpace(parts[0]) == "soundcloud" {
			entry.ID = strings.TrimSpace(parts[1])
			entry.FilePath = strings.TrimSpace(parts[2])
			if entry.ID != "" {
				state.ByID[entry.ID] = entry
			}
		}
		state.Entries = append(state.Entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return state, err
	}
	return state, nil
}

func parseSoundCloudArchive(path string) (idSet, error) {
	known := idSet{}
	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return known, nil
		}
		return nil, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(payload))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[0] != "soundcloud" {
			continue
		}
		id := strings.TrimSpace(fields[1])
		if id == "" {
			continue
		}
		known[id] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return known, nil
}

func resolveSoundCloudArchivePath(source config.Source, defaults config.Defaults) (string, error) {
	if raw, ok := extractYTDLPArgs(source.Adapter.ExtraArgs); ok {
		if archiveArg, found := extractDownloadArchiveArg(raw); found {
			return config.ResolveArchiveFile(defaults.StateDir, archiveArg, source.ID)
		}
	}
	return config.ResolveArchiveFile(defaults.StateDir, defaults.ArchiveFile, source.ID)
}

func extractYTDLPArgs(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		token := strings.TrimSpace(args[i])
		if token == "--yt-dlp-args" {
			if i+1 >= len(args) {
				return "", false
			}
			return strings.TrimSpace(args[i+1]), true
		}
		if strings.HasPrefix(token, "--yt-dlp-args=") {
			return strings.TrimSpace(strings.TrimPrefix(token, "--yt-dlp-args=")), true
		}
	}
	return "", false
}

func extractDownloadArchiveArg(raw string) (string, bool) {
	tokens := strings.Fields(strings.TrimSpace(raw))
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if token == "--download-archive" {
			if i+1 >= len(tokens) {
				return "", false
			}
			return strings.TrimSpace(tokens[i+1]), true
		}
		if strings.HasPrefix(token, "--download-archive=") {
			value := strings.TrimSpace(strings.TrimPrefix(token, "--download-archive="))
			if value != "" {
				return value, true
			}
			return "", false
		}
	}
	return "", false
}

func buildSoundCloudPreflight(
	remoteTracks []soundCloudRemoteTrack,
	state soundCloudSyncState,
	archiveKnownIDs idSet,
	targetDir string,
	mode SoundCloudMode,
) (SoundCloudPreflight, map[string]struct{}, map[string]struct{}, map[string]struct{}) {
	knownGapIDs := map[string]struct{}{}
	archiveGapIDs := map[string]struct{}{}
	localMediaByTitle := scanLocalMediaTitleIndex(targetDir)
	availableLocalTitles := copyTitleCountMap(localMediaByTitle)

	knownCount := 0
	firstExisting := 0
	for i, track := range remoteTracks {
		entry, knownFromState := state.ByID[track.ID]
		_, knownFromArchive := archiveKnownIDs[track.ID]
		if !knownFromState && !knownFromArchive {
			archiveGapIDs[track.ID] = struct{}{}
			continue
		}

		knownCount++
		hasLocal := false

		// Known gaps are informational only. They represent items already known
		// in archive/state but not currently present as local media.
		if knownFromState {
			if stateEntryHasLocalFile(entry.FilePath, targetDir) {
				hasLocal = true
			}
			if !hasLocal && consumeLocalTitleMatch(availableLocalTitles, track.Title) {
				hasLocal = true
			}
		} else if consumeLocalTitleMatch(availableLocalTitles, track.Title) {
			hasLocal = true
		}
		if hasLocal {
			if firstExisting == 0 {
				firstExisting = i + 1
			}
		} else {
			knownGapIDs[track.ID] = struct{}{}
		}
	}

	planned := map[string]struct{}{}
	switch mode {
	case SoundCloudModeScanGaps:
		for id := range archiveGapIDs {
			planned[id] = struct{}{}
		}
		for id := range knownGapIDs {
			planned[id] = struct{}{}
		}
	default:
		limit := len(remoteTracks)
		if firstExisting > 0 {
			limit = firstExisting - 1
		}
		for i := 0; i < limit; i++ {
			id := remoteTracks[i].ID
			if _, gap := archiveGapIDs[id]; gap {
				planned[id] = struct{}{}
			}
			if _, knownGap := knownGapIDs[id]; knownGap {
				planned[id] = struct{}{}
			}
		}
	}

	preflight := SoundCloudPreflight{
		RemoteTotal:          len(remoteTracks),
		KnownCount:           knownCount,
		ArchiveGapCount:      len(archiveGapIDs),
		KnownGapCount:        len(knownGapIDs),
		FirstExistingIndex:   firstExisting,
		PlannedDownloadCount: len(planned),
		Mode:                 mode,
	}
	return preflight, archiveGapIDs, knownGapIDs, planned
}

func stateEntryHasLocalFile(rawPath, targetDir string) bool {
	fullPath := strings.TrimSpace(rawPath)
	if fullPath == "" {
		return false
	}
	if !filepath.IsAbs(fullPath) {
		fullPath = filepath.Join(targetDir, fullPath)
	}
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		return false
	}
	return true
}

func writeFilteredSyncStateFile(
	originalPath string,
	state soundCloudSyncState,
	removeIDs map[string]struct{},
) (string, error) {
	stateDir := filepath.Dir(originalPath)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return "", err
	}

	tempFile, err := os.CreateTemp(stateDir, ".udl-sync-*.scdl")
	if err != nil {
		return "", err
	}
	defer func() {
		_ = tempFile.Close()
	}()

	for _, entry := range state.Entries {
		if entry.ID != "" {
			if _, shouldDrop := removeIDs[entry.ID]; shouldDrop {
				continue
			}
		}
		if _, writeErr := tempFile.WriteString(entry.RawLine + "\n"); writeErr != nil {
			_ = os.Remove(tempFile.Name())
			return "", writeErr
		}
	}

	if err := tempFile.Sync(); err != nil {
		_ = os.Remove(tempFile.Name())
		return "", err
	}
	return tempFile.Name(), nil
}

func writeFilteredArchiveFile(originalPath string, removeIDs map[string]struct{}) (string, error) {
	archiveDir := filepath.Dir(originalPath)
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return "", err
	}

	tempFile, err := os.CreateTemp(archiveDir, ".udl-archive-*.txt")
	if err != nil {
		return "", err
	}
	defer func() {
		_ = tempFile.Close()
	}()

	payload, readErr := os.ReadFile(originalPath)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		_ = os.Remove(tempFile.Name())
		return "", readErr
	}

	if len(payload) > 0 {
		scanner := bufio.NewScanner(bytes.NewReader(payload))
		for scanner.Scan() {
			raw := scanner.Text()
			line := strings.TrimSpace(raw)
			if line == "" {
				if _, err := tempFile.WriteString("\n"); err != nil {
					_ = os.Remove(tempFile.Name())
					return "", err
				}
				continue
			}

			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == "soundcloud" {
				if _, shouldDrop := removeIDs[fields[1]]; shouldDrop {
					continue
				}
			}
			if _, err := tempFile.WriteString(raw + "\n"); err != nil {
				_ = os.Remove(tempFile.Name())
				return "", err
			}
		}
		if err := scanner.Err(); err != nil {
			_ = os.Remove(tempFile.Name())
			return "", err
		}
	}

	if err := tempFile.Sync(); err != nil {
		_ = os.Remove(tempFile.Name())
		return "", err
	}
	return tempFile.Name(), nil
}

func scanLocalMediaTitleIndex(targetDir string) map[string]int {
	index := map[string]int{}
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return index
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if !isMediaExt(ext) {
			continue
		}
		stem := strings.TrimSpace(strings.TrimSuffix(name, ext))
		key := normalizeTrackKey(stem)
		if key == "" {
			continue
		}
		index[key]++
	}
	return index
}

func isMediaExt(ext string) bool {
	switch ext {
	case ".m4a", ".mp3", ".flac", ".opus", ".ogg", ".wav", ".aac":
		return true
	default:
		return false
	}
}

func normalizeTrackKey(raw string) string {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	if trimmed == "" {
		return ""
	}

	var b strings.Builder
	prevSpace := false
	for _, r := range trimmed {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevSpace = false
		case unicode.IsSpace(r):
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
		default:
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func copyTitleCountMap(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func consumeLocalTitleMatch(available map[string]int, title string) bool {
	key := normalizeTrackKey(title)
	if key == "" {
		return false
	}
	count := available[key]
	if count <= 0 {
		return false
	}
	available[key] = count - 1
	return true
}
