import Foundation

// MARK: - Configuration

struct NavidromeServerConfig: Codable, Sendable, Equatable {
    var url: String
    var username: String
    var port: Int
    var address: String
}

struct NavidromePathsConfig: Codable, Sendable, Equatable {
    var musicDir: String
    var dataDir: String
    var cacheDir: String
    var logFile: String
    var configFile: String
    var backupDir: String
    var playlistsPath: String

    enum CodingKeys: String, CodingKey {
        case musicDir = "music_dir"
        case dataDir = "data_dir"
        case cacheDir = "cache_dir"
        case logFile = "log_file"
        case configFile = "config_file"
        case backupDir = "backup_dir"
        case playlistsPath = "playlists_path"
    }
}

struct NavidromeScanConfig: Codable, Sendable, Equatable {
    var schedule: String
}

struct NavidromeBackupConfig: Codable, Sendable, Equatable {
    var schedule: String
    var count: Int
}

struct NavidromePlaylistsConfig: Codable, Sendable, Equatable {
    @DefaultEmpty var hardBounceGenres: [String]
    var appleHardBouncePlaylist: String

    enum CodingKeys: String, CodingKey {
        case hardBounceGenres = "hard_bounce_genres"
        case appleHardBouncePlaylist = "apple_hard_bounce_playlist"
    }
}

/// The phone-side setup step, which nothing on this Mac can observe. The phone
/// app does not announce itself through any API UDL reads, so this is the user's
/// own acknowledgement rather than a probe.
struct NavidromePhoneConfig: Codable, Sendable, Equatable {
    var connected: Bool
}

/// The Navidrome feature config. There is deliberately no password field: the
/// account password lives only in macOS Keychain and travels through
/// `credentials.save`.
struct NavidromeConfig: Codable, Sendable, Equatable {
    var version: Int
    var enabled: Bool
    var server: NavidromeServerConfig
    var paths: NavidromePathsConfig
    var scan: NavidromeScanConfig
    var backup: NavidromeBackupConfig
    var playlists: NavidromePlaylistsConfig
    var phone: NavidromePhoneConfig
}

struct NavidromeConfigResult: Codable, Sendable {
    let path: String
    let config: NavidromeConfig
    let content: String
}

struct NavidromeConfigWriteParams: Codable, Sendable { let config: NavidromeConfig }

// MARK: - Dependency and service

struct NavidromeDependencyStatus: Codable, Sendable, Equatable {
    let homebrewInstalled: Bool
    let homebrewPath: String?
    let installed: Bool
    let binaryPath: String?
    let version: String?
    let minimumVersion: String
    let versionSupported: Bool
    @DefaultEmpty var problems: [String]

    enum CodingKeys: String, CodingKey {
        case homebrewInstalled = "homebrew_installed"
        case homebrewPath = "homebrew_path"
        case installed
        case binaryPath = "binary_path"
        case version
        case minimumVersion = "minimum_version"
        case versionSupported = "version_supported"
        case problems
    }

    var ready: Bool { installed && versionSupported }
}

struct NavidromeServiceStatus: Codable, Sendable, Equatable {
    let state: String
    let loaded: Bool
    let pid: Int?
    let lastExitStatus: Int?
    let launchAgentPath: String?
    let owned: Bool
    let localURL: String?
    let lanURL: String?
    let hostname: String?
    let logPath: String?
    @DefaultEmpty var problems: [String]

    enum CodingKeys: String, CodingKey {
        case state, loaded, pid, owned, hostname
        case lastExitStatus = "last_exit_status"
        case launchAgentPath = "launch_agent_path"
        case localURL = "local_url"
        case lanURL = "lan_url"
        case logPath = "log_path"
        case problems
    }

    var isRunning: Bool { state == "running" }

    var label: String {
        switch state {
        case "running": "Running"
        case "stopped": "Stopped"
        case "not_installed": "Not installed"
        default: "Unknown"
        }
    }

    var severity: Severity {
        switch state {
        case "running": .ok
        case "stopped": .warn
        case "not_installed": .idle
        default: .error
        }
    }
}

struct NavidromeServerInfo: Codable, Sendable, Equatable {
    let type: String?
    let version: String?
    let serverVersion: String?
    let openSubsonic: Bool?

    enum CodingKeys: String, CodingKey {
        case type, version
        case serverVersion = "server_version"
        case openSubsonic = "open_subsonic"
    }
}

struct NavidromePlaylistInfo: Codable, Sendable, Equatable, Identifiable {
    let id: String
    let name: String
    let owner: String?
    let trackCount: Int

    enum CodingKeys: String, CodingKey {
        case id, name, owner
        case trackCount = "track_count"
    }
}

struct NavidromeBackupInfo: Codable, Sendable, Equatable {
    let path: String
    let sizeBytes: Int64
    let createdAt: String?

    enum CodingKeys: String, CodingKey {
        case path
        case sizeBytes = "size_bytes"
        case createdAt = "created_at"
    }
}

struct NavidromeStatus: Codable, Sendable, Equatable {
    let enabled: Bool
    let configPath: String?
    let musicDir: String
    let dataDir: String
    let playlistsDir: String
    let username: String?
    let passwordStored: Bool
    let dependency: NavidromeDependencyStatus
    let service: NavidromeServiceStatus
    let reachable: Bool
    let server: NavidromeServerInfo
    let libraryTracks: Int
    let scanning: Bool
    @DefaultEmpty var managedPlaylists: [NavidromePlaylistInfo]
    let backupCount: Int
    let latestBackup: NavidromeBackupInfo?
    let logPath: String?
    /// Self-reported, from the config. Not an observation of the phone.
    let phoneConnected: Bool
    @DefaultEmpty var problems: [String]
    /// `"remote"` when Navidrome runs on another machine and this Mac pushes
    /// a mirror to it. Absent from local-mode statuses and from backends that
    /// predate remote mode, which both mean local. These are `var` so the
    /// memberwise initialiser defaults them to `nil` at existing call sites.
    var mode: String?
    /// The server side of the mirror, absent when the server could not be
    /// asked (the reason is then one of `problems`).
    var remote: NavidromeRemoteStatus?
    /// What this Mac recorded about its pushes.
    var mirror: NavidromeMirrorState?
    /// The weekly off-server copy of the server's database (`udl navidrome
    /// export-db`). Absent from backends that predate it.
    var serverDBExport: NavidromeServerDBExportState?

    enum CodingKeys: String, CodingKey {
        case enabled, reachable, server, scanning, username
        case configPath = "config_path"
        case musicDir = "music_dir"
        case dataDir = "data_dir"
        case playlistsDir = "playlists_dir"
        case passwordStored = "password_stored"
        case dependency, service
        case libraryTracks = "library_tracks"
        case managedPlaylists = "managed_playlists"
        case backupCount = "backup_count"
        case latestBackup = "latest_backup"
        case logPath = "log_path"
        case phoneConnected = "phone_connected"
        case problems
        case mode, remote, mirror
        case serverDBExport = "export"
    }

    /// In remote mode the dependency and service fields describe nothing on
    /// this Mac, and the local-only controls do not apply.
    var isRemote: Bool { mode == "remote" }
}

// MARK: - Remote mirror

/// The mirror on the server, as `udl-mirror status` reports it. Every field is
/// optional: the gate's reply is passed through, and a missing number must
/// read as "unknown" rather than fail the whole status.
struct NavidromeRemoteStatus: Codable, Sendable, Equatable {
    let sshTarget: String?
    let library: NavidromeRemoteLibrary?
    let database: NavidromeRemoteDatabase?
    @DefaultEmpty var udlBackups: [NavidromeBackupInfo]
    @DefaultEmpty var navidromeBackups: [NavidromeBackupInfo]
    let disk: NavidromeRemoteDisk?

    enum CodingKeys: String, CodingKey {
        case sshTarget = "ssh_target"
        case library, database, disk
        case udlBackups = "udl_backups"
        case navidromeBackups = "navidrome_backups"
    }
}

struct NavidromeRemoteLibrary: Codable, Sendable, Equatable {
    let path: String?
    let files: Int?
    let bytes: Int64?
    /// Raw RFC 3339 text; parsed only for display.
    let newestMtime: String?

    enum CodingKeys: String, CodingKey {
        case path, files, bytes
        case newestMtime = "newest_mtime"
    }
}

struct NavidromeRemoteDatabase: Codable, Sendable, Equatable {
    let path: String?
    let present: Bool?
    let sizeBytes: Int64?
    let tracks: Int?
    let error: String?

    enum CodingKeys: String, CodingKey {
        case path, present, tracks, error
        case sizeBytes = "size_bytes"
    }
}

struct NavidromeRemoteDisk: Codable, Sendable, Equatable {
    let freeBytes: Int64?
    let totalBytes: Int64?

    enum CodingKeys: String, CodingKey {
        case freeBytes = "free_bytes"
        case totalBytes = "total_bytes"
    }
}

/// What this Mac knows about the mirror between pushes. Timestamps stay raw
/// RFC 3339 strings, like every other display-only time in this feature, so a
/// format this build cannot parse degrades one row instead of the status.
struct NavidromeMirrorState: Codable, Sendable, Equatable {
    let lastPushAt: String?
    let lastScanAt: String?
    let lastErrorAt: String?
    let lastPushError: String?
    let scanPending: Bool?
    let datesPending: Bool?

    enum CodingKeys: String, CodingKey {
        case lastPushAt = "last_push_at"
        case lastScanAt = "last_scan_at"
        case lastErrorAt = "last_error_at"
        case lastPushError = "last_push_error"
        case scanPending = "scan_pending"
        case datesPending = "dates_pending"
    }
}

/// What this Mac recorded about pulling copies of the server's database.
/// Raw timestamps, like the mirror state.
struct NavidromeServerDBExportState: Codable, Sendable, Equatable {
    let lastExportAt: String?
    let lastExportError: String?
    let lastExportErrorAt: String?

    enum CodingKeys: String, CodingKey {
        case lastExportAt = "last_export_at"
        case lastExportError = "last_export_error"
        case lastExportErrorAt = "last_export_error_at"
    }
}

struct NavidromeEnsureParams: Codable, Sendable { let confirm: Bool }

/// `navidrome.mirror.push` params. `allow_deletes` is deliberately not sent:
/// raising the delete limit stays an explicit CLI step
/// (`udl navidrome push --allow-deletes N`), never a button.
struct NavidromeMirrorPushParams: Codable, Sendable {
    let forceScan: Bool

    enum CodingKeys: String, CodingKey {
        case forceScan = "force_scan"
    }
}

struct NavidromeServiceControlParams: Codable, Sendable { let action: String }

// MARK: - Setup plan

struct NavidromePlannedDirectory: Codable, Sendable, Equatable, Identifiable {
    let label: String
    let path: String
    let exists: Bool
    var id: String { path }
}

struct NavidromePlannedFile: Codable, Sendable, Equatable, Identifiable {
    let label: String
    let path: String
    let action: String
    let exists: Bool
    let owned: Bool
    let content: String
    var id: String { path }

    enum CodingKeys: String, CodingKey {
        case label, path, action, exists, owned, content
    }

    var isChange: Bool { action != "unchanged" }
}

struct NavidromePlannedService: Codable, Sendable, Equatable {
    let label: String
    let action: String
    let running: Bool
}

/// The setup plan, kept verbatim as `JSONValue` for the round trip and decoded
/// separately for display.
///
/// Re-encoding a decoded Swift model would drop fields the Go plan omitted and
/// break the checksum, the same trap `RekordboxPlanPresentation` documents.
struct NavidromeSetupPlanPresentation: Equatable {
    let value: JSONValue
    let checksum: String
    let version: String
    let binaryPath: String
    let serverVersion: String
    let musicDir: String
    let dataDir: String
    let port: Int
    let directories: [NavidromePlannedDirectory]
    let files: [NavidromePlannedFile]
    let service: NavidromePlannedService
    let blockers: [String]
    let warnings: [String]

    init?(_ value: JSONValue) {
        guard let object = value.objectValue else { return nil }
        self.value = value
        checksum = object["checksum_sha256"]?.stringValue ?? ""
        version = object["version"]?.stringValue ?? ""
        binaryPath = object["binary_path"]?.stringValue ?? ""
        serverVersion = object["server_version"]?.stringValue ?? ""
        musicDir = object["music_dir"]?.stringValue ?? ""
        dataDir = object["data_dir"]?.stringValue ?? ""
        port = object["port"]?.intValue ?? 0
        directories = (object["directories"]?.arrayValue ?? []).compactMap { item in
            guard let entry = item.objectValue, let path = entry["path"]?.stringValue else { return nil }
            return NavidromePlannedDirectory(
                label: entry["label"]?.stringValue ?? "",
                path: path,
                exists: entry["exists"]?.boolValue ?? false
            )
        }
        files = (object["files"]?.arrayValue ?? []).compactMap { item in
            guard let entry = item.objectValue, let path = entry["path"]?.stringValue else { return nil }
            return NavidromePlannedFile(
                label: entry["label"]?.stringValue ?? "",
                path: path,
                action: entry["action"]?.stringValue ?? "unknown",
                exists: entry["exists"]?.boolValue ?? false,
                owned: entry["owned"]?.boolValue ?? false,
                content: entry["content"]?.stringValue ?? ""
            )
        }
        let serviceObject = object["service"]?.objectValue
        service = NavidromePlannedService(
            label: serviceObject?["label"]?.stringValue ?? "",
            action: serviceObject?["action"]?.stringValue ?? "none",
            running: serviceObject?["running"]?.boolValue ?? false
        )
        blockers = (object["blockers"]?.arrayValue ?? []).compactMap(\.stringValue)
        warnings = (object["warnings"]?.arrayValue ?? []).compactMap(\.stringValue)
    }

    var applicable: Bool { blockers.isEmpty && !checksum.isEmpty }

    var changedFiles: [NavidromePlannedFile] { files.filter(\.isChange) }

    var newDirectories: [NavidromePlannedDirectory] { directories.filter { !$0.exists } }

    var changeCount: Int {
        changedFiles.count + newDirectories.count + (service.action == "none" ? 0 : 1)
    }

    var shortChecksum: String {
        guard checksum.count > 12 else { return checksum }
        return "\(checksum.prefix(8))…\(checksum.suffix(4))"
    }
}

struct NavidromeSetupApplyParams: Codable, Sendable { let plan: JSONValue }

struct NavidromeSetupApplyResult: Codable, Sendable {
    @DefaultEmpty var writtenFiles: [String]
    @DefaultEmpty var createdDirectories: [String]
    let serviceAction: String?
    let message: String?

    enum CodingKeys: String, CodingKey {
        case writtenFiles = "written_files"
        case createdDirectories = "created_directories"
        case serviceAction = "service_action"
        case message
    }
}

// MARK: - Playlists

struct NavidromeGeneratedPlaylist: Codable, Sendable, Equatable, Identifiable {
    let id: String
    let name: String
    let path: String
    let written: Bool
}

struct NavidromePlaylistRefreshResult: Codable, Sendable, Equatable {
    @DefaultEmpty var generated: [NavidromeGeneratedPlaylist]
    @DefaultEmpty var imported: [NavidromePlaylistInfo]
    let scanned: Bool
    @DefaultEmpty var warnings: [String]
}

struct NavidromePlaylistRegistrationResult: Codable, Sendable, Equatable {
    @DefaultEmpty var added: [String]
}

struct NavidromeGenreDerivation: Codable, Sendable, Equatable {
    let sourcePlaylist: String
    let sourceTrackCount: Int
    let matchedCount: Int
    @DefaultEmpty var genres: [String]
    @DefaultEmpty var unmatchedPaths: [String]
    @DefaultEmpty var genrelessPaths: [String]
    @DefaultEmpty var outsideLibrary: [String]
    @DefaultEmpty var currentAllowlist: [String]
    let allowlistChanged: Bool

    enum CodingKeys: String, CodingKey {
        case sourcePlaylist = "source_playlist"
        case sourceTrackCount = "source_track_count"
        case matchedCount = "matched_count"
        case genres
        case unmatchedPaths = "unmatched_paths"
        case genrelessPaths = "genreless_paths"
        case outsideLibrary = "outside_library"
        case currentAllowlist = "current_allowlist"
        case allowlistChanged = "allowlist_changed"
    }
}

struct NavidromeSaveGenresParams: Codable, Sendable { let genres: [String] }

// MARK: - Favorite migration

struct NavidromeFavoriteCounts: Codable, Sendable, Equatable {
    let sourceTotal: Int
    let matched: Int
    let alreadyStarred: Int
    let outsideLibrary: Int
    let missing: Int
    let ambiguous: Int
    let metadataOnly: Int
    let serverStarred: Int
    let expectedStarred: Int

    enum CodingKeys: String, CodingKey {
        case sourceTotal = "source_total"
        case matched
        case alreadyStarred = "already_starred"
        case outsideLibrary = "outside_library"
        case missing, ambiguous
        case metadataOnly = "metadata_only"
        case serverStarred = "server_starred"
        case expectedStarred = "expected_starred"
    }

    static let zero = NavidromeFavoriteCounts(
        sourceTotal: 0, matched: 0, alreadyStarred: 0, outsideLibrary: 0,
        missing: 0, ambiguous: 0, metadataOnly: 0, serverStarred: 0, expectedStarred: 0
    )
}

struct NavidromeFavoriteRowView: Identifiable, Equatable {
    let id: String
    let index: Int
    let status: String
    let title: String
    let artist: String
    let applePath: String
    let detail: String

    init?(_ value: JSONValue) {
        guard let object = value.objectValue else { return nil }
        index = object["index"]?.intValue ?? 0
        status = object["status"]?.stringValue ?? "unknown"
        title = object["title"]?.stringValue ?? "Untitled"
        artist = object["artist"]?.stringValue ?? ""
        applePath = object["apple_path"]?.stringValue ?? ""
        detail = object["detail"]?.stringValue ?? ""
        id = "\(index):\(object["apple_persistent_id"]?.stringValue ?? title)"
    }

    /// A row apply will act on.
    var isApplied: Bool { status == "matched" }

    /// A row apply will deliberately skip. Naming these is the point of the
    /// preview: a silently dropped favorite is the failure this screen exists
    /// to prevent.
    var isExcluded: Bool {
        ["outside_library", "missing", "ambiguous", "metadata_only"].contains(status)
    }

    var statusLabel: String { status.replacingOccurrences(of: "_", with: " ") }

    var severity: Severity {
        switch status {
        case "matched": .ok
        case "already_starred": .idle
        case "outside_library": .info
        case "missing", "ambiguous": .warn
        case "metadata_only": .warn
        default: .idle
        }
    }
}

/// The favorite migration plan, verbatim for the round trip and decoded for
/// display. Same rule as the setup plan: never re-encode from Swift.
struct NavidromeFavoritePlanPresentation: Equatable {
    let value: JSONValue
    let checksum: String
    let version: String
    let generatedAt: String
    let serverURL: String
    let username: String
    let musicDir: String
    let counts: NavidromeFavoriteCounts
    let rows: [NavidromeFavoriteRowView]
    let blockers: [String]
    let warnings: [String]

    init?(_ value: JSONValue) {
        guard let object = value.objectValue else { return nil }
        self.value = value
        checksum = object["checksum_sha256"]?.stringValue ?? ""
        version = object["version"]?.stringValue ?? ""
        generatedAt = object["generated_at"]?.stringValue ?? ""
        serverURL = object["server_url"]?.stringValue ?? ""
        username = object["username"]?.stringValue ?? ""
        musicDir = object["music_dir"]?.stringValue ?? ""
        let countsObject = object["counts"]?.objectValue ?? [:]
        func count(_ key: String) -> Int { countsObject[key]?.intValue ?? 0 }
        counts = NavidromeFavoriteCounts(
            sourceTotal: count("source_total"),
            matched: count("matched"),
            alreadyStarred: count("already_starred"),
            outsideLibrary: count("outside_library"),
            missing: count("missing"),
            ambiguous: count("ambiguous"),
            metadataOnly: count("metadata_only"),
            serverStarred: count("server_starred"),
            expectedStarred: count("expected_starred")
        )
        rows = (object["rows"]?.arrayValue ?? []).compactMap(NavidromeFavoriteRowView.init)
        blockers = (object["blockers"]?.arrayValue ?? []).compactMap(\.stringValue)
        warnings = (object["warnings"]?.arrayValue ?? []).compactMap(\.stringValue)
    }

    var applicable: Bool { blockers.isEmpty && !checksum.isEmpty }

    var excludedRows: [NavidromeFavoriteRowView] { rows.filter(\.isExcluded) }

    var shortChecksum: String {
        guard checksum.count > 12 else { return checksum }
        return "\(checksum.prefix(8))…\(checksum.suffix(4))"
    }
}

struct NavidromeFavoriteApplyParams: Codable, Sendable { let plan: JSONValue }

struct NavidromeFavoriteApplyResult: Codable, Sendable, Equatable {
    let backupPath: String?
    @DefaultEmpty var newlyStarred: [String]
    let alreadyStarred: Int
    let finalStarred: Int
    let parityVerified: Bool
    @DefaultEmpty var compensated: [String]
    let recoveryCommand: String?
    let message: String?

    enum CodingKeys: String, CodingKey {
        case backupPath = "backup_path"
        case newlyStarred = "newly_starred"
        case alreadyStarred = "already_starred"
        case finalStarred = "final_starred"
        case parityVerified = "parity_verified"
        case compensated
        case recoveryCommand = "recovery_command"
        case message
    }
}

/// What the server has starred right now — the return path for a like made on
/// the phone. `tracks` is always an array on the wire, but Go nil slices have
/// historically arrived as `null`, so it decodes defensively like every other
/// Phone Library collection.
struct NavidromeStarredListResult: Codable, Sendable, Equatable {
    let count: Int
    @DefaultEmpty var tracks: [NavidromeStarredTrack]

    /// The managed snapshot these stars flow into. It is a separate playlist
    /// from the Apple Music `favorites` snapshot by design, and it targets its
    /// own Rekordbox playlist.
    static let playlistID = "navidrome-favorites"
    static let playlistName = "Favourites (Navidrome)"
}

struct NavidromeStarredTrack: Codable, Sendable, Equatable, Identifiable {
    let id: String
    let title: String
    let artist: String?
    let album: String?
    let path: String?
}

// MARK: - Mirror push

/// One rsync push, as `navidrome.mirror.push` reports it.
struct NavidromePushReport: Codable, Sendable, Equatable {
    let changed: Int?
    let deleted: Int?
    @DefaultEmpty var items: [String]
    let warning: String?
    let plannedDeletes: Int?

    enum CodingKeys: String, CodingKey {
        case changed, deleted, items, warning
        case plannedDeletes = "planned_deletes"
    }
}

/// The Date Added reconcile that follows a scan; only its summary is shown.
struct NavidromeMirrorDates: Codable, Sendable, Equatable {
    let updated: Int?
    let message: String?
}

/// Why a push did not go through. `kind` is `busy`, `delete_limit`,
/// `server_scanning`, `canceled` or `failed`; anything else is treated as
/// `failed`.
struct NavidromeMirrorPushFailure: Codable, Sendable, Equatable {
    let kind: String
    let detail: String?
    let plannedDeletes: Int?
    let allowDeletesCommand: String?

    enum CodingKeys: String, CodingKey {
        case kind, detail
        case plannedDeletes = "planned_deletes"
        case allowDeletesCommand = "allow_deletes_command"
    }
}

/// The terminal result of `navidrome.mirror.push`, on success and on failure.
struct NavidromeMirrorPushResult: Codable, Sendable, Equatable {
    let push: NavidromePushReport?
    let scanned: Bool?
    let dates: NavidromeMirrorDates?
    @DefaultEmpty var warnings: [String]
    let mirror: NavidromeMirrorState?
    let failure: NavidromeMirrorPushFailure?
}

/// What the screen says after a push, derived from the run's terminal frame
/// alone so it can be tested without the UI. A delete-limit refusal carries
/// its planned count and the CLI override; the app never offers that
/// override as a button.
struct NavidromeMirrorPushOutcome: Equatable {
    enum Kind: Equatable {
        case pushed
        case busy
        case deleteLimit(plannedDeletes: Int, command: String)
        /// Deferred before the transfer because the server was scanning or
        /// its scan state could not be read. Nothing changed.
        case serverScanning
        case canceled
        case failed
    }

    let kind: Kind
    let message: String
    let severity: Severity

    init(exitCode: Int, error: String?, result: NavidromeMirrorPushResult?) {
        let failure = result?.failure
        if exitCode == 0 && failure == nil {
            kind = .pushed
            let push = result?.push
            let changes = (push?.changed ?? 0) + (push?.deleted ?? 0)
            // What happened to the scan and Date Added comes from `scanned`
            // and the pending flags recorded after the run: a push that
            // transferred nothing can still owe, or have just run, a scan.
            let mirror = result?.mirror
            let scanOwed = mirror?.scanPending == true
            let datesOwed = mirror?.datesPending == true
            var parts = ["Pushed to the server: \(push?.changed ?? 0) changed, \(push?.deleted ?? 0) deleted."]
            if result?.scanned == true {
                parts.append(changes == 0
                    ? "Nothing new was pushed, but the server ran the scan an earlier push still owed."
                    : "The server rescanned the library.")
            } else if scanOwed {
                parts.append("The server has not finished scanning the pushed files yet; the next push retries the scan.")
            } else if changes == 0 && mirror != nil {
                parts.append("Nothing changed, so no scan was needed.")
            }
            if datesOwed {
                parts.append("Date Added was not re-applied yet; the next push retries it.")
            } else if let dates = result?.dates?.message, !dates.isEmpty {
                parts.append(dates)
            }
            let warnings = result?.warnings ?? []
            parts.append(contentsOf: warnings)
            message = parts.joined(separator: " ")
            severity = warnings.isEmpty && !scanOwed && !datesOwed ? .ok : .warn
            return
        }
        let reported = failure?.detail.flatMap { $0.isEmpty ? nil : $0 }
            ?? error.flatMap { $0.isEmpty ? nil : $0 }
        let detail = reported ?? "The push failed with exit code \(exitCode)."
        // rsync is not transactional across the tree, so a push that stopped
        // after its transfer began may have changed part of the mirror. The
        // owed scan is recorded before the transfer starts; without it, and
        // without changes, nothing reached the server. Unknown reads as maybe.
        let changes = (result?.push?.changed ?? 0) + (result?.push?.deleted ?? 0)
        let mirror = result?.mirror
        let mayBePartial = mirror == nil || changes > 0
            || mirror?.scanPending == true || mirror?.datesPending == true
        let canceledMessage = (mayBePartial
            ? "Push canceled before it finished. The server may be partly updated; push again to complete it."
            : "Push canceled before anything was transferred; nothing changed on the server.")
            + (reported.map { " \($0)" } ?? "")
        switch failure?.kind {
        case "busy":
            kind = .busy
            message = detail
            severity = .warn
        case "delete_limit":
            let planned = failure?.plannedDeletes ?? result?.push?.plannedDeletes ?? 0
            let command = failure?.allowDeletesCommand ?? "udl navidrome push --allow-deletes \(planned)"
            kind = .deleteLimit(plannedDeletes: planned, command: command)
            message = "The push was refused: it would delete \(planned) file(s) from the server, more than the delete limit allows. Nothing was changed. If the deletions are intended, run `\(command)` in Terminal; the app does not override the limit."
            severity = .error
        case "server_scanning":
            // A scan reading a half-applied mirror gives moved tracks new IDs,
            // so the push waits for the server rather than racing its scan.
            kind = .serverScanning
            message = "The push was deferred: the server is scanning, or its scan state could not be read. Nothing was changed. Push again once the scan finishes; the scheduled push also retries. \(detail)"
            severity = .warn
        case "canceled":
            kind = .canceled
            message = canceledMessage
            severity = .warn
        default:
            if exitCode == 130 {
                kind = .canceled
                message = canceledMessage
                severity = .warn
            } else {
                kind = .failed
                message = mayBePartial
                    ? "The push did not complete, and the server may be partly updated; push again once the cause is fixed. \(detail)"
                    : "The push failed before anything was transferred; nothing changed on the server. \(detail)"
                severity = .error
            }
        }
    }

    /// The planned deletion count of a delete-limit refusal, if this is one.
    var plannedDeletes: Int? {
        if case .deleteLimit(let planned, _) = kind { return planned }
        return nil
    }
}

/// Time and size wording for the remote cards. Pure functions over raw wire
/// strings, so a timestamp this build cannot parse shows as-is.
enum PhoneLibraryFormat {
    /// Parses Go's RFC 3339 output, with or without (nanosecond) fractions.
    static func date(_ raw: String?) -> Date? {
        guard let raw, !raw.isEmpty else { return nil }
        let fractional = ISO8601DateFormatter()
        fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = fractional.date(from: raw) { return date }
        let plain = ISO8601DateFormatter()
        plain.formatOptions = [.withInternetDateTime]
        if let date = plain.date(from: raw) { return date }
        // Some Foundation versions reject more than three fractional digits;
        // the sub-second part is irrelevant for display.
        let trimmed = raw.replacingOccurrences(of: #"\.[0-9]+"#, with: "", options: .regularExpression)
        return plain.date(from: trimmed)
    }

    /// "5 minutes ago", stated coarsely; a time in the future reads as now.
    static func relative(_ date: Date, now: Date) -> String {
        let seconds = Int(now.timeIntervalSince(date))
        switch seconds {
        case ..<60: return "just now"
        case ..<3600:
            let minutes = seconds / 60
            return minutes == 1 ? "1 minute ago" : "\(minutes) minutes ago"
        case ..<86_400:
            let hours = seconds / 3600
            return hours == 1 ? "1 hour ago" : "\(hours) hours ago"
        default:
            let days = seconds / 86_400
            return days == 1 ? "1 day ago" : "\(days) days ago"
        }
    }

    static func absolute(_ date: Date, timeZone: TimeZone = .current) -> String {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = timeZone
        formatter.dateFormat = "yyyy-MM-dd HH:mm"
        return formatter.string(from: date)
    }

    /// "5 minutes ago (2026-10-22 10:15)", the raw text when it cannot be
    /// parsed, or `never` when there is nothing.
    static func moment(_ raw: String?, now: Date = Date(), timeZone: TimeZone = .current, never: String = "never") -> String {
        guard let raw, !raw.isEmpty else { return never }
        guard let date = date(raw) else { return raw }
        return "\(relative(date, now: now)) (\(absolute(date, timeZone: timeZone)))"
    }

    static func bytes(_ value: Int64?) -> String {
        guard let value, value >= 0 else { return "unknown size" }
        let gib = Double(value) / Double(1 << 30)
        if gib >= 1 { return String(format: "%.1f GiB", gib) }
        return String(format: "%.0f MiB", Double(value) / Double(1 << 20))
    }
}

/// The remote card's rows, derived from status and config alone.
struct PhoneLibraryRemoteSummary: Equatable {
    /// The address the phone uses: the configured `server.url`.
    let serverURL: String
    let sshTarget: String
    let lastPush: String
    let lastScan: String
    let lastPushError: String?
    let serverLibrary: String
    let serverDatabase: String
    let databaseHealthy: Bool
    let latestServerBackup: String
    /// When the server's database was last copied to this Mac, and why the
    /// most recent attempt failed, if it did.
    let lastServerDBCopy: String
    let lastServerDBCopyError: String?
    /// The status problems — stale or failed pushes, an unreachable server,
    /// a library rooted at the wrong path.
    let problems: [String]

    init(status: NavidromeStatus?, configuredServerURL: String?, now: Date = Date(), timeZone: TimeZone = .current) {
        serverURL = configuredServerURL ?? ""
        let remote = status?.remote
        let mirror = status?.mirror
        sshTarget = remote?.sshTarget ?? "—"
        lastPush = PhoneLibraryFormat.moment(mirror?.lastPushAt, now: now, timeZone: timeZone)
        lastScan = PhoneLibraryFormat.moment(mirror?.lastScanAt, now: now, timeZone: timeZone)
        if let error = mirror?.lastPushError, !error.isEmpty {
            let when = PhoneLibraryFormat.moment(mirror?.lastErrorAt, now: now, timeZone: timeZone, never: "at an unknown time")
            lastPushError = "Last push failed \(when): \(error)"
        } else {
            lastPushError = nil
        }
        if let library = remote?.library {
            let files = library.files.map { "\($0) files" } ?? "unknown file count"
            serverLibrary = "\(files) · \(PhoneLibraryFormat.bytes(library.bytes))"
        } else {
            serverLibrary = "unknown (the server did not answer)"
        }
        if let database = remote?.database {
            if let error = database.error, !error.isEmpty {
                serverDatabase = "unreadable: \(error)"
                databaseHealthy = false
            } else if database.present == true {
                serverDatabase = database.tracks.map { "\($0) tracks" } ?? "present"
                databaseHealthy = true
            } else {
                serverDatabase = "not created yet"
                databaseHealthy = false
            }
        } else {
            serverDatabase = "unknown (the server did not answer)"
            databaseHealthy = false
        }
        if let backup = status?.latestBackup {
            latestServerBackup = "\(backup.path) · \(PhoneLibraryFormat.moment(backup.createdAt, now: now, timeZone: timeZone, never: "unknown time"))"
        } else {
            latestServerBackup = "none yet"
        }
        if let export = status?.serverDBExport {
            lastServerDBCopy = PhoneLibraryFormat.moment(export.lastExportAt, now: now, timeZone: timeZone)
            if let error = export.lastExportError, !error.isEmpty {
                let when = PhoneLibraryFormat.moment(export.lastExportErrorAt, now: now, timeZone: timeZone, never: "at an unknown time")
                lastServerDBCopyError = "Last copy failed \(when): \(error)"
            } else {
                lastServerDBCopyError = nil
            }
        } else {
            lastServerDBCopy = "not reported by this backend"
            lastServerDBCopyError = nil
        }
        problems = status?.problems ?? []
    }
}

struct NavidromeBackupResult: Codable, Sendable, Equatable {
    let backup: NavidromeBackupInfo?
    let command: String?
    let message: String?
}

struct NavidromeEnsureResult: Codable, Sendable, Equatable {
    let ran: Bool
    let command: String?
    let output: String?
    let status: NavidromeDependencyStatus?
    let message: String?
}

// MARK: - Workflow state

/// Which long-running Phone Library step is in flight. Only one runs at a time,
/// so the screen never has to reconcile two.
enum PhoneLibraryOperation: Equatable, Sendable {
    case installDependency
    case serviceControl(action: String)
    case setupPlan
    case setupApply
    case registerPlaylists
    case refreshPlaylists
    case deriveGenres
    case saveGenres
    case favoritePlan
    case favoriteApply
    case favoriteList
    case backup
    case mirrorPush

    /// True for steps that change the machine or the server. These are never
    /// replayed after a backend restart.
    var isMutating: Bool {
        switch self {
        case .setupPlan, .deriveGenres, .favoritePlan, .favoriteList: false
        default: true
        }
    }

    var label: String {
        switch self {
        case .installDependency: "Installing Navidrome…"
        case .serviceControl(let action): "\(action.capitalized)ing the service…"
        case .setupPlan: "Building the setup plan…"
        case .setupApply: "Applying setup…"
        case .registerPlaylists: "Registering managed snapshots…"
        case .refreshPlaylists: "Refreshing smart playlists…"
        case .deriveGenres: "Reading the Apple Music playlist…"
        case .saveGenres: "Saving the genre allowlist…"
        case .favoritePlan: "Comparing Apple Music favorites to Navidrome…"
        case .favoriteApply: "Backing up, then starring exact matches…"
        case .favoriteList: "Reading what is starred on the server…"
        case .backup: "Creating a database backup…"
        case .mirrorPush: "Pushing the library to the server, then rescanning…"
        }
    }
}

/// One setup step and whether it is done, derived from status alone so the
/// checklist can never disagree with the rest of the screen.
struct PhoneLibraryStep: Identifiable, Equatable {
    enum State: Equatable {
        case done
        case current
        case blocked(String)
        case pending
    }

    let id: String
    let title: String
    let state: State

    var isDone: Bool { state == .done }
}

/// The setup checklist, derived once from status so the sidebar badge, the
/// step list, and every action's enabled state read the same source.
struct PhoneLibraryProgress: Equatable {
    let steps: [PhoneLibraryStep]

    init(status: NavidromeStatus?) {
        guard let status else {
            self.steps = [PhoneLibraryStep(id: "load", title: "Load Phone Library state", state: .current)]
            return
        }
        var steps: [PhoneLibraryStep] = []
        func step(_ id: String, _ title: String, done: Bool, blocked: String? = nil) {
            let state: PhoneLibraryStep.State
            if done {
                state = .done
            } else if let blocked {
                state = .blocked(blocked)
            } else if steps.allSatisfy(\.isDone) {
                state = .current
            } else {
                state = .pending
            }
            steps.append(PhoneLibraryStep(id: id, title: title, state: state))
        }

        if status.isRemote {
            // The server runs Navidrome, so nothing is installed or started
            // here; the Mac's one job is to keep the mirror current.
            // Done only once the server has indexed the push: files that
            // arrived but were not scanned, or whose Date Added was not
            // re-applied, leave the step current until a push finishes them.
            let mirror = status.mirror
            let failed = !(mirror?.lastPushError ?? "").isEmpty
            let current = mirror?.lastPushAt != nil && !failed
                && mirror?.scanPending != true && mirror?.datesPending != true
            step("mirror", "Push the library to the server",
                 done: current,
                 blocked: failed ? "The last push failed. Push again once the cause is fixed." : nil)
        } else {
            step("dependency", "Install Navidrome",
                 done: status.dependency.ready,
                 blocked: status.dependency.homebrewInstalled ? nil : "Homebrew is not installed.")
            step("service", "Install and start the managed service",
                 done: status.service.isRunning,
                 blocked: status.service.owned || status.service.state == "not_installed"
                    ? nil
                    : "A Navidrome LaunchAgent exists that UDL does not manage.")
        }
        step("account", "Create the admin account and save its password",
             done: status.passwordStored && !(status.username ?? "").isEmpty)
        step("library", "Scan the music library",
             done: status.reachable && status.libraryTracks > 0)
        step("playlists", "Create the managed smart playlists",
             done: status.managedPlaylists.count >= 3)
        // The only step that happens on another device. Nothing here can see
        // the phone app, so this reflects what the user marked in the Connect
        // card rather than a probe — stated as such wherever it is shown.
        step("phone", status.isRemote ? "Connect your phone app to the server" : "Connect your phone app over home Wi-Fi",
             done: status.phoneConnected)
        self.steps = steps
    }

    var completed: Int { steps.filter(\.isDone).count }
    var total: Int { steps.count }
    var remaining: Int { total - completed }

    var current: PhoneLibraryStep? { steps.first { $0.state == .current } }
}

/// The connection details a user copies into the phone app.
struct PhoneConnectionDetails: Equatable {
    let serverURL: String
    let username: String
    /// The approximate download size, stated so the user can check free space
    /// before starting a full offline download.
    let approximateLibrarySize: String

    /// In remote mode the phone talks to the server, so the address is the
    /// configured `server.url`; locally it is the running service's LAN URL.
    init(status: NavidromeStatus?, configuredServerURL: String? = nil) {
        if status?.isRemote == true {
            serverURL = configuredServerURL ?? ""
        } else {
            serverURL = status?.service.lanURL ?? status?.service.localURL ?? ""
        }
        username = status?.username ?? ""
        let tracks = status?.libraryTracks ?? 0
        if tracks > 0 {
            // ~6.1 MiB per track, measured against the current library
            // (1,574 tracks ≈ 9.4 GiB). Stated as approximate because it is.
            let gib = Double(tracks) * 6.1 / 1024
            approximateLibrarySize = String(format: "%.1f GiB", gib)
        } else {
            approximateLibrarySize = "unknown"
        }
    }

    var isReady: Bool { !serverURL.isEmpty && !username.isEmpty }
}
