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

    func testUnloadedConfigSurfacesWorkflowErrorInsteadOfSilentlyFailing() async {
        let state = AppState()
        let saved = await state.savePlaylistSyncJob(job("new"), intent: .create)
        XCTAssertFalse(saved)
        XCTAssertEqual(state.playlistSyncStatus?.severity, .error)
        XCTAssertTrue(state.playlistSyncStatus?.message.contains("configuration is not loaded") == true)
        XCTAssertNil(state.alertMessage)
        XCTAssertNil(state.playlistConfig)
    }
}
