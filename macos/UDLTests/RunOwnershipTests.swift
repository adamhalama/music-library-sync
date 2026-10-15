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
        let rpc = RunOwnershipRPC()
        await rpc.connection.start()
        let state = AppState(client: rpc.client)
        defer { rpc.closePipes() }
        let starters: [(AgentMethod, () async -> Void)] = [
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
            result: .object(["plan": .object(["checksum_sha256": .string("fixture")]), "plan_path": .string("/fixture/plan")]),
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
        let rpc = RunOwnershipRPC()
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
        let rpc = RunOwnershipRPC()
        await rpc.connection.start()
        let state = AppState(client: rpc.client)
        defer { rpc.closePipes() }
        let starters: [(AgentMethod, () async -> Void)] = [
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
        let guarded: [(AgentMethod, () async -> Void)] = [
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

    func testPlaylistSyncPlanAndApplyConsumeEarlyFinishAndCanStartAgain() async throws {
        let rpc = RunOwnershipRPC()
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
        let rpc = RunOwnershipRPC()
        await rpc.connection.start()
        let state = AppState(client: rpc.client)
        defer { rpc.closePipes() }
        let starters: [(AgentMethod, () async -> Void)] = [
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
        let rpc = RunOwnershipRPC()
        await rpc.connection.start()
        let state = AppState(client: rpc.client)
        defer { rpc.closePipes() }
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
        let rpc = RunOwnershipRPC()
        await rpc.connection.start()
        let state = AppState(client: rpc.client)
        defer { rpc.closePipes() }
        let freeStart = Task { await state.startFreeDLPlan(jobID: "fixture") }
        let request = try await rpc.readRequest(.freeDLPlanStart)
        XCTAssertNotNil(state.freeDLOperation)
        XCTAssertNil(state.freeDLRunID)
        state.syncRun = SyncRunState(phase: .starting)
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
        state.syncRun = SyncRunState()
        state.routeFinished(finished("free-run"))
        try rpc.reply(request, result: .object(["run_id": .string("free-run")]))
        await freeStart.value
        assertFreeDLIdle(state)
        await rpc.connection.close()
    }

    func testEarlyFreeDLPlanEventCannotLetLateStartReplyReviveFinishedRun() async throws {
        let rpc = RunOwnershipRPC()
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
        XCTAssertEqual(state.freeDLStage, "Scanning fixture")
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
        let rpc = RunOwnershipRPC()
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
        let rpc = RunOwnershipRPC()
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
private final class RunOwnershipRPC {
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

    func readRequest(_ method: AgentMethod) async throws -> [String: JSONValue] {
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
        XCTAssertEqual(request["method"]?.stringValue, method.rawValue)
        return request
    }

    func reply(_ request: [String: JSONValue], result: JSONValue) throws {
        try write(["jsonrpc": .string("2.0"), "id": try XCTUnwrap(request["id"]), "result": result])
    }

    func fail(_ request: [String: JSONValue]) throws {
        try write([
            "jsonrpc": .string("2.0"), "id": try XCTUnwrap(request["id"]),
            "error": .object(["code": .number(-32000), "message": .string("fixture RPC failure")]),
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
