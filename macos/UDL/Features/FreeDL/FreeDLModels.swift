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
    // `freedl.config.read` returns null here until the first job is defined.
    @DefaultEmpty var jobs: [FreeDLJob]
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
    @DefaultEmpty var rows: [FreeDLPlanRow]
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
    @DefaultEmpty var rows: [FreeDLPromotionRow]
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

    var label: String {
        switch self {
        case .all: "All"
        case .selectable: "Upgrades"
        case .selected: "Selected"
        case .blocked: "Blocked"
        }
    }

    func includes(_ row: FreeDLPlanRow, overrides: [String: Bool]) -> Bool {
        switch self {
        case .all: true
        case .selectable: row.selectable
        case .selected: row.selectable && (overrides[row.remoteID] ?? row.selected)
        case .blocked: !row.selectable
        }
    }
}

// MARK: - C7 — the four RPCs, as four steps

/// C7 — `freedl.plan.start`, `freedl.capture.start`,
/// `freedl.promotionPlan.build` and `freedl.promote.apply` are four separate
/// backend runs with four run IDs. The mockup draws three steps; there are
/// four, so there are four here. Nothing here advances on its own: the phase is
/// view `@State` that only a click changes.
enum FreeDLPhase: Int, CaseIterable, Identifiable, Sendable {
    case plan
    case capture
    case promotionPlan
    case promote

    var id: Int { rawValue }
    var number: Int { rawValue + 1 }

    var title: String {
        switch self {
        case .plan: "Plan"
        case .capture: "Capture"
        case .promotionPlan: "Promotion plan"
        case .promote: "Promote"
        }
    }

    /// The single RPC this step calls. Named on screen so the four run IDs are
    /// never mistaken for one workflow the backend is driving.
    var method: String {
        switch self {
        case .plan: "freedl.plan.start"
        case .capture: "freedl.capture.start"
        case .promotionPlan: "freedl.promotionPlan.build"
        case .promote: "freedl.promote.apply"
        }
    }

    var actionLabel: String {
        switch self {
        case .plan: "Build capture plan"
        case .capture: "Capture selected"
        case .promotionPlan: "Build promotion plan"
        case .promote: "Promote…"
        }
    }

    /// What this step writes, stated before it is run.
    var effect: String {
        switch self {
        case .plan: "Reads SoundCloud and your library. Writes nothing."
        case .capture: "Downloads into the buffer directory. Your library is untouched."
        case .promotionPlan: "Matches captured files to library files. Writes nothing."
        case .promote: "Replaces library files, after copying the originals to the backup directory."
        }
    }
}

// MARK: - Row presentation

extension FreeDLQuality {
    /// `—` is reserved for "the backend reported no quality at all". A probe
    /// error says so rather than rendering as an absent value.
    var summary: String {
        if let error, !error.isEmpty { return "probe failed" }
        var parts: [String] = []
        if let codec, !codec.isEmpty { parts.append(codec) }
        if lossless {
            parts.append("lossless")
        } else if let rate = [effectiveBitrate, bitrate, formatBitrate].compactMap({ $0 }).first(where: { $0 > 0 }) {
            parts.append("\(rate / 1000)k")
        }
        return parts.isEmpty ? "—" : parts.joined(separator: " ")
    }

    /// Under 320 kbps is the mockup's `.q.low` tint. Lossless is `.q.hi`.
    var severity: Severity {
        if let error, !error.isEmpty { return .warn }
        if lossless { return .ok }
        guard let rate = [effectiveBitrate, bitrate, formatBitrate].compactMap({ $0 }).first(where: { $0 > 0 }) else {
            return .idle
        }
        return rate < 320_000 ? .warn : .ok
    }
}

extension FreeDLPlanRow {
    /// C8 — `local_state` passes through `pending`, `matching` and `probing`
    /// before it means anything. Those are "still checking", not "nothing
    /// found", and the cell must be able to tell them apart.
    var localResolved: Bool {
        guard let localState, !localState.isEmpty else { return false }
        return !["pending", "matching", "probing"].contains(localState)
    }

    var localLabel: String {
        switch localState {
        case "matched", "cached": localQuality.summary
        case "not_found": "not in library"
        case "error": "lookup failed"
        default: localQuality.summary
        }
    }

    var localSeverity: Severity {
        switch localState {
        case "not_found": .idle
        case "error": .warn
        default: localQuality.summary == "—" ? .idle : localQuality.severity
        }
    }

    /// C8 — an empty probe status means the availability check has not returned
    /// for this row yet.
    var freeDLResolved: Bool {
        guard let status = freeDLProbe.status else { return false }
        return !status.isEmpty
    }

    var freeDLLabel: String {
        switch freeDLProbe.status {
        case "available": freeDLProbe.host.map { "available · \($0)" } ?? "available"
        case "no_free_dl": "no free DL"
        case "unsupported_host": "unsupported host"
        case "lookup_failed": "lookup failed"
        case let other?: other.replacingOccurrences(of: "_", with: " ")
        case nil: ""
        }
    }

    var freeDLSeverity: Severity {
        switch freeDLProbe.status {
        case "available": .ok
        case "lookup_failed": .warn
        default: .idle
        }
    }

    /// C8 — rows are emitted before their probes return, so `selectable: false`
    /// on a streaming row means "not decided yet", not "blocked".
    ///
    /// `recomputeSelectable` in `internal/freedl/progressive.go` is what
    /// finally writes a skip reason, and it only runs once the source plan has
    /// been enumerated. Until then a row can be `selectable: false` with an
    /// *available* probe and no reason at all — udl has simply not decided.
    /// The absence of a reason is therefore the signal: a genuinely blocked
    /// row always carries one.
    var isStillResolving: Bool {
        guard !selectable else { return false }
        guard let skipReason, !skipReason.isEmpty else { return true }
        return skipReason == "pending" || !freeDLResolved
    }

    /// The `skip_reason` vocabulary `internal/freedl/progressive.go` writes,
    /// turned into the sentence a locked row states when you click it.
    var lockReason: String {
        if isStillResolving {
            return "udl has not finished checking this row. Its Free DL availability has not come back yet."
        }
        switch skipReason {
        case "already-present": return "Your local copy is already at or above the free download quality."
        case "not-in-playlist": return "This track is not in the playlist this plan was scoped to."
        case "playlist-ambiguous": return "This track matches more than one playlist entry, so udl will not guess."
        case "no_free_dl": return "This track has no Free DL link on SoundCloud."
        case "unsupported_host": return "The Free DL link points at a host udl cannot capture from."
        case "lookup_failed": return "The Free DL availability lookup failed for this track."
        case let other? where !other.isEmpty: return other.replacingOccurrences(of: "-", with: " ")
        default: return "udl marked this row as not capturable."
        }
    }

    var statusLabel: String {
        if selectable { return "Upgrade" }
        if isStillResolving { return "Checking" }
        switch skipReason {
        case "already-present": return "Have"
        case "no_free_dl", "unsupported_host": return "No free DL"
        default: return "Blocked"
        }
    }

    var statusSeverity: Severity {
        if selectable { return .ok }
        if isStillResolving { return .info }
        return skipReason == "already-present" ? .idle : .warn
    }
}

extension FreeDLPromotionRow {
    var actionLabel: String {
        switch action {
        case "copy-audio": "Copy audio"
        case "encode-aac": "Encode AAC"
        case "encode-mp3": "Encode MP3"
        case "encode-wav": "Encode WAV"
        case "skip": "Skip"
        default: action
        }
    }

    var isApplicable: Bool { action != "skip" }

    /// `internal/freedl` never applies a `skip` row, so a checkbox on one would
    /// be a control that does nothing.
    var lockReason: String {
        if let reason, !reason.isEmpty {
            return reason == "replace-limit"
                ? "The job's replace limit was reached before this row."
                : "udl will not promote this row: \(reason)."
        }
        return "udl marked this row as skip, so it is never written."
    }
}
