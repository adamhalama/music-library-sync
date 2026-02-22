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

	itemIndex    int
	itemTotal    int
	plannedTotal int
	track        trackState
	barWidth     int

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
		w.track = trackState{}
	}

	if match := preflightSummaryPattern.FindStringSubmatch(line); len(match) == 2 {
		planned, _ := strconv.Atoi(match[1])
		if planned < 0 {
			planned = 0
		}
		w.plannedTotal = planned
		w.itemIndex = 0
		w.itemTotal = 0
		w.track = trackState{}
	}

	if match := downloadItemPattern.FindStringSubmatch(line); len(match) == 3 {
		if err := w.finalizeTrackLocked(); err != nil {
			return err
		}
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

	index, total := w.currentTrackCounters()
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

	w.track = trackState{}
	return w.printPersistentLocked(line)
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
	completedItems := w.itemIndex - 1
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

func (w *CompactLogWriter) currentTrackCounters() (int, int) {
	total := w.effectiveItemTotal()
	if total <= 0 {
		return 0, 0
	}
	index := w.itemIndex
	if index < 1 {
		index = 1
	}
	if index > total {
		index = total
	}
	return index, total
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
