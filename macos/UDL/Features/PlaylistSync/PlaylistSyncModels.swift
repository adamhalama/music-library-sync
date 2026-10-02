import Foundation

struct PlaylistSyncPairState: Codable, Sendable {
    let version: Int
    let jobID: String
    let configFingerprint: String
    let rekordbox: PlaylistSyncBinding
    let navidrome: PlaylistSyncBinding
    let lastDirection: String
    let lastVerifiedAt: Date
    let finalPathChecksum: String
    let checksumSHA256: String

    enum CodingKeys: String, CodingKey {
        case version
        case jobID = "job_id"
        case configFingerprint = "config_fingerprint"
        case rekordbox, navidrome
        case lastDirection = "last_direction"
        case lastVerifiedAt = "last_verified_at"
        case finalPathChecksum = "final_path_checksum"
        case checksumSHA256 = "checksum_sha256"
    }
}

struct PlaylistSyncBinding: Codable, Sendable {
    let playlistID: String
    let playlistName: String
    enum CodingKeys: String, CodingKey {
        case playlistID = "playlist_id"
        case playlistName = "playlist_name"
    }
}

struct PlaylistSyncInspectRow: Codable, Sendable, Identifiable {
    var id: String { job.id }
    let job: PlaylistSyncJob
    let state: PlaylistSyncPairState?
    let stateError: String?
    enum CodingKeys: String, CodingKey {
        case job, state
        case stateError = "state_error"
    }
}

struct PlaylistSyncInspectResult: Codable, Sendable {
    @DefaultEmpty var jobs: [PlaylistSyncInspectRow]
}

struct PlaylistSyncDescriptor: Codable, Sendable {
    let provider: String
    let id: String?
    let name: String
    let owner: String?
    let smart: Bool?
    let trackCount: Int
    enum CodingKeys: String, CodingKey {
        case provider, id, name, owner, smart
        case trackCount = "track_count"
    }
}

struct PlaylistSyncSummary: Codable, Sendable {
    let sourceTotal: Int
    let destinationTotal: Int
    let finalTotal: Int
    let willAdd: Int
    let willRemove: Int
    let willMove: Int
    let willKeep: Int
    let blocked: Int
    enum CodingKeys: String, CodingKey {
        case sourceTotal = "source_total"
        case destinationTotal = "destination_total"
        case finalTotal = "final_total"
        case willAdd = "will_add"
        case willRemove = "will_remove"
        case willMove = "will_move"
        case willKeep = "will_keep"
        case blocked
    }
}

struct PlaylistSyncPlanRow: Codable, Sendable, Identifiable {
    var id: String { "\(sourceIndex ?? 0):\(destinationIndex ?? 0):\(sourceProviderID ?? ""):\(destinationProviderID ?? ""):\(action)" }
    let sourceIndex: Int?
    let destinationIndex: Int?
    let artist: String?
    let title: String
    let album: String?
    let duration: String?
    let rawPath: String
    let normalizedPath: String
    let sourceProviderID: String?
    let destinationProviderID: String?
    let action: String
    let blocker: String?

    enum CodingKeys: String, CodingKey {
        case sourceIndex = "source_index"
        case destinationIndex = "destination_index"
        case artist, title, album, duration
        case rawPath = "raw_path"
        case normalizedPath = "normalized_path"
        case sourceProviderID = "source_provider_id"
        case destinationProviderID = "destination_provider_id"
        case action, blocker
    }

    var actionLabel: String {
        switch action {
        case "add": "Add"
        case "remove": "Remove"
        case "move": "Move"
        case "keep": "Keep"
        case "blocked": "Blocked"
        default: action.capitalized
        }
    }
}

struct PlaylistSyncPreconditions: Codable, Sendable {
    @DefaultEmpty var sourceProviderIDs: [String]
    @DefaultEmpty var sourceNormalizedPaths: [String]
    @DefaultEmpty var destinationProviderIDs: [String]
    @DefaultEmpty var destinationNormalizedPaths: [String]
    @DefaultEmpty var finalDestinationProviderIDs: [String]
    @DefaultEmpty var matched: [PlaylistSyncMatchedPair]
    enum CodingKeys: String, CodingKey {
        case sourceProviderIDs = "source_provider_ids"
        case sourceNormalizedPaths = "source_normalized_paths"
        case destinationProviderIDs = "destination_provider_ids"
        case destinationNormalizedPaths = "destination_normalized_paths"
        case finalDestinationProviderIDs = "final_destination_provider_ids"
        case matched
    }
}

struct PlaylistSyncMatchedPair: Codable, Sendable {
    let normalizedPath: String
    let sourceProviderID: String
    let destinationProviderID: String
    enum CodingKeys: String, CodingKey {
        case normalizedPath = "normalized_path"
        case sourceProviderID = "source_provider_id"
        case destinationProviderID = "destination_provider_id"
    }
}

struct PlaylistSyncPlan: Codable, Sendable {
    let version: Int
    // Checksummed Go time.Time text must survive nanosecond precision unchanged.
    let generatedAt: String
    let jobID: String
    let configFingerprint: String
    let direction: String
    let source: PlaylistSyncDescriptor
    let destination: PlaylistSyncDescriptor
    let destinationCreate: Bool
    let summary: PlaylistSyncSummary
    @DefaultEmpty var rows: [PlaylistSyncPlanRow]
    @DefaultEmpty var blockers: [String]
    let preconditions: PlaylistSyncPreconditions
    let checksumSHA256: String
    enum CodingKeys: String, CodingKey {
        case version
        case generatedAt = "generated_at"
        case jobID = "job_id"
        case configFingerprint = "config_fingerprint"
        case direction, source, destination
        case destinationCreate = "destination_create"
        case summary, rows, blockers, preconditions
        case checksumSHA256 = "checksum_sha256"
    }
}

struct PlaylistSyncPlanResult: Codable, Sendable {
    let plan: PlaylistSyncPlan
    let planPath: String
    enum CodingKeys: String, CodingKey { case plan; case planPath = "plan_path" }
}

struct PlaylistSyncPlanParams: Codable, Sendable {
    let jobID: String
    let direction: String
    let outPath: String?
    enum CodingKeys: String, CodingKey {
        case jobID = "job_id"
        case direction
        case outPath = "out_path"
    }
}

struct PlaylistSyncApplyParams: Codable, Sendable {
    let plan: PlaylistSyncPlan
    let dryRun: Bool
    enum CodingKeys: String, CodingKey { case plan; case dryRun = "dry_run" }
}

struct PlaylistSyncApplyResult: Codable, Sendable {
    let dryRun: Bool
    let noOp: Bool
    let backupPath: String?
    let pairStatePath: String?
    enum CodingKeys: String, CodingKey {
        case dryRun = "dry_run"
        case noOp = "no_op"
        case backupPath = "backup_path"
        case pairStatePath = "pair_state_path"
    }
}

enum PlaylistSyncOperation: Sendable {
    case plan
    case apply(dryRun: Bool)
}

enum PlaylistSyncRowFilter: String, CaseIterable, Identifiable {
    case all, add, remove, move, keep, blocked
    var id: String { rawValue }
    var label: String { rawValue.capitalized }
}
