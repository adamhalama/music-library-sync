import Foundation

struct RekordboxWritePath: Codable, Sendable {
    let path: String
    let kind: String
    let exists: Bool
}

struct RekordboxDefaults: Codable, Sendable {
    var dbDir: String
    var pythonBin: String?
    var pythonPath: String?
    var backupDir: String
    var mode: String
    var createFolders: Bool
    var createPlaylists: Bool

    enum CodingKeys: String, CodingKey {
        case dbDir = "db_dir"
        case pythonBin = "python_bin"
        case pythonPath = "python_path"
        case backupDir = "backup_dir"
        case mode
        case createFolders = "create_folders"
        case createPlaylists = "create_playlists"
    }
}

struct RekordboxFolderMapping: Codable, Sendable, Identifiable {
    let id: String
    var musicFolder: String?
    var musicFolderID: String?
    var rekordboxFolder: String?
    var rekordboxFolderID: String?
    var playlistNameMap: [String: String]?
    var includePlaylists: [String]?
    var excludePlaylists: [String]?
    var onMissingTracks: String
    var createFolders: Bool?
    var createPlaylists: Bool?

    enum CodingKeys: String, CodingKey {
        case id
        case musicFolder = "music_folder"
        case musicFolderID = "music_folder_id"
        case rekordboxFolder = "rekordbox_folder"
        case rekordboxFolderID = "rekordbox_folder_id"
        case playlistNameMap = "playlist_name_map"
        case includePlaylists = "include_playlists"
        case excludePlaylists = "exclude_playlists"
        case onMissingTracks = "on_missing_tracks"
        case createFolders = "create_folders"
        case createPlaylists = "create_playlists"
    }
}

struct RekordboxPlaylistJob: Codable, Sendable, Identifiable {
    let id: String
    var musicPlaylist: String?
    var musicPlaylistID: String?
    var rekordboxPlaylist: String?
    var rekordboxPlaylistID: String?
    var mode: String?
    var createPlaylist: Bool?

    enum CodingKeys: String, CodingKey {
        case id
        case musicPlaylist = "music_playlist"
        case musicPlaylistID = "music_playlist_id"
        case rekordboxPlaylist = "rekordbox_playlist"
        case rekordboxPlaylistID = "rekordbox_playlist_id"
        case mode
        case createPlaylist = "create_playlist"
    }
}

struct RekordboxSyncConfig: Codable, Sendable {
    @DefaultEmpty var folders: [RekordboxFolderMapping]
    @DefaultEmpty var jobs: [RekordboxPlaylistJob]
}

struct RekordboxConfig: Codable, Sendable {
    let version: Int
    var defaults: RekordboxDefaults
    var sync: RekordboxSyncConfig
    let warnings: [String]?
}

struct RekordboxConfigResult: Codable, Sendable {
    let path: RekordboxWritePath
    let config: RekordboxConfig
    let content: String
}

struct RekordboxConfigWriteParams: Codable, Sendable { let config: RekordboxConfig }

struct RekordboxRuntimeStatus: Codable, Sendable {
    let pythonBin: String
    let pythonPath: String?
    let managed: Bool
    let venvDir: String?
    let version: String?
    let installed: Bool
    let healthy: Bool
    let message: String

    enum CodingKeys: String, CodingKey {
        case pythonBin = "python_bin"
        case pythonPath = "python_path"
        case managed
        case venvDir = "venv_dir"
        case version = "pyrekordbox_version"
        case installed, healthy, message
    }
}

struct RekordboxDependencyResult: Codable, Sendable {
    let status: RekordboxRuntimeStatus
    let exitCode: Int
    enum CodingKeys: String, CodingKey {
        case status
        case exitCode = "exit_code"
    }
}

struct RekordboxPlanParams: Codable, Sendable {
    let jobID: String?
    let mappingID: String?
    let playlistID: String?

    enum CodingKeys: String, CodingKey {
        case jobID = "job_id"
        case mappingID = "mapping_id"
        case playlistID = "playlist_id"
    }
}

struct RekordboxApplyParams: Codable, Sendable {
    // Preserve every checksummed plan field structurally through the Swift
    // layer; decoding it into a newly evolving Swift model risks drift.
    let plan: JSONValue
    let dryRun: Bool

    enum CodingKeys: String, CodingKey {
        case plan
        case dryRun = "dry_run"
    }
}

struct RekordboxPlaylistInfo: Codable, Sendable, Identifiable {
    let id: String
    let name: String
    let attribute: Int
    let parentID: String
    @DefaultEmpty var contentIDs: [String]
    enum CodingKeys: String, CodingKey {
        case id, name, attribute
        case parentID = "parent_id"
        case contentIDs = "content_ids"
    }
}

struct RekordboxContentInfo: Codable, Sendable, Identifiable {
    let id: String
    let title: String
    let folderPath: String
    enum CodingKeys: String, CodingKey {
        case id, title
        case folderPath = "folder_path"
    }
}

struct RekordboxInspectResult: Codable, Sendable {
    @DefaultEmpty var playlists: [RekordboxPlaylistInfo]
    @DefaultEmpty var contents: [RekordboxContentInfo]
}

struct RekordboxPlanResult: Codable, Sendable {
    let resolved: JSONValue
    let plan: JSONValue
    let planPath: String
    enum CodingKeys: String, CodingKey {
        case resolved, plan
        case planPath = "plan_path"
    }
}

struct RekordboxPlanRowView: Identifiable {
    let id: String
    let index: Int
    let artist: String
    let title: String
    let path: String
    let duration: String
    let matchStatus: String
    let action: String

    init?(_ value: JSONValue) {
        guard let object = value.objectValue else { return nil }
        index = object["music_index"]?.intValue ?? 0
        artist = object["artist"]?.stringValue ?? ""
        title = object["title"]?.stringValue ?? "Untitled"
        path = object["path"]?.stringValue ?? ""
        duration = object["duration"]?.stringValue ?? ""
        matchStatus = object["match_status"]?.stringValue ?? "unknown"
        action = object["action"]?.stringValue ?? "unknown"
        id = "\(index):\(object["music_persistent_id"]?.stringValue ?? title)"
    }

    var isBlocked: Bool {
        action == "skip" && ["missing", "ambiguous", "ambiguous_path", "duplicate_path"].contains(matchStatus)
    }

    var isChange: Bool { action != "keep" && !isBlocked }

    /// Shared with Playlists: both read the same AppleScript field. See
    /// `MusicDuration`.
    var durationLabel: String { MusicDuration.label(duration) }

    var matchLabel: String {
        matchStatus.replacingOccurrences(of: "_", with: " ")
    }

    var matchSeverity: Severity {
        switch matchStatus {
        case "matched": .ok
        case "missing": .error
        case "ambiguous", "ambiguous_path", "duplicate_path": .warn
        default: .idle
        }
    }

    var actionLabel: String {
        action.replacingOccurrences(of: "_", with: " ")
    }
}

/// The plan, read for display only.
///
/// C9 — `value` is the verbatim `JSONValue` the backend sent and is the only
/// thing `rekordbox.apply` is ever given. Everything else on this type is a
/// read of that value for the screen; nothing here is re-encoded and sent back,
/// because a Swift round trip would drop `omitempty` fields and break the
/// checksum (see `AGENTS.md`).
struct RekordboxPlanPresentation {
    let value: JSONValue
    let checksum: String
    let version: String
    let generatedAt: String
    let jobID: String
    let musicPlaylist: String
    let rekordboxPlaylist: String
    let dbDir: String
    let backupDir: String
    let planWarnings: [String]
    let rows: [RekordboxPlanRowView]
    let musicTotal: Int
    let matched: Int
    let missing: Int
    let ambiguous: Int
    let willAdd: Int
    let willRemove: Int
    let willMove: Int
    let willKeep: Int

    init?(_ value: JSONValue) {
        guard let object = value.objectValue else { return nil }
        self.value = value
        checksum = object["checksum_sha256"]?.stringValue ?? ""
        version = object["version"]?.stringValue ?? ""
        generatedAt = object["generated_at"]?.stringValue ?? ""
        jobID = object["job_id"]?.stringValue ?? ""
        dbDir = object["rekordbox_db_dir"]?.stringValue ?? ""
        backupDir = object["backup_dir"]?.stringValue ?? ""
        planWarnings = (object["warnings"]?.arrayValue ?? []).compactMap(\.stringValue)
        let operations = object["operations"]?.arrayValue ?? []
        // A folder plan names the folders; a single-playlist plan names the
        // playlists. Neither is invented when the field is absent.
        if let folder = object["music_folder"]?.objectValue?["name"]?.stringValue, !folder.isEmpty {
            musicPlaylist = folder
        } else {
            musicPlaylist = object["music_playlist"]?.objectValue?["name"]?.stringValue ?? ""
        }
        if let folder = object["rekordbox_folder"]?.objectValue?["name"]?.stringValue, !folder.isEmpty {
            rekordboxPlaylist = folder
        } else {
            rekordboxPlaylist = object["rekordbox_playlist"]?.objectValue?["name"]?.stringValue ?? ""
        }
        let direct = object["rows"]?.arrayValue ?? []
        let operationRows = operations.flatMap { $0.objectValue?["rows"]?.arrayValue ?? [] }
        rows = (direct + operationRows).compactMap(RekordboxPlanRowView.init)

        // A folder plan carries no top-level summary; its counts are the sum of
        // its per-operation summaries. Both shapes fall back to counting rows
        // rather than reporting a zero the plan never stated.
        let summaries = [object["summary"]?.objectValue].compactMap { $0 }
            + operations.compactMap { $0.objectValue?["summary"]?.objectValue }
        func total(_ key: String, fallback: @autoclosure () -> Int) -> Int {
            let values = summaries.compactMap { $0[key]?.intValue }
            return values.isEmpty ? fallback() : values.reduce(0, +)
        }
        musicTotal = total("music_total", fallback: rows.count)
        matched = total("matched_by_path", fallback: rows.filter { $0.matchStatus == "matched" }.count)
        missing = total("missing_in_rekordbox", fallback: rows.filter { $0.matchStatus == "missing" }.count)
        ambiguous = total("ambiguous_in_rekordbox", fallback: rows.filter { $0.matchStatus == "ambiguous" }.count)
        willAdd = total("will_add", fallback: rows.filter { $0.action == "add" }.count)
        willRemove = total("will_remove", fallback: rows.filter { $0.action == "remove" }.count)
        willMove = total("will_move", fallback: rows.filter { $0.action == "move" }.count)
        willKeep = total("will_keep", fallback: rows.filter { $0.action == "keep" }.count)
    }

    var blockers: [RekordboxPlanRowView] { rows.filter(\.isBlocked) }

    var changeCount: Int { willAdd + willRemove + willMove }

    var title: String {
        switch (musicPlaylist.isEmpty, rekordboxPlaylist.isEmpty) {
        case (false, false): "\(musicPlaylist) → \(rekordboxPlaylist)"
        case (false, true): musicPlaylist
        case (true, false): rekordboxPlaylist
        case (true, true): jobID.isEmpty ? "Checksummed mirror plan" : jobID
        }
    }

    var shortChecksum: String {
        guard checksum.count > 12 else { return checksum }
        return "\(checksum.prefix(8))…\(checksum.suffix(4))"
    }
}

/// C10 — everything the screen knows about whether apply can proceed, derived
/// once so the plan-bar pill, the primary button, the dry-run button and the
/// help text cannot disagree.
///
/// The primary action always names its blocker. There is no state in which this
/// produces a disabled button with no stated reason.
struct RekordboxApplyGate {
    enum Action: Equatable {
        case generatePlan
        case confirmApply
        case showBlockedRows
    }

    var pillTitle: String
    var severity: Severity
    var actionLabel: String
    var action: Action
    var reason: String?
    var blocksDryRun: Bool

    init(plan: RekordboxPlanPresentation?, obstacle: RekordboxObstacle?, isPlanning: Bool) {
        guard let plan else {
            self.init(
                pillTitle: isPlanning ? "Planning" : "No plan",
                severity: isPlanning ? .info : .idle,
                actionLabel: "Generate plan",
                action: .generatePlan,
                reason: "Planning is read-only: it re-reads both inputs and writes nothing.",
                blocksDryRun: true
            )
            return
        }
        // A refusal the backend actually returned outranks anything the plan
        // alone implies, because it is the only report of a live precondition.
        if let obstacle, let label = obstacle.actionLabel {
            self.init(
                pillTitle: obstacle.requiresRegeneration ? "Plan drifted" : "Apply blocked",
                severity: .error,
                actionLabel: label,
                // Once Rekordbox is quit, the honest action is to try again;
                // the backend re-checks. Drift can only be answered by a plan.
                action: obstacle.requiresRegeneration ? .generatePlan : .confirmApply,
                reason: obstacle.message,
                blocksDryRun: true
            )
            return
        }
        if plan.checksum.isEmpty {
            self.init(
                pillTitle: "No checksum",
                severity: .error,
                actionLabel: "Regenerate plan — no checksum",
                action: .generatePlan,
                reason: "rekordbox.plan returned no checksum_sha256, so apply would be refused.",
                blocksDryRun: true
            )
            return
        }
        if !plan.blockers.isEmpty {
            let missing = plan.blockers.filter { $0.matchStatus == "missing" }.count
            let label = missing > 0
                ? "\(missing) \(missing == 1 ? "track" : "tracks") missing — resolve to apply"
                : "\(plan.blockers.count) ambiguous \(plan.blockers.count == 1 ? "match" : "matches") — resolve to apply"
            self.init(
                pillTitle: "Apply blocked",
                severity: .error,
                actionLabel: label,
                action: .showBlockedRows,
                reason: "udl refuses a partial mirror. Resolve every blocked row, then regenerate the plan.",
                blocksDryRun: true
            )
            return
        }
        self.init(
            pillTitle: "Ready to apply",
            severity: .ok,
            actionLabel: "Apply \(plan.changeCount) \(plan.changeCount == 1 ? "change" : "changes")…",
            action: .confirmApply,
            reason: nil,
            blocksDryRun: false
        )
    }

    private init(
        pillTitle: String,
        severity: Severity,
        actionLabel: String,
        action: Action,
        reason: String?,
        blocksDryRun: Bool
    ) {
        self.pillTitle = pillTitle
        self.severity = severity
        self.actionLabel = actionLabel
        self.action = action
        self.reason = reason
        self.blocksDryRun = blocksDryRun
    }
}

/// C10 — a precondition the backend actually refused on.
///
/// The protocol reports no Rekordbox process state, no database checksum and no
/// backup-directory state before a run attempts them, so this is never
/// predicted: it is only ever constructed from a real refusal message returned
/// by `rekordbox.inspect` or `rekordbox.apply`.
enum RekordboxObstacle: Equatable, Sendable {
    case rekordboxRunning(String)
    case planDrift(String)
    case partialMirrorRefused(String)
    case other(String)

    init(backendMessage: String) {
        let text = backendMessage.lowercased()
        if text.contains("rekordbox is running") || text.contains("sidecar exists") {
            self = .rekordboxRunning(backendMessage)
        } else if text.contains("checksum") {
            self = .planDrift(backendMessage)
        } else if text.contains("refuses partial mirror")
            || text.contains("missing rekordbox tracks")
            || text.contains("ambiguous rekordbox path")
            || text.contains("refuses to mirror duplicates") {
            self = .partialMirrorRefused(backendMessage)
        } else {
            self = .other(backendMessage)
        }
    }

    var message: String {
        switch self {
        case .rekordboxRunning(let text), .planDrift(let text),
             .partialMirrorRefused(let text), .other(let text):
            text
        }
    }

    /// The primary button's label. It names the blocker instead of leaving an
    /// unexplained disabled button on screen.
    var actionLabel: String? {
        switch self {
        case .rekordboxRunning: "Rekordbox is open — quit it to apply"
        case .planDrift: "Regenerate plan"
        case .partialMirrorRefused: nil
        case .other: nil
        }
    }

    /// True when the only honest next step is a fresh plan, not a retry.
    var requiresRegeneration: Bool {
        if case .planDrift = self { return true }
        return false
    }
}

enum RekordboxOperation: Sendable {
    case ensure
    case reset
    case inspect
    case plan
    case apply(dryRun: Bool)
}
