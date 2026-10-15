import Foundation

/// Creation must never replace a pair; editing must still refer to the pair
/// originally opened by the editor, even if the config changed in the meantime.
enum PlaylistSyncJobSaveIntent {
    case create
    case edit(originalID: String)

    static func revisionConstraint(_ revision: String?) -> String? {
        guard let revision, !revision.trimmed.isEmpty else {
            return PlaylistSyncJobSaveError.missingRevision.localizedDescription
        }
        return nil
    }

    func updatedConfig(_ current: PlaylistConfig?, saving job: PlaylistSyncJob) throws -> PlaylistConfig {
        guard let current else { throw PlaylistSyncJobSaveError.configUnavailable }
        var jobs = current.syncJobs
        switch self {
        case .create:
            guard !jobs.contains(where: { $0.id.caseInsensitiveCompare(job.id) == .orderedSame }) else {
                throw PlaylistSyncJobSaveError.duplicateID(job.id)
            }
            jobs.append(job)
        case .edit(let originalID):
            guard job.id == originalID else { throw PlaylistSyncJobSaveError.changedID }
            guard let index = jobs.firstIndex(where: { $0.id == originalID }) else {
                throw PlaylistSyncJobSaveError.missingOriginal(originalID)
            }
            guard !jobs.enumerated().contains(where: {
                $0.offset != index && $0.element.id.caseInsensitiveCompare(job.id) == .orderedSame
            }) else {
                throw PlaylistSyncJobSaveError.duplicateID(job.id)
            }
            jobs[index] = job
        }
        return PlaylistConfig(version: current.version, playlists: current.playlists, syncJobs: jobs)
    }
}

enum PlaylistSyncJobSaveError: LocalizedError {
    case configUnavailable
    case missingRevision
    case duplicateID(String)
    case changedID
    case missingOriginal(String)

    var errorDescription: String? {
        switch self {
        case .configUnavailable:
            "Playlist configuration is not loaded. Reload it before saving a pair."
        case .missingRevision:
            "No pinned playlist revision is available. Reload jobs before saving. If reload still provides no revision, update or restart the backend."
        case .duplicateID(let id):
            "A playlist pair with ID ‘\(id)’ already exists (IDs are case-insensitive). Choose a different ID or edit the existing pair."
        case .changedID:
            "The pair ID cannot be changed because it names its binding state file."
        case .missingOriginal(let id):
            "The original playlist pair ‘\(id)’ no longer exists. Reload the configuration before editing."
        }
    }
}

extension AppState {
    @discardableResult
    func loadPlaylistSync() async -> Bool {
        guard let client else { return false }
        do {
            let plannedJob = playlistSyncPlan.flatMap { result in
                playlistSyncJobs.first { $0.id == result.plan.jobID }?.job
            }
            async let config: Void = reloadPlaylistConfig()
            let result = try await client.inspectPlaylistSync()
            try await config
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
            return true
        } catch {
            playlistSyncStatus = .failure("Playlist sync jobs could not load: \(error.localizedDescription)")
            return false
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
            await cancelPendingWorkIfReady(.playlistSync)
        } catch {
            playlistSyncOperation = nil
            await cancelPendingWorkIfReady(.playlistSync)
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
            await cancelPendingWorkIfReady(.playlistSync)
        } catch {
            playlistSyncOperation = nil
            await cancelPendingWorkIfReady(.playlistSync)
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

    func savePlaylistSyncJob(_ job: PlaylistSyncJob, intent: PlaylistSyncJobSaveIntent, expectedContentSHA256: String?) async -> Bool {
        do {
            let updated = try intent.updatedConfig(playlistConfig?.config, saving: job)
            try await writePlaylistSyncConfig(updated, expectedContentSHA256: expectedContentSHA256)
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
