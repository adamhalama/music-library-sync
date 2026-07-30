import Foundation

struct FreeDLDefaults: Codable, Sendable {
    let planLimit: Int
    let downloadOrder: String
    let targetFormat: String
    let minMatchScore: Int
    let ambiguityGap: Int
    let replaceLimit: Int
    let commandTimeoutSeconds: Int

    enum CodingKeys: String, CodingKey {
        case planLimit = "plan_limit"
        case downloadOrder = "download_order"
        case targetFormat = "target_format"
        case minMatchScore = "min_match_score"
        case ambiguityGap = "ambiguity_gap"
        case replaceLimit = "replace_limit"
        case commandTimeoutSeconds = "command_timeout_seconds"
    }
}

struct FreeDLJob: Codable, Sendable, Identifiable {
    let id: String
    var enabled: Bool
    var sourceURL: String
    var libraryDir: String
    var bufferDir: String
    var backupDir: String
    var logDir: String
    var stateFile: String
    var planLimit: Int
    var downloadOrder: String
    var targetFormat: String
    var minMatchScore: Int
    var ambiguityGap: Int
    var replaceLimit: Int
    var applyPromotions: Bool

    enum CodingKeys: String, CodingKey {
        case id, enabled
        case sourceURL = "source_url"
        case libraryDir = "library_dir"
        case bufferDir = "buffer_dir"
        case backupDir = "backup_dir"
        case logDir = "log_dir"
        case stateFile = "state_file"
        case planLimit = "plan_limit"
        case downloadOrder = "download_order"
        case targetFormat = "target_format"
        case minMatchScore = "min_match_score"
        case ambiguityGap = "ambiguity_gap"
        case replaceLimit = "replace_limit"
        case applyPromotions = "apply_promotions"
    }
}

struct FreeDLConfig: Codable, Sendable {
    let version: Int
    let defaults: FreeDLDefaults
    var jobs: [FreeDLJob]
}

struct FreeDLConfigResult: Codable, Sendable {
    let path: String
    let config: FreeDLConfig
    let content: String
}

struct FreeDLConfigWriteParams: Codable, Sendable { let config: FreeDLConfig }

struct FreeDLQuality: Codable, Sendable {
    let codec: String?
    let bitrate: Int?
    let formatBitrate: Int?
    let effectiveBitrate: Int?
    let lossless: Bool
    let error: String?

    enum CodingKeys: String, CodingKey {
        case codec, bitrate
        case formatBitrate = "format_bitrate"
        case effectiveBitrate = "effective_bitrate"
        case lossless, error
    }
}

struct FreeDLProbe: Codable, Sendable {
    let status: String?
    let purchaseURL: String?
    let host: String?
    let error: String?

    enum CodingKeys: String, CodingKey {
        case status
        case purchaseURL = "purchase_url"
        case host, error
    }
}

struct FreeDLPlanRow: Codable, Sendable, Identifiable {
    var id: String { remoteID }
    let index: Int
    let remoteID: String
    let remoteURL: String?
    let title: String
    let localPath: String?
    let localQuality: FreeDLQuality
    let localState: String?
    let freeDLProbe: FreeDLProbe
    let selectable: Bool
    var selected: Bool
    let skipReason: String?
    let playlistMatch: String?
    let playlistTrackIndex: Int?

    enum CodingKeys: String, CodingKey {
        case index
        case remoteID = "remote_id"
        case remoteURL = "remote_url"
        case title
        case localPath = "local_path"
        case localQuality = "local_quality"
        case localState = "local_state"
        case freeDLProbe = "free_dl_probe"
        case selectable, selected
        case skipReason = "skip_reason"
        case playlistMatch = "playlist_match"
        case playlistTrackIndex = "playlist_track_index"
    }
}

struct FreeDLCapturePlan: Codable, Sendable {
    let runID: String
    let createdAt: Date
    let job: FreeDLJob
    var rows: [FreeDLPlanRow]
    let bufferRoot: String
    let logDir: String
    let playlistID: String?
    let playlistChecksum: String?

    enum CodingKeys: String, CodingKey {
        case runID = "run_id"
        case createdAt = "created_at"
        case job, rows
        case bufferRoot = "buffer_root"
        case logDir = "log_dir"
        case playlistID = "playlist_id"
        case playlistChecksum = "playlist_checksum"
    }
}

struct FreeDLPlanStartParams: Codable, Sendable {
    let jobID: String
    let planLimit: Int?
    let playlistID: String?
    let selectionOverrides: [String: Bool]

    enum CodingKeys: String, CodingKey {
        case jobID = "job_id"
        case planLimit = "plan_limit"
        case playlistID = "playlist_id"
        case selectionOverrides = "selection_overrides"
    }
}

struct FreeDLPlanEvent: Codable, Sendable {
    let kind: String
    let stage: String?
    let status: String?
    let detail: String?
    let row: FreeDLPlanRow?
    let plan: FreeDLCapturePlan?
    let error: String?
    let current: Int?
    let total: Int?
}

struct FreeDLPlanEventNotification: Codable, Sendable {
    let runID: String
    let event: FreeDLPlanEvent
    enum CodingKeys: String, CodingKey {
        case runID = "run_id"
        case event
    }
}

struct FreeDLCaptureStartParams: Codable, Sendable {
    let plan: FreeDLCapturePlan
    let selectedRemoteIDs: [String]
    enum CodingKeys: String, CodingKey {
        case plan
        case selectedRemoteIDs = "selected_remote_ids"
    }
}

struct FreeDLPromotionPlanBuildParams: Codable, Sendable {
    let jobID: String
    let captureRunID: String

    enum CodingKeys: String, CodingKey {
        case jobID = "job_id"
        case captureRunID = "capture_run_id"
    }
}

struct FreeDLPromotionApplyParams: Codable, Sendable {
    let plan: FreeDLPromotionPlan
}

struct FreeDLPromotionRow: Codable, Sendable, Identifiable {
    var id: String { libraryPath }
    let index: Int
    let libraryPath: String
    let freeDLPath: String
    let backupPath: String
    let outputPath: String
    let title: String
    let score: Int
    let originalQuality: FreeDLQuality
    let sourceQuality: FreeDLQuality
    let action: String
    let reason: String?
    var selected: Bool

    enum CodingKeys: String, CodingKey {
        case index
        case libraryPath = "library_path"
        case freeDLPath = "free_dl_path"
        case backupPath = "backup_path"
        case outputPath = "output_path"
        case title, score
        case originalQuality = "original_quality"
        case sourceQuality = "source_quality"
        case action, reason, selected
    }
}

struct FreeDLPromotionPlan: Codable, Sendable {
    let runID: String
    let createdAt: Date
    let job: FreeDLJob
    let targetFormat: String
    var rows: [FreeDLPromotionRow]
    let backupRoot: String
    let logDir: String

    enum CodingKeys: String, CodingKey {
        case runID = "run_id"
        case createdAt = "created_at"
        case job
        case targetFormat = "target_format"
        case rows
        case backupRoot = "backup_root"
        case logDir = "log_dir"
    }
}

enum FreeDLOperation: Sendable {
    case planning
    case capture
    case promotionBuild
    case promotionApply
}

enum FreeDLRowFilter: String, CaseIterable, Identifiable {
    case all
    case selectable
    case selected
    case blocked
    var id: String { rawValue }

    func includes(_ row: FreeDLPlanRow, overrides: [String: Bool]) -> Bool {
        switch self {
        case .all: true
        case .selectable: row.selectable
        case .selected: row.selectable && (overrides[row.remoteID] ?? row.selected)
        case .blocked: !row.selectable
        }
    }
}
