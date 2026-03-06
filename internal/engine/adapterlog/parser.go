package adapterlog

import "github.com/jaa/update-downloads/internal/engine/progress"

type Parser interface {
	OnStdoutLine(line string)
	OnStderrLine(line string)
	Flush() []progress.TrackEvent
}

type NoopParser struct{}

func (NoopParser) OnStdoutLine(line string) {}
func (NoopParser) OnStderrLine(line string) {}
func (NoopParser) Flush() []progress.TrackEvent {
	return nil
}
