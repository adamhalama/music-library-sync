import Foundation

struct MainConfigDefaults: Codable, Sendable {
    var stateDir: String
    var archiveFile: String
    var threads: Int
    var continueOnError: Bool
    var commandTimeoutSeconds: Int

    enum CodingKeys: String, CodingKey {
        case stateDir = "state_dir"
        case archiveFile = "archive_file"
        case threads
        case continueOnError = "continue_on_error"
        case commandTimeoutSeconds = "command_timeout_seconds"
    }
}

struct SourceSyncPolicy: Codable, Sendable {
    var breakOnExisting: Bool?
    var askOnExisting: Bool?
    var localIndexCache: Bool?

    enum CodingKeys: String, CodingKey {
        case breakOnExisting = "break_on_existing"
        case askOnExisting = "ask_on_existing"
        case localIndexCache = "local_index_cache"
    }
}

struct SourceAdapter: Codable, Sendable {
    var kind: String
    var extraArgs: [String]?
    var minVersion: String?

    enum CodingKeys: String, CodingKey {
        case kind
        case extraArgs = "extra_args"
        case minVersion = "min_version"
    }
}

struct MainConfigSource: Codable, Sendable, Identifiable {
    let id: String
    var type: String
    var enabled: Bool
    var targetDir: String
    var url: String
    var stateFile: String?
    var sync: SourceSyncPolicy
    var adapter: SourceAdapter

    enum CodingKeys: String, CodingKey {
        case id, type, enabled
        case targetDir = "target_dir"
        case url
        case stateFile = "state_file"
        case sync, adapter
    }
}

struct MainConfig: Codable, Sendable {
    let version: Int
    var defaults: MainConfigDefaults
    var sources: [MainConfigSource]
    let rekordbox: JSONValue?
}

struct MainConfigLoadResult: Codable, Sendable {
    let config: MainConfig
}

struct ConfigFileResult: Codable, Sendable {
    let path: String
    let config: MainConfig
    let content: String
    let contentSHA256: String
    enum CodingKeys: String, CodingKey {
        case path, config, content
        case contentSHA256 = "content_sha256"
    }
}

struct ConfigFileParams: Codable, Sendable {
    let path: String
    let config: MainConfig
    let expectedContentSHA256: String?
    enum CodingKeys: String, CodingKey {
        case path, config
        case expectedContentSHA256 = "expected_content_sha256"
    }
}

struct ConfigReadParams: Codable, Sendable {
    let path: String
}

struct ConfigValidateParams: Codable, Sendable { let config: MainConfig }

struct OnboardingState: Codable, Sendable {
    let reason: String
    let autoStarted: Bool
    let configPath: String
    let configContextLabel: String
    let detailLines: [String]
    let defaults: MainConfigDefaults

    enum CodingKeys: String, CodingKey {
        case reason
        case autoStarted = "auto_started"
        case configPath = "config_path"
        case configContextLabel = "config_context_label"
        case detailLines = "detail_lines"
        case defaults
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        reason = try container.decode(String.self, forKey: .reason)
        autoStarted = try container.decode(Bool.self, forKey: .autoStarted)
        configPath = try container.decode(String.self, forKey: .configPath)
        configContextLabel = try container.decode(String.self, forKey: .configContextLabel)
        detailLines = try container.decodeIfPresent([String].self, forKey: .detailLines) ?? []
        defaults = try container.decode(MainConfigDefaults.self, forKey: .defaults)
    }
}

struct OnboardingResult: Codable, Sendable {
    let needed: Bool
    let state: OnboardingState
}

struct StartupAttention: Codable, Sendable {
    let severity: String
    let primaryKind: CredentialKind
    let primarySourceID: String
    let affectedSourceIDs: [String]
    let issueCount: Int
    let primaryActionLabel: String
    let headline: String
    let summaryText: String

    enum CodingKeys: String, CodingKey {
        case severity
        case primaryKind = "primary_kind"
        case primarySourceID = "primary_source_id"
        case affectedSourceIDs = "affected_source_ids"
        case issueCount = "issue_count"
        case primaryActionLabel = "primary_action_label"
        case headline
        case summaryText = "summary_text"
    }
}

struct StartupAttentionResult: Codable, Sendable {
    let status: String
    let attention: StartupAttention?
}
