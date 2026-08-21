import Foundation

extension AppState {
    func loadPlaylistSync() async {
        guard let client else { return }
        do {
            let plannedJob = playlistSyncPlan.flatMap { result in
                playlistSyncJobs.first { $0.id == result.plan.jobID }?.job
            }
            let result = try await client.inspectPlaylistSync()
            playlistSyncJobs = result.jobs
            if let selected = playlistSyncPlan,
               !result.jobs.contains(where: { $0.id == selected.plan.jobID }) {
                playlistSyncPlan = nil
            } else if let selected = playlistSyncPlan,
                      let plannedJob,
                      result.jobs.first(where: { $0.id == selected.plan.jobID })?.job != plannedJob {
                playlistSyncPlan = nil
                playlistSyncStatus = "Pair configuration changed. The previous plan was discarded; choose a direction again."
            }
        } catch {
            playlistSyncStatus = .failure("Playlist sync jobs could not load: \(error.localizedDescription)")
        }
    }

    func planPlaylistSync(jobID: String, direction: String) async {
        guard let client, playlistSyncOperation == nil else { return }
        clearNotResumed(.playlistSync)
        playlistSyncOperation = .plan
        playlistSyncStatus = "Reading both playlists and building an exact-path preview…"
        do {
            let started = try await client.planPlaylistSync(
                PlaylistSyncPlanParams(jobID: jobID, direction: direction, outPath: nil)
            )
            playlistSyncRunID = started.runID
            consumeBufferedFinished(started.runID)
        } catch {
            playlistSyncOperation = nil
            playlistSyncRunID = nil
            playlistSyncStatus = .failure("Playlist plan could not start: \(error.localizedDescription)")
        }
    }

    func applyPlaylistSync(dryRun: Bool = false) async {
        guard let client, let plan = playlistSyncPlan?.plan, playlistSyncOperation == nil else { return }
        clearNotResumed(.playlistSync)
        playlistSyncOperation = .apply(dryRun: dryRun)
        playlistSyncStatus = dryRun
            ? "Revalidating the plan without writing or backing up…"
            : "Revalidating both playlists before the destination backup…"
        do {
            let started = try await client.applyPlaylistSync(
                PlaylistSyncApplyParams(plan: plan, dryRun: dryRun)
            )
            playlistSyncRunID = started.runID
            consumeBufferedFinished(started.runID)
        } catch {
            playlistSyncOperation = nil
            playlistSyncRunID = nil
            playlistSyncStatus = .failure("Playlist apply could not start: \(error.localizedDescription)")
        }
    }

    func cancelPlaylistSync() async {
        guard let runID = playlistSyncRunID else { return }
        await cancelRun(runID)
        if playlistSyncRunID == runID, playlistSyncOperation != nil {
            playlistSyncStatus = .canceled("Cancellation requested. The destination is not reported as changed unless verification completed.")
        }
    }

    func savePlaylistSyncJob(_ job: PlaylistSyncJob) async -> Bool {
        guard let current = playlistConfig?.config else { return false }
        var jobs = current.syncJobs
        if let index = jobs.firstIndex(where: { $0.id.caseInsensitiveCompare(job.id) == .orderedSame }) {
            jobs[index] = job
        } else {
            jobs.append(job)
        }
        let updated = PlaylistConfig(version: current.version, playlists: current.playlists, syncJobs: jobs)
        do {
            try await writePlaylistSyncConfig(updated)
            playlistSyncPlan = nil
            await loadPlaylistSync()
            playlistSyncStatus = WorkflowStatus(message: "Playlist pair saved. Generate a fresh directional plan before applying.", severity: .ok)
            return true
        } catch {
            playlistSyncStatus = .failure("Playlist pair could not save: \(error.localizedDescription)")
            return false
        }
    }

    func discardPlaylistSyncPlan() {
        playlistSyncPlan = nil
        playlistSyncStatus = "Plan discarded. Choose a direction to read fresh provider state."
    }

    func applyPlaylistSyncFinished(_ finished: RunFinishedNotification) {
        let operation = playlistSyncOperation
        playlistSyncRunID = nil
        playlistSyncOperation = nil
        if finished.exitCode != 0 {
            let partial = finished.result.flatMap { try? decodeWire(PlaylistSyncApplyResult.self, from: $0) }
            let backup = partial?.backupPath.map { " The destination backup is at \($0)." } ?? ""
            playlistSyncStatus = finished.exitCode == 130
                ? .canceled("Playlist sync canceled. No operation was resumed or replayed.")
                : .failure((finished.error ?? "Playlist sync failed with exit code \(finished.exitCode).") + backup)
            return
        }
        guard let result = finished.result else {
            playlistSyncStatus = .failure("Playlist sync completed without a result payload.")
            return
        }
        switch operation {
        case .plan:
            if let decoded = try? decodeWire(PlaylistSyncPlanResult.self, from: result) {
                playlistSyncPlan = decoded
                playlistSyncStatus = decoded.plan.blockers.isEmpty
                    ? WorkflowStatus(message: "Checksummed plan ready for review.", severity: .ok)
                    : WorkflowStatus(message: "Plan found \(decoded.plan.blockers.count) blocker(s). Resolve them and plan again.", severity: .error)
            } else {
                playlistSyncStatus = .failure("Playlist plan result could not be decoded.")
            }
        case .apply(let dryRun):
            if let decoded = try? decodeWire(PlaylistSyncApplyResult.self, from: result) {
                switch (dryRun, decoded.noOp) {
                case (true, _):
                    playlistSyncStatus = WorkflowStatus(message: "Dry run passed. No backup or playlist write was performed.", severity: .ok)
                case (_, true):
                    playlistSyncStatus = WorkflowStatus(message: "Verified parity. No playlist write or backup was needed.", severity: .ok)
                default:
                    playlistSyncStatus = WorkflowStatus(message: "Playlist mirror applied, read back, and verified. Backup: \(decoded.backupPath ?? "reported by backend")", severity: .ok)
                }
                Task { await loadPlaylistSync() }
            } else {
                playlistSyncStatus = .failure("Playlist apply result could not be decoded.")
            }
        case nil:
            break
        }
    }
}
