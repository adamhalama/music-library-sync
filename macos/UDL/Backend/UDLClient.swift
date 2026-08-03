import Foundation

actor UDLClient {
    let connection: JSONRPCConnection

    init(connection: JSONRPCConnection) {
        self.connection = connection
    }

    func initialize() async throws -> InitializeResult {
        try await connection.call(.sessionInitialize, params: InitializeParams(protocolVersion: 2))
    }

    func shutdown() async throws -> ShutdownResult {
        try await connection.call(.sessionShutdown, params: EmptyParams())
    }
    func cancelRun(_ runID: String) async throws -> CancelRunResult {
        try await connection.call(.runCancel, params: RunCancelParams(runID: runID))
    }
    func startSync(_ params: SyncStartParams) async throws -> RunIDResult {
        try await connection.call(.syncStart, params: params)
    }
    func cancelSync(_ runID: String) async throws -> CancelRunResult {
        try await connection.call(.syncCancel, params: RunCancelParams(runID: runID))
    }

    func loadConfig() async throws -> MainConfigLoadResult {
        try await connection.call(.configLoad, params: EmptyParams())
    }
    func validateConfig(_ config: MainConfig) async throws -> ValidationResult {
        try await connection.call(.configValidate, params: ConfigValidateParams(config: config))
    }
    func readConfigFile(path: String = "") async throws -> ConfigFileResult {
        try await connection.call(
            .configReadFile,
            params: ConfigReadParams(path: path)
        )
    }
    func writeConfigFile(_ params: ConfigFileParams) async throws -> ConfigFileResult {
        try await connection.call(.configWriteFile, params: params)
    }

    func runDoctor() async throws -> DoctorResult {
        try await connection.call(.doctorRun, params: EmptyParams())
    }

    func listCredentials() async throws -> CredentialsListResult {
        try await connection.call(.credentialsList, params: EmptyParams())
    }

    func saveCredential(_ params: CredentialMutationParams) async throws -> CredentialMutationResult {
        try await connection.call(.credentialsSave, params: params)
    }

    func clearCredential(_ kind: CredentialKind) async throws -> CredentialMutationResult {
        try await connection.call(
            .credentialsClear,
            params: CredentialMutationParams(kind: kind, value: nil, clientID: nil, clientSecret: nil)
        )
    }

    func onboardingState() async throws -> OnboardingResult {
        try await connection.call(.startupOnboardingState, params: EmptyParams())
    }
    func startupAttention() async throws -> StartupAttentionResult {
        try await connection.call(.startupAttention, params: EmptyParams())
    }
    func sourceCapabilities() async throws -> SourceCapabilitiesResult {
        try await connection.call(.sourcesCapabilities, params: EmptyParams())
    }

    func listPlaylists() async throws -> PlaylistListResult {
        try await connection.call(.playlistsList, params: EmptyParams())
    }
    func listProviderPlaylists(_ params: ProviderPlaylistListParams) async throws -> RunIDResult {
        try await connection.call(.playlistsProviderList, params: params)
    }
    func showPlaylist(_ playlistID: String) async throws -> PlaylistShowResult {
        try await connection.call(.playlistsShow, params: PlaylistIDParams(playlistID: playlistID))
    }
    func refreshPlaylist(_ playlistID: String) async throws -> RunIDResult {
        try await connection.call(.playlistsRefresh, params: PlaylistIDParams(playlistID: playlistID))
    }
    func savePlaylistDefinition(_ definition: PlaylistDefinition) async throws -> PlaylistDefinitionSaveResult {
        try await connection.call(
            .playlistsSaveDefinition,
            params: PlaylistDefinitionParams(definition: definition)
        )
    }
    func readPlaylistsConfig() async throws -> PlaylistConfigResult {
        try await connection.call(.playlistsConfigRead, params: EmptyParams())
    }
    func writePlaylistsConfig(_ config: PlaylistConfig) async throws -> PlaylistConfigResult {
        try await connection.call(.playlistsConfigWrite, params: PlaylistConfigWriteParams(config: config))
    }

    func readFreeDLConfig() async throws -> FreeDLConfigResult {
        try await connection.call(.freeDLConfigRead, params: EmptyParams())
    }
    func writeFreeDLConfig(_ config: FreeDLConfig) async throws -> FreeDLConfigResult {
        try await connection.call(.freeDLConfigWrite, params: FreeDLConfigWriteParams(config: config))
    }
    func startFreeDLPlan(_ params: FreeDLPlanStartParams) async throws -> RunIDResult {
        try await connection.call(.freeDLPlanStart, params: params)
    }
    func startFreeDLCapture(_ params: FreeDLCaptureStartParams) async throws -> RunIDResult {
        try await connection.call(.freeDLCaptureStart, params: params)
    }
    func buildFreeDLPromotionPlan(_ params: FreeDLPromotionPlanBuildParams) async throws -> RunIDResult {
        try await connection.call(.freeDLPromotionPlanBuild, params: params)
    }
    func applyFreeDLPromotion(_ params: FreeDLPromotionApplyParams) async throws -> RunIDResult {
        try await connection.call(.freeDLPromoteApply, params: params)
    }

    func readRekordboxConfig() async throws -> RekordboxConfigResult {
        try await connection.call(.rekordboxConfigRead, params: EmptyParams())
    }
    func writeRekordboxConfig(_ config: RekordboxConfig) async throws -> RekordboxConfigResult {
        try await connection.call(.rekordboxConfigWrite, params: RekordboxConfigWriteParams(config: config))
    }
    func rekordboxDependencyStatus() async throws -> RekordboxDependencyResult {
        try await connection.call(.rekordboxDepsStatus, params: EmptyParams())
    }
    func ensureRekordboxDependencies() async throws -> RunIDResult {
        try await connection.call(.rekordboxDepsEnsure, params: EmptyParams())
    }
    func resetRekordboxDependencies() async throws -> RunIDResult {
        try await connection.call(.rekordboxDepsReset, params: EmptyParams())
    }
    func inspectRekordbox() async throws -> RunIDResult {
        try await connection.call(.rekordboxInspect, params: EmptyParams())
    }
    func planRekordbox(_ params: RekordboxPlanParams) async throws -> RunIDResult {
        try await connection.call(.rekordboxPlan, params: params)
    }
    func applyRekordbox(_ params: RekordboxApplyParams) async throws -> RunIDResult {
        try await connection.call(.rekordboxApply, params: params)
    }
}
