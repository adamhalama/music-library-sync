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
    @DefaultEmpty var sources: [SourceCapability]
}

struct SyncSourceOptions: Sendable {
    var selected = true
    var downloadOrder: DownloadOrder
    var planWindow: PlanWindow
}

/// C17 — `SyncStartParams` carries `ask_on_existing` *and* a separate
/// `ask_on_existing_set` flag, so "leave the decision to udl" is a third state
/// rather than a synonym for "never ask". The GUI used to hardcode both to
/// `false`, silently choosing on the user's behalf.
enum AskOnExistingPolicy: String, CaseIterable, Identifiable, Sendable {
    case backendDefault
    case ask
    case never

    var id: String { rawValue }

    var label: String {
        switch self {
        case .backendDefault: "udl decides"
        case .ask: "Ask me"
        case .never: "Never ask"
        }
    }

    /// The `ask_on_existing_set` flag: false leaves the choice to udl.
    var isSet: Bool { self != .backendDefault }
    /// The `ask_on_existing` value, meaningful only when `isSet`.
    var value: Bool { self == .ask }
}

/// C17 — `track_status` on the wire. `none` is spelled `off` here so it never
/// collides with `Optional.none` at a call site.
enum TrackStatusMode: String, Codable, CaseIterable, Identifiable, Sendable {
    case off = "none"
    case count
    case names

    var id: String { rawValue }

    var label: String {
        switch self {
        case .off: "Off"
        case .count: "Count only"
        case .names: "Track names"
        }
    }
}

/// The one place a sync run's defaults are written. `AppState` initialises its
/// published properties from here and `resetSyncAdvanced()` returns to here, so
/// an initialiser and a reset cannot drift apart — which is exactly how
/// `dryRun` once ended up claiming one default in a comment and another in code.
enum SyncDefaults {
    /// C1 — the plan exists only inside a run, so the run the app offers by
    /// default has to be the reversible one.
    static let dryRun = true
    static let unlimited = false
    static let planLimit = 50
    static let timeoutSeconds = 0
    // C17 — these five were hardcoded inside `AppState.startSync()` before the
    // redesign surfaced them. The values reproduce exactly what it used to send.
    static let planWindow: PlanWindow = .first
    static let askOnExisting: AskOnExistingPolicy = .backendDefault
    static let scanGaps = false
    static let noPreflight = false
    static let trackStatus: TrackStatusMode = .off
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
    let trackStatus: TrackStatusMode

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

    /// The short label the plan table and its filter both use.
    var statusLabel: String {
        switch status {
        case "missing_new": "New"
        case "missing_known_gap": "Gap"
        case "already_downloaded": "Have"
        default: status.replacingOccurrences(of: "_", with: " ")
        }
    }

    var statusSeverity: Severity {
        switch status {
        case "missing_new": .info
        case "missing_known_gap": .warn
        case "already_downloaded": .ok
        default: .idle
        }
    }

    /// Why a locked row cannot be queued. Shown when the user clicks the lock
    /// rather than letting the click do nothing.
    var lockReason: String {
        switch status {
        case "already_downloaded":
            "Already downloaded — udl will not re-queue a track its state file already records."
        default:
            "udl sent this row as not toggleable (status: \(status.replacingOccurrences(of: "_", with: " ")))."
        }
    }
}

/// The plan table's segmented filter, matching `sync-plan.html`.
enum PlanRowFilter: String, CaseIterable, Identifiable, Sendable {
    case all
    case new
    case gaps
    case have

    var id: String { rawValue }

    var label: String {
        switch self {
        case .all: "All"
        case .new: "New"
        case .gaps: "Gaps"
        case .have: "Have"
        }
    }

    func includes(_ row: PlanRow) -> Bool {
        switch self {
        case .all: true
        case .new: row.status == "missing_new"
        case .gaps: row.status == "missing_known_gap"
        case .have: row.status == "already_downloaded"
        }
    }
}

struct SelectRowsParams: Codable, Sendable {
    let runID: String
    let sourceID: String
    // A source udl planned to nothing still asks, and sends no rows at all.
    @DefaultEmpty var rows: [PlanRow]
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
    @DefaultEmpty var rows: [TrackRow]
    @DefaultEmpty var activity: [ActivityEntry]

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
    /// C2 — the sources sent to `sync.start`, in order. udl plans them one at a
    /// time, so this is the only thing that makes "Source 2 of 4" honest: the
    /// sources that have not been reached yet emit no events at all.
    var requestedSourceIDs: [String] = []
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
