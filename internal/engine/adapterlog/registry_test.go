package adapterlog

import (
	"testing"

	"github.com/jaa/update-downloads/internal/engine/progress"
)

type fakeParser struct{}

func (fakeParser) OnStdoutLine(line string) {}
func (fakeParser) OnStderrLine(line string) {}
func (fakeParser) Flush() []progress.TrackEvent {
	return []progress.TrackEvent{{Kind: progress.TrackDone}}
}

func TestRegistryReturnsNoopParserWhenMissing(t *testing.T) {
	reg := NewRegistry()
	parser := reg.ParserFor("missing")
	events := parser.Flush()
	if events != nil {
		t.Fatalf("expected nil events from noop parser, got %+v", events)
	}
}

func TestRegistryReturnsRegisteredParser(t *testing.T) {
	reg := NewRegistry()
	reg.Register("scdl", fakeParser{})
	parser := reg.ParserFor("scdl")
	events := parser.Flush()
	if len(events) != 1 || events[0].Kind != progress.TrackDone {
		t.Fatalf("unexpected parser events: %+v", events)
	}
}

func TestRegistryReturnsFactoryParser(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterFactory("spotdl", func() Parser {
		return fakeParser{}
	})
	parser := reg.ParserFor("spotdl")
	events := parser.Flush()
	if len(events) != 1 || events[0].Kind != progress.TrackDone {
		t.Fatalf("unexpected parser events: %+v", events)
	}
}
