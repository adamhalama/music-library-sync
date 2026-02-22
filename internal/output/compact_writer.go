package output

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	compactstate "github.com/jaa/update-downloads/internal/output/compact"
)

type CompactLogOptions struct {
	Interactive bool
}

type CompactLogWriter struct {
	dst         io.Writer
	interactive bool

	mu          sync.Mutex
	buf         []byte
	activeTrack string
	activeTotal string
	liveVisible bool

	progress *compactstate.StateMachine
	track    trackState
	barWidth int

	breakOnExistingDetected bool
	breakStopPrinted        bool
}

type trackState struct {
	Name               string
	HasThumbnail       bool
	HasMetadata        bool
	ProgressKnown      bool
	ProgressPercent    float64
	CompletionObserved bool
	AlreadyPresent     bool
}

func NewCompactLogWriter(dst io.Writer) *CompactLogWriter {
	return NewCompactLogWriterWithOptions(dst, CompactLogOptions{
		Interactive: SupportsInPlaceUpdates(dst),
	})
}

func NewCompactLogWriterWithOptions(dst io.Writer, opts CompactLogOptions) *CompactLogWriter {
	return &CompactLogWriter{
		dst:         dst,
		interactive: opts.Interactive,
		buf:         make([]byte, 0, 256),
		progress:    compactstate.NewStateMachine(),
	}
}

func SupportsInPlaceUpdates(dst io.Writer) bool {
	file, ok := dst.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func (w *CompactLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, b := range p {
		switch b {
		case '\n', '\r':
			if err := w.flushLineLocked(); err != nil {
				return 0, err
			}
		default:
			w.buf = append(w.buf, b)
		}
	}
	return len(p), nil
}

func (w *CompactLogWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.flushLineLocked(); err != nil {
		return err
	}
	if err := w.finalizeTrackLocked(); err != nil {
		return err
	}
	return w.clearLiveLinesLocked()
}

func (w *CompactLogWriter) ObserveEvent(event Event) {
	w.mu.Lock()
	defer w.mu.Unlock()

	switch event.Event {
	case EventSyncStarted:
		w.progress.Reset()
		w.track = trackState{}
		w.breakOnExistingDetected = false
		w.breakStopPrinted = false
	case EventSourcePreflight:
		planned, ok := eventDetailInt(event.Details, "planned_download_count")
		if !ok {
			return
		}
		w.progress.SetPlanningSource(event.SourceID, planned)
		w.track = trackState{}
		if planned > 0 {
			_ = w.renderIdleStatusLocked()
		}
	case EventSourceStarted:
		w.progress.BeginSource(event.SourceID)
	case EventSourceFinished:
		w.progress.FinishSource(event.SourceID, false)
	case EventSourceFailed:
		w.progress.FinishSource(event.SourceID, true)
	}
}

func (w *CompactLogWriter) flushLineLocked() error {
	if len(w.buf) == 0 {
		return nil
	}

	line := strings.TrimSpace(string(w.buf))
	w.buf = w.buf[:0]
	if line == "" {
		return nil
	}

	return w.handleLineLocked(line)
}

func (w *CompactLogWriter) handleLineLocked(line string) error {
	if strings.HasPrefix(line, "sync started (") {
		w.progress.Reset()
		w.track = trackState{}
	}

	if parsed, ok := compactstate.ParseLine(line); ok {
		switch parsed.Kind {
		case compactstate.LineEventSpotDLFoundSongs:
			w.progress.SetItemTotal(parsed.Total)
			w.track = trackState{}
			if parsed.Total > 0 {
				return w.renderIdleStatusLocked()
			}
			return nil
		case compactstate.LineEventSpotDLDownloaded:
			return w.recordSpotDLCompletedTrackLocked(parsed.Text)
		case compactstate.LineEventSpotDLLookupError:
			return w.recordSpotDLFailedTrackLocked(parsed.Text, "no-match")
		case compactstate.LineEventSpotDLAudioProvider:
			return w.recordSpotDLFailedTrackLocked(w.nextSpotDLTrackLabelLocked(), "download-error")
		case compactstate.LineEventDownloadItem:
			if err := w.finalizeTrackLocked(); err != nil {
				return err
			}
			w.breakOnExistingDetected = false
			w.breakStopPrinted = false
			w.progress.SetItemIndex(parsed.Index, parsed.Total)
			w.track = trackState{}
			return w.renderStatusLocked("preparing track")
		case compactstate.LineEventDownloadDestination:
			w.track.Name = trackNameFromPath(parsed.Text)
			w.track.CompletionObserved = false
			w.track.AlreadyPresent = false
			w.track.ProgressKnown = false
			w.track.ProgressPercent = 0
			return w.renderStatusLocked("downloading")
		case compactstate.LineEventAlreadyDownloaded:
			if w.track.Name == "" {
				w.track.Name = trackNameFromPath(parsed.Text)
			}
			w.track.AlreadyPresent = true
			w.track.ProgressKnown = true
			w.track.ProgressPercent = 100
			w.track.CompletionObserved = true
			return w.renderStatusLocked("already present")
		case compactstate.LineEventFragmentProgress:
			clamped := compactstate.ClampPercent(parsed.Percent)
			if w.track.ProgressKnown && clamped < w.track.ProgressPercent {
				clamped = w.track.ProgressPercent
			}
			w.track.ProgressKnown = true
			w.track.ProgressPercent = clamped
			return w.renderStatusLocked("downloading")
		case compactstate.LineEventNoisyDownloadProgress:
			return nil
		}
	}

	if strings.HasPrefix(line, "[info] Writing video thumbnail") {
		w.track.HasThumbnail = true
		return w.renderStatusLocked("thumbnail")
	}

	if strings.HasPrefix(line, "[Metadata] ") ||
		strings.HasPrefix(line, "[EmbedThumbnail] ") ||
		strings.HasPrefix(line, "[Mutagen] ") {
		w.track.HasMetadata = true
		return w.renderStatusLocked("metadata")
	}

	if strings.HasPrefix(line, "[download] 100% of ") {
		w.track.ProgressKnown = true
		w.track.ProgressPercent = 100
		w.track.CompletionObserved = true
		return w.renderStatusLocked("finalizing")
	}

	if isBreakOnExistingLine(line) {
		w.breakOnExistingDetected = true
		if w.breakStopPrinted {
			return nil
		}
		w.breakStopPrinted = true
		return w.printPersistentLocked("[stop] reached existing track in archive (break_on_existing)")
	}

	if w.breakOnExistingDetected && isBreakOnExistingTraceLine(line) {
		return nil
	}
	if w.breakOnExistingDetected && !strings.HasPrefix(strings.TrimSpace(line), "[") {
		return nil
	}
	if shouldSuppressPythonTracebackNoise(line) {
		return nil
	}
	if shouldSuppressSpotDLSpotifyNoise(line) {
		return nil
	}

	if shouldSuppressSCDLNoise(line) {
		return nil
	}

	if looksLikeWarningOrError(line) {
		return w.printPersistentLocked(line)
	}

	return w.printPersistentLocked(line)
}

func (w *CompactLogWriter) renderStatusLocked(stage string) error {
	if w.track.Name == "" {
		return nil
	}
	if !w.interactive {
		return nil
	}

	total := w.effectiveItemTotal()
	index := w.currentTrackIndex(total)
	trackLine := compactstate.RenderTrackLine(
		w.track.Name,
		stage,
		w.track.HasThumbnail,
		w.track.HasMetadata,
		w.track.ProgressKnown,
		w.track.ProgressPercent,
		w.track.AlreadyPresent,
		index,
		total,
	)

	overallPercent := w.globalProgressPercent()
	globalLine := compactstate.RenderGlobalLine(overallPercent, w.globalBarWidth(), index, total)

	if trackLine == w.activeTrack && globalLine == w.activeTotal {
		return nil
	}
	return w.renderLiveLinesLocked(trackLine, globalLine)
}

func (w *CompactLogWriter) renderIdleStatusLocked() error {
	if !w.interactive {
		return nil
	}
	total := w.effectiveItemTotal()
	if total <= 0 {
		return nil
	}
	done := w.progress.Completed()
	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}
	trackLine := compactstate.RenderIdleTrackLine(done, total)
	globalLine := compactstate.RenderGlobalLine(w.globalProgressPercent(), w.globalBarWidth(), done, total)
	if trackLine == w.activeTrack && globalLine == w.activeTotal {
		return nil
	}
	return w.renderLiveLinesLocked(trackLine, globalLine)
}

func (w *CompactLogWriter) finalizeTrackLocked() error {
	if !w.track.CompletionObserved || strings.TrimSpace(w.track.Name) == "" {
		w.track = trackState{}
		return nil
	}

	result := "[done]"
	if w.track.AlreadyPresent {
		result = "[skip]"
	}

	line := fmt.Sprintf("%s %s", result, w.track.Name)
	flags := []string{}
	if w.track.HasThumbnail {
		flags = append(flags, "thumb")
	}
	if w.track.HasMetadata {
		flags = append(flags, "meta")
	}
	if w.track.AlreadyPresent {
		flags = append(flags, "already-present")
	}
	if len(flags) > 0 {
		line += " (" + strings.Join(flags, ", ") + ")"
	}

	w.progress.CompleteTrack()
	w.track = trackState{}
	if err := w.printPersistentLocked(line); err != nil {
		return err
	}
	return w.renderIdleStatusLocked()
}

func (w *CompactLogWriter) recordSpotDLCompletedTrackLocked(name string) error {
	if strings.TrimSpace(name) == "" {
		name = w.nextSpotDLTrackLabelLocked()
	}
	if err := w.finalizeTrackLocked(); err != nil {
		return err
	}
	w.track = trackState{
		Name:               name,
		ProgressKnown:      true,
		ProgressPercent:    100,
		CompletionObserved: true,
	}
	if err := w.renderStatusLocked("downloaded"); err != nil {
		return err
	}
	return w.finalizeTrackLocked()
}

func (w *CompactLogWriter) recordSpotDLFailedTrackLocked(name string, reason string) error {
	if strings.TrimSpace(name) == "" {
		name = w.nextSpotDLTrackLabelLocked()
	}
	if err := w.finalizeTrackLocked(); err != nil {
		return err
	}
	w.track = trackState{
		Name:            name,
		ProgressKnown:   true,
		ProgressPercent: 100,
	}
	if err := w.renderStatusLocked("failed"); err != nil {
		return err
	}
	w.progress.CompleteTrack()
	w.track = trackState{}
	line := fmt.Sprintf("[skip] %s", name)
	if strings.TrimSpace(reason) != "" {
		line = fmt.Sprintf("%s (%s)", line, reason)
	}
	if err := w.printPersistentLocked(line); err != nil {
		return err
	}
	return w.renderIdleStatusLocked()
}

func (w *CompactLogWriter) nextSpotDLTrackLabelLocked() string {
	total := w.effectiveItemTotal()
	index := w.currentTrackIndex(total)
	if total > 0 {
		return fmt.Sprintf("track %d/%d", index, total)
	}
	if index > 0 {
		return fmt.Sprintf("track %d", index)
	}
	return "track"
}

func (w *CompactLogWriter) printPersistentLocked(line string) error {
	if err := w.clearLiveLinesLocked(); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w.dst, line)
	return err
}

func (w *CompactLogWriter) renderLiveLinesLocked(trackLine, globalLine string) error {
	if !w.interactive {
		return nil
	}
	// Cursor is kept on the track line while rendering two dynamic lines:
	// track status (line 1) + overall progress (line 2).
	if w.liveVisible {
		if _, err := fmt.Fprint(w.dst, "\r\033[2K"); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w.dst, "%s\n\033[2K%s\033[1A", trackLine, globalLine); err != nil {
		return err
	}
	w.liveVisible = true
	w.activeTrack = trackLine
	w.activeTotal = globalLine
	return nil
}

func (w *CompactLogWriter) clearLiveLinesLocked() error {
	if !w.interactive || !w.liveVisible {
		return nil
	}
	w.activeTrack = ""
	w.activeTotal = ""
	w.liveVisible = false
	_, err := fmt.Fprint(w.dst, "\r\033[2K\033[1B\r\033[2K\033[1A\r")
	return err
}

func shouldSuppressSCDLNoise(line string) bool {
	return strings.HasPrefix(line, "[scdl] ") ||
		strings.HasPrefix(line, "[soundcloud] ") ||
		strings.HasPrefix(line, "[soundcloud:user] ") ||
		strings.HasPrefix(line, "[info] ") ||
		strings.HasPrefix(line, "[hlsnative] ") ||
		strings.HasPrefix(line, "[download] Destination: ") ||
		strings.HasPrefix(line, "[download] Downloading item ") ||
		strings.HasPrefix(line, "[download] Downloading playlist: ") ||
		strings.HasPrefix(line, "[download] 100% of ") ||
		strings.HasPrefix(line, "[info] There are no playlist thumbnails") ||
		strings.HasPrefix(line, "[info] Downloading video thumbnail ") ||
		strings.HasPrefix(line, "[info] Writing video thumbnail ") ||
		strings.HasPrefix(line, "[FixupM4a] ") ||
		strings.HasPrefix(line, "[Metadata] ") ||
		strings.HasPrefix(line, "[EmbedThumbnail] ") ||
		strings.HasPrefix(line, "[Mutagen] ") ||
		strings.HasPrefix(line, "Skipping embedding the thumbnail")
}

func shouldSuppressPythonTracebackNoise(line string) bool {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(trimmed, "Traceback (most recent call last):") ||
		strings.HasPrefix(trimmed, "During handling of the above exception, another exception occurred:") ||
		strings.HasPrefix(trimmed, "File \"") ||
		strings.HasPrefix(trimmed, "╭") ||
		strings.HasPrefix(trimmed, "╰") ||
		strings.HasPrefix(trimmed, "│") ||
		strings.HasPrefix(trimmed, "╮") ||
		strings.HasPrefix(trimmed, "╯") ||
		strings.HasPrefix(lower, "nameerror: name 'raw_input' is not defined") ||
		strings.HasPrefix(lower, "eoferror: eof when reading a line")
}

func shouldSuppressSpotDLSpotifyNoise(line string) bool {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	return trimmed == "An error occurred" ||
		strings.HasPrefix(trimmed, "Processing query:") ||
		strings.HasPrefix(trimmed, "https://open.spotify.com/playlist/") ||
		strings.Contains(lower, "rate/request limit") ||
		compactstate.MatchesSpotDLFoundSongs(trimmed) ||
		compactstate.MatchesSpotDLDownloaded(trimmed) ||
		compactstate.MatchesSpotDLLookupError(trimmed) ||
		compactstate.MatchesSpotDLAudioProvider(trimmed) ||
		strings.HasPrefix(trimmed, "found for song: ") ||
		strings.HasPrefix(trimmed, "YT-DLP download error - ") ||
		strings.HasPrefix(trimmed, "https://www.youtube.com/watch?v=") ||
		strings.HasPrefix(trimmed, "https://music.youtube.com/watch?v=") ||
		strings.HasPrefix(trimmed, "Nothing to delete...") ||
		strings.HasPrefix(trimmed, "Saved archive with ") ||
		(strings.HasPrefix(trimmed, "https://open.spotify.com/track/") && strings.Contains(trimmed, " - LookupError:")) ||
		(strings.HasPrefix(trimmed, "https://open.spotify.com/track/") && strings.Contains(trimmed, " - AudioProviderError:")) ||
		strings.HasPrefix(lower, "http error for get to https://api.spotify.com/") ||
		(strings.HasPrefix(lower, "httperror: 401 client error:") && strings.Contains(lower, "api.spotify.com")) ||
		(strings.HasPrefix(lower, "httperror: 403 client error:") && strings.Contains(lower, "api.spotify.com")) ||
		(strings.HasPrefix(lower, "spotifyexception: http status: 401") && strings.Contains(lower, "api.spotify.com")) ||
		(strings.HasPrefix(lower, "spotifyexception: http status: 403") && strings.Contains(lower, "api.spotify.com")) ||
		(strings.Contains(lower, "valid user authentication required") && strings.Contains(lower, "reason: none")) ||
		(strings.HasPrefix(lower, "forbidden, reason: none"))
}

func isBreakOnExistingLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	return strings.Contains(lower, "has already been recorded in the archive") ||
		strings.Contains(lower, "stopping due to --break-on-existing") ||
		strings.Contains(lower, "existingvideoreached")
}

func isBreakOnExistingTraceLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(trimmed, "Traceback (most recent call last):") ||
		strings.HasPrefix(trimmed, "File \"") ||
		strings.HasPrefix(trimmed, "^^^^^^^^") ||
		strings.HasPrefix(lower, "yt_dlp.utils.existingvideoreached:") ||
		strings.Contains(lower, "stopping due to --break-on-existing")
}

func looksLikeWarningOrError(line string) bool {
	lower := strings.ToLower(line)
	return strings.HasPrefix(lower, "warning:") ||
		strings.HasPrefix(lower, "warn:") ||
		strings.HasPrefix(lower, "error:") ||
		strings.Contains(lower, "traceback")
}

func trackNameFromPath(pathLike string) string {
	trimmed := strings.Trim(strings.TrimSpace(pathLike), "\"")
	base := filepath.Base(trimmed)
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}

func (w *CompactLogWriter) globalProgressPercent() float64 {
	total := w.effectiveItemTotal()
	if total <= 0 {
		return 0
	}
	partial := 0.0
	if w.progress.Completed() < total {
		switch {
		case w.track.AlreadyPresent || w.track.CompletionObserved:
			partial = 1.0
		case w.track.ProgressKnown:
			partial = w.track.ProgressPercent / 100.0
		}
	}
	return w.progress.GlobalProgressPercent(partial)
}

func (w *CompactLogWriter) effectiveItemTotal() int {
	return w.progress.EffectiveTotal()
}

func (w *CompactLogWriter) currentTrackIndex(total int) int {
	if total <= 0 {
		return 0
	}
	return w.progress.CurrentIndex()
}

func (w *CompactLogWriter) globalBarWidth() int {
	if w.barWidth > 0 {
		return w.barWidth
	}
	width := 44
	if cols, err := strconv.Atoi(strings.TrimSpace(os.Getenv("COLUMNS"))); err == nil && cols > 0 {
		candidate := cols - 26
		if candidate < 20 {
			candidate = 20
		}
		if candidate > 80 {
			candidate = 80
		}
		width = candidate
	}
	w.barWidth = width
	return width
}

func eventDetailInt(details map[string]any, key string) (int, bool) {
	if details == nil {
		return 0, false
	}
	raw, ok := details[key]
	if !ok {
		return 0, false
	}
	switch v := raw.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}
