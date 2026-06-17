package freedl

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
)

const (
	capturePlanProbeWorkers   = 4
	capturePlanQualityTimeout = 2 * time.Second
)

var (
	streamSoundCloudTracksFn = engine.StreamSoundCloudTracks
	probeSoundCloudFreeDLFn  = engine.ProbeSoundCloudFreeDL
)

type CapturePlanEventKind string

const (
	CapturePlanEventStarted CapturePlanEventKind = "started"
	CapturePlanEventStage   CapturePlanEventKind = "stage"
	CapturePlanEventRow     CapturePlanEventKind = "row"
	CapturePlanEventDone    CapturePlanEventKind = "done"
	CapturePlanEventFailed  CapturePlanEventKind = "failed"
)

type CapturePlanEvent struct {
	Kind    CapturePlanEventKind
	Stage   string
	Status  string
	Detail  string
	Row     PlanRow
	Plan    CapturePlan
	Err     error
	Current int
	Total   int
}

func (s Service) BuildCapturePlanProgress(ctx context.Context, main config.Config, job Job) <-chan CapturePlanEvent {
	events := make(chan CapturePlanEvent, 64)
	go func() {
		defer close(events)
		s.buildCapturePlanProgress(ctx, main, job, events)
	}()
	return events
}

func (s Service) buildCapturePlanProgress(ctx context.Context, main config.Config, job Job, events chan<- CapturePlanEvent) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	send := func(event CapturePlanEvent) bool {
		select {
		case <-ctx.Done():
			return false
		case events <- event:
			return true
		}
	}
	stage := func(name, status, detail string) bool {
		return send(CapturePlanEvent{Kind: CapturePlanEventStage, Stage: name, Status: status, Detail: detail})
	}
	fail := func(err error) {
		_ = send(CapturePlanEvent{Kind: CapturePlanEventFailed, Err: err})
		cancel()
	}

	now := s.now()
	runID := now.Format("20060102-150405")
	libraryDir, err := config.ExpandPath(job.LibraryDir)
	if err != nil {
		fail(fmt.Errorf("resolve library_dir: %w", err))
		return
	}
	bufferRoot, err := config.ExpandPath(job.BufferDir)
	if err != nil {
		fail(fmt.Errorf("resolve buffer_dir: %w", err))
		return
	}
	logDir, err := config.ExpandPath(job.LogDir)
	if err != nil {
		fail(fmt.Errorf("resolve log_dir: %w", err))
		return
	}

	plan := CapturePlan{
		RunID:      runID,
		CreatedAt:  now,
		Job:        job,
		Rows:       []PlanRow{},
		BufferRoot: filepath.Join(bufferRoot, runID),
		LogDir:     filepath.Join(logDir, runID),
	}
	if !send(CapturePlanEvent{Kind: CapturePlanEventStarted, Plan: plan}) {
		return
	}

	source := sourceForJob(job, libraryDir)
	var mu sync.Mutex
	rows := []PlanRow{}
	rowByID := map[string]int{}
	planRowByID := map[string]engine.PlanRow{}
	localByTitle := map[string][]mediaFile{}
	localReady := false

	emitRow := func(row PlanRow) bool {
		return send(CapturePlanEvent{Kind: CapturePlanEventRow, Row: row})
	}
	upsertRow := func(row PlanRow) bool {
		mu.Lock()
		if idx, ok := rowByID[row.RemoteID]; ok {
			rows[idx] = row
		} else {
			rowByID[row.RemoteID] = len(rows)
			rows = append(rows, row)
		}
		out := row
		mu.Unlock()
		return emitRow(out)
	}
	updateRow := func(remoteID string, mutate func(*PlanRow)) bool {
		mu.Lock()
		idx, ok := rowByID[remoteID]
		if !ok {
			mu.Unlock()
			return true
		}
		mutate(&rows[idx])
		out := rows[idx]
		mu.Unlock()
		return emitRow(out)
	}
	rowSnapshot := func() []PlanRow {
		mu.Lock()
		defer mu.Unlock()
		out := make([]PlanRow, len(rows))
		copy(out, rows)
		return out
	}
	applyLocalIfReady := func(row *PlanRow) {
		if !localReady {
			return
		}
		local := bestLocalForTitle(row.Title, localByTitle)
		if local == nil {
			return
		}
		row.LocalPath = local.Path
		row.LocalQuality = local.Quality
	}
	recomputeSelectable := func(row *PlanRow, planRow engine.PlanRow) {
		selectable := planRow.Toggleable && row.FreeDLProbe.Status == engine.SoundCloudFreeDLAvailable
		row.Selectable = selectable
		row.Selected = selectable
		switch {
		case !planRow.Toggleable:
			row.SkipReason = "already-present"
		case row.FreeDLProbe.Status != "" && row.FreeDLProbe.Status != engine.SoundCloudFreeDLAvailable:
			row.SkipReason = string(row.FreeDLProbe.Status)
		case !selectable:
			row.SkipReason = "pending"
		default:
			row.SkipReason = ""
		}
	}

	localDone := make(chan struct{})
	go func() {
		defer close(localDone)
		if !stage("local_quality", "running", "scanning library and probing audio quality") {
			return
		}
		files, scanErr := collectMediaFiles(ctx, libraryDir, capturePlanQualityTimeout, true)
		if scanErr != nil {
			_ = stage("local_quality", "failed", scanErr.Error())
			return
		}
		index := indexMediaByTitle(files)
		mu.Lock()
		localByTitle = index
		localReady = true
		updated := make([]PlanRow, 0, len(rows))
		for idx := range rows {
			applyLocalIfReady(&rows[idx])
			updated = append(updated, rows[idx])
		}
		mu.Unlock()
		for _, row := range updated {
			if !emitRow(row) {
				return
			}
		}
		_ = stage("local_quality", "done", fmt.Sprintf("%d media file(s)", len(files)))
	}()

	probeJobs := make(chan engine.PlanRow, 32)
	var probeWG sync.WaitGroup
	probeJobsClosed := false
	closeProbeJobs := func() {
		if !probeJobsClosed {
			close(probeJobs)
			probeJobsClosed = true
		}
	}
	defer func() {
		closeProbeJobs()
		<-localDone
		probeWG.Wait()
	}()
	if !stage("free_dl", "running", "checking track pages") {
		return
	}
	for i := 0; i < capturePlanProbeWorkers; i++ {
		probeWG.Add(1)
		go func() {
			defer probeWG.Done()
			for row := range probeJobs {
				probe := probeSoundCloudFreeDLFn(ctx, row)
				if ctx.Err() != nil {
					return
				}
				_ = updateRow(row.RemoteID, func(target *PlanRow) {
					target.FreeDLProbe = probe
					if planRow, ok := planRowByID[row.RemoteID]; ok {
						recomputeSelectable(target, planRow)
					} else if !target.Selectable && probe.Status != engine.SoundCloudFreeDLAvailable {
						target.SkipReason = string(probe.Status)
					}
				})
			}
		}()
	}

	if !stage("playlist", "running", "enumerating SoundCloud tracks") {
		return
	}
	tracks, err := streamSoundCloudTracksFn(ctx, source, job.PlanLimit, func(track engine.SoundCloudRemoteTrack) error {
		row := PlanRow{
			Index:     len(rowSnapshot()) + 1,
			RemoteID:  track.ID,
			RemoteURL: track.URL,
			Title:     track.Title,
		}
		mu.Lock()
		applyLocalIfReady(&row)
		mu.Unlock()
		if !upsertRow(row) {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case probeJobs <- engine.PlanRow{Index: row.Index, RemoteID: row.RemoteID, RemoteURL: row.RemoteURL, Title: row.Title}:
			return nil
		}
	})
	closeProbeJobs()
	if err != nil {
		fail(err)
		return
	}
	if !stage("playlist", "done", fmt.Sprintf("%d track(s)", len(tracks))) {
		return
	}

	if !stage("state_archive", "running", "resolving downloaded and archived tracks") {
		return
	}
	provider := engine.NewSCDLPlanProvider()
	sourcePlan, err := provider.BuildWithTracks(ctx, main, source, engine.SyncOptions{PlanLimit: job.PlanLimit}, tracks)
	if err != nil {
		fail(err)
		return
	}
	planRows := sourcePlan.Rows()
	if !stage("state_archive", "done", fmt.Sprintf("%d row(s)", len(planRows))) {
		return
	}
	for _, planRow := range planRows {
		if !updateRow(planRow.RemoteID, func(target *PlanRow) {
			planRowByID[planRow.RemoteID] = planRow
			target.Index = planRow.Index
			target.RemoteID = planRow.RemoteID
			target.RemoteURL = planRow.RemoteURL
			target.Title = planRow.Title
			recomputeSelectable(target, planRow)
		}) {
			return
		}
	}

	<-localDone
	probeWG.Wait()
	for _, planRow := range planRows {
		if !updateRow(planRow.RemoteID, func(target *PlanRow) {
			recomputeSelectable(target, planRow)
		}) {
			return
		}
	}
	if !stage("free_dl", "done", fmt.Sprintf("%d row(s)", len(planRows))) {
		return
	}

	plan.Rows = rowSnapshot()
	_ = WriteJSON(filepath.Join(plan.LogDir, "capture-plan.json"), plan)
	_ = send(CapturePlanEvent{Kind: CapturePlanEventDone, Plan: plan})
}
