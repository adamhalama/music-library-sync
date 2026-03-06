package adapterlog

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/jaa/update-downloads/internal/engine/progress"
)

var deemixTrackProgressPattern = regexp.MustCompile(`^\[[^\]]+\]\s+deemix track ([0-9]+)\/([0-9]+)\s+([A-Za-z0-9]{10,32})(?:\s+\((.+)\))?$`)
var deemixTrackDonePattern = regexp.MustCompile(`^\[[^\]]+\]\s+\[done\]\s+([A-Za-z0-9]{10,32})(?:\s+\(([^)]+)\))?$`)
var deemixTrackSkipPattern = regexp.MustCompile(`^\[[^\]]+\]\s+\[skip\]\s+([A-Za-z0-9]{10,32})(?:\s+\(([^)]+)\))?(?:\s+\(([^)]+)\))?$`)
var deemixTrackFailurePattern = regexp.MustCompile(`^(?:ERROR:\s*)?\[[^\]]+\]\s+command failed with exit code ([0-9]+)$`)
var deemixDownloadProgressPattern = regexp.MustCompile(`^\[([^\]]+)\]\s+Downloading:\s+([0-9]+(?:\.[0-9]+)?)%$`)
var deemixDownloadCompletePattern = regexp.MustCompile(`^\[([^\]]+)\]\s+Download complete$`)

type DeemixParser struct {
	mu           sync.Mutex
	events       []progress.TrackEvent
	currentID    string
	currentName  string
	currentIndex int
	currentTotal int
}

func NewDeemixParser() Parser {
	return &DeemixParser{}
}

func (p *DeemixParser) OnStdoutLine(line string) {
	p.onLine(line)
}

func (p *DeemixParser) OnStderrLine(line string) {
	p.onLine(line)
}

func (p *DeemixParser) onLine(line string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if match := deemixTrackProgressPattern.FindStringSubmatch(trimmed); len(match) >= 4 {
		index, _ := strconv.Atoi(match[1])
		total, _ := strconv.Atoi(match[2])
		trackID := strings.TrimSpace(match[3])
		name := trackID
		if len(match) >= 5 {
			label := strings.TrimSpace(match[4])
			if label != "" {
				name = label
			}
		}
		p.currentIndex = index
		p.currentTotal = total
		p.currentID = trackID
		p.currentName = name
		p.events = append(p.events, progress.TrackEvent{
			Kind:      progress.TrackStarted,
			TrackID:   trackID,
			TrackName: name,
			Index:     index,
			Total:     total,
		})
		return
	}

	if match := deemixDownloadProgressPattern.FindStringSubmatch(trimmed); len(match) == 3 {
		title := strings.TrimSpace(match[1])
		if title != "" {
			p.currentName = title
		}
		percent, _ := strconv.ParseFloat(match[2], 64)
		p.events = append(p.events, progress.TrackEvent{
			Kind:      progress.TrackProgress,
			TrackID:   p.currentID,
			TrackName: p.currentName,
			Index:     p.currentIndex,
			Total:     p.currentTotal,
			Percent:   percent,
		})
		return
	}

	if match := deemixDownloadCompletePattern.FindStringSubmatch(trimmed); len(match) == 2 {
		title := strings.TrimSpace(match[1])
		if title != "" {
			p.currentName = title
		}
		p.events = append(p.events, progress.TrackEvent{
			Kind:      progress.TrackProgress,
			TrackID:   p.currentID,
			TrackName: p.currentName,
			Index:     p.currentIndex,
			Total:     p.currentTotal,
			Percent:   100,
		})
		return
	}

	if match := deemixTrackDonePattern.FindStringSubmatch(trimmed); len(match) >= 2 {
		trackID := strings.TrimSpace(match[1])
		name := p.currentName
		if len(match) >= 3 {
			label := strings.TrimSpace(match[2])
			if label != "" {
				name = label
			}
		}
		if name == "" {
			name = trackID
		}
		p.events = append(p.events, progress.TrackEvent{
			Kind:      progress.TrackDone,
			TrackID:   trackID,
			TrackName: name,
			Index:     p.currentIndex,
			Total:     p.currentTotal,
			Percent:   100,
		})
		return
	}

	if match := deemixTrackSkipPattern.FindStringSubmatch(trimmed); len(match) >= 2 {
		trackID := strings.TrimSpace(match[1])
		name := trackID
		if len(match) >= 3 {
			label := strings.TrimSpace(match[2])
			if label != "" {
				name = label
			}
		}
		reason := ""
		if len(match) >= 4 {
			reason = strings.TrimSpace(match[3])
		}
		p.events = append(p.events, progress.TrackEvent{
			Kind:      progress.TrackSkip,
			TrackID:   trackID,
			TrackName: name,
			Index:     p.currentIndex,
			Total:     p.currentTotal,
			Reason:    reason,
		})
		return
	}

	if match := deemixTrackFailurePattern.FindStringSubmatch(trimmed); len(match) == 2 {
		p.events = append(p.events, progress.TrackEvent{
			Kind:      progress.TrackFail,
			TrackID:   p.currentID,
			TrackName: p.currentName,
			Index:     p.currentIndex,
			Total:     p.currentTotal,
			Reason:    fmt.Sprintf("exit-%s", strings.TrimSpace(match[1])),
		})
	}
}

func (p *DeemixParser) Flush() []progress.TrackEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.events) == 0 {
		return nil
	}
	out := append([]progress.TrackEvent(nil), p.events...)
	p.events = p.events[:0]
	return out
}
