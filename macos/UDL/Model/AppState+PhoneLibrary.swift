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
        guard let client else { return }
        do {
            async let config = client.readNavidromeConfig()
            async let status = client.navidromeStatus()
            let loaded = try await (config, status)
            navidromeConfig = loaded.0
            navidromeStatus = loaded.1
        } catch {
            phoneLibraryStatus = .failure("Phone Library state could not load: \(error.localizedDescription)")
        }
    }

    func refreshPhoneLibraryStatus() async {
        guard let client else { return }
        do {
            navidromeStatus = try await client.navidromeStatus()
        } catch {
            phoneLibraryStatus = .failure("Phone Library status could not refresh: \(error.localizedDescription)")
        }
    }

    func savePhoneLibraryConfig(_ config: NavidromeConfig) async -> Bool {
        guard let client else { return false }
        do {
            navidromeConfig = try await client.writeNavidromeConfig(config)
            await refreshPhoneLibraryStatus()
            return true
        } catch {
            phoneLibraryStatus = .failure("Phone Library config could not save: \(error.localizedDescription)")
            return false
        }
    }

    /// Records the user's acknowledgement that Amperfy reaches this Mac. It is
    /// the last setup step and the only one nothing here can observe, so it is
    /// stored in the feature config where the checklist reads it back.
    func setPhoneConnected(_ connected: Bool) async {
        guard var config = navidromeConfig?.config else {
            phoneLibraryStatus = .failure("Phone Library config has not loaded yet.")
            return
        }
        guard config.phone.connected != connected else { return }
        config.phone.connected = connected
        guard await savePhoneLibraryConfig(config) else { return }
        phoneLibraryStatus = connected
            ? "Marked the phone as connected. UDL cannot verify this itself."
            : "Marked the phone as not connected."
    }

    /// Saves the account password to macOS Keychain through the one credential
    /// path that writes secrets. The value is never held in `AppState`.
    func savePhoneLibraryPassword(_ password: String) async {
        guard let client else { return }
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

    func cancelPhoneLibraryOperation() async {
        guard let runID = phoneLibraryRunID else { return }
        await cancelRun(runID)
    }

    private func startPhoneLibraryOperation(
        _ operation: PhoneLibraryOperation,
        start: (UDLClient) async throws -> RunIDResult
    ) async {
        guard let client, phoneLibraryRunID == nil, phoneLibraryOperation == nil else { return }
        clearNotResumed(.phoneLibrary)
        phoneLibraryOperation = operation
        phoneLibraryStatus = WorkflowStatus(message: operation.label)
        do {
            let started = try await start(client)
            phoneLibraryRunID = started.runID
            consumeBufferedFinished(started.runID)
        } catch JSONRPCConnectionError.remote(_, let message, _) {
            phoneLibraryOperation = nil
            phoneLibraryStatus = .failure(message)
        } catch {
            phoneLibraryOperation = nil
            phoneLibraryStatus = .failure(error.localizedDescription)
        }
    }

    // MARK: - Terminal frames

    func applyPhoneLibraryFinished(_ finished: RunFinishedNotification) {
        let operation = phoneLibraryOperation
        phoneLibraryRunID = nil
        phoneLibraryOperation = nil

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
                ? "Backend connection ended. No install, setup, or import step was resumed or replayed."
                : "Backend connection ended. The step was not resumed.",
            severity: .error
        )
        return true
    }

    func clearPhoneLibrarySession() {
        navidromeConfig = nil
        navidromeStatus = nil
        navidromeSetupPlan = nil
        navidromeFavoritePlan = nil
        navidromeFavoriteResult = nil
        navidromeGenreDerivation = nil
        navidromeGenreApproval = [:]
        navidromePlaylistRefresh = nil
        phoneLibraryOperation = nil
        phoneLibraryRunID = nil
        phoneLibraryStatus = nil
    }

    // MARK: - Derived

    var phoneLibraryProgress: PhoneLibraryProgress {
        PhoneLibraryProgress(status: navidromeStatus)
    }

    var phoneConnection: PhoneConnectionDetails {
        PhoneConnectionDetails(status: navidromeStatus)
    }

    var isPhoneLibraryBusy: Bool { phoneLibraryOperation != nil }
}
