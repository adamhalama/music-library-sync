package adapterlog

import (
	"testing"

	"github.com/jaa/update-downloads/internal/engine/progress"
)

func TestSCDLParserEmitsTrackLifecycle(t *testing.T) {
	parser := NewSCDLParser()
	parser.OnStdoutLine("[download] Downloading item 2 of 9")
	parser.OnStdoutLine("[download] Destination: /music/Artist - Song.mp3")
	parser.OnStdoutLine("[download] 57.2% of 4.14MiB at 1.00MiB/s ETA 00:02 (frag 11/20)")
	parser.OnStdoutLine("[download] 100% of 4.14MiB in 00:03")

	events := parser.Flush()
	if len(events) < 4 {
		t.Fatalf("expected >=4 events, got %+v", events)
	}
	assertEventKind(t, events[0], progress.TrackStarted)
	assertEventKind(t, events[1], progress.TrackProgress)
	assertEventKind(t, events[len(events)-1], progress.TrackDone)
	if events[1].TrackName != "Artist - Song" {
		t.Fatalf("expected parsed track name, got %+v", events[1])
	}
}

func TestSpotDLParserEmitsDoneAndSkip(t *testing.T) {
	parser := NewSpotDLParser()
	parser.OnStdoutLine("Found 2 songs in https://open.spotify.com/playlist/x")
	parser.OnStdoutLine("Downloaded \"Song A\": https://youtube.com/watch?v=1")
	parser.OnStdoutLine("LookupError: No results found for song: Song B")

	events := parser.Flush()
	if len(events) != 4 {
		t.Fatalf("expected 4 events (start/done + start/skip), got %+v", events)
	}
	assertEventKind(t, events[0], progress.TrackStarted)
	assertEventKind(t, events[1], progress.TrackDone)
	assertEventKind(t, events[2], progress.TrackStarted)
	assertEventKind(t, events[3], progress.TrackSkip)
	if events[3].Reason != "no-match" {
		t.Fatalf("expected no-match reason, got %+v", events[3])
	}
}

func TestDeemixParserEmitsTrackLifecycle(t *testing.T) {
	parser := NewDeemixParser()
	parser.OnStdoutLine("[spotify-source] deemix track 1/3 2abc234def (Artist - Track)")
	parser.OnStdoutLine("[Artist - Track] Downloading: 41.2%")
	parser.OnStdoutLine("[spotify-source] [done] 2abc234def (Artist - Track)")
	parser.OnStderrLine("ERROR: [spotify-source] command failed with exit code 1")

	events := parser.Flush()
	if len(events) < 4 {
		t.Fatalf("expected >=4 events, got %+v", events)
	}
	assertEventKind(t, events[0], progress.TrackStarted)
	assertEventKind(t, events[1], progress.TrackProgress)
	assertEventKind(t, events[2], progress.TrackDone)
	assertEventKind(t, events[3], progress.TrackFail)
}

func assertEventKind(t *testing.T, event progress.TrackEvent, kind progress.TrackEventKind) {
	t.Helper()
	if event.Kind != kind {
		t.Fatalf("expected kind %s, got %+v", kind, event)
	}
}
