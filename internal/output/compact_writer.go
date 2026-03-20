package output

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	compactstate "github.com/jaa/update-downloads/internal/output/compact"
)

var preflightSummaryPattern = regexp.MustCompile(`^\[[^\]]+\]\s+preflight:\s+.*\bplanned=([0-9]+)\b`)
var deemixTrackProgressPattern = regexp.MustCompile(`^\[[^\]]+\]\s+deemix track ([0-9]+)\/([0-9]+)\s+([A-Za-z0-9]{10,32})(?:\s+\((.+)\))?$`)
var deemixTrackDonePattern = regexp.MustCompile(`^\[[^\]]+\]\s+\[done\]\s+([A-Za-z0-9]{10,32})(?:\s+\(([^)]+)\))?$`)
var deemixTrackSkipPattern = regexp.MustCompile(`^\[[^\]]+\]\s+\[skip\]\s+([A-Za-z0-9]{10,32})(?:\s+\(([^)]+)\))?(?:\s+\(([^)]+)\))?$`)
var deemixTrackFailurePattern = regexp.MustCompile(`^(?:ERROR:\s*)?\[[^\]]+\]\s+command failed with exit code ([0-9]+)$`)
var deemixDownloadProgressPattern = regexp.MustCompile(`^\[([^\]]+)\]\s+Downloading:\s+([0-9]+(?:\.[0-9]+)?)%$`)
var deemixDownloadCompletePattern = regexp.MustCompile(`^\[([^\]]+)\]\s+Download complete$`)

type CompactLogOptions struct {
	Interactive      bool
	PreflightSummary string
	TrackStatus      string
}

type CompactLogWriter struct {
	dst         io.Writer
	interactive bool

	mu          sync.Mutex
	buf         []byte
	activeTrack string
	activeTotal string
	liveVisible bool

	progress   *compactstate.StateMachine
	structured *StructuredProgressTracker
	track      trackState
	barWidth   int

	structuredTrackEvents bool
	preflightSummaryMode  string
	trackStatusMode       string
}

const (
	CompactPreflightAuto   = "auto"
	CompactPreflightAlways = "always"
	CompactPreflightNever  = "never"
)

const (
	CompactTrackStatusNames = "names"
	CompactTrackStatusCount = "count"
	CompactTrackStatusNone  = "none"
)

type trackState struct {
	Name            string
	ProgressKnown   bool
	ProgressPercent float64
}

func NewCompactLogWriter(dst io.Writer) *CompactLogWriter {
	return NewCompactLogWriterWithOptions(dst, CompactLogOptions{
		Interactive:      SupportsInPlaceUpdates(dst),
		PreflightSummary: CompactPreflightAuto,
		TrackStatus:      CompactTrackStatusNames,
	})
}

func NewCompactLogWriterWithOptions(dst io.Writer, opts CompactLogOptions) *CompactLogWriter {
	preflightSummary := strings.TrimSpace(strings.ToLower(opts.PreflightSummary))
	switch preflightSummary {
	case "", CompactPreflightAuto, CompactPreflightAlways, CompactPreflightNever:
	default:
		preflightSummary = CompactPreflightAuto
	}
	if preflightSummary == "" {
		preflightSummary = CompactPreflightAuto
	}

	trackStatus := strings.TrimSpace(strings.ToLower(opts.TrackStatus))
	switch trackStatus {
	case "", CompactTrackStatusNames, CompactTrackStatusCount, CompactTrackStatusNone:
	default:
		trackStatus = CompactTrackStatusNames
	}
	if trackStatus == "" {
		trackStatus = CompactTrackStatusNames
	}

	progress := compactstate.NewStateMachine()
	return &CompactLogWriter{
		dst:                  dst,
		interactive:          opts.Interactive,
		buf:                  make([]byte, 0, 256),
		progress:             progress,
		structured:           NewStructuredProgressTracker(progress),
		preflightSummaryMode: preflightSummary,
		trackStatusMode:      trackStatus,
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
	return w.clearLiveLinesLocked()
}

func (w *CompactLogWriter) ObserveEvent(event Event) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.progress == nil {
		w.progress = compactstate.NewStateMachine()
	}
	if w.structured == nil {
		w.structured = NewStructuredProgressTracker(w.progress)
	}
	w.structured.ObserveEvent(event)
	snapshot := w.structured.Snapshot()
	w.structuredTrackEvents = snapshot.StructuredTrackEvents

	switch event.Event {
	case EventSyncStarted:
		w.track = trackState{}
	case EventSourcePreflight:
		if snapshot.Progress.Source.PlannedTotal <= 0 {
			return
		}
		w.track = trackState{}
		_ = w.renderIdleStatusLocked()
	case EventTrackStarted:
		w.applyStructuredTrackLocked(snapshot.Track)
		_ = w.renderStatusLocked(stageForStructuredLifecycle(snapshot.Track.Lifecycle))
	case EventTrackProgress:
		w.applyStructuredTrackLocked(snapshot.Track)
		_ = w.renderStatusLocked(stageForStructuredLifecycle(snapshot.Track.Lifecycle))
	case EventTrackDone:
		w.track = trackState{}
		w.printStructuredOutcomesLocked()
		_ = w.renderIdleStatusLocked()
	case EventTrackSkip:
		w.track = trackState{}
		w.printStructuredOutcomesLocked()
		_ = w.renderIdleStatusLocked()
	case EventTrackFail:
		w.track = trackState{}
		w.printStructuredOutcomesLocked()
		_ = w.renderIdleStatusLocked()
	case EventSourceFinished, EventSourceFailed:
		w.track = trackState{}
		_ = w.renderIdleStatusLocked()
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
	if match := preflightSummaryPattern.FindStringSubmatch(line); len(match) == 2 {
		if w.showPreflightSummaryLocked() {
			return w.printPersistentLocked(line)
		}
		return nil
	}

	if shouldSuppressAdapterChatter(line, w.structuredTrackEvents) ||
		shouldSuppressPythonTracebackNoise(line) ||
		shouldSuppressSpotDLSpotifyNoise(line) ||
		shouldSuppressDeemixNoise(line) ||
		isBreakOnExistingLine(line) ||
		isBreakOnExistingTraceLine(line) {
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
		false,
		false,
		w.track.ProgressKnown,
		w.track.ProgressPercent,
		false,
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

func (w *CompactLogWriter) showPreflightSummaryLocked() bool {
	switch w.preflightSummaryMode {
	case CompactPreflightNever:
		return false
	case CompactPreflightAlways:
		return true
	default:
		return true
	}
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

func shouldSuppressAdapterChatter(line string, structuredTrackEvents bool) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return true
	}

	if shouldSuppressSCDLNoise(trimmed) {
		return true
	}

	if parsed, ok := compactstate.ParseLine(trimmed); ok {
		switch parsed.Kind {
		case compactstate.LineEventDownloadItem,
			compactstate.LineEventDownloadDestination,
			compactstate.LineEventAlreadyDownloaded,
			compactstate.LineEventFragmentProgress,
			compactstate.LineEventNoisyDownloadProgress,
			compactstate.LineEventSpotDLFoundSongs,
			compactstate.LineEventSpotDLDownloaded,
			compactstate.LineEventSpotDLLookupError,
			compactstate.LineEventSpotDLAudioProvider:
			return true
		}
	}

	if deemixDownloadProgressPattern.MatchString(trimmed) || deemixDownloadCompletePattern.MatchString(trimmed) {
		return true
	}
	if deemixTrackProgressPattern.MatchString(trimmed) || deemixTrackFailurePattern.MatchString(trimmed) {
		return true
	}
	if structuredTrackEvents &&
		(deemixTrackDonePattern.MatchString(trimmed) || deemixTrackSkipPattern.MatchString(trimmed)) {
		return true
	}

	return false
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

func shouldSuppressDeemixNoise(line string) bool {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)

	if strings.HasPrefix(lower, "gwapierror: track unavailable on deezer") {
		return true
	}
	if strings.HasPrefix(trimmed, "at ") && strings.Contains(trimmed, "/snapshot/cli/dist/main.cjs") {
		return true
	}
	if strings.HasPrefix(trimmed, "at process.processTicksAndRejections") {
		return true
	}
	if strings.HasPrefix(lower, "typeerror: cannot read properties of undefined") {
		return true
	}
	if strings.Contains(lower, "spotifyplugin.gettrack") && strings.Contains(trimmed, "/snapshot/cli/dist/main.cjs") {
		return true
	}
	return false
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

func (w *CompactLogWriter) globalProgressPercent() float64 {
	total := w.effectiveItemTotal()
	if total <= 0 {
		return 0
	}
	partial := 0.0
	if w.progress.Completed() < total {
		if w.track.ProgressKnown {
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

func eventDetailFloat(details map[string]any, key string) (float64, bool) {
	if details == nil {
		return 0, false
	}
	raw, ok := details[key]
	if !ok {
		return 0, false
	}
	switch v := raw.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func eventDetailString(details map[string]any, key string) string {
	if details == nil {
		return ""
	}
	raw, ok := details[key]
	if !ok {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func (w *CompactLogWriter) applyStructuredTrackLocked(track StructuredTrackState) {
	w.track = trackState{
		Name:            strings.TrimSpace(track.Name),
		ProgressKnown:   track.ProgressKnown,
		ProgressPercent: track.ProgressPercent,
	}
}

func (w *CompactLogWriter) printStructuredOutcomesLocked() {
	if w.structured == nil {
		return
	}
	for _, outcome := range w.structured.DrainTrackOutcomes() {
		if w.trackStatusMode == CompactTrackStatusNone {
			continue
		}
		_ = w.printPersistentLocked(FormatCompactTrackOutcome(outcome, w.trackStatusMode))
	}
}

func stageForStructuredLifecycle(lifecycle compactstate.TrackLifecycle) string {
	switch lifecycle {
	case compactstate.TrackLifecyclePreparing:
		return "preparing"
	case compactstate.TrackLifecycleFinalizing:
		return "finalizing"
	case compactstate.TrackLifecycleDone:
		return "downloaded"
	case compactstate.TrackLifecycleSkipped:
		return "skipped"
	case compactstate.TrackLifecycleFailed:
		return "failed"
	default:
		return "downloading"
	}
}
