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
var downloadItemPattern = regexp.MustCompile(`^\[download\] Downloading item ([0-9]+) of ([0-9]+)$`)
var downloadDestinationPattern = regexp.MustCompile(`^\[download\] Destination: (.+)$`)
var alreadyDownloadedPattern = regexp.MustCompile(`^\[download\] (.+) has already been downloaded$`)

type CompactLogOptions struct {
	Interactive bool
}

type CompactLogWriter struct {
	dst         io.Writer
	interactive bool

	mu         sync.Mutex
	buf        []byte
	activeLine string

	itemIndex int
	itemTotal int
	track     trackState
}

type trackState struct {
	Name               string
	HasThumbnail       bool
	HasMetadata        bool
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
	return w.clearActiveLineLocked()
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
	if noisyDownloadProgressPattern.MatchString(line) {
		return nil
	}

	if match := downloadItemPattern.FindStringSubmatch(line); len(match) == 3 {
		if err := w.finalizeTrackLocked(); err != nil {
			return err
		}
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
		return w.renderStatusLocked("downloading")
	}

	if match := alreadyDownloadedPattern.FindStringSubmatch(line); len(match) == 2 {
		if w.track.Name == "" {
			w.track.Name = trackNameFromPath(match[1])
		}
		w.track.AlreadyPresent = true
		w.track.CompletionObserved = true
		return w.renderStatusLocked("already present")
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
		w.track.CompletionObserved = true
		return w.renderStatusLocked("finalizing")
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

	status := fmt.Sprintf("[in-progress] %s", w.track.Name)
	if w.itemIndex > 0 && w.itemTotal > 0 {
		status = fmt.Sprintf("[in-progress %d/%d] %s", w.itemIndex, w.itemTotal, w.track.Name)
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
		status = status + " (" + strings.Join(bits, ", ") + ")"
	}

	if status == w.activeLine {
		return nil
	}
	w.activeLine = status
	_, err := fmt.Fprintf(w.dst, "\r\033[2K%s", status)
	return err
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
	if err := w.clearActiveLineLocked(); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w.dst, line)
	return err
}

func (w *CompactLogWriter) clearActiveLineLocked() error {
	if !w.interactive || w.activeLine == "" {
		return nil
	}
	w.activeLine = ""
	_, err := fmt.Fprint(w.dst, "\r\033[2K")
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
