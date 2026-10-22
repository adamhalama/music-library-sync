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
          "phone_connected": false,
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

        let registration = try JSONDecoder.agent.decode(
            NavidromePlaylistRegistrationResult.self,
            from: #"{"added": null}"#.data(using: .utf8)!
        )
        XCTAssertEqual(registration.added, [])

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
            "playlists": {"hard_bounce_genres": null, "apple_hard_bounce_playlist": "HARD BOUNCE"},
            "phone": {"connected": false}
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

    func testNavidromeFavoritesHandoffUsesTheManagedSnapshotIdentity() {
        XCTAssertEqual(NavidromeStarredListResult.playlistID, "navidrome-favorites")
        XCTAssertEqual(NavidromeStarredListResult.playlistName, "Favourites (Navidrome)")

        let definition = PlaylistDefinition(
            id: NavidromeStarredListResult.playlistID,
            name: NavidromeStarredListResult.playlistName,
            provider: "navidrome",
            providerPlaylist: NavidromeStarredListResult.playlistName,
            providerPlaylistID: "starred",
            defaultFreeDLJob: nil,
            defaultRekordboxTarget: "nav_fav_imports"
        )
        XCTAssertEqual(definition.providerDisplayName, "Navidrome")

        let target = RekordboxPlanTarget.playlist(definition.id)
        XCTAssertEqual(target.id, "playlist:navidrome-favorites")
        XCTAssertEqual(RekordboxPlanTarget(id: target.id), target)
        XCTAssertEqual(target.playlistID, definition.id)
        XCTAssertNil(target.jobID)
        XCTAssertNil(target.mappingID)
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
            playlists: NavidromePlaylistsConfig(hardBounceGenres: ["Hard Bounce"], appleHardBouncePlaylist: "HARD BOUNCE"),
            phone: NavidromePhoneConfig(connected: true)
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
        // Everything except the phone connection, which nothing here can
        // observe: it stays current until the user acknowledges it.
        XCTAssertEqual(progress.completed, 5)
        XCTAssertEqual(progress.remaining, 1)
        XCTAssertEqual(progress.current?.id, "phone")
    }

    /// The last step is self-reported. Without a way to record it the checklist
    /// would read "5 of 6" forever, which is what this replaced.
    func testAcknowledgingThePhoneCompletesTheChecklist() {
        let status = makeStatus(
            dependencyReady: true,
            homebrewInstalled: true,
            serviceState: "running",
            passwordStored: true,
            username: "jaa",
            reachable: true,
            libraryTracks: 1574,
            playlistCount: 3,
            phoneConnected: true
        )
        let progress = PhoneLibraryProgress(status: status)
        XCTAssertEqual(progress.completed, progress.total)
        XCTAssertEqual(progress.remaining, 0)
        XCTAssertNil(progress.current)
        XCTAssertTrue(progress.steps.first { $0.id == "phone" }?.isDone ?? false)
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
            logPath: nil, phoneConnected: status.phoneConnected, problems: []
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
        XCTAssertTrue(PhoneLibraryOperation.registerPlaylists.isMutating)
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

    // MARK: Remote mode

    /// What `navidrome.status` sends in remote mode, including the nulls Go
    /// produces for empty slices and its nanosecond timestamps.
    static let remoteStatusJSON = """
    {
      "enabled": true,
      "music_dir": "/Users/jaa/Music/downloaded",
      "data_dir": "/Users/jaa/Library/Application Support/UDL/Navidrome",
      "playlists_dir": "/Users/jaa/Music/downloaded/.udl/playlists",
      "username": "jaa",
      "password_stored": true,
      "dependency": {"homebrew_installed": false, "installed": false, "minimum_version": "", "version_supported": false, "ffmpeg_installed": false, "problems": []},
      "service": {"state": "remote", "loaded": false, "owned": false, "problems": []},
      "reachable": true,
      "server": {"type": "navidrome", "server_version": "0.63.2"},
      "library_tracks": 1574,
      "scanning": false,
      "managed_playlists": null,
      "backup_count": 2,
      "latest_backup": {"path": "/srv/navidrome/backups/udl-20261022.db", "size_bytes": 1048576, "created_at": "2026-10-22T09:30:00.123456789Z"},
      "phone_connected": false,
      "problems": ["the server has not finished scanning the last push; the next `udl navidrome push` retries it"],
      "mode": "remote",
      "remote": {
        "ssh_target": "udl-mirror",
        "library": {"path": "/Users/jaa/Music/downloaded", "files": 1580, "bytes": 10093173555, "newest_mtime": "2026-10-22T08:00:00Z"},
        "database": {"path": "/data/navidrome.db", "present": true, "size_bytes": 52428800, "tracks": 1574, "libraries": null},
        "udl_backups": null,
        "navidrome_backups": [{"path": "/srv/navidrome/backups/navidrome_backup_x.db", "size_bytes": 9, "created_at": "2026-10-21T03:00:00Z"}],
        "disk": {"free_bytes": 400000000000, "total_bytes": 500000000000}
      },
      "mirror": {
        "last_push_at": "2026-10-22T10:00:00.123456789Z",
        "last_scan_at": "2026-10-22T10:01:00Z",
        "scan_pending": true,
        "a_future_field": 1
      },
      "export": {"last_export_at": "2026-10-20T11:00:00.5Z"}
    }
    """

    func testRemoteModeStatusDecodesServerAndMirrorState() throws {
        let status = try JSONDecoder.agent.decode(NavidromeStatus.self, from: Data(Self.remoteStatusJSON.utf8))
        XCTAssertTrue(status.isRemote)
        XCTAssertEqual(status.managedPlaylists, [])
        XCTAssertEqual(status.remote?.sshTarget, "udl-mirror")
        XCTAssertEqual(status.remote?.library?.files, 1580)
        XCTAssertEqual(status.remote?.library?.bytes, 10_093_173_555)
        XCTAssertEqual(status.remote?.library?.newestMtime, "2026-10-22T08:00:00Z")
        XCTAssertEqual(status.remote?.database?.tracks, 1574)
        XCTAssertEqual(status.remote?.database?.present, true)
        XCTAssertNil(status.remote?.database?.error)
        // Go's nil slice arrives as null and must read as empty.
        XCTAssertEqual(status.remote?.udlBackups, [])
        XCTAssertEqual(status.remote?.navidromeBackups.count, 1)
        XCTAssertEqual(status.remote?.disk?.freeBytes, 400_000_000_000)
        // Timestamps stay verbatim; unknown mirror fields are ignored.
        XCTAssertEqual(status.mirror?.lastPushAt, "2026-10-22T10:00:00.123456789Z")
        XCTAssertEqual(status.mirror?.scanPending, true)
        XCTAssertNil(status.mirror?.datesPending)
        XCTAssertNil(status.mirror?.lastPushError)
        XCTAssertEqual(status.serverDBExport?.lastExportAt, "2026-10-20T11:00:00.5Z")
        XCTAssertNil(status.serverDBExport?.lastExportError)
    }

    /// A backend that predates remote mode — or a local install — sends no
    /// mode, remote, or mirror. That must decode and read as local.
    func testAStatusWithoutRemoteFieldsDecodesAsLocal() throws {
        let payload = """
        {
          "enabled": true, "music_dir": "/m", "data_dir": "/d", "playlists_dir": "/p",
          "password_stored": false,
          "dependency": {"homebrew_installed": true, "installed": true, "minimum_version": "0.63.2", "version_supported": true, "problems": null},
          "service": {"state": "running", "loaded": true, "owned": true, "problems": null},
          "reachable": false, "server": {}, "library_tracks": 0, "scanning": false,
          "managed_playlists": null, "backup_count": 0, "phone_connected": false, "problems": null
        }
        """
        let status = try JSONDecoder.agent.decode(NavidromeStatus.self, from: Data(payload.utf8))
        XCTAssertNil(status.mode)
        XCTAssertNil(status.remote)
        XCTAssertNil(status.mirror)
        XCTAssertNil(status.serverDBExport)
        XCTAssertFalse(status.isRemote)
    }

    /// The server could not be asked: remote mode with no `remote` object, and
    /// a gate reply missing most of its numbers. Neither may fail decoding.
    func testARemoteStatusWithoutServerDetailsStillDecodes() throws {
        let unreachable = """
        {
          "enabled": true, "music_dir": "/m", "data_dir": "/d", "playlists_dir": "/p",
          "password_stored": true,
          "dependency": {"homebrew_installed": false, "installed": false, "minimum_version": "", "version_supported": false, "problems": []},
          "service": {"state": "remote", "loaded": false, "owned": false, "problems": []},
          "reachable": false, "server": {}, "library_tracks": 0, "scanning": false,
          "managed_playlists": null, "backup_count": 0, "phone_connected": false,
          "problems": ["the mirror server is not reachable over SSH (udl-mirror): timeout"],
          "mode": "remote", "mirror": {}
        }
        """
        let status = try JSONDecoder.agent.decode(NavidromeStatus.self, from: Data(unreachable.utf8))
        XCTAssertTrue(status.isRemote)
        XCTAssertNil(status.remote)
        XCTAssertNil(status.mirror?.lastPushAt)

        let sparse = try JSONDecoder.agent.decode(
            NavidromeRemoteStatus.self,
            from: Data(#"{"library": {}, "database": {"present": false}, "udl_backups": null}"#.utf8)
        )
        XCTAssertNil(sparse.sshTarget)
        XCTAssertNil(sparse.library?.files)
        XCTAssertEqual(sparse.udlBackups, [])
        XCTAssertEqual(sparse.navidromeBackups, [], "an absent collection reads as empty too")
    }

    func testRemoteSummaryStatesWhatThePhoneUsesAndHowCurrentTheMirrorIs() throws {
        let status = try JSONDecoder.agent.decode(NavidromeStatus.self, from: Data(Self.remoteStatusJSON.utf8))
        let now = try XCTUnwrap(PhoneLibraryFormat.date("2026-10-22T10:05:30Z"))
        let utc = try XCTUnwrap(TimeZone(identifier: "UTC"))
        let summary = PhoneLibraryRemoteSummary(
            status: status, configuredServerURL: "https://music-server.example.ts.net:4533", now: now, timeZone: utc
        )
        XCTAssertEqual(summary.serverURL, "https://music-server.example.ts.net:4533")
        XCTAssertEqual(summary.sshTarget, "udl-mirror")
        XCTAssertEqual(summary.lastPush, "5 minutes ago (2026-10-22 10:00)")
        XCTAssertEqual(summary.lastScan, "4 minutes ago (2026-10-22 10:01)")
        XCTAssertNil(summary.lastPushError)
        XCTAssertEqual(summary.serverLibrary, "1580 files · 9.4 GiB")
        XCTAssertEqual(summary.serverDatabase, "1574 tracks")
        XCTAssertTrue(summary.databaseHealthy)
        XCTAssertEqual(summary.latestServerBackup, "/srv/navidrome/backups/udl-20261022.db · 35 minutes ago (2026-10-22 09:30)")
        XCTAssertEqual(summary.lastServerDBCopy, "1 day ago (2026-10-20 11:00)")
        XCTAssertNil(summary.lastServerDBCopyError)
        XCTAssertEqual(summary.problems.count, 1)

        let never = PhoneLibraryRemoteSummary(status: nil, configuredServerURL: nil, now: now, timeZone: utc)
        XCTAssertEqual(never.lastPush, "never")
        XCTAssertEqual(never.serverURL, "")
        XCTAssertEqual(never.latestServerBackup, "none yet")
        XCTAssertFalse(never.databaseHealthy)
    }

    /// The weekly off-server copy of the server's database: when it last
    /// worked, or why the last one failed. A backend that predates the
    /// export reports nothing, which is not the same as "never".
    func testRemoteSummaryShowsTheLastServerDatabaseCopy() throws {
        let now = try XCTUnwrap(PhoneLibraryFormat.date("2026-10-22T10:05:30Z"))
        let utc = try XCTUnwrap(TimeZone(identifier: "UTC"))
        var status = try JSONDecoder.agent.decode(NavidromeStatus.self, from: Data(Self.remoteStatusJSON.utf8))
        func summary() -> PhoneLibraryRemoteSummary {
            PhoneLibraryRemoteSummary(status: status, configuredServerURL: nil, now: now, timeZone: utc)
        }

        status.serverDBExport = try JSONDecoder.agent.decode(NavidromeServerDBExportState.self, from: Data("""
        {"last_export_at": "2026-10-11T11:00:00Z", "last_export_error": "the mirror server is not reachable over SSH (udl-mirror): timeout",
         "last_export_error_at": "2026-10-22T10:00:00Z"}
        """.utf8))
        XCTAssertEqual(summary().lastServerDBCopy, "10 days ago (2026-10-11 11:00)")
        XCTAssertEqual(summary().lastServerDBCopyError,
                       "Last copy failed 5 minutes ago (2026-10-22 10:00): the mirror server is not reachable over SSH (udl-mirror): timeout")

        status.serverDBExport = try JSONDecoder.agent.decode(NavidromeServerDBExportState.self, from: Data("{}".utf8))
        XCTAssertEqual(summary().lastServerDBCopy, "never")
        XCTAssertNil(summary().lastServerDBCopyError)

        status.serverDBExport = nil
        XCTAssertEqual(summary().lastServerDBCopy, "not reported by this backend")
    }

    func testFormatParsesGoTimestampsAndKeepsWhatItCannotParse() throws {
        let utc = try XCTUnwrap(TimeZone(identifier: "UTC"))
        let nanos = try XCTUnwrap(PhoneLibraryFormat.date("2026-10-22T10:00:00.123456789Z"))
        XCTAssertEqual(PhoneLibraryFormat.absolute(nanos, timeZone: utc), "2026-10-22 10:00")
        XCTAssertNotNil(PhoneLibraryFormat.date("2026-10-22T12:00:00+02:00"))
        XCTAssertEqual(PhoneLibraryFormat.moment("yesterday-ish"), "yesterday-ish")
        XCTAssertEqual(PhoneLibraryFormat.moment(nil), "never")
        XCTAssertEqual(PhoneLibraryFormat.relative(nanos, now: nanos.addingTimeInterval(-30)), "just now")
        XCTAssertEqual(PhoneLibraryFormat.relative(nanos, now: nanos.addingTimeInterval(3 * 86_400 + 5)), "3 days ago")
        XCTAssertEqual(PhoneLibraryFormat.bytes(nil), "unknown size")
        XCTAssertEqual(PhoneLibraryFormat.bytes(5 << 20), "5 MiB")
    }

    /// Remote mode has nothing to install or start on this Mac; the checklist
    /// asks for a successful push instead, so the badge can reach zero.
    func testRemoteProgressReplacesTheLocalInstallSteps() {
        let local = makeStatus(
            dependencyReady: false, homebrewInstalled: false, serviceState: "remote",
            passwordStored: true, username: "jaa", reachable: true,
            libraryTracks: 1574, playlistCount: 3, phoneConnected: true
        )
        func remote(_ mirror: NavidromeMirrorState?) -> NavidromeStatus {
            var status = local
            status.mode = "remote"
            status.mirror = mirror
            return status
        }
        let neverPushed = PhoneLibraryProgress(status: remote(nil))
        XCTAssertEqual(neverPushed.steps.map(\.id), ["mirror", "account", "library", "playlists", "phone"])
        XCTAssertEqual(neverPushed.current?.id, "mirror")

        let pushed = PhoneLibraryProgress(status: remote(NavidromeMirrorState(
            lastPushAt: "2026-10-22T10:00:00Z", lastScanAt: nil, lastErrorAt: nil,
            lastPushError: nil, scanPending: nil, datesPending: nil
        )))
        XCTAssertEqual(pushed.remaining, 0)

        let failed = PhoneLibraryProgress(status: remote(NavidromeMirrorState(
            lastPushAt: "2026-10-22T10:00:00Z", lastScanAt: nil, lastErrorAt: "2026-10-22T11:00:00Z",
            lastPushError: "mirror push failed (rsync exit 12)", scanPending: nil, datesPending: nil
        )))
        guard case .blocked(let reason)? = failed.steps.first?.state else {
            return XCTFail("a failed last push must block the mirror step")
        }
        XCTAssertTrue(reason.contains("last push failed"))

        // Files reached the server but its scan (or the Date Added step after
        // it) has not completed: the mirror is not current yet.
        for (scanPending, datesPending) in [(true, false), (false, true)] {
            let owed = PhoneLibraryProgress(status: remote(NavidromeMirrorState(
                lastPushAt: "2026-10-22T10:00:00Z", lastScanAt: nil, lastErrorAt: nil,
                lastPushError: nil, scanPending: scanPending, datesPending: datesPending
            )))
            XCTAssertFalse(owed.steps.first?.isDone ?? true, "scan_pending=\(scanPending) dates_pending=\(datesPending)")
            XCTAssertEqual(owed.current?.id, "mirror")
        }
    }

    func testRemoteConnectionUsesTheConfiguredServerURL() {
        var status = makeStatus(
            dependencyReady: false, homebrewInstalled: false, serviceState: "remote",
            passwordStored: true, username: "jaa", reachable: true,
            libraryTracks: 10, playlistCount: 3
        )
        // A local status ignores the configured URL and keeps the LAN address.
        XCTAssertEqual(
            PhoneConnectionDetails(status: status, configuredServerURL: "https://server:4533").serverURL,
            "http://mac.local:4533"
        )
        status.mode = "remote"
        let remote = PhoneConnectionDetails(status: status, configuredServerURL: "https://server:4533")
        XCTAssertEqual(remote.serverURL, "https://server:4533")
        XCTAssertTrue(remote.isReady)
        XCTAssertFalse(PhoneConnectionDetails(status: status).isReady, "remote mode without server.url has no address")
    }

    func testMirrorPushIsAMutatingOperation() {
        XCTAssertTrue(PhoneLibraryOperation.mirrorPush.isMutating)
        XCTAssertFalse(PhoneLibraryOperation.mirrorPush.label.isEmpty)
    }

    func testMirrorPushParamsNeverRaiseTheDeleteLimit() throws {
        let encoded = try JSONEncoder.agent.encode(NavidromeMirrorPushParams(forceScan: false))
        XCTAssertEqual(String(decoding: encoded, as: UTF8.self), #"{"force_scan":false}"#)
    }

    // MARK: Push outcomes

    private func decodePush(_ json: String) throws -> NavidromeMirrorPushResult {
        try JSONDecoder.agent.decode(NavidromeMirrorPushResult.self, from: Data(json.utf8))
    }

    func testASuccessfulPushReportsCountsAndScan() throws {
        let result = try decodePush("""
        {
          "push": {"command": "rsync …", "started_at": "2026-10-22T10:00:00Z", "finished_at": "2026-10-22T10:00:05Z",
                   "changed": 3, "deleted": 1, "items": null},
          "scanned": true,
          "dates": {"database_path": "udl-mirror:navidrome.db", "candidates": 3, "updated": 3,
                    "message": "Date Added now matches file creation time for 3 track(s)"},
          "warnings": null,
          "mirror": {"last_push_at": "2026-10-22T10:00:05Z", "last_scan_at": "2026-10-22T10:01:00Z"}
        }
        """)
        XCTAssertEqual(result.push?.items, [])
        XCTAssertEqual(result.warnings, [])
        XCTAssertEqual(result.mirror?.lastScanAt, "2026-10-22T10:01:00Z")
        let outcome = NavidromeMirrorPushOutcome(exitCode: 0, error: nil, result: result)
        XCTAssertEqual(outcome.kind, .pushed)
        XCTAssertEqual(outcome.severity, .ok)
        XCTAssertTrue(outcome.message.contains("3 changed, 1 deleted"))
        XCTAssertTrue(outcome.message.contains("rescanned"))
        XCTAssertTrue(outcome.message.contains("Date Added now matches"))
        XCTAssertNil(outcome.plannedDeletes)
    }

    func testAPushWithWarningsIsAWarningNotASuccess() throws {
        let result = try decodePush("""
        {"push": {"changed": 0, "deleted": 0}, "scanned": false,
         "warnings": ["pushed, but no scan was requested: the Navidrome API is not available"]}
        """)
        let outcome = NavidromeMirrorPushOutcome(exitCode: 0, error: nil, result: result)
        XCTAssertEqual(outcome.kind, .pushed)
        XCTAssertEqual(outcome.severity, .warn)
        XCTAssertTrue(outcome.message.contains("no scan was requested"))
        // Without the recorded mirror state, nothing says no scan was owed.
        XCTAssertFalse(outcome.message.contains("no scan was needed"))
    }

    /// What the push says about its scan comes from the recorded pending
    /// flags and `scanned`, never from the transfer counts alone.
    func testAPushDescribesItsScanFromThePendingFlags() throws {
        // Nothing new, but the scan an earlier push owed ran now.
        let retried = NavidromeMirrorPushOutcome(exitCode: 0, error: nil, result: try decodePush("""
        {"push": {"changed": 0, "deleted": 0}, "scanned": true, "warnings": [],
         "mirror": {"last_push_at": "2026-10-22T10:00:05Z", "last_scan_at": "2026-10-22T10:01:00Z"}}
        """))
        XCTAssertEqual(retried.severity, .ok)
        XCTAssertTrue(retried.message.contains("still owed"), retried.message)
        XCTAssertFalse(retried.message.contains("no scan was needed"))

        // Nothing new, and the owed scan timed out again.
        let stillOwed = NavidromeMirrorPushOutcome(exitCode: 0, error: nil, result: try decodePush("""
        {"push": {"changed": 0, "deleted": 0}, "scanned": false,
         "warnings": ["pushed, but the scan did not complete: wait for the running Navidrome scan: context deadline exceeded"],
         "mirror": {"last_push_at": "2026-10-22T10:00:05Z", "scan_pending": true}}
        """))
        XCTAssertEqual(stillOwed.severity, .warn)
        XCTAssertFalse(stillOwed.message.contains("no scan was needed"), stillOwed.message)
        XCTAssertTrue(stillOwed.message.contains("not finished scanning"), stillOwed.message)

        // The scan ran but Date Added did not.
        let datesOwed = NavidromeMirrorPushOutcome(exitCode: 0, error: nil, result: try decodePush("""
        {"push": {"changed": 2, "deleted": 0}, "scanned": true,
         "warnings": ["Date Added was not reconciled: udl-mirror apply-dates: database is locked"],
         "mirror": {"last_push_at": "2026-10-22T10:00:05Z", "last_scan_at": "2026-10-22T10:01:00Z", "dates_pending": true}}
        """))
        XCTAssertEqual(datesOwed.severity, .warn)
        XCTAssertTrue(datesOwed.message.contains("rescanned"))
        XCTAssertTrue(datesOwed.message.contains("Date Added was not re-applied yet"), datesOwed.message)

        // A genuine no-op.
        let noop = NavidromeMirrorPushOutcome(exitCode: 0, error: nil, result: try decodePush("""
        {"push": {"changed": 0, "deleted": 0}, "scanned": false, "warnings": [],
         "mirror": {"last_push_at": "2026-10-22T10:00:05Z"}}
        """))
        XCTAssertEqual(noop.severity, .ok)
        XCTAssertTrue(noop.message.contains("no scan was needed"))
    }

    /// The refusal names the planned count and the CLI override; there is no
    /// one-click way past it.
    func testADeleteLimitRefusalCarriesTheCountAndTheCLIOverride() throws {
        let error = "push refused by the delete limit: it would delete 300 files from the server (limit 250), so nothing was pushed. … run `udl navidrome push --allow-deletes 300`"
        let result = try decodePush("""
        {
          "push": {"changed": 0, "deleted": 0, "planned_deletes": 300},
          "scanned": false, "warnings": [],
          "mirror": {"last_push_error": "push refused by the delete limit", "last_error_at": "2026-10-22T10:00:00Z"},
          "failure": {"kind": "delete_limit", "detail": "\(error)", "planned_deletes": 300,
                      "allow_deletes_command": "udl navidrome push --allow-deletes 300"}
        }
        """)
        let outcome = NavidromeMirrorPushOutcome(exitCode: 1, error: error, result: result)
        XCTAssertEqual(outcome.kind, .deleteLimit(plannedDeletes: 300, command: "udl navidrome push --allow-deletes 300"))
        XCTAssertEqual(outcome.plannedDeletes, 300)
        XCTAssertEqual(outcome.severity, .error)
        XCTAssertTrue(outcome.message.contains("300 file(s)"))
        XCTAssertTrue(outcome.message.contains("`udl navidrome push --allow-deletes 300`"))
        XCTAssertTrue(outcome.message.contains("Nothing was changed"))
    }

    func testBusyCanceledAndFailedPushesMapToDistinctOutcomes() throws {
        let busy = NavidromeMirrorPushOutcome(exitCode: 1, error: "A push is already running", result: try decodePush("""
        {"push": {"changed": 0, "deleted": 0}, "scanned": false, "warnings": [],
         "failure": {"kind": "busy", "detail": "A push is already running (a sync's post-sync push…); this one was skipped and nothing changed."}}
        """))
        XCTAssertEqual(busy.kind, .busy)
        XCTAssertEqual(busy.severity, .warn)
        XCTAssertTrue(busy.message.contains("already running"))

        let canceled = NavidromeMirrorPushOutcome(exitCode: 130, error: "mirror preflight did not finish: context canceled", result: try decodePush("""
        {"push": {"changed": 0, "deleted": 0}, "scanned": false, "warnings": [],
         "failure": {"kind": "canceled", "detail": "mirror preflight did not finish: context canceled"}}
        """))
        XCTAssertEqual(canceled.kind, .canceled)
        XCTAssertEqual(canceled.severity, .warn)

        let failed = NavidromeMirrorPushOutcome(exitCode: 1, error: "mirror push failed (rsync exit 12): protocol", result: try decodePush("""
        {"push": {"changed": 0, "deleted": 0}, "scanned": false, "warnings": [],
         "failure": {"kind": "failed", "detail": "mirror push failed (rsync exit 12): protocol"}}
        """))
        XCTAssertEqual(failed.kind, .failed)
        XCTAssertEqual(failed.severity, .error)
        XCTAssertTrue(failed.message.contains("rsync exit 12"))
        XCTAssertTrue(failed.message.contains("push again"))

        let deferred = NavidromeMirrorPushOutcome(exitCode: 1, error: "the server is scanning", result: try decodePush("""
        {"push": {"changed": 0, "deleted": 0}, "scanned": false, "warnings": [],
         "failure": {"kind": "server_scanning", "detail": "the server is scanning or its scan state is unknown; the push was deferred and nothing was changed: Navidrome was still scanning after 5m0s; the next push retries"}}
        """))
        XCTAssertEqual(deferred.kind, .serverScanning)
        XCTAssertEqual(deferred.severity, .warn)
        XCTAssertTrue(deferred.message.contains("deferred"))
        XCTAssertTrue(deferred.message.contains("Nothing was changed"))
        XCTAssertTrue(deferred.message.contains("still scanning after 5m0s"))

        // A kind this build does not know reads as a plain failure.
        let unknown = NavidromeMirrorPushOutcome(exitCode: 1, error: "x", result: try decodePush("""
        {"push": {"changed": 0, "deleted": 0}, "scanned": false, "warnings": [],
         "failure": {"kind": "some_future_kind", "detail": "something new"}}
        """))
        XCTAssertEqual(unknown.kind, .failed)
        XCTAssertTrue(unknown.message.contains("something new"))

        // A terminal frame without a decodable result still says what failed.
        let bare = NavidromeMirrorPushOutcome(exitCode: 1, error: "run result exceeds frame limit", result: nil)
        XCTAssertEqual(bare.kind, .failed)
        XCTAssertTrue(bare.message.contains("run result exceeds frame limit"))
        let bareCancel = NavidromeMirrorPushOutcome(exitCode: 130, error: nil, result: nil)
        XCTAssertEqual(bareCancel.kind, .canceled)
    }

    /// rsync is not transactional across the tree: a failed or canceled push
    /// may have changed part of the mirror, so the app says so and asks for
    /// another push instead of promising the previous mirror.
    func testAnInterruptedPushSaysItIsIncomplete() throws {
        let partialFailure = NavidromeMirrorPushOutcome(exitCode: 1, error: "mirror push did not finish: signal: killed", result: try decodePush("""
        {"push": {"changed": 5, "deleted": 2}, "scanned": false, "warnings": [],
         "mirror": {"last_push_error": "mirror push did not finish: signal: killed", "scan_pending": true},
         "failure": {"kind": "failed", "detail": "mirror push did not finish: signal: killed"}}
        """))
        XCTAssertEqual(partialFailure.kind, .failed)
        for text in ["did not complete", "partly updated", "push again", "signal: killed"] {
            XCTAssertTrue(partialFailure.message.contains(text), "\(text): \(partialFailure.message)")
        }
        XCTAssertFalse(partialFailure.message.contains("previous mirror"))

        // Canceled mid-transfer: nothing itemized arrived, but the recorded
        // owed scan says the transfer had started. The detail is kept.
        let partialCancel = NavidromeMirrorPushOutcome(exitCode: 130, error: "mirror push did not finish: context canceled", result: try decodePush("""
        {"push": {"changed": 0, "deleted": 0}, "scanned": false, "warnings": [],
         "mirror": {"scan_pending": true},
         "failure": {"kind": "canceled", "detail": "mirror push did not finish: context canceled"}}
        """))
        XCTAssertEqual(partialCancel.kind, .canceled)
        for text in ["canceled", "partly updated", "push again", "mirror push did not finish: context canceled"] {
            XCTAssertTrue(partialCancel.message.contains(text), "\(text): \(partialCancel.message)")
        }
        XCTAssertFalse(partialCancel.message.contains("previous mirror"))

        // Canceled in the preflight: the recorded state shows nothing owed.
        let early = NavidromeMirrorPushOutcome(exitCode: 130, error: nil, result: try decodePush("""
        {"push": {"changed": 0, "deleted": 0}, "scanned": false, "warnings": [],
         "mirror": {"last_push_error": "mirror preflight did not finish: context canceled"},
         "failure": {"kind": "canceled", "detail": "mirror preflight did not finish: context canceled"}}
        """))
        XCTAssertTrue(early.message.contains("nothing changed on the server"), early.message)
        XCTAssertTrue(early.message.contains("mirror preflight did not finish"))

        // Without a result nothing is known, so the cautious wording applies.
        let bare = NavidromeMirrorPushOutcome(exitCode: 130, error: nil, result: nil)
        XCTAssertTrue(bare.message.contains("partly updated"), bare.message)
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
        playlistCount: Int,
        phoneConnected: Bool = false
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
            phoneConnected: phoneConnected,
            problems: []
        )
    }
}
