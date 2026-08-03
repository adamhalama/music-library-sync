package engine

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
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

type spotifyStateStore struct {
	ReadPath  string
	WritePath string
}

type spotifyStateBackfillEntry struct {
	ID          string
	DisplayName string
	LocalPath   string
}

// spotifyStateWriter keeps the parsed line layout for a whole source run.
// Every mutation still rewrites through the existing atomic rename boundary,
// but no completed track rereads or reparses the state file.
type spotifyStateWriter struct {
	path       string
	mode       os.FileMode
	prefix     []byte
	lines      []string
	firstByID  map[string]int
	duplicates map[string][]int
	removed    map[int]bool
}

var readSpotifyStateWriterFileFn = os.ReadFile

func newSpotifyStateWriter(path string) (*spotifyStateWriter, error) {
	stateDir := filepath.Dir(path)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, err
	}
	payload, err := readSpotifyStateWriterFileFn(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	prefix, lines := splitSpotifyStatePayload(payload)
	if len(payload) == 0 {
		prefix = []byte("# udl spotify state v2\n")
	}
	writer := &spotifyStateWriter{
		path: path, mode: mode, prefix: prefix, lines: lines,
		firstByID: map[string]int{}, duplicates: map[string][]int{}, removed: map[int]bool{},
	}
	for index, line := range lines {
		id, _ := parseSpotifyStateLine(line)
		if id == "" {
			continue
		}
		if _, exists := writer.firstByID[id]; !exists {
			writer.firstByID[id] = index
		} else {
			writer.duplicates[id] = append(writer.duplicates[id], index)
		}
	}
	return writer, nil
}

func (w *spotifyStateWriter) Upsert(id, displayName, localPath string) error {
	if w == nil {
		return errors.New("spotify state writer is nil")
	}
	trackID := extractSpotifyTrackID(id)
	if trackID == "" {
		return errors.New("spotify track id must not be empty")
	}
	entry := spotifyStateEntry{
		DisplayName: strings.TrimSpace(displayName),
		LocalPath:   normalizeSpotifyStatePath(localPath),
	}
	if index, exists := w.firstByID[trackID]; exists {
		_, existing := parseSpotifyStateLine(w.lines[index])
		if entry.DisplayName != "" {
			existing.DisplayName = entry.DisplayName
		}
		if entry.LocalPath != "" {
			existing.LocalPath = entry.LocalPath
		}
		w.lines[index] = formatSpotifyStateLine(trackID, existing)
		for _, duplicate := range w.duplicates[trackID] {
			w.removed[duplicate] = true
		}
	} else {
		w.firstByID[trackID] = len(w.lines)
		w.lines = append(w.lines, formatSpotifyStateLine(trackID, entry))
	}
	return w.persist()
}

func (w *spotifyStateWriter) persist() error {
	last := len(w.lines) - 1
	for last >= 0 && (w.removed[last] || strings.TrimSpace(w.lines[last]) == "") {
		last--
	}
	lineCount := 0
	for index := 0; index <= last; index++ {
		if !w.removed[index] {
			lineCount++
		}
	}
	var payload bytes.Buffer
	payload.Grow(len(w.prefix) + lineCount*64)
	payload.Write(w.prefix)
	if payload.Len() > 0 && w.prefix[len(w.prefix)-1] != '\n' {
		payload.WriteByte('\n')
	}
	for index := 0; index <= last; index++ {
		if w.removed[index] {
			continue
		}
		payload.WriteString(w.lines[index])
		payload.WriteByte('\n')
	}
	return writeSpotifyStateAtomically(w.path, payload.Bytes(), w.mode)
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

	if bytes.HasPrefix(bytes.TrimSpace(payload), []byte("{")) {
		offset, ok, err := parseSpotDLJSONState(payload, &state)
		if err != nil {
			return state, err
		}
		if ok {
			if offset < int64(len(payload)) {
				if err := parseSpotifyStateLines(payload[offset:], &state); err != nil {
					return state, err
				}
			}
			return state, nil
		}
	}

	if err := parseSpotifyStateLines(payload, &state); err != nil {
		return state, err
	}
	return state, nil
}

func resolveSpotifyStateStore(configuredPath string) spotifyStateStore {
	readPath := strings.TrimSpace(configuredPath)
	writePath := readPath
	switch {
	case strings.HasSuffix(writePath, ".sync.spotdl"):
		writePath = strings.TrimSuffix(writePath, ".sync.spotdl") + ".sync.spotify"
	case strings.HasSuffix(writePath, ".spotdl"):
		writePath = strings.TrimSuffix(writePath, ".spotdl") + ".spotify"
	}
	return spotifyStateStore{ReadPath: readPath, WritePath: writePath}
}

func loadSpotifySyncState(configuredPath string) (spotifySyncState, spotifyStateStore, error) {
	store := resolveSpotifyStateStore(configuredPath)
	state, err := parseSpotifySyncState(store.ReadPath)
	if err != nil {
		return state, store, err
	}
	if store.WritePath == "" || store.WritePath == store.ReadPath {
		return state, store, nil
	}
	writeState, err := parseSpotifySyncState(store.WritePath)
	if err != nil {
		return state, store, err
	}
	mergeSpotifySyncState(&state, writeState)
	return state, store, nil
}

func mergeSpotifySyncState(dst *spotifySyncState, src spotifySyncState) {
	if dst.KnownIDs == nil {
		dst.KnownIDs = map[string]struct{}{}
	}
	if dst.Entries == nil {
		dst.Entries = map[string]spotifyStateEntry{}
	}
	for id := range src.KnownIDs {
		dst.KnownIDs[id] = struct{}{}
	}
	for id, entry := range src.Entries {
		mergeSpotifyStateEntry(dst, id, entry)
	}
}

func parseSpotifyStateLines(payload []byte, state *spotifySyncState) error {
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
		mergeSpotifyStateEntry(state, id, entry)
	}
	return scanner.Err()
}

type spotDLSyncStateFile struct {
	Songs []spotDLSyncSong `json:"songs"`
}

type spotDLSyncSong struct {
	SongID  string          `json:"song_id"`
	Name    string          `json:"name"`
	Artist  string          `json:"artist"`
	Artists json.RawMessage `json:"artists"`
}

func parseSpotDLJSONState(payload []byte, state *spotifySyncState) (int64, bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	var decoded spotDLSyncStateFile
	if err := decoder.Decode(&decoded); err != nil {
		if errors.Is(err, io.EOF) {
			return 0, false, nil
		}
		return 0, false, nil
	}
	for _, song := range decoded.Songs {
		id := extractSpotifyTrackID(song.SongID)
		if id == "" {
			continue
		}
		mergeSpotifyStateEntry(state, id, spotifyStateEntry{
			DisplayName: spotDLDisplayName(song),
		})
	}
	return decoder.InputOffset(), true, nil
}

func spotDLDisplayName(song spotDLSyncSong) string {
	title := strings.TrimSpace(song.Name)
	artist := strings.TrimSpace(song.Artist)
	if artist == "" {
		artist = strings.Join(parseSpotDLArtists(song.Artists), ", ")
	}
	switch {
	case artist != "" && title != "":
		return artist + " - " + title
	case title != "":
		return title
	default:
		return ""
	}
}

func parseSpotDLArtists(raw json.RawMessage) []string {
	payload := bytes.TrimSpace(raw)
	if len(payload) == 0 || bytes.Equal(payload, []byte("null")) {
		return nil
	}
	var names []string
	if err := json.Unmarshal(payload, &names); err == nil {
		return trimNonEmptyStrings(names)
	}
	var objects []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(payload, &objects); err == nil {
		out := make([]string, 0, len(objects))
		for _, object := range objects {
			if trimmed := strings.TrimSpace(object.Name); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	}
	return nil
}

func trimNonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func mergeSpotifyStateEntry(state *spotifySyncState, id string, entry spotifyStateEntry) {
	trackID := extractSpotifyTrackID(id)
	if trackID == "" {
		return
	}
	if state.KnownIDs == nil {
		state.KnownIDs = map[string]struct{}{}
	}
	if state.Entries == nil {
		state.Entries = map[string]spotifyStateEntry{}
	}
	state.KnownIDs[trackID] = struct{}{}
	if entry.DisplayName == "" && entry.LocalPath == "" {
		return
	}
	existing := state.Entries[trackID]
	if entry.DisplayName != "" {
		existing.DisplayName = entry.DisplayName
	}
	if entry.LocalPath != "" {
		existing.LocalPath = entry.LocalPath
	}
	state.Entries[trackID] = existing
}

func appendSpotifySyncStateID(path string, id string) error {
	return upsertSpotifySyncStateEntry(path, id, "", "")
}

func upsertSpotifySyncStateEntry(path string, id string, displayName string, localPath string) error {
	writer, err := newSpotifyStateWriter(path)
	if err != nil {
		return err
	}
	return writer.Upsert(id, displayName, localPath)
}

func splitSpotifyStatePayload(payload []byte) ([]byte, []string) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if !bytes.HasPrefix(trimmed, []byte("{")) {
		return nil, strings.Split(string(payload), "\n")
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return nil, strings.Split(string(payload), "\n")
	}
	offset := decoder.InputOffset()
	return append([]byte(nil), payload[:offset]...), strings.Split(string(payload[offset:]), "\n")
}

func formatSpotifyStateLine(trackID string, entry spotifyStateEntry) string {
	fields := []string{trackID}
	if title := strings.TrimSpace(entry.DisplayName); title != "" {
		fields = append(fields, "title="+encodeSpotifyStateValue(title))
	}
	if localPath := normalizeSpotifyStatePath(entry.LocalPath); localPath != "" {
		fields = append(fields, "path="+encodeSpotifyStateValue(localPath))
	}
	return strings.Join(fields, "\t")
}

func writeSpotifyStateAtomically(path string, payload []byte, mode os.FileMode) error {
	tempFile, err := os.CreateTemp(filepath.Dir(path), ".udl-spotify-state-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if err := tempFile.Chmod(mode); err != nil {
		_ = tempFile.Close()
		return err
	}
	if _, err := tempFile.Write(payload); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func appendSpotifyStateBackfillEntries(path string, entries []spotifyStateBackfillEntry, state *spotifySyncState) (int, error) {
	if len(entries) == 0 {
		return 0, nil
	}
	writer, err := newSpotifyStateWriter(path)
	if err != nil {
		return 0, err
	}
	return appendSpotifyStateBackfillEntriesWithWriter(writer, entries, state)
}

func appendSpotifyStateBackfillEntriesWithWriter(writer *spotifyStateWriter, entries []spotifyStateBackfillEntry, state *spotifySyncState) (int, error) {
	if len(entries) == 0 {
		return 0, nil
	}
	if state.KnownIDs == nil {
		state.KnownIDs = map[string]struct{}{}
	}
	written := 0
	for _, entry := range entries {
		id := extractSpotifyTrackID(entry.ID)
		if id == "" {
			continue
		}
		if _, exists := state.KnownIDs[id]; exists {
			continue
		}
		if err := writer.Upsert(id, entry.DisplayName, entry.LocalPath); err != nil {
			return written, err
		}
		mergeSpotifyStateEntry(state, id, spotifyStateEntry{
			DisplayName: strings.TrimSpace(entry.DisplayName),
			LocalPath:   normalizeSpotifyStatePath(entry.LocalPath),
		})
		written++
	}
	return written, nil
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
