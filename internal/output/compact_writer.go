package output

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var noisyDownloadProgressPattern = regexp.MustCompile(`^\[download\]\s+[0-9]+(?:\.[0-9]+)?%.*\(frag\s+[0-9]+/[0-9]+\)$`)
var fragmentProgressPattern = regexp.MustCompile(`^\[download\]\s+([0-9]+(?:\.[0-9]+)?)%.*\(frag\s+[0-9]+/[0-9]+\)$`)
var downloadItemPattern = regexp.MustCompile(`^\[download\] Downloading item ([0-9]+) of ([0-9]+)$`)
var downloadDestinationPattern = regexp.MustCompile(`^\[download\] Destination: (.+)$`)
var alreadyDownloadedPattern = regexp.MustCompile(`^\[download\] (.+) has already been downloaded$`)
var preflightSummaryPattern = regexp.MustCompile(`^\[[^\]]+\]\s+preflight:\s+.*\bplanned=([0-9]+)\b`)
var deemixTrackProgressPattern = regexp.MustCompile(`^\[[^\]]+\]\s+deemix track ([0-9]+)\/([0-9]+)\s+([A-Za-z0-9]{10,32})(?:\s+\((.+)\))?$`)
var deemixTrackDonePattern = regexp.MustCompile(`^\[[^\]]+\]\s+\[done\]\s+([A-Za-z0-9]{10,32})(?:\s+\(([^)]+)\))?$`)
var deemixTrackSkipPattern = regexp.MustCompile(`^\[[^\]]+\]\s+\[skip\]\s+([A-Za-z0-9]{10,32})(?:\s+\(([^)]+)\))?(?:\s+\(([^)]+)\))?$`)
var deemixTrackFailurePattern = regexp.MustCompile(`^(?:ERROR:\s*)?\[[^\]]+\]\s+command failed with exit code ([0-9]+)$`)
var deemixDownloadProgressPattern = regexp.MustCompile(`^\[([^\]]+)\]\s+Downloading:\s+([0-9]+(?:\.[0-9]+)?)%$`)
var deemixDownloadCompletePattern = regexp.MustCompile(`^\[([^\]]+)\]\s+Download complete$`)
var deemixTrackIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{10,32}$`)
var sourceCompletedPattern = regexp.MustCompile(`^\[[^\]]+\]\s+completed$`)
var spotDLFoundSongsPattern = regexp.MustCompile(`^Found ([0-9]+) songs in .+$`)
var spotDLDownloadedPattern = regexp.MustCompile(`^Downloaded "(.+)":\s+https?://.+$`)
var spotDLLookupErrorPattern = regexp.MustCompile(`^LookupError: No results found for song: (.+)$`)
var spotDLAudioProviderPattern = regexp.MustCompile(`^AudioProviderError: YT-DLP download error -.*$`)

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

	itemIndex    int
	itemTotal    int
	plannedTotal int
	completed    int
	track        trackState
	barWidth     int

	breakOnExistingDetected bool
	breakStopPrinted        bool
	activeAdapter           string
	deemixTrackTitle        string
	preflightSummaryMode    string
	trackStatusMode         string
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

	return &CompactLogWriter{
		dst:                  dst,
		interactive:          opts.Interactive,
		buf:                  make([]byte, 0, 256),
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
	if err := w.finalizeTrackLocked(); err != nil {
		return err
	}
	return w.clearLiveLinesLocked()
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
		w.itemIndex = 0
		w.itemTotal = 0
		w.plannedTotal = 0
		w.completed = 0
		w.track = trackState{}
		w.activeAdapter = ""
		w.deemixTrackTitle = ""
	}

	if match := preflightSummaryPattern.FindStringSubmatch(line); len(match) == 2 {
		planned, _ := strconv.Atoi(match[1])
		if planned < 0 {
			planned = 0
		}
		w.plannedTotal = planned
		w.itemIndex = 0
		w.itemTotal = 0
		w.completed = 0
		w.track = trackState{}
		if w.showPreflightSummaryLocked() {
			if err := w.printPersistentLocked(line); err != nil {
				return err
			}
		}
		if planned > 0 {
			return w.renderIdleStatusLocked()
		}
		return nil
	}
	if match := deemixTrackProgressPattern.FindStringSubmatch(line); len(match) >= 4 {
		if err := w.finalizeTrackLocked(); err != nil {
			return err
		}
		index, _ := strconv.Atoi(match[1])
		total, _ := strconv.Atoi(match[2])
		if index < 1 {
			index = 1
		}
		if total < 0 {
			total = 0
		}
		w.activeAdapter = "deemix"
		w.deemixTrackTitle = ""
		w.itemIndex = index
		w.itemTotal = total
		trackName := strings.TrimSpace(match[3])
		if len(match) >= 5 {
			label := strings.TrimSpace(match[4])
			if label != "" {
				trackName = label
			}
		}
		w.track = trackState{
			Name: trackName,
		}
		return w.renderStatusLocked("downloading")
	}
	if match := deemixTrackDonePattern.FindStringSubmatch(line); len(match) >= 2 {
		w.activeAdapter = "deemix"
		label := ""
		if len(match) >= 3 {
			label = strings.TrimSpace(match[2])
		}
		if label != "" && (strings.TrimSpace(w.track.Name) == "" || deemixTrackIDPattern.MatchString(strings.TrimSpace(w.track.Name))) {
			w.track.Name = label
		}
		if w.deemixTrackTitle != "" && (strings.TrimSpace(w.track.Name) == "" || deemixTrackIDPattern.MatchString(strings.TrimSpace(w.track.Name))) {
			w.track.Name = w.deemixTrackTitle
		}
		if strings.TrimSpace(w.track.Name) == "" {
			w.track.Name = strings.TrimSpace(match[1])
		}
		w.track.ProgressKnown = true
		w.track.ProgressPercent = 100
		w.track.CompletionObserved = true
		w.deemixTrackTitle = ""
		if err := w.renderStatusLocked("downloaded"); err != nil {
			return err
		}
		return w.finalizeTrackLocked()
	}
	if match := deemixTrackSkipPattern.FindStringSubmatch(line); len(match) >= 2 {
		w.activeAdapter = "deemix"
		label := ""
		if len(match) >= 3 {
			label = strings.TrimSpace(match[2])
		}
		reason := ""
		if len(match) >= 4 {
			reason = strings.TrimSpace(match[3])
		}
		trackName := label
		if trackName == "" {
			trackName = strings.TrimSpace(match[1])
		}
		if strings.TrimSpace(w.track.Name) != "" && !deemixTrackIDPattern.MatchString(strings.TrimSpace(w.track.Name)) {
			trackName = strings.TrimSpace(w.track.Name)
		}
		if strings.TrimSpace(trackName) == "" {
			trackName = w.nextSpotDLTrackLabelLocked()
		}
		if total := w.effectiveItemTotal(); total > 0 && w.completed < total {
			w.completed++
		}
		w.track = trackState{}
		w.deemixTrackTitle = ""
		if w.trackStatusMode != CompactTrackStatusNone {
			line := w.compactTrackResultLineLocked("[skip]", trackName)
			if reason != "" {
				line = fmt.Sprintf("%s (%s)", line, reason)
			}
			if err := w.printPersistentLocked(line); err != nil {
				return err
			}
		}
		return w.renderIdleStatusLocked()
	}
	if match := deemixTrackFailurePattern.FindStringSubmatch(line); len(match) == 2 && w.activeAdapter == "deemix" {
		trackName := strings.TrimSpace(w.track.Name)
		if trackName == "" {
			trackName = w.nextSpotDLTrackLabelLocked()
		}
		if total := w.effectiveItemTotal(); total > 0 && w.completed < total {
			w.completed++
		}
		w.track = trackState{}
		w.deemixTrackTitle = ""
		if w.trackStatusMode != CompactTrackStatusNone {
			line := fmt.Sprintf("%s (exit-%s)", w.compactTrackResultLineLocked("[fail]", trackName), match[1])
			if err := w.printPersistentLocked(line); err != nil {
				return err
			}
		}
		return w.renderIdleStatusLocked()
	}
	if match := deemixDownloadProgressPattern.FindStringSubmatch(line); len(match) == 3 && w.activeAdapter == "deemix" {
		title := strings.TrimSpace(match[1])
		if title != "" {
			w.deemixTrackTitle = title
		}
		if title != "" && (strings.TrimSpace(w.track.Name) == "" || deemixTrackIDPattern.MatchString(strings.TrimSpace(w.track.Name))) {
			w.track.Name = title
		}
		percent, _ := strconv.ParseFloat(match[2], 64)
		clamped := clampPercent(percent)
		if w.track.ProgressKnown && clamped < w.track.ProgressPercent {
			clamped = w.track.ProgressPercent
		}
		w.track.ProgressKnown = true
		w.track.ProgressPercent = clamped
		return w.renderStatusLocked("downloading")
	}
	if match := deemixDownloadCompletePattern.FindStringSubmatch(line); len(match) == 2 && w.activeAdapter == "deemix" {
		title := strings.TrimSpace(match[1])
		if title != "" {
			w.deemixTrackTitle = title
		}
		if title != "" && (strings.TrimSpace(w.track.Name) == "" || deemixTrackIDPattern.MatchString(strings.TrimSpace(w.track.Name))) {
			w.track.Name = title
		}
		w.track.ProgressKnown = true
		w.track.ProgressPercent = 100
		return w.renderStatusLocked("downloading")
	}
	if sourceCompletedPattern.MatchString(line) {
		w.activeAdapter = ""
	}
	if match := spotDLFoundSongsPattern.FindStringSubmatch(line); len(match) == 2 {
		total, _ := strconv.Atoi(match[1])
		if total < 0 {
			total = 0
		}
		w.activeAdapter = "spotdl"
		w.itemTotal = total
		w.plannedTotal = 0
		w.completed = 0
		w.track = trackState{}
		if total > 0 {
			return w.renderIdleStatusLocked()
		}
		return nil
	}
	if match := spotDLDownloadedPattern.FindStringSubmatch(line); len(match) == 2 {
		return w.recordSpotDLCompletedTrackLocked(strings.TrimSpace(match[1]))
	}
	if match := spotDLLookupErrorPattern.FindStringSubmatch(line); len(match) == 2 {
		return w.recordSpotDLFailedTrackLocked(strings.TrimSpace(match[1]), "no-match")
	}
	if spotDLAudioProviderPattern.MatchString(line) {
		return w.recordSpotDLFailedTrackLocked(w.nextSpotDLTrackLabelLocked(), "download-error")
	}

	if match := downloadItemPattern.FindStringSubmatch(line); len(match) == 3 {
		if err := w.finalizeTrackLocked(); err != nil {
			return err
		}
		w.activeAdapter = "scdl"
		w.breakOnExistingDetected = false
		w.breakStopPrinted = false
		index, _ := strconv.Atoi(match[1])
		total, _ := strconv.Atoi(match[2])
		w.itemIndex = index
		w.itemTotal = total
		w.track = trackState{}
		return w.renderStatusLocked("preparing track")
	}

	if match := downloadDestinationPattern.FindStringSubmatch(line); len(match) == 2 {
		w.track.Name = trackNameFromPath(match[1])
		w.track.CompletionObserved = false
		w.track.AlreadyPresent = false
		w.track.ProgressKnown = false
		w.track.ProgressPercent = 0
		return w.renderStatusLocked("downloading")
	}

	if match := alreadyDownloadedPattern.FindStringSubmatch(line); len(match) == 2 {
		if w.track.Name == "" {
			w.track.Name = trackNameFromPath(match[1])
		}
		w.track.AlreadyPresent = true
		w.track.ProgressKnown = true
		w.track.ProgressPercent = 100
		w.track.CompletionObserved = true
		return w.renderStatusLocked("already present")
	}

	if match := fragmentProgressPattern.FindStringSubmatch(line); len(match) == 2 {
		percent, _ := strconv.ParseFloat(match[1], 64)
		clamped := clampPercent(percent)
		if w.track.ProgressKnown && clamped < w.track.ProgressPercent {
			clamped = w.track.ProgressPercent
		}
		w.track.ProgressKnown = true
		w.track.ProgressPercent = clamped
		return w.renderStatusLocked("downloading")
	}
	if noisyDownloadProgressPattern.MatchString(line) {
		return nil
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
	if shouldSuppressDeemixNoise(line) {
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
	trackLine := fmt.Sprintf("[track] %s", w.track.Name)
	if total > 0 {
		trackLine = fmt.Sprintf("[track %d/%d] %s", index, total, w.track.Name)
	}

	bits := []string{}
	if stage != "" {
		bits = append(bits, stage)
	}
	if w.track.HasThumbnail {
		bits = append(bits, "thumb:yes")
	}
	if w.track.HasMetadata {
		bits = append(bits, "meta:yes")
	}
	if len(bits) > 0 {
		trackLine = trackLine + " (" + strings.Join(bits, ", ") + ")"
	}
	if w.track.ProgressKnown && !w.track.AlreadyPresent {
		trackLine += " " + renderProgress(w.track.ProgressPercent)
	}

	overallPercent := w.globalProgressPercent()
	globalLine := fmt.Sprintf("[overall] %s (%d/%d)", renderProgressWithWidth(overallPercent, w.globalBarWidth()), index, total)
	if total <= 0 {
		globalLine = fmt.Sprintf("[overall] %s", renderProgressWithWidth(overallPercent, w.globalBarWidth()))
	}

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
	done := w.completed
	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}
	trackLine := fmt.Sprintf("[track] waiting for next track (%d/%d done)", done, total)
	if done >= total {
		trackLine = fmt.Sprintf("[track] all planned tracks complete (%d/%d)", done, total)
	}
	globalLine := fmt.Sprintf("[overall] %s (%d/%d)", renderProgressWithWidth(w.globalProgressPercent(), w.globalBarWidth()), done, total)
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

	display := w.track.Name
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
		display += " (" + strings.Join(flags, ", ") + ")"
	}

	if total := w.effectiveItemTotal(); total > 0 && w.completed < total {
		w.completed++
	}
	w.track = trackState{}
	if w.trackStatusMode != CompactTrackStatusNone {
		if err := w.printPersistentLocked(w.compactTrackResultLineLocked(result, display)); err != nil {
			return err
		}
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
	if total := w.effectiveItemTotal(); total > 0 && w.completed < total {
		w.completed++
	}
	w.track = trackState{}
	line := w.compactTrackResultLineLocked("[skip]", name)
	if strings.TrimSpace(reason) != "" {
		line = fmt.Sprintf("%s (%s)", line, reason)
	}
	if w.trackStatusMode != CompactTrackStatusNone {
		if err := w.printPersistentLocked(line); err != nil {
			return err
		}
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

func (w *CompactLogWriter) compactTrackResultLineLocked(prefix string, display string) string {
	name := strings.TrimSpace(display)
	switch w.trackStatusMode {
	case CompactTrackStatusCount:
		name = w.completedTrackLabelLocked()
	case CompactTrackStatusNone:
		name = ""
	}
	if name == "" {
		return strings.TrimSpace(prefix)
	}
	return fmt.Sprintf("%s %s", prefix, name)
}

func (w *CompactLogWriter) completedTrackLabelLocked() string {
	total := w.effectiveItemTotal()
	done := w.completed
	if done < 1 {
		done = 1
	}
	if total > 0 {
		if done > total {
			done = total
		}
		return fmt.Sprintf("track %d/%d", done, total)
	}
	return fmt.Sprintf("track %d", done)
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
		spotDLFoundSongsPattern.MatchString(trimmed) ||
		spotDLDownloadedPattern.MatchString(trimmed) ||
		spotDLLookupErrorPattern.MatchString(trimmed) ||
		spotDLAudioProviderPattern.MatchString(trimmed) ||
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

func trackNameFromPath(pathLike string) string {
	trimmed := strings.Trim(strings.TrimSpace(pathLike), "\"")
	base := filepath.Base(trimmed)
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}

func renderProgress(percent float64) string {
	return renderProgressWithWidth(percent, 16)
}

func renderProgressWithWidth(percent float64, width int) string {
	clamped := clampPercent(percent)
	if width <= 0 {
		width = 16
	}
	filled := int((clamped / 100) * float64(width))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("#", filled) + strings.Repeat("-", width-filled)
	return fmt.Sprintf("[%s] %5.1f%%", bar, clamped)
}

func (w *CompactLogWriter) globalProgressPercent() float64 {
	total := w.effectiveItemTotal()
	if total <= 0 {
		return 0
	}
	completedItems := w.completed
	if completedItems < 0 {
		completedItems = 0
	}
	if completedItems > total {
		completedItems = total
	}

	partial := 0.0
	if completedItems < total {
		switch {
		case w.track.AlreadyPresent || w.track.CompletionObserved:
			partial = 1.0
		case w.track.ProgressKnown:
			partial = w.track.ProgressPercent / 100.0
		}
	}

	return clampPercent(((float64(completedItems) + partial) / float64(total)) * 100.0)
}

func (w *CompactLogWriter) effectiveItemTotal() int {
	if w.plannedTotal > 0 {
		return w.plannedTotal
	}
	return w.itemTotal
}

func (w *CompactLogWriter) currentTrackIndex(total int) int {
	if total <= 0 {
		return 0
	}
	index := w.completed + 1
	if index < 1 {
		index = 1
	}
	if index > total {
		index = total
	}
	return index
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

func clampPercent(percent float64) float64 {
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}
