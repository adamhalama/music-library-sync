import Darwin
import Foundation
import XCTest
@testable import UDL

/// Exercise real RPC suspension points, not a synchronous stand-in for a starter.
/// Routing the terminal frame before releasing the RPC reply makes the ordering
/// deterministic even when the main actor is heavily loaded.
@MainActor
final class RunOwnershipTests: XCTestCase {
    func testEveryRekordboxStarterConsumesItsEarlyFinishAndCanStartAgain() async throws {
        let rpc = AgentRPCFixture()
        await rpc.connection.start()
        let state = AppState(client: rpc.client)
        defer { rpc.closePipes() }
        let starters: [(AgentMethod, @MainActor @Sendable () async -> Void)] = [
            (.rekordboxDepsEnsure, { await state.ensureRekordboxRuntime() }),
            (.rekordboxDepsReset, { await state.resetRekordboxRuntime() }),
            (.rekordboxInspect, { await state.inspectRekordbox() }),
            (.rekordboxPlan, { await state.planRekordbox(jobID: "fixture") }),
        ]
        for (method, start) in starters {
            for _ in 0..<2 {
                try await rpc.finishBeforeReply(state, method: method, start: start)
                assertRekordboxIdle(state)
            }
        }
        try await rpc.finishBeforeReply(
            state, method: .rekordboxPlan,
            result: .object([
                "resolved": .object([:]),
                "plan": .object(["checksum_sha256": .string("fixture")]),
                "plan_path": .string("/fixture/plan"),
            ]),
            start: { await state.planRekordbox(jobID: "fixture") }
        )
        XCTAssertNotNil(state.rekordboxPlan)
        for dryRun in [true, false] {
            try await rpc.finishBeforeReply(state, method: .rekordboxApply) {
                await state.applyRekordbox(dryRun: dryRun)
            }
            assertRekordboxIdle(state)
        }
        await rpc.connection.close()
    }

    func testEveryFreeDLStarterConsumesItsEarlyFinishAndCanStartAgain() async throws {
        let rpc = AgentRPCFixture()
        await rpc.connection.start()
        let state = AppState(client: rpc.client)
        defer { rpc.closePipes() }
        for _ in 0..<2 {
            try await rpc.finishBeforeReply(state, method: .freeDLPlanStart) {
                await state.startFreeDLPlan(jobID: "fixture")
            }
            assertFreeDLIdle(state)
        }
        // Establish private plan state through the public workflow, with an
        // actual decoded successful planning result rather than review hooks.
        try await rpc.finishBeforeReply(state, method: .freeDLPlanStart, result: capturePlan()) {
            await state.startFreeDLPlan(jobID: "fixture")
        }
        XCTAssertNotNil(state.freeDLCapturePlan)
        for _ in 0..<2 {
            try await rpc.finishBeforeReply(state, method: .freeDLCaptureStart) {
                await state.startFreeDLCapture()
            }
            assertFreeDLIdle(state)
        }
        // Capture success publishes the ID needed by promotion planning. It
        // does not launch any background refresh or touch the filesystem.
        try await rpc.finishBeforeReply(state, method: .freeDLCaptureStart, result: .object([:])) {
            await state.startFreeDLCapture()
        }
        XCTAssertNotNil(state.freeDLCaptureRunID)
        for _ in 0..<2 {
            try await rpc.finishBeforeReply(state, method: .freeDLPromotionPlanBuild) {
                await state.buildFreeDLPromotionPlan()
            }
            assertFreeDLIdle(state)
        }
        var promotion = try XCTUnwrap(capturePlan().objectValue)
        promotion["target_format"] = .string("mp3")
        promotion["backup_root"] = .string("/fixture/backups")
        promotion["rows"] = .array([])
        try await rpc.finishBeforeReply(state, method: .freeDLPromotionPlanBuild, result: .object(promotion)) {
            await state.buildFreeDLPromotionPlan()
        }
        XCTAssertNotNil(state.freeDLPromotionPlan)
        for _ in 0..<2 {
            try await rpc.finishBeforeReply(state, method: .freeDLPromoteApply) {
                await state.applyFreeDLPromotion()
            }
            assertFreeDLIdle(state)
        }
        await rpc.connection.close()
    }

    func testEveryPhoneLibraryStarterConsumesItsEarlyFinishAndCanStartAgain() async throws {
        let rpc = AgentRPCFixture()
        await rpc.connection.start()
        let state = AppState(client: rpc.client)
        defer { rpc.closePipes() }
        let starters: [(AgentMethod, @MainActor @Sendable () async -> Void)] = [
            (.navidromeDepsEnsure, { await state.installNavidrome() }),
            (.navidromeServiceControl, { await state.controlNavidromeService("start") }),
            (.navidromeSetupPlan, { await state.planNavidromeSetup() }),
            (.navidromePlaylistsRefresh, { await state.refreshNavidromePlaylists() }),
            (.navidromePlaylistsDeriveGenres, { await state.deriveNavidromeGenres() }),
            (.navidromeFavoritesPlan, { await state.planNavidromeFavorites() }),
            (.navidromeFavoritesList, { await state.listNavidromeStarred() }),
            (.navidromeBackupCreate, { await state.createNavidromeBackup() }),
        ]
        for (method, start) in starters {
            for _ in 0..<2 {
                try await rpc.finishBeforeReply(state, method: method, start: start)
                assertPhoneIdle(state)
            }
        }
        let plan: JSONValue = .object(["checksum_sha256": .string("fixture")])
        state.navidromeSetupPlan = NavidromeSetupPlanPresentation(plan)
        state.navidromeFavoritePlan = NavidromeFavoritePlanPresentation(plan)
        state.navidromeGenreDerivation = NavidromeGenreDerivation(
            sourcePlaylist: "fixture", sourceTrackCount: 1, matchedCount: 1, genres: ["House"],
            unmatchedPaths: [], genrelessPaths: [], outsideLibrary: [], currentAllowlist: [], allowlistChanged: true
        )
        let guarded: [(AgentMethod, @MainActor @Sendable () async -> Void)] = [
            (.navidromeSetupApply, { await state.applyNavidromeSetup() }),
            (.navidromeFavoritesApply, { await state.applyNavidromeFavorites() }),
            (.navidromePlaylistsSaveGenres, { await state.saveNavidromeGenres() }),
        ]
        for (method, start) in guarded {
            for _ in 0..<2 {
                try await rpc.finishBeforeReply(state, method: method, start: start)
                assertPhoneIdle(state)
            }
        }
        await rpc.connection.close()
    }

    func testPhoneLibraryLoadAndRefreshReloadConfigWithStatus() async throws {
        for remote in [true, false] {
            let rpc = AgentRPCFixture()
            await rpc.connection.start()
            let state = AppState(client: rpc.client)
            defer { rpc.closePipes() }
            // First open, reopen with cached status, then explicit Refresh.
            let loaders: [@MainActor () async -> Void] = [
                { await state.loadPhoneLibrary() },
                { await state.loadPhoneLibrary() },
                { await state.refreshPhoneLibraryStatus() },
            ]
            for (index, load) in loaders.enumerated() {
                let url = "https://server-\(index).example:4533"
                let config = try phoneLibraryConfigValue(url: url)
                let status = try phoneLibraryStatusValue(remote: remote, tracks: index + 1)
                let task = Task { await load() }
                try await answerPhoneLibraryLoad(rpc, config: config, status: status)
                await task.value
                XCTAssertEqual(state.navidromeConfig?.config.server.url, url)
                XCTAssertEqual(state.navidromeStatus?.libraryTracks, index + 1)
                XCTAssertEqual(state.phoneConnection.serverURL, remote ? url : "http://mac.local:4533")
                XCTAssertEqual(state.phoneLibraryRemoteSummary.serverURL, url)
                XCTAssertNil(state.phoneLibraryStatus)
            }
            await rpc.connection.close()
        }
    }

    func testPhoneLibraryLoadAndRefreshPreserveBothCachesIfEitherReadFails() async throws {
        for refresh in [false, true] {
            for failedMethod in [AgentMethod.navidromeConfigRead, .navidromeStatus] {
                let rpc = AgentRPCFixture()
                await rpc.connection.start()
                let state = AppState(client: rpc.client)
                defer { rpc.closePipes() }
                let oldConfig = try phoneLibraryConfigValue(url: "https://server-a.example:4533")
                let oldStatus = try phoneLibraryStatusValue(remote: true, tracks: 1)
                state.navidromeConfig = try JSONDecoder.agent.decode(
                    NavidromeConfigResult.self, from: JSONEncoder.agent.encode(oldConfig)
                )
                state.navidromeStatus = try JSONDecoder.agent.decode(
                    NavidromeStatus.self, from: JSONEncoder.agent.encode(oldStatus)
                )
                let task = Task {
                    if refresh { await state.refreshPhoneLibraryStatus() }
                    else { await state.loadPhoneLibrary() }
                }
                try await answerPhoneLibraryLoad(
                    rpc, config: phoneLibraryConfigValue(url: "https://server-b.example:4533"),
                    status: phoneLibraryStatusValue(remote: true, tracks: 2), failedMethod: failedMethod
                )
                await task.value
                XCTAssertEqual(state.navidromeConfig?.config.server.url, "https://server-a.example:4533")
                XCTAssertEqual(state.navidromeStatus?.libraryTracks, 1)
                XCTAssertEqual(state.phoneConnection.serverURL, "https://server-a.example:4533")
                XCTAssertEqual(state.phoneLibraryStatus?.severity, .error)
                XCTAssertTrue(state.phoneLibraryStatus?.message.contains("fixture read failed") ?? false)
                await rpc.connection.close()
            }
        }
    }

    private func answerPhoneLibraryLoad(
        _ rpc: AgentRPCFixture, config: JSONValue, status: JSONValue, failedMethod: AgentMethod? = nil
    ) async throws {
        for (method, result) in [(AgentMethod.navidromeConfigRead, config), (.navidromeStatus, status)] {
            let request = try await rpc.readRequest(method)
            if method == failedMethod {
                try rpc.fail(request, message: "fixture read failed")
                return
            }
            try rpc.reply(request, result: result)
        }
    }

    private func phoneLibraryConfigValue(url: String) throws -> JSONValue {
        let config = NavidromeConfig(
            version: 1, enabled: true,
            server: NavidromeServerConfig(url: url, username: "jaa", port: 4533, address: "0.0.0.0"),
            paths: NavidromePathsConfig(
                musicDir: "/m", dataDir: "/d", cacheDir: "/c", logFile: "/l",
                configFile: "/f", backupDir: "/b", playlistsPath: ".udl/playlists"
            ),
            scan: NavidromeScanConfig(schedule: "@every 1h"),
            backup: NavidromeBackupConfig(schedule: "0 3 * * *", count: 7),
            playlists: NavidromePlaylistsConfig(hardBounceGenres: [], appleHardBouncePlaylist: "HARD BOUNCE"),
            phone: NavidromePhoneConfig(connected: false)
        )
        return try JSONDecoder().decode(JSONValue.self, from: JSONEncoder.agent.encode(
            NavidromeConfigResult(path: "/fixture/navidrome.yaml", config: config, content: "")
        ))
    }

    private func phoneLibraryStatusValue(remote: Bool, tracks: Int) throws -> JSONValue {
        let value = try JSONDecoder().decode(
            JSONValue.self, from: Data(PhoneLibraryTests.remoteStatusJSON.utf8)
        )
        var status = try XCTUnwrap(value.objectValue)
        status["mode"] = .string(remote ? "remote" : "local")
        status["library_tracks"] = .number(Double(tracks))
        if !remote {
            status["service"] = .object([
                "state": .string("running"), "loaded": .bool(true), "owned": .bool(true),
                "lan_url": .string("http://mac.local:4533"), "local_url": .string("http://localhost:4533"),
                "problems": .null,
            ])
        }
        return .object(status)
    }

    /// Push now reports every outcome on the Phone Library screen and then
    /// refreshes config and status together.
    func testMirrorPushConsumesItsEarlyFinishAndReportsOnTheScreen() async throws {
        let rpc = AgentRPCFixture()
        await rpc.connection.start()
        let state = AppState(client: rpc.client)
        defer { rpc.closePipes() }
        let remoteStatus = try JSONDecoder().decode(JSONValue.self, from: Data(PhoneLibraryTests.remoteStatusJSON.utf8))
        state.navidromeStatus = try JSONDecoder.agent.decode(
            NavidromeStatus.self, from: Data(PhoneLibraryTests.remoteStatusJSON.utf8)
        )
        func answerStatusRefresh() async throws {
            let config = try phoneLibraryConfigValue(url: "https://server.example:4533")
            try await answerPhoneLibraryLoad(rpc, config: config, status: remoteStatus)
            // Writing the replies does not wait for the reload to publish them
            // and release its busy constraint on the main actor.
            let deadline = ContinuousClock.now + .seconds(5)
            while !state.phoneLibraryLoads.isEmpty && ContinuousClock.now < deadline {
                try await Task.sleep(for: .milliseconds(20))
            }
        }

        try await rpc.finishBeforeReply(state, method: .navidromeMirrorPush) {
            await state.pushNavidromeMirror()
        }
        try await answerStatusRefresh()
        assertPhoneIdle(state)
        XCTAssertEqual(state.navidromeMirrorPushOutcome?.kind, .failed)
        XCTAssertEqual(state.phoneLibraryStatus?.severity, .error)
        XCTAssertTrue(state.phoneLibraryStatus?.message.contains("terminal fixture") ?? false)

        let refused: JSONValue = .object([
            "push": .object(["changed": .number(0), "deleted": .number(0), "planned_deletes": .number(300)]),
            "scanned": .bool(false),
            "warnings": .null,
            "failure": .object([
                "kind": .string("delete_limit"),
                "detail": .string("push refused by the delete limit"),
                "planned_deletes": .number(300),
                "allow_deletes_command": .string("udl navidrome push --allow-deletes 300"),
            ]),
        ])
        try await rpc.finishBeforeReply(state, method: .navidromeMirrorPush, result: refused) {
            await state.pushNavidromeMirror()
        }
        try await answerStatusRefresh()
        assertPhoneIdle(state)
        XCTAssertEqual(state.navidromeMirrorPushOutcome?.plannedDeletes, 300)
        XCTAssertEqual(state.phoneLibraryStatus?.severity, .error)
        XCTAssertTrue(state.phoneLibraryStatus?.message.contains("--allow-deletes 300") ?? false)

        let pushed: JSONValue = .object([
            "push": .object(["changed": .number(2), "deleted": .number(0)]),
            "scanned": .bool(true),
            "warnings": .array([]),
            "mirror": .object(["last_push_at": .string("2026-10-22T10:00:00Z")]),
        ])
        try await rpc.finishBeforeReply(state, method: .navidromeMirrorPush, result: pushed) {
            await state.pushNavidromeMirror()
        }
        try await answerStatusRefresh()
        assertPhoneIdle(state)
        XCTAssertEqual(state.navidromeMirrorPushOutcome?.kind, .pushed)
        XCTAssertEqual(state.navidromeMirrorPush?.mirror?.lastPushAt, "2026-10-22T10:00:00Z")
        XCTAssertEqual(state.phoneLibraryStatus?.severity, .ok)
        await rpc.connection.close()
    }

    func testPlaylistSyncPlanAndApplyConsumeEarlyFinishAndCanStartAgain() async throws {
        let rpc = AgentRPCFixture()
        await rpc.connection.start()
        let state = AppState(client: rpc.client)
        defer { rpc.closePipes() }
        for _ in 0..<2 {
            try await rpc.finishBeforeReply(state, method: .playlistSyncPlan) {
                await state.planPlaylistSync(jobID: "fixture", direction: "rekordbox_to_navidrome")
            }
            assertPairIdle(state)
        }
        state.playlistSyncPlan = try JSONDecoder.agent.decode(
            PlaylistSyncPlanResult.self, from: Data(pairPlanJSON.utf8)
        )
        for dryRun in [true, false] {
            try await rpc.finishBeforeReply(state, method: .playlistSyncApply) {
                await state.applyPlaylistSync(dryRun: dryRun)
            }
            assertPairIdle(state)
        }
        await rpc.connection.close()
    }

    func testPlaylistRefreshAndProviderDiscoveryConsumeEarlyFinishAndCanStartAgain() async throws {
        let rpc = AgentRPCFixture()
        await rpc.connection.start()
        let state = AppState(client: rpc.client)
        defer { rpc.closePipes() }
        let starters: [(AgentMethod, @MainActor @Sendable () async -> Void)] = [
            (.playlistsRefresh, { await state.refreshPlaylist("fixture") }),
            (.playlistsProviderList, { await state.discoverProviderPlaylists() }),
        ]
        for (method, start) in starters {
            for _ in 0..<2 {
                try await rpc.finishBeforeReply(state, method: method, start: start)
                XCTAssertNil(state.playlistActiveRunID)
                XCTAssertEqual(state.playlistStatus?.severity, .error)
                assertGloballyIdle(state)
            }
        }
        await rpc.connection.close()
    }

    func testSyncAndPreviewConsumeEarlyFinishWithoutLeavingBusyState() async throws {
        let rpc = AgentRPCFixture()
        await rpc.connection.start()
        let state = AppState(client: rpc.client)
        defer { rpc.closePipes() }
        try await loadSyncFixture(state, rpc: rpc)
        for preview in [false, true] {
            try await rpc.finishBeforeReply(state, method: .syncStart) {
                if preview { await state.startDryRunPlan() } else { await state.startSync() }
            }
            // Sync intentionally retains its terminal ID for the result view.
            XCTAssertEqual(state.syncRun.exitCode, 1)
            XCTAssertFalse(state.syncRun.phase.isActive)
            assertGloballyIdle(state)
            state.resetSyncRun()
            XCTAssertNil(state.syncRun.runID)
        }
        await rpc.connection.close()
    }

    func testKnownPairFinishIsNotStolenByNilIDFreeDLSyncOrPhoneStartup() async throws {
        let rpc = AgentRPCFixture()
        await rpc.connection.start()
        let state = AppState(client: rpc.client)
        defer { rpc.closePipes() }
        try await loadSyncFixture(state, rpc: rpc)
        let syncStart = Task { await state.startSync() }
        let syncRequest = try await rpc.readRequest(.syncStart)
        XCTAssertNil(state.syncRun.runID)
        let freeStart = Task { await state.startFreeDLPlan(jobID: "fixture") }
        let request = try await rpc.readRequest(.freeDLPlanStart)
        XCTAssertNotNil(state.freeDLOperation)
        XCTAssertNil(state.freeDLRunID)
        state.phoneLibraryOperation = .setupPlan
        state.playlistSyncOperation = .plan
        state.playlistSyncRunID = "known-pair-run"
        state.routeFinished(finished("known-pair-run"))
        XCTAssertNil(state.playlistSyncRunID)
        XCTAssertNil(state.playlistSyncOperation)
        XCTAssertEqual(state.playlistSyncStatus?.message, "terminal fixture")
        XCTAssertNotNil(state.freeDLOperation)
        XCTAssertNil(state.freeDLStatus)
        XCTAssertEqual(state.syncRun.phase, .starting)
        XCTAssertNil(state.syncRun.exitCode)
        XCTAssertNotNil(state.phoneLibraryOperation)
        XCTAssertNil(state.phoneLibraryStatus)
        state.phoneLibraryOperation = nil
        state.routeFinished(finished("sync-run"))
        try rpc.reply(syncRequest, result: .object(["run_id": .string("sync-run")]))
        await syncStart.value
        state.resetSyncRun()
        state.routeFinished(finished("free-run"))
        try rpc.reply(request, result: .object(["run_id": .string("free-run")]))
        await freeStart.value
        assertFreeDLIdle(state)
        await rpc.connection.close()
    }

    func testEarlyFreeDLPlanEventCannotLetLateStartReplyReviveFinishedRun() async throws {
        let rpc = AgentRPCFixture()
        await rpc.connection.start()
        let state = AppState(client: rpc.client)
        defer { rpc.closePipes() }
        let task = Task { await state.startFreeDLPlan(jobID: "fixture") }
        let request = try await rpc.readRequest(.freeDLPlanStart)
        let event = FreeDLPlanEvent(
            kind: "stage", stage: "Scanning fixture", status: nil, detail: nil,
            row: nil, plan: nil, error: nil, current: nil, total: nil
        )
        state.applyFreeDLPlanNotification(FreeDLPlanEventNotification(runID: "early-plan", event: event))
        XCTAssertEqual(state.freeDLStage, "Starting plan…")
        XCTAssertNil(state.freeDLRunID)
        state.routeFinished(finished("early-plan"))
        try rpc.reply(request, result: .object(["run_id": .string("early-plan")]))
        await task.value
        assertFreeDLIdle(state)
        XCTAssertEqual(state.freeDLStatus?.message, "terminal fixture")
        try await rpc.finishBeforeReply(state, method: .freeDLPlanStart) {
            await state.startFreeDLPlan(jobID: "fixture")
        }
        assertFreeDLIdle(state)
        await rpc.connection.close()
    }

    func testBufferedFinishIsConsumedOnlyByItsOwnIDWhileFreeDLStarts() async throws {
        let rpc = AgentRPCFixture()
        await rpc.connection.start()
        let state = AppState(client: rpc.client)
        defer { rpc.closePipes() }
        let freeStart = Task { await state.startFreeDLPlan(jobID: "fixture") }
        let request = try await rpc.readRequest(.freeDLPlanStart)
        state.routeFinished(finished("pair-run"))
        state.playlistSyncOperation = .plan
        state.playlistSyncRunID = "pair-run"
        state.consumeBufferedFinished("not-pair-run")
        XCTAssertEqual(state.playlistSyncRunID, "pair-run")
        XCTAssertNotNil(state.playlistSyncOperation)
        state.consumeBufferedFinished("pair-run")
        XCTAssertNil(state.playlistSyncRunID)
        XCTAssertNil(state.playlistSyncOperation)
        XCTAssertEqual(state.playlistSyncStatus?.message, "terminal fixture")
        XCTAssertNotNil(state.freeDLOperation)
        XCTAssertNil(state.freeDLRunID)
        XCTAssertNil(state.freeDLStatus)
        state.routeFinished(finished("free-run"))
        try rpc.reply(request, result: .object(["run_id": .string("free-run")]))
        await freeStart.value
        assertFreeDLIdle(state)
        await rpc.connection.close()
    }

    func testNonRunPhoneRegistrationDoesNotClaimAnUnrelatedFinish() async throws {
        let rpc = AgentRPCFixture()
        await rpc.connection.start()
        let state = AppState(client: rpc.client)
        defer { rpc.closePipes() }
        let registration = Task { await state.registerNavidromePlaylists() }
        let request = try await rpc.readRequest(.navidromePlaylistsRegister)
        XCTAssertNotNil(state.phoneLibraryOperation)
        let message = state.phoneLibraryStatus?.message
        state.routeFinished(finished("unrelated"))
        XCTAssertNotNil(state.phoneLibraryOperation)
        XCTAssertNil(state.phoneLibraryRunID)
        XCTAssertEqual(state.phoneLibraryStatus?.message, message)
        state.playlistSyncOperation = .plan
        state.playlistSyncRunID = "unrelated"
        state.consumeBufferedFinished("unrelated")
        XCTAssertNil(state.playlistSyncOperation)
        XCTAssertEqual(state.playlistSyncStatus?.message, "terminal fixture")
        // A direct RPC error avoids the registration's successful list refresh.
        try rpc.fail(request)
        await registration.value
        assertPhoneIdle(state)
        await rpc.connection.close()
    }

    func testStartupOperationsAreGloballyBusyBeforeTheyHaveRunIDs() async throws {
        let state = AppState()
        state.phoneLibraryOperation = .setupPlan
        XCTAssertTrue(state.isAnythingRunning)
        XCTAssertNotNil(state.busyReason)
        state.phoneLibraryOperation = nil
        state.playlistSyncOperation = .plan
        XCTAssertTrue(state.isAnythingRunning)
        XCTAssertNotNil(state.busyReason)
        state.playlistSyncOperation = nil
        assertGloballyIdle(state)

        let rpc = AgentRPCFixture()
        await rpc.connection.start()
        let connected = AppState(client: rpc.client)
        defer { rpc.closePipes() }
        let start = Task { await connected.startFreeDLPlan(jobID: "fixture") }
        let request = try await rpc.readRequest(.freeDLPlanStart)
        XCTAssertNil(connected.freeDLRunID)
        XCTAssertNotNil(connected.freeDLOperation)
        XCTAssertTrue(connected.isAnythingRunning)
        XCTAssertNotNil(connected.busyReason)
        connected.routeFinished(finished("free-run"))
        try rpc.reply(request, result: .object(["run_id": .string("free-run")]))
        await start.value
        assertFreeDLIdle(connected)
        try await loadSyncFixture(connected, rpc: rpc)
        let syncStart = Task { await connected.startSync() }
        let syncRequest = try await rpc.readRequest(.syncStart)
        XCTAssertNil(connected.syncRun.runID)
        XCTAssertEqual(connected.syncRun.phase, .starting)
        XCTAssertTrue(connected.isAnythingRunning)
        XCTAssertNotNil(connected.busyReason)
        connected.routeFinished(finished("sync-run"))
        try rpc.reply(syncRequest, result: .object(["run_id": .string("sync-run")]))
        await syncStart.value
        assertGloballyIdle(connected)
        connected.resetSyncRun()
        XCTAssertNil(connected.syncRun.runID)
        await rpc.connection.close()
    }

    func testGlobalCancellationQueuesUntilEachStartupReceivesItsOwnRunID() async throws {
        let rpc = AgentRPCFixture()
        await rpc.connection.start()
        let state = AppState(client: rpc.client)
        defer { rpc.closePipes() }
        let starters: [(AgentMethod, @MainActor @Sendable () async -> Void)] = [
            (.freeDLPlanStart, { await state.startFreeDLPlan(jobID: "fixture") }),
            (.rekordboxInspect, { await state.inspectRekordbox() }),
            (.playlistSyncPlan, {
                await state.planPlaylistSync(jobID: "fixture", direction: "rekordbox_to_navidrome")
            }),
            (.navidromeSetupPlan, { await state.planNavidromeSetup() }),
            (.playlistsRefresh, { await state.refreshPlaylist("fixture") }),
            (.playlistsProviderList, { await state.discoverProviderPlaylists() }),
        ]
        for (index, entry) in starters.enumerated() {
            let (method, start) = entry
            let task = Task { await start() }
            let request = try await rpc.readRequest(method)
            XCTAssertTrue(state.isAnythingRunning)
            XCTAssertNotNil(state.busyReason)
            await state.cancelActiveWork()
            let runID = "cancel-startup-\(index)"
            try rpc.reply(request, result: .object(["run_id": .string(runID)]))
            let cancel = try await rpc.readRequest(.runCancel)
            XCTAssertEqual(cancel["params"]?.objectValue?["run_id"]?.stringValue, runID)
            try rpc.reply(cancel, result: .object(["canceled": .bool(true)]))
            await task.value
            // Cancellation acknowledgement alone is not terminal. Busy clears
            // only when the matching run finishes, not at the start reply.
            XCTAssertTrue(state.isAnythingRunning)
            state.routeFinished(finished(runID))
            assertGloballyIdle(state)
            XCTAssertNil(state.freeDLRunID)
            XCTAssertNil(state.freeDLOperation)
            XCTAssertNil(state.rekordboxRunID)
            XCTAssertNil(state.rekordboxOperation)
            XCTAssertNil(state.playlistSyncRunID)
            XCTAssertNil(state.playlistSyncOperation)
            XCTAssertNil(state.phoneLibraryRunID)
            XCTAssertNil(state.phoneLibraryOperation)
            XCTAssertNil(state.playlistActiveRunID)
        }
        await rpc.connection.close()
    }

    func testPhoneAndPairBusyStatesRejectSyncAndPreviewWithoutChangingOptions() async throws {
        let rpc = AgentRPCFixture()
        await rpc.connection.start()
        let state = AppState(client: rpc.client)
        defer { rpc.closePipes() }
        try await loadSyncFixture(state, rpc: rpc)
        for destination in [AppState.Destination.phoneLibrary, .playlistSync] {
            if destination == .phoneLibrary { state.phoneLibraryOperation = .setupApply }
            else { state.playlistSyncOperation = .apply(dryRun: false) }
            let dryRun = state.syncDryRun
            await state.startSync()
            XCTAssertEqual(state.syncRun.phase, .idle)
            XCTAssertEqual(state.syncValidationMessage, state.busyReason)
            await state.startDryRunPlan()
            XCTAssertEqual(state.syncRun.phase, .idle)
            XCTAssertEqual(state.syncDryRun, dryRun)
            state.phoneLibraryOperation = nil
            state.playlistSyncOperation = nil
        }
        // No rejected start may have sent a hidden RPC or left startup state.
        try await rpc.finishBeforeReply(state, method: .syncStart) { await state.startSync() }
        assertGloballyIdle(state)
        await rpc.connection.close()
    }

    func testEarlySyncFramesWaitForRegisteredIDAndReplayBeforeTerminal() async throws {
        let rpc = AgentRPCFixture()
        await rpc.connection.start()
        defer { rpc.closePipes() }
        let state = AppState(client: rpc.client)
        try await loadSyncFixture(state, rpc: rpc)
        let start = Task { await state.startSync() }
        let request = try await rpc.readRequest(.syncStart)
        state.applyLosslessSyncEvent(event("unrelated", message: "Wrong workflow"))
        state.applySyncProgress(progress("unrelated"))
        XCTAssertNil(state.syncRun.runID)
        XCTAssertTrue(state.syncRun.sources.isEmpty)
        XCTAssertTrue(state.syncRun.activity.isEmpty)
        state.applyLosslessSyncEvent(event("sync-owned", message: "Owned lifecycle"))
        state.applySyncProgress(progress("sync-owned"))
        XCTAssertNil(state.syncRun.runID)
        XCTAssertNil(state.syncRun.progress)
        try rpc.reply(request, result: .object(["run_id": .string("sync-owned")]))
        await start.value
        XCTAssertEqual(state.syncRun.runID, "sync-owned")
        XCTAssertEqual(state.syncRun.activity.map(\.message), ["Owned lifecycle"])
        XCTAssertEqual(state.syncRun.progress?.track.progressPercent, 73)
        state.routeFinished(finished("sync-owned"))
        state.resetSyncRun()
        let next = Task { await state.startSync() }
        let nextRequest = try await rpc.readRequest(.syncStart)
        state.applyLosslessSyncEvent(event("sync-owned", message: "Stale lifecycle"))
        state.applySyncProgress(progress("sync-owned"))
        state.routeFinished(finished("sync-owned"))
        XCTAssertNil(state.syncRun.runID)
        XCTAssertTrue(state.syncRun.activity.isEmpty)
        try rpc.reply(nextRequest, result: .object(["run_id": .string("next-sync")]))
        await next.value
        XCTAssertEqual(state.syncRun.runID, "next-sync")
        XCTAssertTrue(state.syncRun.sources.isEmpty)
        state.routeFinished(finished("next-sync"))
        await rpc.connection.close()
    }

    func testConcurrentCaptureAndSyncRegisterOnlyTheirOwnEarlyFrames() async throws {
        let rpc = AgentRPCFixture()
        await rpc.connection.start()
        defer { rpc.closePipes() }
        let state = AppState(client: rpc.client)
        try await rpc.finishBeforeReply(state, method: .freeDLPlanStart, result: capturePlan()) {
            await state.startFreeDLPlan(jobID: "fixture")
        }
        try await loadSyncFixture(state, rpc: rpc)
        let sync = Task { await state.startSync() }
        let syncRequest = try await rpc.readRequest(.syncStart)
        let capture = Task { await state.startFreeDLCapture() }
        let captureRequest = try await rpc.readRequest(.freeDLCaptureStart)
        state.applyLosslessSyncEvent(event("capture", message: "Capture lifecycle"))
        state.applySyncProgress(progress("capture"))
        state.applyLosslessSyncEvent(event("sync", message: "Sync lifecycle"))
        state.applySyncProgress(progress("sync"))
        XCTAssertNil(state.syncRun.runID)
        XCTAssertNil(state.freeDLRunID)
        XCTAssertTrue(state.syncRun.activity.isEmpty)
        XCTAssertTrue(state.freeDLCaptureActivity.isEmpty)
        try rpc.reply(captureRequest, result: .object(["run_id": .string("capture")]))
        await capture.value
        XCTAssertEqual(state.freeDLRunID, "capture")
        XCTAssertEqual(state.freeDLCaptureActivity.map(\.message), ["Capture lifecycle"])
        XCTAssertNil(state.syncRun.runID)
        try rpc.reply(syncRequest, result: .object(["run_id": .string("sync")]))
        await sync.value
        XCTAssertEqual(state.syncRun.runID, "sync")
        XCTAssertEqual(state.syncRun.activity.map(\.message), ["Sync lifecycle"])
        XCTAssertEqual(state.syncRun.progress?.track.progressPercent, 73)
        state.routeFinished(finished("capture"))
        state.routeFinished(finished("sync"))
        await rpc.connection.close()
    }

    func testCaptureTerminalRowsRejectProgressWithoutBlockingNextTrack() async throws {
        let rpc = AgentRPCFixture()
        await rpc.connection.start()
        defer { rpc.closePipes() }
        let state = AppState(client: rpc.client)
        try await rpc.finishBeforeReply(state, method: .freeDLPlanStart, result: capturePlan()) {
            await state.startFreeDLPlan(jobID: "fixture")
        }
        let capture = Task { await state.startFreeDLCapture() }
        let request = try await rpc.readRequest(.freeDLCaptureStart)
        try rpc.reply(request, result: .object(["run_id": .string("capture")]))
        await capture.value
        func row(_ index: Int, status: String) -> TrackRow {
            TrackRow(sourceID: "fixture", sourceLabel: "fixture", remoteID: "track-\(index)", title: "Fixture",
                index: index, executionSlot: index + 1, toggleable: true, planStatus: "missing_new",
                planClass: "new", selected: true, runScope: "included", runtimeStatus: status,
                statusLabel: status, failureDetail: status == "failed" ? "Fixture failure" : nil,
                progressKnown: true, progressPercent: status == "downloading" ? 73 : 100)
        }
        for status in ["downloaded", "failed", "skipped"] {
            state.applyLosslessSyncEvent(event("capture", message: "Track outcome", rows: [row(0, status: status)]))
            state.applySyncProgress(progress("capture", row: row(0, status: "downloading")))
            XCTAssertEqual(state.freeDLCaptureSources["fixture"]?.rows.first?.runtimeStatus, status)
            XCTAssertEqual(state.freeDLCaptureSources["fixture"]?.rows.first?.progressPercent, 100)
            state.applySyncProgress(progress("capture", row: row(1, status: "downloading")))
            XCTAssertEqual(state.freeDLCaptureSources["fixture"]?.rows.first?.runtimeStatus, status)
            XCTAssertEqual(state.freeDLCaptureSources["fixture"]?.rows.last?.runtimeStatus, "downloading")
            XCTAssertEqual(state.freeDLCaptureSources["fixture"]?.rows.count, 2)
        }
        state.routeFinished(finished("capture"))
        await rpc.connection.close()
    }

    func testEarlyLifecycleIsRetainedWhenTerminalAlsoBeatsStartReply() async throws {
        let rpc = AgentRPCFixture()
        await rpc.connection.start()
        defer { rpc.closePipes() }
        let state = AppState(client: rpc.client)
        try await loadSyncFixture(state, rpc: rpc)
        let start = Task { await state.startSync() }
        let request = try await rpc.readRequest(.syncStart)
        state.applyLosslessSyncEvent(event("early-sync", message: "Completed source", lifecycle: "finished"))
        state.routeFinished(finished("early-sync"))
        XCTAssertNil(state.syncRun.exitCode)
        try rpc.reply(request, result: .object(["run_id": .string("early-sync")]))
        await start.value
        XCTAssertEqual(state.syncRun.exitCode, 1)
        XCTAssertEqual(state.syncRun.activity.map(\.message), ["Completed source"])
        XCTAssertEqual(state.syncRun.sources["fixture"]?.lifecycle, "finished")
        await rpc.connection.close()
    }

    func testNotFoundCancellationRequiresDeliveredPromptForSameRun() async throws {
        for promptRunID in ["other-run", "sync-cancel"] {
            let rpc = AgentRPCFixture()
            await rpc.connection.start()
            let state = AppState(client: rpc.client)
            try await loadSyncFixture(state, rpc: rpc)
            let start = Task { await state.startSync() }
            let request = try await rpc.readRequest(.syncStart)
            try rpc.reply(request, result: .object(["run_id": .string("sync-cancel")]))
            await start.value
            state.receivePrompt(prompt(promptRunID))
            let stop = Task { await state.cancelActiveSync() }
            if promptRunID == "sync-cancel" {
                let response = try await rpc.readRequest()
                XCTAssertEqual(response["id"], .string("prompt-sync-cancel"))
            }
            let cancel = try await rpc.readRequest(.runCancel)
            try rpc.fail(cancel, code: -32002, message: "run not found")
            await stop.value
            XCTAssertEqual(state.syncRun.cancelAcknowledged, promptRunID == "sync-cancel")
            if promptRunID == "other-run" {
                XCTAssertNotNil(state.pendingPrompt)
                XCTAssertTrue(state.syncRun.terminalMessage?.contains("Stop failed") == true)
            }
            state.routeFinished(finished("sync-cancel"))
            await rpc.connection.close()
            rpc.closePipes()
        }
    }

    func testLateCancellationReplyCannotChangeReplacementSync() async throws {
        let rpc = AgentRPCFixture()
        await rpc.connection.start()
        defer { rpc.closePipes() }
        let state = AppState(client: rpc.client)
        try await loadSyncFixture(state, rpc: rpc)
        let start = Task { await state.startSync() }
        let request = try await rpc.readRequest(.syncStart)
        try rpc.reply(request, result: .object(["run_id": .string("old-sync")]))
        await start.value
        state.receivePrompt(prompt("old-sync"))
        let stop = Task { await state.cancelActiveSync() }
        _ = try await rpc.readRequest() // canceled prompt reply
        let cancel = try await rpc.readRequest(.runCancel)
        state.routeFinished(finished("old-sync"))
        state.resetSyncRun()
        let next = Task { await state.startSync() }
        let nextRequest = try await rpc.readRequest(.syncStart)
        try rpc.reply(nextRequest, result: .object(["run_id": .string("new-sync")]))
        await next.value
        try rpc.fail(cancel, code: -32002, message: "run not found")
        await stop.value
        XCTAssertEqual(state.syncRun.runID, "new-sync")
        XCTAssertFalse(state.syncRun.cancelAcknowledged)
        XCTAssertNil(state.syncRun.terminalMessage)
        state.routeFinished(finished("new-sync"))
        await rpc.connection.close()
    }

    func testTerminalClearsOnlyItsPromptAndRejectsLatePrompts() {
        let state = AppState()
        state.playlistSyncRunID = "pair"
        state.playlistSyncOperation = .plan
        state.receivePrompt(prompt("pair"))
        state.routeFinished(finished("pair"))
        XCTAssertNil(state.pendingPrompt)
        state.receivePrompt(prompt("pair"))
        XCTAssertNil(state.pendingPrompt)
        state.receivePrompt(prompt("live-other"))
        state.routeFinished(finished("unknown-terminal"))
        XCTAssertEqual(state.pendingPrompt?.request.params.objectValue?["run_id"]?.stringValue, "live-other")
        state.receivePrompt(prompt("unknown-terminal"))
        XCTAssertEqual(state.pendingPrompt?.request.params.objectValue?["run_id"]?.stringValue, "live-other")
    }

    private func prompt(_ runID: String) -> UIRequest {
        UIRequest(id: .string("prompt-\(runID)"), kind: .confirm,
                  params: .object(["run_id": .string(runID), "message": .string("Continue?")]))
    }

    private func event(_ runID: String, message: String, lifecycle: String = "running", rows: [TrackRow] = []) -> SyncEventNotification {
        SyncEventNotification(runID: runID,
            event: OutputEvent(timestamp: nil, level: "info", event: "source_started", sourceID: "fixture", message: message, details: nil),
            source: SourceSnapshot(lifecycle: lifecycle, confirmed: true, rows: rows, activity: []))
    }

    private func progress(_ runID: String, row: TrackRow? = nil) -> SyncProgressNotification {
        SyncProgressNotification(runID: runID, sourceID: "fixture", progress: StructuredProgressSnapshot(
            progress: ProgressModel(
                source: SourceProgress(id: "fixture", lifecycle: "running", plannedTotal: 1, itemTotal: 1, itemIndex: 1, completed: 0),
                track: TrackProgress(name: "Fixture", lifecycle: "downloading", progressPercent: 73),
                global: GlobalProgress(total: 1, completed: 0)),
            track: StructuredTrackState(name: "Fixture", progressKnown: true, progressPercent: 73, lifecycle: "downloading"),
            structuredTrackEvents: true), row: row)
    }

    private func loadSyncFixture(_ state: AppState, rpc: AgentRPCFixture) async throws {
        let load = Task { await state.loadSyncSources() }
        let request = try await rpc.readRequest(.sourcesCapabilities)
        try rpc.reply(request, result: .object(["sources": .array([.object([
            "source_id": .string("fixture"), "source_type": .string("soundcloud"),
            "adapter": .string("scdl"), "supports_plan": .bool(true),
            "supports_plan_window": .bool(true), "supports_download_order": .bool(true),
            "default_plan_window": .string("first"), "default_download_order": .string("newest_first"),
        ])])]))
        await load.value
        XCTAssertEqual(state.syncSources.count, 1)
    }

    private func finished(_ id: String) -> RunFinishedNotification {
        RunFinishedNotification(runID: id, result: nil, error: "terminal fixture", exitCode: 1)
    }

    private func assertGloballyIdle(_ state: AppState, file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertFalse(state.isAnythingRunning, file: file, line: line)
        XCTAssertNil(state.busyReason, file: file, line: line)
    }

    private func assertFreeDLIdle(_ state: AppState, file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertNil(state.freeDLRunID, file: file, line: line)
        XCTAssertNil(state.freeDLOperation, file: file, line: line)
        assertGloballyIdle(state, file: file, line: line)
    }

    private func assertRekordboxIdle(_ state: AppState, file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertNil(state.rekordboxRunID, file: file, line: line)
        XCTAssertNil(state.rekordboxOperation, file: file, line: line)
        assertGloballyIdle(state, file: file, line: line)
    }

    private func assertPhoneIdle(_ state: AppState, file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertNil(state.phoneLibraryRunID, file: file, line: line)
        XCTAssertNil(state.phoneLibraryOperation, file: file, line: line)
        XCTAssertFalse(state.isPhoneLibraryBusy, file: file, line: line)
        assertGloballyIdle(state, file: file, line: line)
    }

    private func assertPairIdle(_ state: AppState, file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertNil(state.playlistSyncRunID, file: file, line: line)
        XCTAssertNil(state.playlistSyncOperation, file: file, line: line)
        assertGloballyIdle(state, file: file, line: line)
    }

    private func capturePlan() -> JSONValue {
        .object([
            "run_id": .string("capture-plan"), "created_at": .string("2026-01-01T00:00:00Z"),
            "buffer_root": .string("/fixture/buffer"), "log_dir": .string("/fixture/log"),
            "job": .object([
                "id": .string("fixture"), "enabled": .bool(true), "source_url": .string("https://soundcloud.com/fixture"),
                "library_dir": .string("/fixture/library"), "buffer_dir": .string("/fixture/buffer"),
                "backup_dir": .string("/fixture/backups"), "log_dir": .string("/fixture/log"),
                "state_file": .string("/fixture/state"), "plan_limit": .number(1),
                "download_order": .string("newest_first"), "target_format": .string("mp3"),
                "min_match_score": .number(90), "ambiguity_gap": .number(5), "replace_limit": .number(1),
                "apply_promotions": .bool(false),
            ]),
            "rows": .array([.object([
                "index": .number(0), "remote_id": .string("track"), "title": .string("Fixture"),
                "local_quality": .object(["lossless": .bool(false)]), "free_dl_probe": .object([:]),
                "selectable": .bool(true), "selected": .bool(true),
            ])]),
        ])
    }

    private var pairPlanJSON: String {
        """
        {"plan_path":"/fixture/plan","plan":{
          "version":1,"generated_at":"2026-01-01T00:00:00Z","job_id":"fixture",
          "config_fingerprint":"fixture","direction":"rekordbox_to_navidrome",
          "source":{"provider":"rekordbox","name":"Source","track_count":0},
          "destination":{"provider":"navidrome","name":"Destination","track_count":0},
          "destination_create":false,"summary":{"source_total":0,"destination_total":0,
          "final_total":0,"will_add":0,"will_remove":0,"will_move":0,"will_keep":0,"blocked":0},
          "rows":[],"blockers":[],"preconditions":{},"checksum_sha256":"fixture"
        }}
        """
    }
}

@MainActor
final class AgentRPCFixture {
    let inbound = Pipe()
    let outbound = Pipe()
    let connection: JSONRPCConnection
    let client: UDLClient
    private var sequence = 0

    init() {
        connection = JSONRPCConnection(
            readHandle: inbound.fileHandleForReading, writeHandle: outbound.fileHandleForWriting
        )
        client = UDLClient(connection: connection)
    }

    func finishBeforeReply(
        _ state: AppState, method: AgentMethod, result: JSONValue? = nil,
        start: @escaping @MainActor () async -> Void
    ) async throws {
        sequence += 1
        let runID = "early-\(sequence)"
        let task = Task { await start() }
        let request = try await readRequest(method)
        state.routeFinished(RunFinishedNotification(
            runID: runID, result: result, error: result == nil ? "terminal fixture" : nil,
            exitCode: result == nil ? 1 : 0
        ))
        try reply(request, result: .object(["run_id": .string(runID)]))
        await task.value
    }

    func readRequest(_ method: AgentMethod? = nil) async throws -> [String: JSONValue] {
        let handle = outbound.fileHandleForReading
        let data: Data = try await withCheckedThrowingContinuation { continuation in
            DispatchQueue.global().async {
                do {
                    var data = Data()
                    while true {
                        var descriptor = pollfd(fd: handle.fileDescriptor, events: Int16(POLLIN), revents: 0)
                        guard Darwin.poll(&descriptor, 1, 5_000) > 0 else {
                            throw JSONRPCConnectionError.disconnected("timed out waiting for starter RPC")
                        }
                        let byte = try handle.read(upToCount: 1) ?? Data()
                        guard !byte.isEmpty else { throw JSONRPCConnectionError.disconnected("test pipe EOF") }
                        if byte[0] == 0x0A { break }
                        data.append(byte)
                    }
                    continuation.resume(returning: data)
                } catch { continuation.resume(throwing: error) }
            }
        }
        let request = try JSONDecoder().decode([String: JSONValue].self, from: data)
        if let method { XCTAssertEqual(request["method"]?.stringValue, method.rawValue) }
        return request
    }

    func reply(_ request: [String: JSONValue], result: JSONValue) throws {
        try write(["jsonrpc": .string("2.0"), "id": try XCTUnwrap(request["id"]), "result": result])
    }

    func fail(_ request: [String: JSONValue], code: Int = -32000, message: String = "fixture RPC failure") throws {
        try write([
            "jsonrpc": .string("2.0"), "id": try XCTUnwrap(request["id"]),
            "error": .object(["code": .number(Double(code)), "message": .string(message)]),
        ])
    }

    private func write(_ object: [String: JSONValue]) throws {
        var data = try JSONEncoder().encode(object)
        data.append(0x0A)
        try inbound.fileHandleForWriting.write(contentsOf: data)
    }

    func closePipes() {
        try? inbound.fileHandleForWriting.close()
        try? outbound.fileHandleForReading.close()
    }
}
