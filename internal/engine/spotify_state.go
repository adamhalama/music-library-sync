package engine

import (
	"bufio"
	"bytes"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type spotifyStateEntry struct {
	DisplayName string
	LocalPath   string
}

type spotifySyncState struct {
	KnownIDs map[string]struct{}
	Entries  map[string]spotifyStateEntry
}

func parseSpotifySyncState(path string) (spotifySyncState, error) {
	state := spotifySyncState{
		KnownIDs: map[string]struct{}{},
		Entries:  map[string]spotifyStateEntry{},
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
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		id, entry := parseSpotifyStateLine(line)
		if id == "" {
			continue
		}
		state.KnownIDs[id] = struct{}{}
		if entry.DisplayName != "" || entry.LocalPath != "" {
			existing := state.Entries[id]
			if entry.DisplayName != "" {
				existing.DisplayName = entry.DisplayName
			}
			if entry.LocalPath != "" {
				existing.LocalPath = entry.LocalPath
			}
			state.Entries[id] = existing
		}
	}
	if err := scanner.Err(); err != nil {
		return state, err
	}
	return state, nil
}

func appendSpotifySyncStateID(path string, id string) error {
	return appendSpotifySyncStateEntry(path, id, "", "")
}

func appendSpotifySyncStateEntry(path string, id string, displayName string, localPath string) error {
	trackID := extractSpotifyTrackID(id)
	if trackID == "" {
		return errors.New("spotify track id must not be empty")
	}

	stateDir := filepath.Dir(path)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}

	writeHeader := false
	if info, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeHeader = true
		} else {
			return err
		}
	} else if info.Size() == 0 {
		writeHeader = true
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	if writeHeader {
		if _, err := file.WriteString("# udl spotify state v2\n"); err != nil {
			return err
		}
	}

	fields := []string{trackID}
	title := strings.TrimSpace(displayName)
	if title != "" {
		fields = append(fields, "title="+encodeSpotifyStateValue(title))
	}
	normalizedPath := normalizeSpotifyStatePath(localPath)
	if normalizedPath != "" {
		fields = append(fields, "path="+encodeSpotifyStateValue(normalizedPath))
	}
	_, err = file.WriteString(strings.Join(fields, "\t") + "\n")
	return err
}

func parseSpotifyStateLine(line string) (string, spotifyStateEntry) {
	entry := spotifyStateEntry{}
	raw := strings.TrimSpace(line)
	if raw == "" {
		return "", entry
	}

	parts := strings.Split(raw, "\t")
	id := extractSpotifyTrackID(strings.TrimSpace(parts[0]))
	if id == "" {
		id = extractSpotifyTrackID(raw)
		if id == "" {
			fields := strings.Fields(raw)
			if len(fields) >= 2 && fields[0] == "spotify" {
				id = extractSpotifyTrackID(fields[1])
			}
		}
		return id, entry
	}

	for _, field := range parts[1:] {
		trimmed := strings.TrimSpace(field)
		switch {
		case strings.HasPrefix(trimmed, "title="):
			entry.DisplayName = decodeSpotifyStateValue(strings.TrimPrefix(trimmed, "title="))
		case strings.HasPrefix(trimmed, "path="):
			entry.LocalPath = normalizeSpotifyStatePath(decodeSpotifyStateValue(strings.TrimPrefix(trimmed, "path=")))
		case entry.DisplayName == "":
			entry.DisplayName = trimmed
		}
	}

	return id, entry
}

func encodeSpotifyStateValue(raw string) string {
	return url.QueryEscape(strings.TrimSpace(raw))
}

func decodeSpotifyStateValue(raw string) string {
	decoded, err := url.QueryUnescape(strings.TrimSpace(raw))
	if err != nil {
		return strings.TrimSpace(raw)
	}
	return strings.TrimSpace(decoded)
}

func normalizeSpotifyStatePath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	clean := filepath.Clean(filepath.FromSlash(trimmed))
	slashed := filepath.ToSlash(clean)
	slashed = strings.TrimPrefix(slashed, "./")
	slashed = strings.TrimPrefix(slashed, "/")
	if slashed == "." || slashed == "" {
		return ""
	}
	if slashed == ".." || strings.HasPrefix(slashed, "../") {
		return ""
	}
	return slashed
}
