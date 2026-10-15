package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/output"
	"github.com/jaa/update-downloads/internal/runstate"
)

func TestRunCancellationRemainsBoundedDuringProgressFlood(t *testing.T) {
	emitter, _ := newTestAgentSyncEmitter(t, []engine.PlanRow{
		{Index: 1, RemoteID: "track-1", Title: "Track", Status: engine.PlanRowMissingNew, Toggleable: true},
	}, []int{1}, engine.DownloadOrderNewestFirst)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			_ = emitter.Emit(output.Event{
				Timestamp: time.Now(), Event: output.EventTrackProgress, SourceID: "source-a",
				Details: map[string]any{"index": 1, "percent": float64(i % 101)},
			})
		}
	}()

	registry := NewRunRegistry()
	runID, ctx, err := registry.Register(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if !registry.Cancel(runID) {
		t.Fatal("cancel did not acknowledge registered run")
	}
	select {
	case <-ctx.Done():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("cancellation acknowledgement was starved by progress flood")
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("cancellation acknowledgement took %s during flood", elapsed)
	}
	<-done
	if err := emitter.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAgentSyncEmitterCoalescesThousandProgressEventsAndKeepsNewest(t *testing.T) {
	emitter, outputBuffer := newTestAgentSyncEmitter(t, []engine.PlanRow{
		{Index: 1, RemoteID: "track-1", Title: "Track", Status: engine.PlanRowMissingNew, Toggleable: true},
	}, []int{1}, engine.DownloadOrderNewestFirst)

	for i := 0; i < 1000; i++ {
		if err := emitter.Emit(output.Event{
			Timestamp: time.Now(), Event: output.EventTrackProgress, SourceID: "source-a",
			Details: map[string]any{"index": 1, "total": 1, "percent": float64(i % 101)},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := emitter.Close(); err != nil {
		t.Fatal(err)
	}

	frames := decodeEmitterFrames(t, outputBuffer.Bytes())
	if len(frames) > 2 {
		t.Fatalf("1000-event burst produced %d frames, want at most first+latest", len(frames))
	}
	if len(frames) == 0 || frames[len(frames)-1].Method != "sync.progress" {
		t.Fatalf("missing final sync.progress frame: %+v", frames)
	}
	var latest syncProgressParams
	if err := json.Unmarshal(frames[len(frames)-1].Params, &latest); err != nil {
		t.Fatal(err)
	}
	if latest.Progress.Track.ProgressPercent != 90 { // 999 % 101
		t.Fatalf("latest coalesced percent = %v, want 90", latest.Progress.Track.ProgressPercent)
	}
}

func TestAgentSyncEmitterForcedFlushStillRespectsTenHertz(t *testing.T) {
	emitter, _ := newTestAgentSyncEmitter(t, []engine.PlanRow{
		{Index: 1, RemoteID: "track-1", Title: "Track", Status: engine.PlanRowMissingNew, Toggleable: true},
	}, []int{1}, engine.DownloadOrderNewestFirst)

	for _, percent := range []float64{10, 20} {
		if err := emitter.Emit(output.Event{
			Timestamp: time.Now(), Event: output.EventTrackProgress, SourceID: "source-a",
			Details: map[string]any{"index": 1, "total": 1, "percent": percent},
		}); err != nil {
			t.Fatal(err)
		}
	}
	started := time.Now()
	if err := emitter.Emit(output.Event{
		Timestamp: time.Now(), Event: output.EventTrackDone, SourceID: "source-a",
		Details: map[string]any{"index": 1, "total": 1},
	}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 80*time.Millisecond {
		t.Fatalf("forced progress flush bypassed 100 ms interval: %s", elapsed)
	}
	if err := emitter.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAgentSyncEmitterFlushesProgressBeforeOutcomeAndResolvesCanonicalRow(t *testing.T) {
	rows := []engine.PlanRow{
		{Index: 5, RemoteID: "newer", Title: "Newer", Status: engine.PlanRowMissingNew, Toggleable: true},
		{Index: 8, RemoteID: "older", Title: "Older", Status: engine.PlanRowMissingKnownGap, Toggleable: true},
	}
	emitter, outputBuffer := newTestAgentSyncEmitter(t, rows, []int{5, 8}, engine.DownloadOrderOldestFirst)

	for _, percent := range []float64{10, 73} {
		if err := emitter.Emit(output.Event{
			Timestamp: time.Now(), Event: output.EventTrackProgress, SourceID: "source-a",
			Details: map[string]any{"index": 1, "total": 2, "percent": percent},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := emitter.Emit(output.Event{
		Timestamp: time.Now(), Event: output.EventTrackDone, SourceID: "source-a",
		Details: map[string]any{"index": 1, "total": 2},
	}); err != nil {
		t.Fatal(err)
	}
	if err := emitter.Close(); err != nil {
		t.Fatal(err)
	}

	frames := decodeEmitterFrames(t, outputBuffer.Bytes())
	doneIndex := -1
	for i, frame := range frames {
		if frame.Method != "sync.event" {
			continue
		}
		var event syncEventParams
		if err := json.Unmarshal(frame.Params, &event); err != nil {
			t.Fatal(err)
		}
		if event.Event.Event == output.EventTrackDone {
			doneIndex = i
			break
		}
	}
	if doneIndex < 1 || frames[doneIndex-1].Method != "sync.progress" {
		t.Fatalf("pending progress was not immediately before track_done: %+v", frames)
	}
	var progress syncProgressParams
	if err := json.Unmarshal(frames[doneIndex-1].Params, &progress); err != nil {
		t.Fatal(err)
	}
	if progress.Progress.Track.ProgressPercent != 73 {
		t.Fatalf("flushed percent = %v, want newest 73", progress.Progress.Track.ProgressPercent)
	}
	if progress.Row == nil || progress.Row.Index != 8 || progress.Row.ExecutionSlot != 1 {
		t.Fatalf("progress row = %+v, want canonical oldest row index 8 / slot 1", progress.Row)
	}
}

func TestAgentSyncEmitterNeverCoalescesLifecycleEvents(t *testing.T) {
	emitter, outputBuffer := newTestAgentSyncEmitter(t, nil, nil, engine.DefaultDownloadOrder)
	want := []output.EventName{
		output.EventSourcePreflight,
		output.EventSourceStarted,
		output.EventSourceFailed,
		output.EventSyncFinished,
	}
	for _, name := range want {
		if err := emitter.Emit(output.Event{Timestamp: time.Now(), Event: name, SourceID: "source-a"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := emitter.Close(); err != nil {
		t.Fatal(err)
	}

	got := []output.EventName{}
	for _, frame := range decodeEmitterFrames(t, outputBuffer.Bytes()) {
		if frame.Method != "sync.event" {
			continue
		}
		var event syncEventParams
		if err := json.Unmarshal(frame.Params, &event); err != nil {
			t.Fatal(err)
		}
		got = append(got, event.Event.Event)
	}
	if strings.Join(eventNames(got), ",") != strings.Join(eventNames(want), ",") {
		t.Fatalf("lifecycle events = %v, want %v", got, want)
	}
}

func newTestAgentSyncEmitter(
	t *testing.T,
	rows []engine.PlanRow,
	selected []int,
	order engine.DownloadOrder,
) (*agentSyncEmitter, *bytes.Buffer) {
	t.Helper()
	tracker := runstate.NewTracker()
	tracker.Reset([]config.Source{{ID: "source-a"}})
	if rows != nil {
		manifest, err := engine.BuildExecutionManifest("source-a", rows, selected, order)
		if err != nil {
			t.Fatal(err)
		}
		selectedSet := make(map[int]bool, len(selected))
		for _, index := range selected {
			selectedSet[index] = true
		}
		planRows := make([]runstate.PlanTrackRow, 0, len(rows))
		for _, row := range rows {
			planRows = append(planRows, runstate.PlanTrackRow{
				SourceID: "source-a", SourceLabel: "source-a", RemoteID: row.RemoteID,
				Title: row.Title, Index: row.Index, Toggleable: row.Toggleable,
				PlanStatus: row.Status, PlanClass: runstate.TrackPlanClassFromPlanStatus(row.Status),
			})
		}
		tracker.ConfirmSelection("source-a", planRows, manifest, func(index int) bool { return selectedSet[index] })
	}
	buffer := &bytes.Buffer{}
	return &agentSyncEmitter{
		conn: NewConn(nil, buffer), runID: "run-1", tracker: tracker,
		progress: output.NewStructuredProgressTracker(nil),
	}, buffer
}

func decodeEmitterFrames(t *testing.T, payload []byte) []envelope {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(string(payload)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	frames := make([]envelope, 0, len(lines))
	for _, line := range lines {
		var frame envelope
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("decode frame: %v\n%s", err, line)
		}
		frames = append(frames, frame)
	}
	return frames
}

func eventNames(values []output.EventName) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = string(value)
	}
	return result
}
