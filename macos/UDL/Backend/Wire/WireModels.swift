import Foundation

/// A collection whose empty value is well defined, so a wire field that arrives
/// absent or `null` has something unambiguous to decode to.
protocol EmptyRepresentable {
    static var emptyValue: Self { get }
}

extension Array: EmptyRepresentable {
    static var emptyValue: Self { [] }
}

extension Dictionary: EmptyRepresentable {
    static var emptyValue: Self { [:] }
}

/// Go marshals a nil slice or map as JSON `null`, and the agent builds most of
/// its collections lazily — so `"playlists": null`, `"checks": null` and
/// `"resolved_dependencies": null` are all valid protocol values that a
/// non-optional Swift collection cannot decode. Every wire-inbound collection is
/// wrapped so an absent or null field decodes to empty, which is what the
/// backend means by it. A fresh install, where every one of these arrives null
/// at once, is the case that proves it.
///
/// `init(wrappedValue:)` is what keeps each synthesised memberwise initialiser
/// taking the bare collection, so wrapping a field changes no construction site.
@propertyWrapper
struct DefaultEmpty<Value: Codable & Sendable & EmptyRepresentable>: Codable, Sendable {
    var wrappedValue: Value

    /// This must stay the *only* initialiser callable with no arguments, and
    /// `wrappedValue` must not have a default. Either one makes Swift synthesise
    /// memberwise initialisers taking `DefaultEmpty<Value>` instead of the bare
    /// collection, which would rewrite every construction site in the app.
    init(wrappedValue: Value) {
        self.wrappedValue = wrappedValue
    }

    init(from decoder: any Decoder) throws {
        let container = try decoder.singleValueContainer()
        wrappedValue = container.decodeNil() ? .emptyValue : try container.decode(Value.self)
    }

    func encode(to encoder: any Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(wrappedValue)
    }
}

extension DefaultEmpty: Equatable where Value: Equatable {}
extension DefaultEmpty: Hashable where Value: Hashable {}

extension KeyedDecodingContainer {
    /// A missing key says the same thing `null` does: the backend has nothing
    /// for this field. Synthesised decoding routes through here.
    func decode<Value>(
        _ type: DefaultEmpty<Value>.Type,
        forKey key: Key
    ) throws -> DefaultEmpty<Value> {
        try decodeIfPresent(type, forKey: key) ?? DefaultEmpty(wrappedValue: .emptyValue)
    }
}

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
    case navidromeConfigRead = "navidrome.config.read"
    case navidromeConfigWrite = "navidrome.config.write"
    case navidromeDepsStatus = "navidrome.deps.status"
    case navidromeDepsEnsure = "navidrome.deps.ensure"
    case navidromeStatus = "navidrome.status"
    case navidromeServiceControl = "navidrome.service.control"
    case navidromeSetupPlan = "navidrome.setup.plan"
    case navidromeSetupApply = "navidrome.setup.apply"
    case navidromePlaylistsRefresh = "navidrome.playlists.refresh"
    case navidromePlaylistsDeriveGenres = "navidrome.playlists.deriveGenres"
    case navidromePlaylistsSaveGenres = "navidrome.playlists.saveGenres"
    case navidromeFavoritesPlan = "navidrome.favorites.plan"
    case navidromeFavoritesApply = "navidrome.favorites.apply"
    case navidromeFavoritesList = "navidrome.favorites.list"
    case navidromeBackupCreate = "navidrome.backup.create"
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
    @DefaultEmpty var methods: [String]
    let workingDir: String
    @DefaultEmpty var configPaths: [String]
    @DefaultEmpty var featureConfigPaths: [String: [String]]
    @DefaultEmpty var capabilities: [String: JSONValue]

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
    @DefaultEmpty var checks: [DoctorCheck]
    let effectivePath: String
    @DefaultEmpty var resolvedDependencies: [String: String]
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
    case navidromePassword = "navidrome_password"
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
    @DefaultEmpty var credentials: [CredentialStatus]
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

    /// A cancellation must answer the backend's blocked request with the exact
    /// shape each prompt decoder expects. Keeping the mapping on the wire kind
    /// makes all prompt types independently testable and prevents a new prompt
    /// from accidentally becoming non-cancelable.
    var canceledResult: JSONValue {
        switch self {
        case .confirm:
            .object(["confirmed": .bool(false), "canceled": .bool(true)])
        case .input:
            .object(["value": .string(""), "canceled": .bool(true)])
        case .selectRows:
            .object([
                "selected_indices": .array([]),
                "download_order": .string("oldest_first"),
                "canceled": .bool(true),
                "rebuild": .bool(false),
                "plan_window": .string("first"),
            ])
        }
    }
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
