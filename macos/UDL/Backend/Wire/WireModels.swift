import Foundation

struct ShutdownResult: Codable, Sendable { let shutdown: Bool }
struct CancelRunResult: Codable, Sendable { let canceled: Bool }
struct ValidationResult: Codable, Sendable { let valid: Bool }

enum AgentMethod: String, Codable, CaseIterable, Sendable {
    case sessionInitialize = "session.initialize"
    case sessionShutdown = "session.shutdown"
    case runCancel = "run.cancel"
    case syncStart = "sync.start"
    case syncCancel = "sync.cancel"
    case configLoad = "config.load"
    case configValidate = "config.validate"
    case configReadFile = "config.readFile"
    case configWriteFile = "config.writeFile"
    case doctorRun = "doctor.run"
    case credentialsList = "credentials.list"
    case credentialsSave = "credentials.save"
    case credentialsClear = "credentials.clear"
    case startupOnboardingState = "startup.onboardingState"
    case startupAttention = "startup.attention"
    case sourcesCapabilities = "sources.capabilities"
    case playlistsList = "playlists.list"
    case playlistsProviderList = "playlists.providerList"
    case playlistsShow = "playlists.show"
    case playlistsRefresh = "playlists.refresh"
    case playlistsSaveDefinition = "playlists.saveDefinition"
    case playlistsConfigRead = "playlists.config.read"
    case playlistsConfigWrite = "playlists.config.write"
    case freeDLConfigRead = "freedl.config.read"
    case freeDLConfigWrite = "freedl.config.write"
    case freeDLPlanStart = "freedl.plan.start"
    case freeDLCaptureStart = "freedl.capture.start"
    case freeDLPromotionPlanBuild = "freedl.promotionPlan.build"
    case freeDLPromoteApply = "freedl.promote.apply"
    case rekordboxConfigRead = "rekordbox.config.read"
    case rekordboxConfigWrite = "rekordbox.config.write"
    case rekordboxDepsStatus = "rekordbox.deps.status"
    case rekordboxDepsEnsure = "rekordbox.deps.ensure"
    case rekordboxDepsReset = "rekordbox.deps.reset"
    case rekordboxInspect = "rekordbox.inspect"
    case rekordboxPlan = "rekordbox.plan"
    case rekordboxApply = "rekordbox.apply"
}

struct EmptyParams: Codable, Sendable {}

struct BuildInfo: Codable, Sendable, Equatable {
    let version: String
    let commit: String
    let date: String
}

struct InitializeParams: Codable, Sendable {
    let protocolVersion: Int

    enum CodingKeys: String, CodingKey {
        case protocolVersion = "protocol_version"
    }
}

struct InitializeResult: Codable, Sendable {
    let protocolVersion: Int
    let build: BuildInfo
    let methods: [String]
    let workingDir: String
    let configPaths: [String]
    let featureConfigPaths: [String: [String]]
    let capabilities: [String: JSONValue]

    enum CodingKeys: String, CodingKey {
        case protocolVersion = "protocol_version"
        case build, methods
        case workingDir = "working_dir"
        case configPaths = "config_paths"
        case featureConfigPaths = "feature_config_paths"
        case capabilities
    }
}

struct RunIDResult: Codable, Sendable, Equatable {
    let runID: String
    enum CodingKeys: String, CodingKey { case runID = "run_id" }
}

struct RunCancelParams: Codable, Sendable {
    let runID: String
    enum CodingKeys: String, CodingKey { case runID = "run_id" }
}

struct BooleanResult: Codable, Sendable {
    let value: Bool
}

enum DoctorSeverity: String, Codable, Sendable {
    case info
    case warn
    case error
    case unknown

    init(from decoder: Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        self = DoctorSeverity(rawValue: raw) ?? .unknown
    }
}

struct DoctorCheck: Codable, Identifiable, Sendable {
    var id: String { "\(name)-\(detail)" }
    let name: String
    let severity: DoctorSeverity
    let status: String
    let detail: String
    let remediation: String?
}

struct DoctorResult: Codable, Sendable {
    let checks: [DoctorCheck]
    let effectivePath: String
    let resolvedDependencies: [String: String]
    let exitCode: Int

    enum CodingKeys: String, CodingKey {
        case checks
        case effectivePath = "effective_path"
        case resolvedDependencies = "resolved_dependencies"
        case exitCode = "exit_code"
    }
}

enum CredentialKind: String, Codable, CaseIterable, Sendable, Identifiable {
    case soundCloudClientID = "soundcloud_client_id"
    case deemixARL = "deemix_arl"
    case spotifyApp = "spotify_app"
    var id: String { rawValue }
}

struct CredentialStatus: Codable, Identifiable, Sendable {
    var id: CredentialKind { kind }
    let kind: CredentialKind
    let title: String
    let health: String
    let storageSource: String
    let summary: String
    let lastFailureKind: String?
    let lastFailureMessage: String?

    enum CodingKeys: String, CodingKey {
        case kind, title, health, summary
        case storageSource = "storage_source"
        case lastFailureKind = "last_failure_kind"
        case lastFailureMessage = "last_failure_message"
    }
}

struct CredentialsListResult: Codable, Sendable {
    let credentials: [CredentialStatus]
}

struct CredentialMutationParams: Codable, Sendable {
    let kind: CredentialKind
    var value: String?
    var clientID: String?
    var clientSecret: String?

    enum CodingKeys: String, CodingKey {
        case kind, value
        case clientID = "client_id"
        case clientSecret = "client_secret"
    }
}

struct CredentialMutationResult: Codable, Sendable {
    let kind: CredentialKind
    let saved: Bool?
    let cleared: Bool?
}

struct EventDetails: Codable, Sendable {
    let phase: String?
    let status: String?
    let trackID: String?
    let trackTitle: String?
    let current: Int?
    let total: Int?
    let exitCode: Int?
    let failureKind: String?

    enum CodingKeys: String, CodingKey {
        case phase, status, current, total
        case trackID = "track_id"
        case trackTitle = "track_title"
        case exitCode = "exit_code"
        case failureKind = "failure_kind"
    }
}

struct OutputEvent: Codable, Sendable {
    let timestamp: Date?
    let level: String
    let event: String
    let sourceID: String?
    let message: String
    let details: EventDetails?

    enum CodingKeys: String, CodingKey {
        case timestamp, level, event, message, details
        case sourceID = "source_id"
    }
}

struct RunFinishedNotification: Codable, Sendable {
    let runID: String
    let result: JSONValue?
    let error: String?
    let exitCode: Int

    enum CodingKeys: String, CodingKey {
        case runID = "run_id"
        case result, error
        case exitCode = "exit_code"
    }
}

enum UIRequestKind: String, Codable, Sendable {
    case confirm = "ui.confirm"
    case input = "ui.input"
    case selectRows = "ui.selectRows"
}

struct UIRequest: Sendable {
    let id: JSONValue
    let kind: UIRequestKind
    let params: JSONValue
}

struct AgentNotification: Sendable {
    let method: String
    let params: JSONValue
}
