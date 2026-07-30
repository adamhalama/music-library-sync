package freedl

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/playlists"
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
	return s.buildCapturePlanProgressStream(ctx, main, job, nil)
}

func (s Service) BuildCapturePlanProgressForPlaylist(ctx context.Context, main config.Config, job Job, snapshot playlists.Snapshot) <-chan CapturePlanEvent {
	return s.buildCapturePlanProgressStream(ctx, main, job, &snapshot)
}

func (s Service) buildCapturePlanProgressStream(ctx context.Context, main config.Config, job Job, snapshot *playlists.Snapshot) <-chan CapturePlanEvent {
	events := make(chan CapturePlanEvent, 64)
	go func() {
		defer close(events)
		s.buildCapturePlanProgress(ctx, main, job, snapshot, events)
	}()
	return events
}

func (s Service) buildCapturePlanProgress(ctx context.Context, main config.Config, job Job, snapshot *playlists.Snapshot, events chan<- CapturePlanEvent) {
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
	stageProgress := func(name, status, detail string, current, total int) bool {
		return send(CapturePlanEvent{
			Kind:    CapturePlanEventStage,
			Stage:   name,
			Status:  status,
			Detail:  detail,
			Current: current,
			Total:   total,
		})
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
	if snapshot != nil {
		if err := playlists.ValidateSnapshot(*snapshot); err != nil {
			fail(err)
			return
		}
		plan.PlaylistID = snapshot.PlaylistID
		plan.PlaylistChecksum = snapshot.ChecksumSHA256
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
	localByPath := map[string]mediaFile{}
	localPathByRemoteID := map[string]string{}
	localReady := false
	localTitleCounts := map[string]int{}
	var localErr error
	localProbeReady := make(chan struct{})
	var localProbeReadyOnce sync.Once
	releaseLocalProbes := func() {
		localProbeReadyOnce.Do(func() { close(localProbeReady) })
	}
	defer releaseLocalProbes()
	if statePath, resolveErr := config.ResolveStateFile(main.Defaults.StateDir, job.StateFile); resolveErr == nil {
		if entries, readErr := readCaptureState(statePath); readErr == nil {
			for _, entry := range entries {
				path := strings.TrimSpace(entry.Path)
				if path == "" {
					continue
				}
				if !filepath.IsAbs(path) {
					path = filepath.Join(libraryDir, filepath.FromSlash(path))
				}
				localPathByRemoteID[strings.TrimSpace(entry.TrackID)] = cleanPath(path)
			}
		}
	}

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
		if row.LocalPath != "" {
			if row.LocalState == "" {
				if row.LocalQuality.Error != "" {
					row.LocalState = LocalLookupError
				} else {
					row.LocalState = LocalLookupMatched
				}
			}
			return
		}
		if path := localPathByRemoteID[row.RemoteID]; path != "" {
			if local, ok := localByPath[path]; ok {
				row.LocalPath = local.Path
				row.LocalQuality = local.Quality
				if local.Quality.Error != "" {
					row.LocalState = LocalLookupError
				} else if local.Cached {
					row.LocalState = LocalLookupCached
				} else {
					row.LocalState = LocalLookupMatched
				}
				return
			}
		}
		local := bestLocalForTitle(row.Title, localByTitle)
		if local == nil {
			if localReady {
				row.LocalState = LocalLookupNotFound
			}
			return
		}
		row.LocalPath = local.Path
		row.LocalQuality = local.Quality
		if local.Quality.Error != "" {
			row.LocalState = LocalLookupError
		} else if local.Cached {
			row.LocalState = LocalLookupCached
		} else if localReady {
			row.LocalState = LocalLookupMatched
		}
	}
	recomputeSelectable := func(row *PlanRow, planRow engine.PlanRow) {
		playlistAllowed := true
		if snapshot != nil {
			row.PlaylistMatch, row.PlaylistTrackIndex, playlistAllowed = capturePlaylistMatch(*snapshot, row.LocalPath, row.Title)
		}
		selectable := planRow.Toggleable && row.FreeDLProbe.Status == engine.SoundCloudFreeDLAvailable && playlistAllowed
		row.Selectable = selectable
		row.Selected = selectable
		switch {
		case snapshot != nil && row.PlaylistMatch == playlists.MatchAmbiguous:
			row.SkipReason = "playlist-ambiguous"
		case snapshot != nil && row.PlaylistMatch == playlists.MatchNone:
			row.SkipReason = "not-in-playlist"
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
		if !stage("local_quality", "running", "indexing local media") {
			return
		}
		files, titleCounts, scanErr := scanLocalMediaCached(
			ctx,
			libraryDir,
			localMediaCachePath(logDir),
			capturePlanQualityTimeout,
			localProbeReady,
			func(candidate localMediaCandidate) bool {
				candidateKey := normalizeKey(strings.TrimSuffix(filepath.Base(candidate.Path), filepath.Ext(candidate.Path)))
				candidatePath := cleanPath(candidate.Path)
				mu.Lock()
				defer mu.Unlock()
				for _, row := range rows {
					if localPathByRemoteID[row.RemoteID] == candidatePath || normalizeKey(row.Title) == candidateKey {
						return true
					}
				}
				return false
			},
			func() bool {
				mu.Lock()
				defer mu.Unlock()
				if len(rows) == 0 {
					return false
				}
				for _, row := range rows {
					if row.LocalPath == "" {
						return true
					}
				}
				return false
			},
			func(candidate localMediaCandidate) {
				candidateKey := normalizeKey(strings.TrimSuffix(filepath.Base(candidate.Path), filepath.Ext(candidate.Path)))
				candidatePath := cleanPath(candidate.Path)
				mu.Lock()
				updated := make([]PlanRow, 0, len(rows))
				for idx := range rows {
					statePathMatch := localPathByRemoteID[rows[idx].RemoteID] == candidatePath
					titleMatch := normalizeKey(rows[idx].Title) == candidateKey
					if rows[idx].LocalPath == "" && (statePathMatch || titleMatch) {
						rows[idx].LocalState = LocalLookupProbing
						updated = append(updated, rows[idx])
					}
				}
				mu.Unlock()
				for _, row := range updated {
					if !emitRow(row) {
						return
					}
				}
			},
			func(progress localMediaScanProgress) {
				detail := fmt.Sprintf("indexed %d files", progress.Discovered)
				if progress.Cached > 0 || progress.Probed > 0 {
					detail = fmt.Sprintf("cache %d/%d · probed %d/%d", progress.Cached, progress.Total, progress.Probed, progress.Total-progress.Cached)
				}
				_ = stageProgress("local_quality", "running", detail, progress.Cached+progress.Probed, progress.Total)
			},
			func(result localMediaResult) {
				result.File.Cached = result.Cached
				mu.Lock()
				localByPath[cleanPath(result.File.Path)] = result.File
				for _, key := range []string{result.File.TitleKey, result.File.Key} {
					if key != "" {
						localByTitle[key] = append(localByTitle[key], result.File)
					}
				}
				updated := make([]PlanRow, 0, len(rows))
				for idx := range rows {
					statePathMatch := localPathByRemoteID[rows[idx].RemoteID] == cleanPath(result.File.Path)
					titleMatch := normalizeKey(rows[idx].Title) == result.File.Key || normalizeKey(rows[idx].Title) == result.File.TitleKey
					if rows[idx].LocalPath != "" || !statePathMatch && !titleMatch {
						continue
					}
					rows[idx].LocalPath = result.File.Path
					rows[idx].LocalQuality = result.File.Quality
					switch {
					case result.File.Quality.Error != "":
						rows[idx].LocalState = LocalLookupError
					case result.Cached:
						rows[idx].LocalState = LocalLookupCached
					default:
						rows[idx].LocalState = LocalLookupMatched
					}
					updated = append(updated, rows[idx])
				}
				mu.Unlock()
				for _, row := range updated {
					if !emitRow(row) {
						return
					}
				}
			},
		)
		if scanErr != nil {
			localErr = scanErr
			if !isContextError(scanErr) {
				_ = stage("local_quality", "failed", scanErr.Error())
			}
			return
		}
		mu.Lock()
		localTitleCounts = titleCounts
		localReady = true
		updated := make([]PlanRow, 0, len(rows))
		for idx := range rows {
			applyLocalIfReady(&rows[idx])
			if rows[idx].LocalPath == "" {
				rows[idx].LocalState = LocalLookupNotFound
			}
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
			Index:      len(rowSnapshot()) + 1,
			RemoteID:   track.ID,
			RemoteURL:  track.URL,
			Title:      track.Title,
			LocalState: LocalLookupMatching,
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
	releaseLocalProbes()
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
	<-localDone
	if localErr != nil {
		if !isContextError(localErr) {
			fail(localErr)
		}
		return
	}
	provider := engine.NewSCDLPlanProvider()
	sourcePlan, err := provider.BuildWithTracksAndLocalIndex(
		ctx,
		main,
		source,
		engine.SyncOptions{PlanLimit: job.PlanLimit},
		tracks,
		localTitleCounts,
	)
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

func capturePlaylistMatch(snapshot playlists.Snapshot, localPath, remoteTitle string) (playlists.MatchStatus, int, bool) {
	match := playlists.MatchTrack(snapshot, localPath, remoteTitle)
	return match.Status, match.Index + 1, match.Status == playlists.MatchPath || match.Status == playlists.MatchMetadata
}
