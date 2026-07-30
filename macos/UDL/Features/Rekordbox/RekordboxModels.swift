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
    var folders: [RekordboxFolderMapping]
    var jobs: [RekordboxPlaylistJob]

    init(folders: [RekordboxFolderMapping], jobs: [RekordboxPlaylistJob]) {
        self.folders = folders
        self.jobs = jobs
    }

    enum CodingKeys: String, CodingKey {
        case folders, jobs
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        folders = try container.decodeIfPresent([RekordboxFolderMapping].self, forKey: .folders) ?? []
        jobs = try container.decodeIfPresent([RekordboxPlaylistJob].self, forKey: .jobs) ?? []
    }
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
    let contentIDs: [String]
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
    let playlists: [RekordboxPlaylistInfo]
    let contents: [RekordboxContentInfo]
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
    let artist: String
    let title: String
    let path: String
    let matchStatus: String
    let action: String

    init?(_ value: JSONValue) {
        guard let object = value.objectValue else { return nil }
        let index = object["music_index"]?.intValue ?? 0
        artist = object["artist"]?.stringValue ?? ""
        title = object["title"]?.stringValue ?? "Untitled"
        path = object["path"]?.stringValue ?? ""
        matchStatus = object["match_status"]?.stringValue ?? "unknown"
        action = object["action"]?.stringValue ?? "unknown"
        id = "\(index):\(object["music_persistent_id"]?.stringValue ?? title)"
    }
}

struct RekordboxPlanPresentation {
    let value: JSONValue
    let checksum: String
    let version: String
    let rows: [RekordboxPlanRowView]
    let musicTotal: Int
    let matched: Int
    let missing: Int
    let ambiguous: Int

    init?(_ value: JSONValue) {
        guard let object = value.objectValue else { return nil }
        self.value = value
        checksum = object["checksum_sha256"]?.stringValue ?? ""
        version = object["version"]?.stringValue ?? ""
        let direct = object["rows"]?.arrayValue ?? []
        let operations = object["operations"]?.arrayValue ?? []
        let operationRows = operations.flatMap { $0.objectValue?["rows"]?.arrayValue ?? [] }
        rows = (direct + operationRows).compactMap(RekordboxPlanRowView.init)
        let summary = object["summary"]?.objectValue ?? [:]
        musicTotal = summary["music_total"]?.intValue ?? rows.count
        matched = summary["matched_by_path"]?.intValue ?? rows.filter { $0.matchStatus == "matched" }.count
        missing = summary["missing_in_rekordbox"]?.intValue ?? rows.filter { $0.matchStatus == "missing" }.count
        ambiguous = summary["ambiguous_in_rekordbox"]?.intValue ?? rows.filter { $0.matchStatus == "ambiguous" }.count
    }

    var blockers: [RekordboxPlanRowView] {
        rows.filter {
            $0.action == "skip" &&
                ["missing", "ambiguous", "ambiguous_path", "duplicate_path"].contains($0.matchStatus)
        }
    }
}

enum RekordboxOperation: Sendable {
    case ensure
    case reset
    case inspect
    case plan
    case apply(dryRun: Bool)
}
