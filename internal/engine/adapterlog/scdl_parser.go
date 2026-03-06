package adapterlog

import (
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/jaa/update-downloads/internal/engine/progress"
	compact "github.com/jaa/update-downloads/internal/output/compact"
)

type SCDLParser struct {
	mu           sync.Mutex
	events       []progress.TrackEvent
	currentName  string
	currentIndex int
	currentTotal int
}

func NewSCDLParser() Parser {
	return &SCDLParser{}
}

func (p *SCDLParser) OnStdoutLine(line string) {
	p.onLine(line)
}

func (p *SCDLParser) OnStderrLine(line string) {
	p.onLine(line)
}

func (p *SCDLParser) onLine(line string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if parsed, ok := compact.ParseLine(trimmed); ok {
		switch parsed.Kind {
		case compact.LineEventDownloadItem:
			p.currentIndex = parsed.Index
			p.currentTotal = parsed.Total
			p.currentName = ""
			p.append(progress.TrackEvent{
				Kind:  progress.TrackStarted,
				Index: parsed.Index,
				Total: parsed.Total,
			})
			return
		case compact.LineEventDownloadDestination:
			p.currentName = trackNameFromPathLike(parsed.Text)
			p.append(progress.TrackEvent{
				Kind:      progress.TrackProgress,
				TrackName: p.currentName,
				Index:     p.currentIndex,
				Total:     p.currentTotal,
				Percent:   0,
			})
			return
		case compact.LineEventAlreadyDownloaded:
			name := trackNameFromPathLike(parsed.Text)
			if name == "" {
				name = p.currentName
			}
			p.append(progress.TrackEvent{
				Kind:      progress.TrackSkip,
				TrackName: name,
				Index:     p.currentIndex,
				Total:     p.currentTotal,
				Reason:    "already_downloaded",
			})
			return
		case compact.LineEventFragmentProgress:
			p.append(progress.TrackEvent{
				Kind:      progress.TrackProgress,
				TrackName: p.currentName,
				Index:     p.currentIndex,
				Total:     p.currentTotal,
				Percent:   parsed.Percent,
			})
			return
		}
	}

	if strings.HasPrefix(trimmed, "[download] 100% of ") {
		p.append(progress.TrackEvent{
			Kind:      progress.TrackDone,
			TrackName: p.currentName,
			Index:     p.currentIndex,
			Total:     p.currentTotal,
			Percent:   100,
		})
	}
}

func (p *SCDLParser) Flush() []progress.TrackEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.events) == 0 {
		return nil
	}
	out := append([]progress.TrackEvent(nil), p.events...)
	p.events = p.events[:0]
	return out
}

func (p *SCDLParser) append(event progress.TrackEvent) {
	p.events = append(p.events, event)
}

func trackNameFromPathLike(pathLike string) string {
	trimmed := strings.Trim(strings.TrimSpace(pathLike), "\"")
	if trimmed == "" {
		return ""
	}
	base := filepath.Base(trimmed)
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return strings.TrimSpace(base)
}

type SpotDLParser struct {
	mu        sync.Mutex
	events    []progress.TrackEvent
	total     int
	completed int
}

func NewSpotDLParser() Parser {
	return &SpotDLParser{}
}

func (p *SpotDLParser) OnStdoutLine(line string) {
	p.onLine(line)
}

func (p *SpotDLParser) OnStderrLine(line string) {
	p.onLine(line)
}

func (p *SpotDLParser) onLine(line string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return
	}

	parsed, ok := compact.ParseLine(trimmed)
	if !ok {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	switch parsed.Kind {
	case compact.LineEventSpotDLFoundSongs:
		p.total = parsed.Total
		p.completed = 0
	case compact.LineEventSpotDLDownloaded:
		index := p.completed + 1
		label := strings.TrimSpace(parsed.Text)
		p.events = append(p.events,
			progress.TrackEvent{Kind: progress.TrackStarted, TrackName: label, Index: index, Total: p.total},
			progress.TrackEvent{Kind: progress.TrackDone, TrackName: label, Index: index, Total: p.total, Percent: 100},
		)
		p.completed = index
	case compact.LineEventSpotDLLookupError:
		index := p.completed + 1
		label := strings.TrimSpace(parsed.Text)
		p.events = append(p.events,
			progress.TrackEvent{Kind: progress.TrackStarted, TrackName: label, Index: index, Total: p.total},
			progress.TrackEvent{Kind: progress.TrackSkip, TrackName: label, Index: index, Total: p.total, Reason: "no-match"},
		)
		p.completed = index
	case compact.LineEventSpotDLAudioProvider:
		index := p.completed + 1
		label := "track"
		if p.total > 0 {
			label = "track " + strconv.Itoa(index) + "/" + strconv.Itoa(p.total)
		}
		p.events = append(p.events,
			progress.TrackEvent{Kind: progress.TrackStarted, TrackName: label, Index: index, Total: p.total},
			progress.TrackEvent{Kind: progress.TrackFail, TrackName: label, Index: index, Total: p.total, Reason: "download-error"},
		)
		p.completed = index
	}
}

func (p *SpotDLParser) Flush() []progress.TrackEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.events) == 0 {
		return nil
	}
	out := append([]progress.TrackEvent(nil), p.events...)
	p.events = p.events[:0]
	return out
}
