import Foundation
import XCTest
@testable import UDL

@MainActor
final class PlaylistSyncJobSaveTests: XCTestCase {
    private func job(_ id: String, playlist: String = "Original") -> PlaylistSyncJob {
        let selector = PlaylistSyncSelector(playlist: playlist, playlistID: nil)
        return PlaylistSyncJob(id: id, rekordbox: selector, navidrome: selector)
    }

    private func config(_ jobs: [PlaylistSyncJob]) -> PlaylistConfig {
        PlaylistConfig(version: 1, playlists: [], syncJobs: jobs)
    }

    func testCreateRejectsExactAndCaseInsensitiveCollisionsWithoutReplacingPair() {
        let original = job("My-Pair")
        let current = config([original])
        for id in ["My-Pair", "my-pair", "MY-PAIR"] {
            do {
                _ = try PlaylistSyncJobSaveIntent.create.updatedConfig(current, saving: job(id, playlist: "Replacement"))
                XCTFail("Create must not overwrite an existing ID: \(id)")
            } catch {
                XCTAssertTrue(error.localizedDescription.contains("already exists"))
                XCTAssertTrue(error.localizedDescription.contains("case-insensitive"))
            }
        }
        XCTAssertEqual(current.syncJobs, [original])
    }

    func testCreateAppendsNewPairAndPreservesExistingConfiguration() throws {
        let original = job("original")
        let added = job("added")
        let current = config([original])
        let updated = try PlaylistSyncJobSaveIntent.create.updatedConfig(current, saving: added)
        XCTAssertEqual(updated.syncJobs, [original, added])
        XCTAssertEqual(updated.version, current.version)
        XCTAssertEqual(updated.playlists.count, current.playlists.count)
    }

    func testEditReplacesOnlyTheOriginalPair() throws {
        let first = job("first")
        let original = job("original")
        let last = job("last")
        let replacement = job(original.id, playlist: "Changed selector")
        let updated = try PlaylistSyncJobSaveIntent.edit(originalID: original.id).updatedConfig(
            config([first, original, last]), saving: replacement
        )
        XCTAssertEqual(updated.syncJobs, [first, replacement, last])
    }

    func testEditCannotRenameOrOverwriteAnotherPair() {
        let current = config([job("original"), job("other")])
        for id in ["other", "ORIGINAL", "new"] {
            do {
                _ = try PlaylistSyncJobSaveIntent.edit(originalID: "original").updatedConfig(current, saving: job(id))
                XCTFail("Editing must retain the original stable ID")
            } catch {
                XCTAssertTrue(error.localizedDescription.contains("ID cannot be changed"))
            }
        }
    }

    func testEditOfRemovedOriginalDoesNotRecreateOrReplaceACaseVariant() {
        for jobs in [[], [job("ORIGINAL")]] {
            do {
                _ = try PlaylistSyncJobSaveIntent.edit(originalID: "original").updatedConfig(config(jobs), saving: job("original"))
                XCTFail("A stale editor must not recreate a removed pair")
            } catch {
                XCTAssertTrue(error.localizedDescription.contains("no longer exists"))
            }
        }
    }

    func testEditRejectsAnotherPairsCaseInsensitiveCollision() {
        do {
            _ = try PlaylistSyncJobSaveIntent.edit(originalID: "original").updatedConfig(
                config([job("original"), job("ORIGINAL")]), saving: job("original", playlist: "Changed")
            )
            XCTFail("An ambiguous existing configuration must not be written")
        } catch {
            XCTAssertTrue(error.localizedDescription.contains("already exists"))
        }
    }

    /// A disconnected writer used to return normally, so its caller announced
    /// success and dismissed the editor even though no config reached disk.
    func testDisconnectedConfigWriteThrowsInsteadOfClaimingSuccess() async {
        let state = AppState()
        do {
            try await state.writePlaylistSyncConfig(config([job("new")]))
            XCTFail("A write without a backend must fail")
        } catch {
            XCTAssertTrue(error.localizedDescription.contains("Backend disconnected"))
            XCTAssertTrue(error.localizedDescription.contains("not saved"))
        }
        XCTAssertNil(state.playlistConfig)
        XCTAssertNil(state.alertMessage)
        XCTAssertNil(state.playlistSyncStatus)
    }

    func testUnloadedConfigSurfacesWorkflowErrorInsteadOfSilentlyFailing() async {
        let state = AppState()
        let saved = await state.savePlaylistSyncJob(job("new"), intent: .create)
        XCTAssertFalse(saved)
        XCTAssertEqual(state.playlistSyncStatus?.severity, .error)
        XCTAssertTrue(state.playlistSyncStatus?.message.contains("configuration is not loaded") == true)
        XCTAssertNil(state.alertMessage)
        XCTAssertNil(state.playlistConfig)
    }
    func testExpectedSHAConflictStaysInPairWorkflowAndReloadRefreshesConfig() async throws {
        let rpc = AgentRPCFixture()
        await rpc.connection.start()
        defer { rpc.closePipes() }
        let state = AppState(client: rpc.client)
        try await reload(state, rpc: rpc, sha: "original-sha", jobs: [job("original")])
        let save = Task { await state.savePlaylistSyncJob(job("new"), intent: .create) }
        let request = try await rpc.readRequest(.playlistsConfigWrite)
        XCTAssertEqual(request["params"]?.objectValue?["expected_content_sha256"]?.stringValue, "original-sha")
        try rpc.fail(request, code: -32003, message: "playlist config changed outside UDL; reload before saving")
        let saved = await save.value
        XCTAssertFalse(saved)
        XCTAssertEqual(state.playlistSyncStatus?.severity, .error)
        XCTAssertTrue(state.playlistSyncStatus?.message.contains("reload before saving") == true)
        XCTAssertEqual(state.playlistConfig?.contentSHA256, "original-sha")
        XCTAssertEqual(state.playlistConfig?.config.syncJobs.map(\.id), ["original"])
        XCTAssertNil(state.alertMessage)
        try await reload(state, rpc: rpc, sha: "external-sha", jobs: [job("external")])
        XCTAssertEqual(state.playlistConfig?.contentSHA256, "external-sha")
        XCTAssertEqual(state.playlistConfig?.config.syncJobs.map(\.id), ["external"])
        XCTAssertEqual(state.playlistSyncStatus?.severity, .info)
        await rpc.connection.close()
    }

    func testSuccessfulWriterPassesExpectedSHAAndRetainsReturnedSHA() async throws {
        let rpc = AgentRPCFixture()
        await rpc.connection.start()
        defer { rpc.closePipes() }
        let state = AppState(client: rpc.client)
        try await reload(state, rpc: rpc, sha: "read-sha", jobs: [])
        let updated = config([job("new")])
        let write = Task { try await state.writePlaylistSyncConfig(updated) }
        let request = try await rpc.readRequest(.playlistsConfigWrite)
        XCTAssertEqual(request["params"]?.objectValue?["expected_content_sha256"]?.stringValue, "read-sha")
        try rpc.reply(request, result: configResult(sha: "written-sha", jobs: updated.syncJobs))
        // The writer refreshes the cache/list without contacting a provider.
        for _ in 0..<2 {
            let request = try await rpc.readRequest()
            if request["method"]?.stringValue == AgentMethod.playlistsList.rawValue {
                try rpc.reply(request, result: .object(["playlists": .null]))
            } else {
                XCTAssertEqual(request["method"]?.stringValue, AgentMethod.playlistsConfigRead.rawValue)
                try rpc.reply(request, result: configResult(sha: "written-sha", jobs: updated.syncJobs))
            }
        }
        try await write.value
        XCTAssertEqual(state.playlistConfig?.contentSHA256, "written-sha")
        XCTAssertEqual(state.playlistConfig?.config.syncJobs, updated.syncJobs)
        XCTAssertNil(state.alertMessage)
        await rpc.connection.close()
    }

    func testPlaylistConfigSHARemainsCompatibleWithOlderWireFrames() throws {
        let data = Data(#"{"path":"/fixture","config":{"version":1,"playlists":null,"sync_jobs":null},"content":""}"#.utf8)
        let result = try JSONDecoder().decode(PlaylistConfigResult.self, from: data)
        XCTAssertNil(result.contentSHA256)
        XCTAssertTrue(result.config.playlists.isEmpty)
        XCTAssertTrue(result.config.syncJobs.isEmpty)
        let params = try JSONDecoder().decode(JSONValue.self, from: JSONEncoder().encode(PlaylistConfigWriteParams(config: result.config)))
        XCTAssertNil(params.objectValue?["expected_content_sha256"])
    }

    private func reload(_ state: AppState, rpc: AgentRPCFixture, sha: String, jobs: [PlaylistSyncJob]) async throws {
        let reload = Task { await state.loadPlaylistSync() }
        for _ in 0..<2 {
            let request = try await rpc.readRequest()
            if request["method"]?.stringValue == AgentMethod.playlistSyncInspect.rawValue {
                try rpc.reply(request, result: .object(["jobs": .null]))
            } else {
                XCTAssertEqual(request["method"]?.stringValue, AgentMethod.playlistsConfigRead.rawValue)
                try rpc.reply(request, result: configResult(sha: sha, jobs: jobs))
            }
        }
        await reload.value
    }

    private func configResult(sha: String, jobs: [PlaylistSyncJob]) throws -> JSONValue {
        let result = PlaylistConfigResult(path: "/fixture/playlists.yaml", config: config(jobs), content: "fixture", contentSHA256: sha)
        return try JSONDecoder().decode(JSONValue.self, from: JSONEncoder().encode(result))
    }

}
