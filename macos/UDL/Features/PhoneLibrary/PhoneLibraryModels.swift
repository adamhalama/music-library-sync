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

/// The phone-side setup step, which nothing on this Mac can observe. Amperfy
/// does not announce itself through any API UDL reads, so this is the user's
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
    }
}

struct NavidromeEnsureParams: Codable, Sendable { let confirm: Bool }

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
    static let playlistName = "Favourites (Navidrome)"
}

struct NavidromeStarredTrack: Codable, Sendable, Equatable, Identifiable {
    let id: String
    let title: String
    let artist: String?
    let album: String?
    let path: String?
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
    case refreshPlaylists
    case deriveGenres
    case saveGenres
    case favoritePlan
    case favoriteApply
    case favoriteList
    case backup

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
        case .refreshPlaylists: "Refreshing smart playlists…"
        case .deriveGenres: "Reading the Apple Music playlist…"
        case .saveGenres: "Saving the genre allowlist…"
        case .favoritePlan: "Comparing Apple Music favorites to Navidrome…"
        case .favoriteApply: "Backing up, then starring exact matches…"
        case .favoriteList: "Reading what is starred on the server…"
        case .backup: "Creating a database backup…"
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

        step("dependency", "Install Navidrome",
             done: status.dependency.ready,
             blocked: status.dependency.homebrewInstalled ? nil : "Homebrew is not installed.")
        step("service", "Install and start the managed service",
             done: status.service.isRunning,
             blocked: status.service.owned || status.service.state == "not_installed"
                ? nil
                : "A Navidrome LaunchAgent exists that UDL does not manage.")
        step("account", "Create the admin account and save its password",
             done: status.passwordStored && !(status.username ?? "").isEmpty)
        step("library", "Scan the music library",
             done: status.reachable && status.libraryTracks > 0)
        step("playlists", "Create the managed smart playlists",
             done: status.managedPlaylists.count >= 3)
        // The only step that happens on another device. Nothing here can see
        // Amperfy, so this reflects what the user marked in the Connect card
        // rather than a probe — stated as such wherever it is shown.
        step("phone", "Connect Amperfy over home Wi-Fi",
             done: status.phoneConnected)
        self.steps = steps
    }

    var completed: Int { steps.filter(\.isDone).count }
    var total: Int { steps.count }
    var remaining: Int { total - completed }

    var current: PhoneLibraryStep? { steps.first { $0.state == .current } }
}

/// The connection details a user copies into Amperfy.
struct PhoneConnectionDetails: Equatable {
    let serverURL: String
    let username: String
    /// The approximate download size, stated so the user can check free space
    /// before starting a full offline download.
    let approximateLibrarySize: String

    init(status: NavidromeStatus?) {
        serverURL = status?.service.lanURL ?? status?.service.localURL ?? ""
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
