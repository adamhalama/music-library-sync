import Foundation

/// The Phone Library workflow.
///
/// Every failure here reports through `phoneLibraryStatus`, never through
/// `alertMessage`: a step that failed on this screen belongs on this screen.
/// Nothing mutating is ever replayed after a backend restart — a lost
/// connection clears the in-flight operation and says so.
@MainActor
extension AppState {
    // MARK: - Loading

    func loadPhoneLibrary() async {
        guard let client, !Task.isCancelled else { return }
        let generation = UUID()
        phoneLibraryLoadGeneration = generation
        phoneLibraryLoads.insert(generation)
        defer { phoneLibraryLoads.remove(generation) }
        do {
            // Keep the configured URL and server state together, even after
            // the config changes outside the app. Publish neither on failure.
            let config = try await client.readNavidromeConfig()
            try Task.checkCancellation()
            guard phoneLibraryLoadGeneration == generation else { return }
            let status = try await client.navidromeStatus()
            try Task.checkCancellation()
            guard phoneLibraryLoadGeneration == generation else { return }
            navidromeConfig = config
            navidromeStatus = status
            if let failure = phoneLibraryLoadFailure, phoneLibraryStatus == failure {
                phoneLibraryStatus = nil
            }
            phoneLibraryLoadFailure = nil
        } catch is CancellationError {
            // Navigation cancels the view's .task; the cached pair stays valid.
        } catch {
            guard !Task.isCancelled, phoneLibraryLoadGeneration == generation else { return }
            let failure = WorkflowStatus.failure("Phone Library state could not load: \(error.localizedDescription)")
            phoneLibraryLoadFailure = failure
            phoneLibraryStatus = failure
        }
    }

    func refreshPhoneLibraryStatus() async {
        await loadPhoneLibrary()
    }

    /// Records the user's acknowledgement that the phone app reaches the server. It is
    /// the last setup step and the only one nothing here can observe, so it is
    /// stored in the feature config where the checklist reads it back.
    func setPhoneConnected(_ connected: Bool) async {
        guard let client, !isPhoneLibraryBusy else { return }
        phoneLibraryConfigSaving = true
        defer { phoneLibraryConfigSaving = false }
        do {
            // This RPC writes the whole config. Never copy the screen's cached
            // value: an external editor may have changed it since the last load.
            var config = try await client.readNavidromeConfig().config
            try Task.checkCancellation()
            if config.phone.connected != connected {
                config.phone.connected = connected
                _ = try await client.writeNavidromeConfig(config)
            }
            // Publish the config/status pair through the same owned load path.
            await refreshPhoneLibraryStatus()
            guard phoneLibraryLoadFailure == nil else { return }
            phoneLibraryStatus = connected
                ? "Marked the phone as connected. UDL cannot verify this itself."
                : "Marked the phone as not connected."
        } catch {
            phoneLibraryStatus = .failure("Phone Library config could not save: \(error.localizedDescription)")
        }
    }

    /// Saves the account password to macOS Keychain through the one credential
    /// path that writes secrets. The value is never held in `AppState`.
    func savePhoneLibraryPassword(_ password: String) async {
        guard let client, !isPhoneLibraryBusy else { return }
        let trimmed = password.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else {
            phoneLibraryStatus = .failure("Enter the Navidrome account password before saving.")
            return
        }
        do {
            _ = try await client.saveCredential(
                CredentialMutationParams(
                    kind: .navidromePassword, value: trimmed, clientID: nil, clientSecret: nil
                )
            )
            phoneLibraryStatus = "Password saved to macOS Keychain."
            await refreshPhoneLibraryStatus()
        } catch {
            phoneLibraryStatus = .failure("Password could not be saved: \(error.localizedDescription)")
        }
    }

    // MARK: - Operations

    func installNavidrome() async {
        await startPhoneLibraryOperation(.installDependency) { client in
            try await client.installNavidrome()
        }
    }

    func controlNavidromeService(_ action: String) async {
        await startPhoneLibraryOperation(.serviceControl(action: action)) { client in
            try await client.controlNavidromeService(action)
        }
    }

    func planNavidromeSetup() async {
        navidromeSetupPlan = nil
        await startPhoneLibraryOperation(.setupPlan) { client in
            try await client.planNavidromeSetup()
        }
    }

    func applyNavidromeSetup() async {
        guard let plan = navidromeSetupPlan else { return }
        guard !plan.checksum.isEmpty else {
            phoneLibraryStatus = .failure("The setup plan has no checksum and cannot be applied.")
            return
        }
        guard plan.blockers.isEmpty else {
            phoneLibraryStatus = .failure("Resolve every setup blocker before applying.")
            return
        }
        await startPhoneLibraryOperation(.setupApply) { client in
            try await client.applyNavidromeSetup(NavidromeSetupApplyParams(plan: plan.value))
        }
    }

    func refreshNavidromePlaylists() async {
        await startPhoneLibraryOperation(.refreshPlaylists) { client in
            try await client.refreshNavidromePlaylists()
        }
    }

    var navidromeFavoritesSnapshotRegistered: Bool {
        playlists.contains { $0.id == NavidromeStarredListResult.playlistID }
    }

    func registerNavidromePlaylists() async {
        guard let client, !isPhoneLibraryBusy else { return }
        phoneLibraryOperation = .registerPlaylists
        defer { phoneLibraryOperation = nil }
        phoneLibraryStatus = "Registering the managed Navidrome snapshots…"
        do {
            let result = try await client.registerNavidromePlaylists()
            await loadPlaylists()
            guard navidromeFavoritesSnapshotRegistered else {
                phoneLibraryStatus = .failure("The Navidrome favorites snapshot was not registered.")
                return
            }
            phoneLibraryStatus = WorkflowStatus(message: result.added.isEmpty
                ? "The managed Navidrome snapshots were already registered."
                : "Registered \(result.added.count) managed Navidrome snapshot(s).")
        } catch JSONRPCConnectionError.remote(_, let message, _) {
            phoneLibraryStatus = .failure(message)
        } catch {
            phoneLibraryStatus = .failure("Navidrome snapshots could not be registered: \(error.localizedDescription)")
        }
    }

    func deriveNavidromeGenres() async {
        navidromeGenreDerivation = nil
        await startPhoneLibraryOperation(.deriveGenres) { client in
            try await client.deriveNavidromeGenres()
        }
    }

    func saveNavidromeGenres() async {
        guard let derivation = navidromeGenreDerivation else { return }
        let approved = derivation.genres.filter { navidromeGenreApproval[$0] ?? true }
        guard !approved.isEmpty else {
            phoneLibraryStatus = .failure("Approve at least one genre before saving the allowlist.")
            return
        }
        await startPhoneLibraryOperation(.saveGenres) { client in
            try await client.saveNavidromeGenres(approved)
        }
    }

    func planNavidromeFavorites() async {
        navidromeFavoritePlan = nil
        navidromeFavoriteResult = nil
        await startPhoneLibraryOperation(.favoritePlan) { client in
            try await client.planNavidromeFavorites()
        }
    }

    func applyNavidromeFavorites() async {
        guard let plan = navidromeFavoritePlan else { return }
        guard !plan.checksum.isEmpty else {
            phoneLibraryStatus = .failure("The favorite plan has no checksum and cannot be applied.")
            return
        }
        guard plan.blockers.isEmpty else {
            phoneLibraryStatus = .failure("Resolve every migration blocker before applying.")
            return
        }
        await startPhoneLibraryOperation(.favoriteApply) { client in
            try await client.applyNavidromeFavorites(NavidromeFavoriteApplyParams(plan: plan.value))
        }
    }

    /// Reads the server's stars. This is the direction Apple Music cannot
    /// serve: a like made on the phone is canonical in Navidrome, and this is
    /// how it becomes visible to UDL.
    func listNavidromeStarred() async {
        await startPhoneLibraryOperation(.favoriteList) { client in
            try await client.listNavidromeStarred()
        }
    }

    func createNavidromeBackup() async {
        await startPhoneLibraryOperation(.backup) { client in
            try await client.createNavidromeBackup()
        }
    }

    /// Remote mode: push the library to the server's mirror, then rescan and
    /// re-apply Date Added. The outcome — including a delete-limit refusal,
    /// which the app never overrides — is reported on this screen.
    func pushNavidromeMirror() async {
        guard navidromeStatus?.isRemote == true else {
            phoneLibraryStatus = .failure("Push now is only available in remote mode.")
            return
        }
        guard !isPhoneLibraryBusy, phoneLibraryRunID == nil else { return }
        navidromeMirrorPush = nil
        navidromeMirrorPushOutcome = nil
        await startPhoneLibraryOperation(.mirrorPush) { client in
            try await client.pushNavidromeMirror()
        }
    }

    func cancelPhoneLibraryOperation() async {
        guard let runID = phoneLibraryRunID else { return }
        await cancelRun(runID)
    }

    private func startPhoneLibraryOperation(
        _ operation: PhoneLibraryOperation,
        start: (UDLClient) async throws -> RunIDResult
    ) async {
        guard let client, phoneLibraryRunID == nil, !isPhoneLibraryBusy else { return }
        clearNotResumed(.phoneLibrary)
        phoneLibraryOperation = operation
        phoneLibraryStatus = WorkflowStatus(message: operation.label)
        do {
            let started = try await start(client)
            phoneLibraryRunID = started.runID
            consumeBufferedFinished(started.runID)
            await cancelPendingWorkIfReady(.phoneLibrary)
        } catch JSONRPCConnectionError.remote(_, let message, _) {
            phoneLibraryOperation = nil
            await cancelPendingWorkIfReady(.phoneLibrary)
            phoneLibraryStatus = .failure(message)
        } catch {
            phoneLibraryOperation = nil
            await cancelPendingWorkIfReady(.phoneLibrary)
            phoneLibraryStatus = .failure(error.localizedDescription)
        }
    }

    // MARK: - Terminal frames

    func applyPhoneLibraryFinished(_ finished: RunFinishedNotification) {
        let operation = phoneLibraryOperation
        phoneLibraryRunID = nil
        phoneLibraryOperation = nil

        // A push reports through its own outcome on success and failure alike:
        // the result carries the refreshed mirror state and, for a refusal,
        // why nothing was pushed.
        if case .mirrorPush = operation {
            let result = finished.result.flatMap { try? decodeWire(NavidromeMirrorPushResult.self, from: $0) }
            let outcome = NavidromeMirrorPushOutcome(exitCode: finished.exitCode, error: finished.error, result: result)
            navidromeMirrorPush = result
            navidromeMirrorPushOutcome = outcome
            phoneLibraryStatus = WorkflowStatus(message: outcome.message, severity: outcome.severity)
            Task { await refreshPhoneLibraryStatus() }
            return
        }

        if finished.exitCode != 0 {
            phoneLibraryStatus = finished.exitCode == 130
                ? .canceled("Canceled. Nothing was resumed or replayed.")
                : .failure(finished.error ?? "The step failed with exit code \(finished.exitCode).")
            // An interrupted favorite apply may still have produced a backup
            // and a compensation report; surfacing it is the whole point of the
            // recovery path, so it is read even on failure.
            if case .favoriteApply = operation, let result = finished.result {
                navidromeFavoriteResult = try? decodeWire(NavidromeFavoriteApplyResult.self, from: result)
            }
            return
        }

        switch operation {
        case .installDependency:
            phoneLibraryStatus = "Navidrome is installed."
            Task { await loadPhoneLibrary() }
        case .serviceControl(let action):
            phoneLibraryStatus = WorkflowStatus(message: "Service \(action) completed.")
            Task { await refreshPhoneLibraryStatus() }
        case .setupPlan:
            if let result = finished.result, let plan = NavidromeSetupPlanPresentation(result) {
                navidromeSetupPlan = plan
                phoneLibraryStatus = plan.applicable
                    ? WorkflowStatus(message: "Setup plan ready: \(plan.changeCount) change(s). Nothing was written.")
                    : .failure("Setup is blocked. Resolve every blocker, then plan again.")
            } else {
                navidromeSetupPlan = nil
                phoneLibraryStatus = .failure("The setup plan could not be decoded.")
            }
        case .setupApply:
            let applied = finished.result.flatMap { try? decodeWire(NavidromeSetupApplyResult.self, from: $0) }
            phoneLibraryStatus = WorkflowStatus(
                message: applied?.message ?? "Setup applied."
            )
            // The applied plan describes a world that no longer exists; keeping
            // it on screen would invite a replay the backend would refuse.
            navidromeSetupPlan = nil
            Task { await loadPhoneLibrary() }
        case .registerPlaylists:
            // Registration is a direct config RPC rather than a run and never
            // reaches this handler. The case still participates in the shared
            // busy/interruption model while that request is in flight.
            break
        case .refreshPlaylists:
            if let result = finished.result,
               let refreshed = try? decodeWire(NavidromePlaylistRefreshResult.self, from: result) {
                navidromePlaylistRefresh = refreshed
                let written = refreshed.generated.filter(\.written).count
                phoneLibraryStatus = refreshed.warnings.isEmpty
                    ? WorkflowStatus(message: "Wrote \(written) playlist file(s); Navidrome was asked to re-import them.")
                    : WorkflowStatus(message: refreshed.warnings.joined(separator: " "), severity: .warn)
            } else {
                phoneLibraryStatus = .failure("The playlist refresh result could not be decoded.")
            }
            Task { await refreshPhoneLibraryStatus() }
        case .deriveGenres:
            if let result = finished.result,
               let derivation = try? decodeWire(NavidromeGenreDerivation.self, from: result) {
                navidromeGenreDerivation = derivation
                // Every derived genre starts approved; the user removes rather
                // than re-adds, because the derivation is the evidence.
                navidromeGenreApproval = Dictionary(uniqueKeysWithValues: derivation.genres.map { ($0, true) })
                phoneLibraryStatus = WorkflowStatus(
                    message: "Derived \(derivation.genres.count) genre(s) from \(derivation.matchedCount) matched track(s). Nothing was saved yet."
                )
            } else {
                phoneLibraryStatus = .failure("The genre derivation could not be decoded.")
            }
        case .saveGenres:
            navidromeGenreDerivation = nil
            navidromeGenreApproval = [:]
            phoneLibraryStatus = "Genre allowlist saved and the playlists were rewritten."
            Task { await loadPhoneLibrary() }
        case .favoritePlan:
            if let result = finished.result, let plan = NavidromeFavoritePlanPresentation(result) {
                navidromeFavoritePlan = plan
                phoneLibraryStatus = plan.applicable
                    ? WorkflowStatus(message: "Migration plan ready: \(plan.counts.matched) favorite(s) to star. Apple Music was only read.")
                    : .failure("The migration is blocked. Resolve every blocker, then plan again.")
            } else {
                navidromeFavoritePlan = nil
                phoneLibraryStatus = .failure("The migration plan could not be decoded.")
            }
        case .favoriteApply:
            let applied = finished.result.flatMap { try? decodeWire(NavidromeFavoriteApplyResult.self, from: $0) }
            navidromeFavoriteResult = applied
            navidromeFavoritePlan = nil
            phoneLibraryStatus = WorkflowStatus(message: applied?.message ?? "Favorites imported.")
            Task { await refreshPhoneLibraryStatus() }
        case .favoriteList:
            if let result = finished.result,
               let starred = try? decodeWire(NavidromeStarredListResult.self, from: result) {
                navidromeStarred = starred
                phoneLibraryStatus = WorkflowStatus(
                    message: "\(starred.count) track(s) starred on the server. Refresh the \"\(NavidromeStarredListResult.playlistName)\" snapshot to send them to Rekordbox."
                )
            } else {
                phoneLibraryStatus = .failure("The starred listing could not be decoded.")
            }
        case .backup:
            let created = finished.result.flatMap { try? decodeWire(NavidromeBackupResult.self, from: $0) }
            phoneLibraryStatus = WorkflowStatus(message: created?.message ?? "Backup created.")
            Task { await refreshPhoneLibraryStatus() }
        case .mirrorPush:
            // Handled above, before the exit-code split.
            break
        case nil:
            break
        }
    }

    /// Called when the backend connection ends. Nothing mutating is resumed.
    func interruptPhoneLibrary() -> Bool {
        guard phoneLibraryOperation != nil || phoneLibraryRunID != nil else { return false }
        let wasMutating = phoneLibraryOperation?.isMutating ?? false
        phoneLibraryOperation = nil
        phoneLibraryRunID = nil
        phoneLibraryStatus = WorkflowStatus(
            message: wasMutating
                ? "Backend connection ended. No install, setup, import, or push step was resumed or replayed."
                : "Backend connection ended. The step was not resumed.",
            severity: .error
        )
        return true
    }

    func clearPhoneLibrarySession() {
        phoneLibraryLoadGeneration = UUID()
        phoneLibraryLoads = []
        phoneLibraryConfigSaving = false
        phoneLibraryLoadFailure = nil
        navidromeConfig = nil
        navidromeStatus = nil
        navidromeSetupPlan = nil
        navidromeFavoritePlan = nil
        navidromeFavoriteResult = nil
        navidromeGenreDerivation = nil
        navidromeGenreApproval = [:]
        navidromePlaylistRefresh = nil
        navidromeMirrorPush = nil
        navidromeMirrorPushOutcome = nil
        phoneLibraryOperation = nil
        phoneLibraryRunID = nil
        phoneLibraryStatus = nil
    }

    // MARK: - Derived

    var phoneLibraryProgress: PhoneLibraryProgress {
        PhoneLibraryProgress(status: navidromeStatus)
    }

    var phoneConnection: PhoneConnectionDetails {
        PhoneConnectionDetails(status: navidromeStatus, configuredServerURL: navidromeConfig?.config.server.url)
    }

    var phoneLibraryRemoteSummary: PhoneLibraryRemoteSummary {
        PhoneLibraryRemoteSummary(status: navidromeStatus, configuredServerURL: navidromeConfig?.config.server.url)
    }

    var isPhoneLibraryRemote: Bool { navidromeStatus?.isRemote == true }

    var isPhoneLibraryBusy: Bool { phoneLibraryBusyReason != nil }

    /// Shared by the action guards and the view's `.constrained(by:)` notes.
    var phoneLibraryBusyReason: String? {
        if !phoneLibraryLoads.isEmpty {
            return "Phone Library configuration and status are loading. Wait for them to finish."
        }
        if phoneLibraryConfigSaving {
            return "Saving Phone Library configuration. Wait for it to finish."
        }
        return phoneLibraryOperation.map { "\($0.label) Wait for it to finish or cancel it." }
    }
}
