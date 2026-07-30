import Foundation

enum DownloadOrder: String, Codable, CaseIterable, Sendable, Identifiable {
    case newestFirst = "newest_first"
    case oldestFirst = "oldest_first"
    var id: String { rawValue }
    var label: String { self == .newestFirst ? "Newest first" : "Oldest first" }
}

enum PlanWindow: String, Codable, CaseIterable, Sendable, Identifiable {
    case first
    case latest
    var id: String { rawValue }
    var label: String { rawValue.capitalized }
}

struct SourceCapability: Codable, Sendable, Identifiable {
    var id: String { sourceID }
    let sourceID: String
    let sourceType: String
    let adapter: String
    let supportsPlan: Bool
    let supportsPlanWindow: Bool
    let supportsDownloadOrder: Bool
    let defaultPlanWindow: PlanWindow
    let defaultDownloadOrder: DownloadOrder

    enum CodingKeys: String, CodingKey {
        case sourceID = "source_id"
        case sourceType = "source_type"
        case adapter
        case supportsPlan = "supports_plan"
        case supportsPlanWindow = "supports_plan_window"
        case supportsDownloadOrder = "supports_download_order"
        case defaultPlanWindow = "default_plan_window"
        case defaultDownloadOrder = "default_download_order"
    }
}

struct SourceCapabilitiesResult: Codable, Sendable {
    let sources: [SourceCapability]
}

struct SyncSourceOptions: Sendable {
    var selected = true
    var downloadOrder: DownloadOrder
    var planWindow: PlanWindow
}

struct SyncStartParams: Codable, Sendable {
    let sourceIDs: [String]
    let dryRun: Bool
    let timeoutSeconds: Int
    let plan: Bool
    let planLimit: Int
    let planWindow: PlanWindow
    let planWindowBySource: [String: PlanWindow]
    let downloadOrderBySource: [String: DownloadOrder]
    let askOnExisting: Bool
    let askOnExistingSet: Bool
    let scanGaps: Bool
    let noPreflight: Bool
    let trackStatus: String

    enum CodingKeys: String, CodingKey {
        case sourceIDs = "source_ids"
        case dryRun = "dry_run"
        case timeoutSeconds = "timeout_seconds"
        case plan
        case planLimit = "plan_limit"
        case planWindow = "plan_window"
        case planWindowBySource = "plan_window_by_source"
        case downloadOrderBySource = "download_order_by_source"
        case askOnExisting = "ask_on_existing"
        case askOnExistingSet = "ask_on_existing_set"
        case scanGaps = "scan_gaps"
        case noPreflight = "no_preflight"
        case trackStatus = "track_status"
    }
}

struct PlanSourceDetails: Codable, Sendable {
    let sourceID: String
    let sourceType: String
    let adapter: String
    let url: String
    let targetDir: String
    let stateFile: String
    let planLimit: Int
    let planWindow: PlanWindow
    let dryRun: Bool

    enum CodingKeys: String, CodingKey {
        case sourceID = "source_id"
        case sourceType = "source_type"
        case adapter, url
        case targetDir = "target_dir"
        case stateFile = "state_file"
        case planLimit = "plan_limit"
        case planWindow = "plan_window"
        case dryRun = "dry_run"
    }
}

struct PlanRow: Codable, Sendable, Identifiable {
    var id: String {
        if !remoteID.isEmpty { return remoteID }
        if !remoteURL.isEmpty { return remoteURL }
        return "row-\(index)"
    }
    let index: Int
    let remoteID: String
    let remoteURL: String
    let title: String
    let status: String
    let toggleable: Bool
    let selectedByDefault: Bool

    enum CodingKeys: String, CodingKey {
        case index
        case remoteID = "remote_id"
        case remoteURL = "remote_url"
        case title, status, toggleable
        case selectedByDefault = "selected_by_default"
    }
}

struct SelectRowsParams: Codable, Sendable {
    let runID: String
    let sourceID: String
    let rows: [PlanRow]
    let details: PlanSourceDetails
    let downloadOrder: DownloadOrder
    let planWindow: PlanWindow

    enum CodingKeys: String, CodingKey {
        case runID = "run_id"
        case sourceID = "source_id"
        case rows, details
        case downloadOrder = "download_order"
        case planWindow = "plan_window"
    }
}

struct SelectRowsResult: Codable, Sendable {
    let selectedIndices: [Int]
    let downloadOrder: DownloadOrder
    let canceled: Bool
    let rebuild: Bool
    let planWindow: PlanWindow

    enum CodingKeys: String, CodingKey {
        case selectedIndices = "selected_indices"
        case downloadOrder = "download_order"
        case canceled, rebuild
        case planWindow = "plan_window"
    }
}

struct TrackRow: Codable, Sendable, Identifiable {
    var id: String { "\(sourceID):\(index)" }
    let sourceID: String
    let sourceLabel: String
    let remoteID: String
    let title: String
    let index: Int
    let executionSlot: Int
    let toggleable: Bool
    let planStatus: String
    let planClass: String
    let selected: Bool
    let runScope: String
    let runtimeStatus: String
    let statusLabel: String
    let failureDetail: String?
    let progressKnown: Bool
    let progressPercent: Double

    enum CodingKeys: String, CodingKey {
        case sourceID = "source_id"
        case sourceLabel = "source_label"
        case remoteID = "remote_id"
        case title, index
        case executionSlot = "execution_slot"
        case toggleable
        case planStatus = "plan_status"
        case planClass = "plan_class"
        case selected
        case runScope = "run_scope"
        case runtimeStatus = "runtime_status"
        case statusLabel = "status_label"
        case failureDetail = "failure_detail"
        case progressKnown = "progress_known"
        case progressPercent = "progress_percent"
    }
}

struct ActivityEntry: Codable, Sendable, Identifiable {
    var id: String { "\(timestamp.timeIntervalSince1970):\(sourceID):\(message)" }
    let timestamp: Date
    let level: String
    let message: String
    let sourceID: String

    enum CodingKeys: String, CodingKey {
        case timestamp, level, message
        case sourceID = "source_id"
    }
}

struct SourceSnapshot: Codable, Sendable {
    let lifecycle: String
    let confirmed: Bool
    let rows: [TrackRow]
    let activity: [ActivityEntry]

    var includedCount: Int { rows.filter { $0.runScope == "included" }.count }
    var downloadedCount: Int { rows.filter { $0.runtimeStatus == "downloaded" }.count }
    var skippedCount: Int { rows.filter { $0.runtimeStatus == "skipped" }.count }
    var failedCount: Int { rows.filter { $0.runtimeStatus == "failed" }.count }
}

struct StructuredProgressSnapshot: Codable, Sendable {
    let progress: ProgressModel
    let track: StructuredTrackState
    let structuredTrackEvents: Bool

    enum CodingKeys: String, CodingKey {
        case progress, track
        case structuredTrackEvents = "structured_track_events"
    }
}

struct ProgressModel: Codable, Sendable {
    let source: SourceProgress
    let track: TrackProgress
    let global: GlobalProgress
}

struct SourceProgress: Codable, Sendable {
    let id: String
    let lifecycle: String
    let plannedTotal: Int
    let itemTotal: Int
    let itemIndex: Int
    let completed: Int

    enum CodingKeys: String, CodingKey {
        case id, lifecycle
        case plannedTotal = "planned_total"
        case itemTotal = "item_total"
        case itemIndex = "item_index"
        case completed
    }
}

struct TrackProgress: Codable, Sendable {
    let name: String
    let lifecycle: String
    let progressPercent: Double

    enum CodingKeys: String, CodingKey {
        case name, lifecycle
        case progressPercent = "progress_percent"
    }
}

struct GlobalProgress: Codable, Sendable {
    let total: Int
    let completed: Int
}

struct StructuredTrackState: Codable, Sendable {
    let name: String
    let progressKnown: Bool
    let progressPercent: Double
    let lifecycle: String

    enum CodingKeys: String, CodingKey {
        case name
        case progressKnown = "progress_known"
        case progressPercent = "progress_percent"
        case lifecycle
    }
}

struct SyncEventNotification: Codable, Sendable {
    let runID: String
    let event: OutputEvent
    let source: SourceSnapshot
    let progress: StructuredProgressSnapshot

    enum CodingKeys: String, CodingKey {
        case runID = "run_id"
        case event, source, progress
    }
}

enum SyncRunPhase: String, Sendable {
    case idle
    case starting
    case running
    case canceling
    case succeeded
    case partialFailure
    case dependencyFailure
    case failed
    case canceled

    var isActive: Bool {
        self == .starting || self == .running || self == .canceling
    }
}

struct SyncRunState: Sendable {
    var runID: String?
    var phase: SyncRunPhase = .idle
    var sources: [String: SourceSnapshot] = [:]
    var activity: [OutputEvent] = []
    var progress: StructuredProgressSnapshot?
    var terminalMessage: String?
    var exitCode: Int?
}

enum SyncRowFilter: String, CaseIterable, Identifiable {
    case all
    case inRun = "in run"
    case remaining
    case downloaded
    case skipped
    case failed
    var id: String { rawValue }

    func includes(_ row: TrackRow) -> Bool {
        switch self {
        case .all: true
        case .inRun: row.runScope == "included"
        case .remaining: row.runScope == "included" && ["idle", "queued", "downloading"].contains(row.runtimeStatus)
        case .downloaded: row.runtimeStatus == "downloaded"
        case .skipped: row.runtimeStatus == "skipped"
        case .failed: row.runtimeStatus == "failed"
        }
    }
}
