import XCTest
@testable import UDL

final class PhoneLibraryTests: XCTestCase {
    // MARK: Wire decoding

    /// Go nil slices encode as JSON `null`. Every collection on the Phone
    /// Library wire types must survive that, or a fresh install — where nearly
    /// every list is empty — fails to decode at all.
    func testEveryPhoneLibraryResultDecodesWithAllCollectionsNull() throws {
        let statusPayload = """
        {
          "enabled": true,
          "config_path": "/tmp/navidrome.yaml",
          "music_dir": "/Users/jaa/Music/downloaded",
          "data_dir": "/Users/jaa/Library/Application Support/UDL/Navidrome",
          "playlists_dir": "/Users/jaa/Music/downloaded/.udl/playlists",
          "username": "jaa",
          "password_stored": false,
          "dependency": {
            "homebrew_installed": true,
            "installed": false,
            "minimum_version": "0.63.2",
            "version_supported": false,
            "problems": null
          },
          "service": {
            "state": "not_installed",
            "loaded": false,
            "owned": false,
            "problems": null
          },
          "reachable": false,
          "server": {},
          "library_tracks": 0,
          "scanning": false,
          "managed_playlists": null,
          "backup_count": 0,
          "problems": null
        }
        """.data(using: .utf8)!
        let status = try JSONDecoder.agent.decode(NavidromeStatus.self, from: statusPayload)
        XCTAssertEqual(status.problems, [])
        XCTAssertEqual(status.managedPlaylists, [])
        XCTAssertEqual(status.dependency.problems, [])
        XCTAssertEqual(status.service.problems, [])

        let refreshPayload = """
        {"generated": null, "imported": null, "scanned": false, "warnings": null}
        """.data(using: .utf8)!
        let refresh = try JSONDecoder.agent.decode(NavidromePlaylistRefreshResult.self, from: refreshPayload)
        XCTAssertEqual(refresh.generated, [])
        XCTAssertEqual(refresh.warnings, [])

        let derivationPayload = """
        {
          "source_playlist": "HARD BOUNCE",
          "source_track_count": 0,
          "matched_count": 0,
          "genres": null,
          "unmatched_paths": null,
          "genreless_paths": null,
          "outside_library": null,
          "current_allowlist": null,
          "allowlist_changed": false
        }
        """.data(using: .utf8)!
        let derivation = try JSONDecoder.agent.decode(NavidromeGenreDerivation.self, from: derivationPayload)
        XCTAssertEqual(derivation.genres, [])
        XCTAssertEqual(derivation.unmatchedPaths, [])

        let applyPayload = """
        {
          "newly_starred": null, "already_starred": 0, "final_starred": 0,
          "parity_verified": false, "compensated": null
        }
        """.data(using: .utf8)!
        let applied = try JSONDecoder.agent.decode(NavidromeFavoriteApplyResult.self, from: applyPayload)
        XCTAssertEqual(applied.newlyStarred, [])
        XCTAssertEqual(applied.compensated, [])

        let setupPayload = """
        {"written_files": null, "created_directories": null}
        """.data(using: .utf8)!
        let setup = try JSONDecoder.agent.decode(NavidromeSetupApplyResult.self, from: setupPayload)
        XCTAssertEqual(setup.writtenFiles, [])
        XCTAssertEqual(setup.createdDirectories, [])

        let configPayload = """
        {
          "path": "/tmp/navidrome.yaml",
          "content": "version: 1\\n",
          "config": {
            "version": 1, "enabled": false,
            "server": {"url": "http://localhost:4533", "username": "", "port": 4533, "address": "0.0.0.0"},
            "paths": {
              "music_dir": "~/Music/downloaded", "data_dir": "/d", "cache_dir": "/c",
              "log_file": "/l", "config_file": "/f", "backup_dir": "/b",
              "playlists_path": ".udl/playlists"
            },
            "scan": {"schedule": "@every 1h"},
            "backup": {"schedule": "0 3 * * *", "count": 7},
            "playlists": {"hard_bounce_genres": null, "apple_hard_bounce_playlist": "HARD BOUNCE"}
          }
        }
        """.data(using: .utf8)!
        let config = try JSONDecoder.agent.decode(NavidromeConfigResult.self, from: configPayload)
        XCTAssertEqual(config.config.playlists.hardBounceGenres, [])
    }

    /// The return path's wire result. An empty star list is the normal state on
    /// a fresh install, and Go encodes a nil slice as `null`, so it must decode.
    func testStarredListingDecodesIncludingAnEmptyLibrary() throws {
        let empty = """
        {"count": 0, "tracks": null}
        """.data(using: .utf8)!
        let none = try JSONDecoder.agent.decode(NavidromeStarredListResult.self, from: empty)
        XCTAssertEqual(none.count, 0)
        XCTAssertEqual(none.tracks, [])

        let payload = """
        {
          "count": 2,
          "tracks": [
            {"id": "a", "title": "First", "artist": "Someone", "path": "/music/a.mp3"},
            {"id": "b", "title": "Second"}
          ]
        }
        """.data(using: .utf8)!
        let starred = try JSONDecoder.agent.decode(NavidromeStarredListResult.self, from: payload)
        XCTAssertEqual(starred.count, 2)
        XCTAssertEqual(starred.tracks.map(\.id), ["a", "b"])
        XCTAssertEqual(starred.tracks[0].path, "/music/a.mp3")
        // The optional fields are genuinely optional on the wire.
        XCTAssertNil(starred.tracks[1].artist)
        XCTAssertNil(starred.tracks[1].path)
    }

    /// Reading stars never writes, so it must not be treated as a step that
    /// cannot be replayed after a backend restart.
    func testReadingStarredTracksIsNotAMutatingOperation() {
        XCTAssertFalse(PhoneLibraryOperation.favoriteList.isMutating)
        XCTAssertTrue(PhoneLibraryOperation.favoriteApply.isMutating)
    }

    /// The config that travels over the wire must never gain a password field:
    /// the secret lives only in Keychain and moves through `credentials.save`.
    func testTheConfigModelCarriesNoSecret() throws {
        let config = NavidromeConfig(
            version: 1,
            enabled: true,
            server: NavidromeServerConfig(url: "http://localhost:4533", username: "jaa", port: 4533, address: "0.0.0.0"),
            paths: NavidromePathsConfig(
                musicDir: "/m", dataDir: "/d", cacheDir: "/c", logFile: "/l",
                configFile: "/f", backupDir: "/b", playlistsPath: ".udl/playlists"
            ),
            scan: NavidromeScanConfig(schedule: "@every 1h"),
            backup: NavidromeBackupConfig(schedule: "0 3 * * *", count: 7),
            playlists: NavidromePlaylistsConfig(hardBounceGenres: ["Hard Bounce"], appleHardBouncePlaylist: "HARD BOUNCE")
        )
        let encoded = try JSONEncoder.agent.encode(config)
        let text = String(decoding: encoded, as: UTF8.self).lowercased()
        for forbidden in ["password", "secret", "token"] {
            XCTAssertFalse(text.contains(forbidden), "the config wire form must not contain \(forbidden)")
        }
    }

    // MARK: Plan presentations

    /// Same rule as the Rekordbox plan: the checksummed value is kept verbatim
    /// and never re-encoded from a Swift model, because a round trip drops the
    /// fields Go omitted and breaks the checksum.
    func testSetupPlanKeepsTheSentValueVerbatim() {
        let value = JSONValue.object([
            "version": .string("1"),
            "checksum_sha256": .string("abcdef0123456789abcdef0123456789"),
            "binary_path": .string("/opt/homebrew/bin/navidrome"),
            "server_version": .string("0.63.2"),
            "music_dir": .string("/m"),
            "data_dir": .string("/d"),
            "port": .number(4533),
            "directories": .array([
                .object(["label": .string("data"), "path": .string("/d"), "exists": .bool(false)]),
                .object(["label": .string("cache"), "path": .string("/c"), "exists": .bool(true)]),
            ]),
            "files": .array([
                .object([
                    "label": .string("navidrome.toml"), "path": .string("/d/navidrome.toml"),
                    "action": .string("create"), "exists": .bool(false), "owned": .bool(false),
                    "content": .string("# udl-managed"),
                ]),
                .object([
                    "label": .string("LaunchAgent"), "path": .string("/a.plist"),
                    "action": .string("unchanged"), "exists": .bool(true), "owned": .bool(true),
                    "content": .string("<plist/>"),
                ]),
            ]),
            "service": .object([
                "label": .string("com.jaa.udl.navidrome"),
                "action": .string("load"),
                "running": .bool(false),
            ]),
            "blockers": .array([]),
            "warnings": .array([.string("no account username is configured yet")]),
            "unknown_future_field": .string("kept"),
        ])
        let plan = NavidromeSetupPlanPresentation(value)
        XCTAssertNotNil(plan)
        XCTAssertEqual(plan?.value, value, "apply must receive exactly what the backend sent")
        XCTAssertEqual(plan?.changedFiles.count, 1, "an unchanged file is not a change")
        XCTAssertEqual(plan?.newDirectories.count, 1, "an existing directory is not a change")
        XCTAssertEqual(plan?.changeCount, 3, "one file, one directory, one service action")
        XCTAssertTrue(plan?.applicable ?? false)
        XCTAssertEqual(plan?.warnings.count, 1)
    }

    func testSetupPlanWithoutAChecksumIsNotApplicable() {
        let plan = NavidromeSetupPlanPresentation(.object([
            "version": .string("1"),
            "blockers": .array([]),
        ]))
        XCTAssertEqual(plan?.checksum, "")
        XCTAssertFalse(plan?.applicable ?? true, "an unsigned plan must never look applicable")
    }

    func testSetupPlanWithBlockersIsNotApplicable() {
        let plan = NavidromeSetupPlanPresentation(.object([
            "checksum_sha256": .string("abc"),
            "blockers": .array([.string("port 4533 is already in use by PID 42")]),
        ]))
        XCTAssertFalse(plan?.applicable ?? true)
        XCTAssertEqual(plan?.blockers.first, "port 4533 is already in use by PID 42")
    }

    func testFavoritePlanClassifiesRowsAndKeepsTheValueVerbatim() {
        let value = JSONValue.object([
            "version": .string("1"),
            "checksum_sha256": .string("deadbeef"),
            "username": .string("jaa"),
            "music_dir": .string("/m"),
            "counts": .object([
                "source_total": .number(6),
                "matched": .number(2),
                "already_starred": .number(1),
                "outside_library": .number(1),
                "missing": .number(1),
                "ambiguous": .number(1),
                "metadata_only": .number(0),
                "server_starred": .number(1),
                "expected_starred": .number(3),
            ]),
            "rows": .array([
                .object(["index": .number(1), "status": .string("matched"), "title": .string("One")]),
                .object(["index": .number(2), "status": .string("already_starred"), "title": .string("Two")]),
                .object(["index": .number(3), "status": .string("missing"), "title": .string("Three")]),
                .object(["index": .number(4), "status": .string("ambiguous"), "title": .string("Four")]),
                .object(["index": .number(5), "status": .string("outside_library"), "title": .string("Five")]),
                .object(["index": .number(6), "status": .string("metadata_only"), "title": .string("Six")]),
            ]),
            "blockers": .array([]),
            "warnings": .array([]),
        ])
        let plan = NavidromeFavoritePlanPresentation(value)
        XCTAssertEqual(plan?.value, value)
        XCTAssertEqual(plan?.counts.matched, 2)
        // Every row apply will skip is named, so nothing is silently dropped.
        XCTAssertEqual(plan?.excludedRows.count, 4)
        XCTAssertTrue(plan?.applicable ?? false)
    }

    /// A metadata-only match is diagnostic. The screen must count it as skipped
    /// so it can never read as something apply will act on.
    func testMetadataOnlyRowsAreShownAsExcluded() {
        let row = NavidromeFavoriteRowView(.object([
            "index": .number(1),
            "status": .string("metadata_only"),
            "title": .string("Track"),
        ]))
        XCTAssertFalse(row?.isApplied ?? true)
        XCTAssertTrue(row?.isExcluded ?? false)
    }

    // MARK: Derived workflow state

    func testProgressMarksEveryCompletedStepAndNamesBlockers() {
        let status = makeStatus(
            dependencyReady: false,
            homebrewInstalled: false,
            serviceState: "not_installed",
            passwordStored: false,
            username: "",
            reachable: false,
            libraryTracks: 0,
            playlistCount: 0
        )
        let progress = PhoneLibraryProgress(status: status)
        XCTAssertEqual(progress.completed, 0)
        XCTAssertEqual(progress.total, 6)
        guard case .blocked(let reason)? = progress.steps.first?.state else {
            return XCTFail("a missing Homebrew must block the dependency step")
        }
        XCTAssertTrue(reason.contains("Homebrew"))
    }

    func testProgressAdvancesAsStateBecomesTrue() {
        let status = makeStatus(
            dependencyReady: true,
            homebrewInstalled: true,
            serviceState: "running",
            passwordStored: true,
            username: "jaa",
            reachable: true,
            libraryTracks: 1574,
            playlistCount: 3
        )
        let progress = PhoneLibraryProgress(status: status)
        // Everything except the phone connection, which no backend state can
        // observe and which is therefore never claimed as done.
        XCTAssertEqual(progress.completed, 5)
        XCTAssertEqual(progress.remaining, 1)
        XCTAssertEqual(progress.current?.id, "phone")
    }

    func testProgressBlocksOnAnUnownedLaunchAgent() {
        var status = makeStatus(
            dependencyReady: true, homebrewInstalled: true, serviceState: "stopped",
            passwordStored: true, username: "jaa", reachable: false,
            libraryTracks: 0, playlistCount: 0
        )
        status = NavidromeStatus(
            enabled: status.enabled, configPath: status.configPath, musicDir: status.musicDir,
            dataDir: status.dataDir, playlistsDir: status.playlistsDir, username: status.username,
            passwordStored: status.passwordStored, dependency: status.dependency,
            service: makeService(state: "stopped", owned: false),
            reachable: status.reachable, server: status.server, libraryTracks: status.libraryTracks,
            scanning: false, managedPlaylists: [], backupCount: 0, latestBackup: nil,
            logPath: nil, problems: []
        )
        let progress = PhoneLibraryProgress(status: status)
        guard case .blocked(let reason)? = progress.steps.first(where: { $0.id == "service" })?.state else {
            return XCTFail("an unowned LaunchAgent must block the service step")
        }
        XCTAssertTrue(reason.contains("does not manage"))
    }

    func testConnectionDetailsAreOnlyReadyWhenBothPartsExist() {
        let incomplete = PhoneConnectionDetails(status: makeStatus(
            dependencyReady: true, homebrewInstalled: true, serviceState: "stopped",
            passwordStored: false, username: "", reachable: false,
            libraryTracks: 0, playlistCount: 0
        ))
        XCTAssertFalse(incomplete.isReady)
        XCTAssertEqual(incomplete.approximateLibrarySize, "unknown")

        let ready = PhoneConnectionDetails(status: makeStatus(
            dependencyReady: true, homebrewInstalled: true, serviceState: "running",
            passwordStored: true, username: "jaa", reachable: true,
            libraryTracks: 1574, playlistCount: 3
        ))
        XCTAssertTrue(ready.isReady)
        XCTAssertEqual(ready.serverURL, "http://mac.local:4533")
        // ~9.4 GiB for the current library, stated as approximate.
        XCTAssertTrue(ready.approximateLibrarySize.hasSuffix("GiB"))
    }

    /// Install, setup apply, and favorite apply change the machine or the
    /// server. After a backend restart none of them may be replayed, so the
    /// operation has to say which it is.
    func testMutatingOperationsAreDistinguishedFromReadOnlyOnes() {
        XCTAssertTrue(PhoneLibraryOperation.installDependency.isMutating)
        XCTAssertTrue(PhoneLibraryOperation.setupApply.isMutating)
        XCTAssertTrue(PhoneLibraryOperation.favoriteApply.isMutating)
        XCTAssertTrue(PhoneLibraryOperation.serviceControl(action: "restart").isMutating)
        XCTAssertTrue(PhoneLibraryOperation.saveGenres.isMutating)
        XCTAssertFalse(PhoneLibraryOperation.setupPlan.isMutating)
        XCTAssertFalse(PhoneLibraryOperation.favoritePlan.isMutating)
        XCTAssertFalse(PhoneLibraryOperation.deriveGenres.isMutating)
    }

    func testServiceStatusMapsEveryStateToALabelAndSeverity() {
        XCTAssertEqual(makeService(state: "running", owned: true).severity, .ok)
        XCTAssertEqual(makeService(state: "stopped", owned: true).severity, .warn)
        XCTAssertEqual(makeService(state: "not_installed", owned: false).severity, .idle)
        XCTAssertEqual(makeService(state: "unknown", owned: false).severity, .error)
        XCTAssertEqual(makeService(state: "running", owned: true).label, "Running")
    }

    func testNavidromePasswordIsACredentialKindNoSyncSourceConsumes() {
        let source = SourceCapability(
            sourceID: "spotify-liked",
            sourceType: "spotify",
            adapter: "deemix",
            supportsPlan: true,
            supportsPlanWindow: true,
            supportsDownloadOrder: true,
            defaultPlanWindow: .first,
            defaultDownloadOrder: .oldestFirst
        )
        XCTAssertFalse(credentialApplies(.navidromePassword, to: source))
        XCTAssertEqual(
            credentialConsumers(.navidromePassword, sources: [source]),
            [],
            "the Used by column must say nothing rather than name a source that does not read it"
        )
    }

    // MARK: Fixtures

    private func makeService(state: String, owned: Bool) -> NavidromeServiceStatus {
        NavidromeServiceStatus(
            state: state,
            loaded: state == "running",
            pid: state == "running" ? 4242 : nil,
            lastExitStatus: nil,
            launchAgentPath: "/Users/jaa/Library/LaunchAgents/com.jaa.udl.navidrome.plist",
            owned: owned,
            localURL: "http://localhost:4533",
            lanURL: "http://mac.local:4533",
            hostname: "mac.local",
            logPath: "/d/navidrome.log",
            problems: []
        )
    }

    private func makeStatus(
        dependencyReady: Bool,
        homebrewInstalled: Bool,
        serviceState: String,
        passwordStored: Bool,
        username: String,
        reachable: Bool,
        libraryTracks: Int,
        playlistCount: Int
    ) -> NavidromeStatus {
        let playlists = (0..<playlistCount).map {
            NavidromePlaylistInfo(id: "pl-\($0)", name: "Playlist \($0)", owner: "jaa", trackCount: 10)
        }
        return NavidromeStatus(
            enabled: true,
            configPath: "/tmp/navidrome.yaml",
            musicDir: "/Users/jaa/Music/downloaded",
            dataDir: "/d",
            playlistsDir: "/Users/jaa/Music/downloaded/.udl/playlists",
            username: username,
            passwordStored: passwordStored,
            dependency: NavidromeDependencyStatus(
                homebrewInstalled: homebrewInstalled,
                homebrewPath: homebrewInstalled ? "/opt/homebrew/bin/brew" : nil,
                installed: dependencyReady,
                binaryPath: dependencyReady ? "/opt/homebrew/bin/navidrome" : nil,
                version: dependencyReady ? "0.63.2" : nil,
                minimumVersion: "0.63.2",
                versionSupported: dependencyReady,
                problems: []
            ),
            service: makeService(state: serviceState, owned: true),
            reachable: reachable,
            server: NavidromeServerInfo(type: "navidrome", version: "1.16.1", serverVersion: "0.63.2", openSubsonic: true),
            libraryTracks: libraryTracks,
            scanning: false,
            managedPlaylists: playlists,
            backupCount: 0,
            latestBackup: nil,
            logPath: "/d/navidrome.log",
            problems: []
        )
    }
}
