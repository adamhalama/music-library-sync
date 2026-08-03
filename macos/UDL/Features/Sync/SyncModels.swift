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

/// Pure source-order projection used while Go is blocked waiting for the plan
/// reply. The backend manifest remains authoritative after acceptance; this
/// gives the editable table the same execution slots without reordering rows.
struct PlanQueueProjection: Sendable, Equatable {
    private let slotsBySourceIndex: [Int: Int]

    init(rows: [PlanRow], selectedIndices: Set<Int>, downloadOrder: DownloadOrder) {
        let selectedCount = rows.reduce(into: 0) { count, row in
            if row.toggleable && selectedIndices.contains(row.index) {
                count += 1
            }
        }
        var slots = Dictionary<Int, Int>(minimumCapacity: rows.count)
        var selectedOffset = 0
        for row in rows {
            guard row.toggleable, selectedIndices.contains(row.index) else {
                slots[row.index] = 0
                continue
            }
            selectedOffset += 1
            slots[row.index] = downloadOrder == .newestFirst
                ? selectedOffset
                : selectedCount - selectedOffset + 1
        }
        slotsBySourceIndex = slots
    }

    func executionSlot(forSourceIndex index: Int) -> Int {
        slotsBySourceIndex[index] ?? 0
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

    func merging(_ row: TrackRow) -> SourceSnapshot {
        var merged = rows
        if let index = merged.firstIndex(where: { $0.id == row.id }) {
            merged[index] = row
        } else {
            merged.append(row)
        }
        return SourceSnapshot(
            lifecycle: lifecycle,
            confirmed: confirmed,
            rows: merged,
            activity: activity
        )
    }
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

    enum CodingKeys: String, CodingKey {
        case runID = "run_id"
        case event, source
    }
}

struct SyncProgressNotification: Codable, Sendable {
    let runID: String
    let sourceID: String
    let progress: StructuredProgressSnapshot
    let row: TrackRow?

    enum CodingKeys: String, CodingKey {
        case runID = "run_id"
        case sourceID = "source_id"
        case progress, row
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

enum SyncSourceFocus {
    /// A source asking for input always wins. Otherwise an explicit sidebar
    /// choice stays put; only an implicit selection follows active work.
    static func target(
        current: String?,
        selectionIsExplicit: Bool,
        pendingInput: String?,
        active: String?
    ) -> String? {
        if let pendingInput, !pendingInput.isEmpty { return pendingInput }
        if selectionIsExplicit { return current }
        if let active, !active.isEmpty { return active }
        return current
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
    var sourceTables: [String: SyncSourceTableState] = [:]
    var activity: [OutputEvent] = []
    var progress: StructuredProgressSnapshot?
    var terminalMessage: String?
    var exitCode: Int?
    /// Stop is meaningful before `sync.start` returns its run ID. These flags
    /// make that request durable across the response race and keep the actual
    /// `run.cancel` send exactly-once.
    var cancelRequested = false
    var cancelRequestInFlight = false
    var cancelAcknowledged = false
    var stillStopping = false

    /// Returns true only for the first request (or a retry after a request
    /// failure). Repeated Stop actions while a request is queued or in flight
    /// are intentionally idempotent.
    mutating func requestCancellation() -> Bool {
        guard phase.isActive, !cancelRequested, !cancelRequestInFlight, !cancelAcknowledged else {
            return false
        }
        cancelRequested = true
        stillStopping = false
        phase = .canceling
        terminalMessage = "Stopping… completed tracks stay saved."
        return true
    }

    /// Records a start/event run ID without allowing a late response to revive
    /// a canceling or terminal run.
    mutating func registerRunID(_ id: String) {
        runID = id
        if phase == .starting {
            phase = .running
        }
    }

    mutating func beginCancellationRequest() -> Bool {
        guard cancelRequested, runID != nil, !cancelRequestInFlight, !cancelAcknowledged else {
            return false
        }
        cancelRequestInFlight = true
        return true
    }

    mutating func acknowledgeCancellation() {
        cancelRequestInFlight = false
        cancelAcknowledged = true
        if exitCode == nil {
            terminalMessage = "Stop acknowledged. Waiting for the run to finish…"
        }
    }

    mutating func failCancellation(_ message: String) {
        cancelRequested = false
        cancelRequestInFlight = false
        cancelAcknowledged = false
        stillStopping = false
        guard exitCode == nil else { return }
        phase = .running
        terminalMessage = "Stop failed: \(message) Try Stop again."
    }

    mutating func markStillStopping() {
        guard phase == .canceling, exitCode == nil else { return }
        stillStopping = true
        terminalMessage = "Still stopping… completed tracks remain saved."
    }

    /// Applies the protocol's canonical terminal mapping without discarding
    /// the run's retained source tables. This is deliberately pure model logic
    /// so exit 130 and every other terminal class can be validated without an
    /// app process or a live backend connection.
    mutating func finish(exitCode: Int, error: String?) {
        self.exitCode = exitCode
        terminalMessage = error
        phase = switch exitCode {
        case 0: .succeeded
        case 4: .dependencyFailure
        case 5: .partialFailure
        case 130: .canceled
        default: .failed
        }
        finalizeSourceTables()
        if terminalMessage == nil {
            terminalMessage = switch phase {
            case .succeeded: "Sync completed successfully."
            case .canceled: "Sync canceled. Completed tracks remain saved."
            default: "Sync finished with exit code \(exitCode)."
            }
        }
    }

    /// Makes every retained source table terminal when a run ends before the
    /// backend emits a final per-source snapshot (notably cancellation while
    /// Go is blocked on a plan reply).
    mutating func finalizeSourceTables() {
        for sourceID in sourceTables.keys {
            guard var table = sourceTables[sourceID],
                  ["planning", "queued", "running"].contains(table.lifecycle) else { continue }
            let lifecycle = switch phase {
            case .succeeded: "finished"
            case .canceled: table.accepted ? "canceled" : "not_run"
            case .partialFailure, .dependencyFailure, .failed: "failed"
            default: table.lifecycle
            }
            table.lifecycle = lifecycle
            sourceTables[sourceID] = table
            if let snapshot = sources[sourceID] {
                sources[sourceID] = SourceSnapshot(
                    lifecycle: lifecycle,
                    confirmed: snapshot.confirmed,
                    rows: snapshot.rows,
                    activity: snapshot.activity
                )
            }
        }
    }

    mutating func installPlan(
        _ params: SelectRowsParams,
        selectedIndices: Set<Int>
    ) {
        sourceTables[params.sourceID] = SyncSourceTableState(
            sourceID: params.sourceID,
            planRows: params.rows,
            selectedIndices: selectedIndices,
            downloadOrder: params.downloadOrder,
            planWindow: params.planWindow,
            accepted: false,
            hasTrackPlan: true,
            lifecycle: "planning"
        )
    }

    mutating func updateDraft(
        sourceID: String,
        selectedIndices: Set<Int>? = nil,
        downloadOrder: DownloadOrder? = nil,
        planWindow: PlanWindow? = nil
    ) {
        guard var table = sourceTables[sourceID], !table.accepted else { return }
        if let selectedIndices { table.selectedIndices = selectedIndices }
        if let downloadOrder { table.downloadOrder = downloadOrder }
        if let planWindow { table.planWindow = planWindow }
        table.rebuildProjectedRows()
        sourceTables[sourceID] = table
    }

    mutating func acceptPlan(sourceID: String) {
        guard var table = sourceTables[sourceID] else { return }
        table.accepted = true
        table.lifecycle = "queued"
        table.rebuildProjectedRows()
        sourceTables[sourceID] = table
    }

    mutating func mergeSnapshot(sourceID: String, snapshot: SourceSnapshot) {
        sources[sourceID] = snapshot
        var table = sourceTables[sourceID] ?? SyncSourceTableState(
            sourceID: sourceID,
            planRows: [],
            selectedIndices: [],
            downloadOrder: .oldestFirst,
            planWindow: .first,
            accepted: true,
            hasTrackPlan: !snapshot.rows.isEmpty,
            lifecycle: snapshot.lifecycle
        )
        table.rows = snapshot.rows
        table.lifecycle = snapshot.lifecycle
        table.accepted = table.accepted || snapshot.confirmed
        table.hasTrackPlan = table.hasTrackPlan || !snapshot.rows.isEmpty
        sourceTables[sourceID] = table
    }

    mutating func mergeProgressRow(sourceID: String, row: TrackRow) {
        let existing = sources[sourceID] ?? SourceSnapshot(
            lifecycle: "running", confirmed: true, rows: [], activity: []
        )
        let merged = existing.merging(row)
        sources[sourceID] = merged
        var table = sourceTables[sourceID] ?? SyncSourceTableState(
            sourceID: sourceID,
            planRows: [],
            selectedIndices: [],
            downloadOrder: .oldestFirst,
            planWindow: .first,
            accepted: true,
            hasTrackPlan: true,
            lifecycle: merged.lifecycle
        )
        if let index = table.rows.firstIndex(where: { $0.id == row.id }) {
            table.rows[index] = row
        } else {
            table.rows.append(row)
        }
        table.lifecycle = merged.lifecycle
        table.accepted = true
        table.hasTrackPlan = true
        sourceTables[sourceID] = table
    }

    @discardableResult
    mutating func applyProgress(_ notification: SyncProgressNotification) -> Bool {
        guard phase != .canceling else { return false }
        progress = notification.progress
        if let row = notification.row {
            mergeProgressRow(sourceID: notification.sourceID, row: row)
        }
        return true
    }
}

struct SyncSourceTableState: Sendable {
    let sourceID: String
    var planRows: [PlanRow]
    var selectedIndices: Set<Int>
    var downloadOrder: DownloadOrder
    var planWindow: PlanWindow
    var accepted: Bool
    var hasTrackPlan: Bool
    var lifecycle: String
    var rows: [TrackRow] = []

    init(
        sourceID: String,
        planRows: [PlanRow],
        selectedIndices: Set<Int>,
        downloadOrder: DownloadOrder,
        planWindow: PlanWindow,
        accepted: Bool,
        hasTrackPlan: Bool,
        lifecycle: String
    ) {
        self.sourceID = sourceID
        self.planRows = planRows
        self.selectedIndices = selectedIndices
        self.downloadOrder = downloadOrder
        self.planWindow = planWindow
        self.accepted = accepted
        self.hasTrackPlan = hasTrackPlan
        self.lifecycle = lifecycle
        rebuildProjectedRows()
    }

    mutating func rebuildProjectedRows() {
        guard !planRows.isEmpty else { return }
        let projection = PlanQueueProjection(
            rows: planRows,
            selectedIndices: selectedIndices,
            downloadOrder: downloadOrder
        )
        rows = planRows.map { row in
            let selected = row.toggleable && selectedIndices.contains(row.index)
            let runtimeStatus = selected ? "queued" : "idle"
            let label = !row.toggleable ? "have-it" : selected ? "pending" : "not-run"
            let planClass = switch row.status {
            case "missing_known_gap": "gap"
            case "already_downloaded": "have"
            default: "new"
            }
            return TrackRow(
                sourceID: sourceID,
                sourceLabel: sourceID,
                remoteID: row.remoteID,
                title: row.title,
                index: row.index,
                executionSlot: projection.executionSlot(forSourceIndex: row.index),
                toggleable: row.toggleable,
                planStatus: row.status,
                planClass: planClass,
                selected: selected,
                runScope: !row.toggleable ? "locked" : selected ? "included" : "excluded",
                runtimeStatus: runtimeStatus,
                statusLabel: label,
                failureDetail: nil,
                progressKnown: false,
                progressPercent: 0
            )
        }
    }
}

enum SyncRowFilter: String, CaseIterable, Identifiable {
    case all
    case inRun = "in run"
    case remaining
    case downloaded
    case skipped
    case failed
    case have
    var id: String { rawValue }

    func includes(_ row: TrackRow) -> Bool {
        switch self {
        case .all: true
        case .inRun: row.runScope == "included"
        case .remaining: row.runScope == "included" && ["idle", "queued", "downloading"].contains(row.runtimeStatus)
        case .downloaded: row.runtimeStatus == "downloaded"
        case .skipped: row.runtimeStatus == "skipped"
        case .failed: row.runtimeStatus == "failed"
        case .have: row.runScope == "locked"
        }
    }
}
