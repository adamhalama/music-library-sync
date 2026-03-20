package progress

type Sink interface {
	RecordTrackEvent(event TrackEvent)
}

type NoopSink struct{}

func (NoopSink) RecordTrackEvent(event TrackEvent) {}
