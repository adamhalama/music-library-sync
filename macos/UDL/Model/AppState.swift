import AppKit
import Combine
import Foundation

@MainActor
final class AppState: ObservableObject {
    enum Destination: String, CaseIterable, Identifiable {
        case onboarding = "Welcome"
        case doctor = "Doctor"
        case credentials = "Credentials"
        case sync = "Sync"
        case playlists = "Playlists"
        case freeDL = "Free DL"
        case rekordbox = "Rekordbox"
        case config = "Configuration"
        var id: String { rawValue }
    }

    struct PendingPrompt: Identifiable {
        let id = UUID()
        let request: UIRequest
        var input = ""
    }

    let backend = AgentProcess()
    @Published var destination: Destination? = .doctor
    @Published private(set) var initialization: InitializeResult?
    @Published private(set) var doctor: DoctorResult?
    @Published private(set) var credentials: [CredentialStatus] = []
    @Published private(set) var isLoadingDoctor = false
    @Published private(set) var isLoadingCredentials = false
    @Published var pendingPrompt: PendingPrompt?
    @Published var alertMessage: String?
    @Published var projectDirectory: URL
    @Published private(set) var syncSources: [SourceCapability] = []
    @Published var syncSourceOptions: [String: SyncSourceOptions] = [:]
    @Published var syncDryRun = false
    @Published var syncUnlimited = false
    @Published var syncPlanLimit = 50
    @Published var syncTimeoutSeconds = 0
    @Published private(set) var syncValidationMessage: String?
    @Published private(set) var syncRun = SyncRunState()
    @Published private(set) var playlists: [PlaylistListRow] = []
    @Published private(set) var playlistConfig: PlaylistConfigResult?
    @Published private(set) var playlistActiveRunID: String?
    @Published private(set) var playlistStatusMessage: String?
    @Published private(set) var providerPlaylists: [ProviderPlaylist] = []
    @Published private(set) var freeDLConfig: FreeDLConfigResult?
    @Published private(set) var freeDLRunID: String?
    @Published private(set) var freeDLOperation: FreeDLOperation?
    @Published private(set) var freeDLRows: [FreeDLPlanRow] = []
    @Published var freeDLSelectionOverrides: [String: Bool] = [:]
    @Published private(set) var freeDLCapturePlan: FreeDLCapturePlan?
    @Published private(set) var freeDLCaptureRunID: String?
    @Published private(set) var freeDLPromotionPlan: FreeDLPromotionPlan?
    @Published private(set) var freeDLStage: String?
    @Published private(set) var freeDLStatusMessage: String?
    @Published private(set) var freeDLCaptureSources: [String: SourceSnapshot] = [:]
    @Published private(set) var freeDLCaptureActivity: [OutputEvent] = []
    @Published private(set) var rekordboxConfig: RekordboxConfigResult?
    @Published private(set) var rekordboxRuntime: RekordboxDependencyResult?
    @Published private(set) var rekordboxInspect: RekordboxInspectResult?
    @Published private(set) var rekordboxPlan: RekordboxPlanPresentation?
    @Published private(set) var rekordboxRunID: String?
    @Published private(set) var rekordboxOperation: RekordboxOperation?
    @Published private(set) var rekordboxStatusMessage: String?
    @Published private(set) var rekordboxApplyBlockers: [RekordboxPlanRowView] = []
    @Published private(set) var onboarding: OnboardingResult?
    @Published private(set) var startupAttention: StartupAttentionResult?
    @Published private(set) var configFile: ConfigFileResult?
    @Published private(set) var configProblems: [String] = []
    @Published private(set) var configStatusMessage: String?

    private var client: UDLClient?
    private var notificationTask: Task<Void, Never>?
    private var promptTask: Task<Void, Never>?
    private var playlistOperations: [String: PlaylistOperation] = [:]
    private var bufferedFinished: [String: RunFinishedNotification] = [:]
    private var planSelectionOverrides: [String: [String: Bool]] = [:]
    private var planCursorBySource: [String: String] = [:]

    init() {
        projectDirectory = FileManager.default.homeDirectoryForCurrentUser
    }

    var backendVersion: String {
        initialization?.build.version ?? "—"
    }

    var alertOffersAutomationSettings: Bool {
        alertMessage?.contains("System Settings > Privacy & Security > Automation") == true
    }

    func openAutomationSettings() {
        guard let url = URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_Automation") else {
            return
        }
        NSWorkspace.shared.open(url)
    }

    func start() async {
        do {
            let client = try await backend.launch(workingDirectory: projectDirectory)
            self.client = client
            let initialization = try await client.initialize()
            let missing = Set(AgentMethod.allCases.map(\.rawValue)).subtracting(initialization.methods)
            guard missing.isEmpty else {
                throw JSONRPCConnectionError.malformedFrame(
                    "backend is missing methods: \(missing.sorted().joined(separator: ", "))"
                )
            }
            self.initialization = initialization
            observe(connection: client.connection)
            let onboarding = try await client.onboardingState()
            self.onboarding = onboarding
            if onboarding.needed {
                destination = .onboarding
            } else {
                startupAttention = try await client.startupAttention()
            }
            async let doctorLoad: Void = refreshDoctor()
            async let credentialsLoad: Void = refreshCredentials()
            if onboarding.needed {
                _ = await (doctorLoad, credentialsLoad)
            } else {
                async let sourcesLoad: Void = loadSyncSources()
                async let playlistsLoad: Void = loadPlaylists()
                async let freeDLLoad: Void = loadFreeDLConfig()
                async let rekordboxLoad: Void = loadRekordbox()
                async let configLoad: Void = loadConfigEditor()
                _ = await (
                    doctorLoad, credentialsLoad, sourcesLoad, playlistsLoad,
                    freeDLLoad, rekordboxLoad, configLoad
                )
            }
        } catch {
            alertMessage = error.localizedDescription
        }
    }

    func restart() async {
        clearSessionState()
        do {
            let client = try await backend.restart(workingDirectory: projectDirectory)
            self.client = client
            initialization = try await client.initialize()
            observe(connection: client.connection)
            onboarding = try await client.onboardingState()
            await refreshDoctor()
            await refreshCredentials()
            if onboarding?.needed == true {
                destination = .onboarding
            } else {
                startupAttention = try await client.startupAttention()
                await loadSyncSources()
                await loadPlaylists()
                await loadFreeDLConfig()
                await loadRekordbox()
                await loadConfigEditor()
            }
        } catch {
            alertMessage = error.localizedDescription
        }
    }

    func shutdown() async {
        notificationTask?.cancel()
        promptTask?.cancel()
        pendingPrompt = nil
        await backend.stop()
        clearSessionState()
    }

    func refreshDoctor() async {
        guard let client else { return }
        isLoadingDoctor = true
        defer { isLoadingDoctor = false }
        do {
            doctor = try await client.runDoctor()
        } catch {
            alertMessage = "Doctor could not run: \(error.localizedDescription)"
        }
    }

    func refreshCredentials() async {
        guard let client else { return }
        isLoadingCredentials = true
        defer { isLoadingCredentials = false }
        do {
            credentials = try await client.listCredentials().credentials
        } catch {
            alertMessage = "Credentials could not load: \(error.localizedDescription)"
        }
    }

    func saveCredential(
        kind: CredentialKind,
        value: String = "",
        clientID: String = "",
        clientSecret: String = ""
    ) async -> Bool {
        guard let client else { return false }
        do {
            _ = try await client.saveCredential(CredentialMutationParams(
                kind: kind,
                value: value.isEmpty ? nil : value,
                clientID: clientID.isEmpty ? nil : clientID,
                clientSecret: clientSecret.isEmpty ? nil : clientSecret
            ))
            await refreshCredentials()
            return true
        } catch {
            alertMessage = "Keychain save failed: \(error.localizedDescription)"
            return false
        }
    }

    func clearCredential(_ kind: CredentialKind) async {
        guard let client else { return }
        do {
            _ = try await client.clearCredential(kind)
            await refreshCredentials()
        } catch {
            alertMessage = "Keychain clear failed: \(error.localizedDescription)"
        }
    }

    func loadSyncSources() async {
        guard let client else { return }
        do {
            let loaded = try await client.sourceCapabilities().sources
            syncSources = loaded
            var preserved = syncSourceOptions
            for source in loaded where preserved[source.id] == nil {
                preserved[source.id] = SyncSourceOptions(
                    downloadOrder: source.defaultDownloadOrder,
                    planWindow: source.defaultPlanWindow
                )
            }
            syncSourceOptions = preserved.filter { key, _ in loaded.contains { $0.id == key } }
        } catch {
            alertMessage = "Sources could not load: \(error.localizedDescription)"
        }
    }

    func setSourceSelected(_ sourceID: String, _ selected: Bool) {
        guard var options = syncSourceOptions[sourceID], options.selected != selected else { return }
        options.selected = selected
        syncSourceOptions[sourceID] = options
    }

    func setSourcePlanWindow(_ sourceID: String, _ window: PlanWindow) {
        guard var options = syncSourceOptions[sourceID], options.planWindow != window else { return }
        options.planWindow = window
        syncSourceOptions[sourceID] = options
    }

    func setSourceDownloadOrder(_ sourceID: String, _ order: DownloadOrder) {
        guard var options = syncSourceOptions[sourceID], options.downloadOrder != order else { return }
        options.downloadOrder = order
        syncSourceOptions[sourceID] = options
    }

    func startSync() async {
        guard let client else { return }
        let selected = syncSources.filter { syncSourceOptions[$0.id]?.selected == true }
        guard !selected.isEmpty else {
            syncValidationMessage = "Select at least one source."
            return
        }
        guard syncTimeoutSeconds >= 0, syncPlanLimit > 0 || syncUnlimited else {
            syncValidationMessage = "Plan limit must be positive and timeout cannot be negative."
            return
        }
        syncValidationMessage = nil
        let windows = Dictionary(uniqueKeysWithValues: selected.compactMap { source in
            syncSourceOptions[source.id].map { (source.id, $0.planWindow) }
        })
        let orders = Dictionary(uniqueKeysWithValues: selected.compactMap { source in
            syncSourceOptions[source.id].map { (source.id, $0.downloadOrder) }
        })
        syncRun = SyncRunState(phase: .starting)
        do {
            let started = try await client.startSync(SyncStartParams(
                sourceIDs: selected.map(\.id),
                dryRun: syncDryRun,
                timeoutSeconds: syncTimeoutSeconds,
                plan: true,
                planLimit: syncUnlimited ? 0 : syncPlanLimit,
                planWindow: .first,
                planWindowBySource: windows,
                downloadOrderBySource: orders,
                askOnExisting: false,
                askOnExistingSet: false,
                scanGaps: false,
                noPreflight: false,
                trackStatus: "none"
            ))
            syncRun.runID = started.runID
            if syncRun.exitCode == nil {
                syncRun.phase = .running
            }
        } catch {
            syncRun.phase = .failed
            syncRun.terminalMessage = error.localizedDescription
        }
    }

    func cancelActiveSync() async {
        guard let runID = syncRun.runID else { return }
        syncRun.phase = .canceling
        await cancelRun(runID)
    }

    func resetSyncRun() {
        guard !syncRun.phase.isActive else { return }
        syncRun = SyncRunState()
    }

    func loadPlaylists() async {
        guard let client else { return }
        do {
            // These are cache/config reads; neither method contacts Music.app.
            async let rows = client.listPlaylists()
            async let config = client.readPlaylistsConfig()
            playlists = try await rows.playlists
            playlistConfig = try await config
        } catch {
            alertMessage = "Playlist cache could not load: \(error.localizedDescription)"
        }
    }

    func savePlaylistDefinition(_ definition: PlaylistDefinition) async -> Bool {
        guard let client else { return false }
        do {
            _ = try await client.savePlaylistDefinition(definition)
            await loadPlaylists()
            return true
        } catch {
            alertMessage = "Playlist definition could not save: \(error.localizedDescription)"
            return false
        }
    }

    func savePlaylistConfig(_ config: PlaylistConfig) async -> Bool {
        guard let client else { return false }
        do {
            playlistConfig = try await client.writePlaylistsConfig(config)
            await loadPlaylists()
            return true
        } catch {
            alertMessage = "Playlist config could not save: \(error.localizedDescription)"
            return false
        }
    }

    func refreshPlaylist(_ playlistID: String) async {
        guard let client, playlistActiveRunID == nil else { return }
        playlistStatusMessage = "Refreshing from Music… the existing snapshot remains active until success."
        do {
            let started = try await client.refreshPlaylist(playlistID)
            playlistActiveRunID = started.runID
            playlistOperations[started.runID] = .refresh(playlistID)
            consumeBufferedFinished(started.runID)
        } catch {
            playlistStatusMessage = "Refresh did not start. The saved snapshot was not changed."
            alertMessage = error.localizedDescription
        }
    }

    func discoverProviderPlaylists() async {
        guard let client, playlistActiveRunID == nil else { return }
        playlistStatusMessage = "Requesting playlists from Music…"
        do {
            let started = try await client.listProviderPlaylists(
                ProviderPlaylistListParams(provider: "apple_music")
            )
            playlistActiveRunID = started.runID
            playlistOperations[started.runID] = .providerList
            consumeBufferedFinished(started.runID)
        } catch {
            playlistStatusMessage = "Music playlist discovery did not start."
            alertMessage = error.localizedDescription
        }
    }

    func cancelPlaylistOperation() async {
        guard let runID = playlistActiveRunID else { return }
        await cancelRun(runID)
    }

    func loadFreeDLConfig() async {
        guard let client else { return }
        do {
            freeDLConfig = try await client.readFreeDLConfig()
        } catch {
            alertMessage = "Free DL config could not load: \(error.localizedDescription)"
        }
    }

    func saveFreeDLConfig(_ config: FreeDLConfig) async -> Bool {
        guard let client else { return false }
        do {
            freeDLConfig = try await client.writeFreeDLConfig(config)
            return true
        } catch {
            alertMessage = "Free DL config could not save: \(error.localizedDescription)"
            return false
        }
    }

    func startFreeDLPlan(jobID: String, playlistID: String? = nil) async {
        guard let client, freeDLRunID == nil else { return }
        freeDLOperation = .planning
        freeDLRows = []
        freeDLCapturePlan = nil
        freeDLPromotionPlan = nil
        freeDLStage = "Starting plan…"
        do {
            let started = try await client.startFreeDLPlan(FreeDLPlanStartParams(
                jobID: jobID,
                planLimit: nil,
                playlistID: playlistID,
                selectionOverrides: freeDLSelectionOverrides
            ))
            freeDLRunID = started.runID
            consumeBufferedFinished(started.runID)
        } catch {
            freeDLOperation = nil
            freeDLStage = nil
            alertMessage = "Free DL planning did not start: \(error.localizedDescription)"
        }
    }

    func setFreeDLSelection(_ remoteID: String, _ selected: Bool) {
        freeDLSelectionOverrides[remoteID] = selected
        if let index = freeDLRows.firstIndex(where: { $0.remoteID == remoteID }) {
            freeDLRows[index].selected = selected && freeDLRows[index].selectable
        }
        if let index = freeDLCapturePlan?.rows.firstIndex(where: { $0.remoteID == remoteID }) {
            freeDLCapturePlan?.rows[index].selected = selected && (freeDLCapturePlan?.rows[index].selectable ?? false)
        }
    }

    func startFreeDLCapture() async {
        guard let client, var plan = freeDLCapturePlan, freeDLRunID == nil else { return }
        reapplyFreeDLOverrides(to: &plan)
        let selected = plan.rows.filter { $0.selectable && $0.selected }.map(\.remoteID)
        guard !selected.isEmpty else {
            alertMessage = "Select at least one Free DL row before capture."
            return
        }
        freeDLOperation = .capture
        freeDLCaptureSources = [:]
        freeDLCaptureActivity = []
        freeDLStatusMessage = "Capturing selected downloads…"
        do {
            let started = try await client.startFreeDLCapture(
                FreeDLCaptureStartParams(plan: plan, selectedRemoteIDs: selected)
            )
            freeDLRunID = started.runID
            consumeBufferedFinished(started.runID)
        } catch {
            freeDLOperation = nil
            alertMessage = "Capture did not start: \(error.localizedDescription)"
        }
    }

    func buildFreeDLPromotionPlan() async {
        guard let client,
              let jobID = freeDLCapturePlan?.job.id,
              let captureRunID = freeDLCaptureRunID,
              freeDLRunID == nil else { return }
        freeDLOperation = .promotionBuild
        do {
            let started = try await client.buildFreeDLPromotionPlan(
                FreeDLPromotionPlanBuildParams(jobID: jobID, captureRunID: captureRunID)
            )
            freeDLRunID = started.runID
            consumeBufferedFinished(started.runID)
        } catch {
            freeDLOperation = nil
            alertMessage = "Promotion planning did not start: \(error.localizedDescription)"
        }
    }

    func applyFreeDLPromotion() async {
        guard let client, let plan = freeDLPromotionPlan, freeDLRunID == nil else { return }
        freeDLOperation = .promotionApply
        do {
            let started = try await client.applyFreeDLPromotion(
                FreeDLPromotionApplyParams(plan: plan)
            )
            freeDLRunID = started.runID
            consumeBufferedFinished(started.runID)
        } catch {
            freeDLOperation = nil
            alertMessage = "Promotion apply did not start: \(error.localizedDescription)"
        }
    }

    func cancelFreeDLOperation() async {
        guard let runID = freeDLRunID else { return }
        await cancelRun(runID)
    }

    func loadRekordbox() async {
        guard let client else { return }
        do {
            async let config = client.readRekordboxConfig()
            async let runtime = client.rekordboxDependencyStatus()
            let loaded = try await (config, runtime)
            rekordboxConfig = loaded.0
            rekordboxRuntime = loaded.1
        } catch {
            alertMessage = "Rekordbox setup could not load: \(error.localizedDescription)"
        }
    }

    func saveRekordboxConfig(_ config: RekordboxConfig) async -> Bool {
        guard let client else { return false }
        do {
            rekordboxConfig = try await client.writeRekordboxConfig(config)
            return true
        } catch {
            alertMessage = "Rekordbox config could not save: \(error.localizedDescription)"
            return false
        }
    }

    func ensureRekordboxRuntime() async {
        await startRekordboxOperation(.ensure) { client in
            try await client.ensureRekordboxDependencies()
        }
    }

    func resetRekordboxRuntime() async {
        await startRekordboxOperation(.reset) { client in
            try await client.resetRekordboxDependencies()
        }
    }

    func inspectRekordbox() async {
        await startRekordboxOperation(.inspect) { client in
            try await client.inspectRekordbox()
        }
    }

    func planRekordbox(jobID: String? = nil, mappingID: String? = nil, playlistID: String? = nil) async {
        let params = RekordboxPlanParams(
            jobID: jobID?.isEmpty == false ? jobID : nil,
            mappingID: mappingID?.isEmpty == false ? mappingID : nil,
            playlistID: playlistID?.isEmpty == false ? playlistID : nil
        )
        rekordboxPlan = nil
        rekordboxApplyBlockers = []
        await startRekordboxOperation(.plan) { client in
            try await client.planRekordbox(params)
        }
    }

    func applyRekordbox(dryRun: Bool) async {
        guard let client, let plan = rekordboxPlan else { return }
        guard !plan.checksum.isEmpty else {
            alertMessage = "The plan has no checksum and cannot be applied."
            return
        }
        guard plan.blockers.isEmpty else {
            rekordboxApplyBlockers = plan.blockers
            alertMessage = "Resolve every missing or ambiguous track before apply."
            return
        }
        rekordboxOperation = .apply(dryRun: dryRun)
        rekordboxStatusMessage = dryRun ? "Validating dry run…" : "Applying after backend integrity, process, and backup checks…"
        do {
            let started = try await client.applyRekordbox(
                RekordboxApplyParams(plan: plan.value, dryRun: dryRun)
            )
            rekordboxRunID = started.runID
            consumeBufferedFinished(started.runID)
        } catch JSONRPCConnectionError.remote(_, let message, let data) {
            rekordboxOperation = nil
            rekordboxApplyBlockers = parseRekordboxBlockers(data)
            alertMessage = message.localizedCaseInsensitiveContains("checksum")
                ? "Plan integrity check failed: \(message)"
                : message
        } catch {
            rekordboxOperation = nil
            alertMessage = error.localizedDescription
        }
    }

    func cancelRekordboxOperation() async {
        guard let runID = rekordboxRunID else { return }
        await cancelRun(runID)
    }

    private func startRekordboxOperation(
        _ operation: RekordboxOperation,
        start: (UDLClient) async throws -> RunIDResult
    ) async {
        guard let client, rekordboxRunID == nil else { return }
        rekordboxOperation = operation
        rekordboxStatusMessage = "Starting…"
        do {
            let started = try await start(client)
            rekordboxRunID = started.runID
            consumeBufferedFinished(started.runID)
        } catch {
            rekordboxOperation = nil
            rekordboxStatusMessage = nil
            alertMessage = error.localizedDescription
        }
    }

    func loadConfigEditor() async {
        guard let client else { return }
        do {
            configFile = try await client.readConfigFile()
            configProblems = []
            configStatusMessage = nil
        } catch {
            configProblems = validationProblems(from: error)
            alertMessage = "Configuration could not load: \(error.localizedDescription)"
        }
    }

    func saveMainConfig(_ config: MainConfig, path: String? = nil, creating: Bool = false) async -> Bool {
        guard let client else { return false }
        configProblems = []
        do {
            _ = try await client.validateConfig(config)
            let saved = try await client.writeConfigFile(ConfigFileParams(
                path: path ?? configFile?.path ?? "",
                config: config,
                expectedContentSHA256: creating ? nil : configFile?.contentSHA256
            ))
            configFile = saved
            configStatusMessage = "Saved canonical configuration."
            return true
        } catch JSONRPCConnectionError.remote(let code, let message, let data) {
            configProblems = validationProblems(from: data)
            if code == -32003 {
                configStatusMessage = "The file changed outside UDL. Reload it before saving; no content was overwritten."
            } else {
                configStatusMessage = message
            }
            return false
        } catch {
            configStatusMessage = error.localizedDescription
            return false
        }
    }

    func createInitialConfig(source: MainConfigSource) async -> Bool {
        guard let state = onboarding?.state else { return false }
        let config = MainConfig(
            version: 1,
            defaults: state.defaults,
            sources: [source],
            rekordbox: nil
        )
        guard await saveMainConfig(config, path: state.configPath, creating: true) else { return false }
        await restart()
        if onboarding?.needed == false {
            destination = .doctor
            return true
        }
        return false
    }

    func useProjectDirectory(_ url: URL) async {
        guard url.isFileURL else { return }
        projectDirectory = url.standardizedFileURL
        await restart()
    }

    func answerPrompt(result: JSONValue) async {
        guard let prompt = pendingPrompt, let connection = backend.connection else { return }
        do {
            try await connection.respond(to: prompt.request, result: result)
        } catch {
            alertMessage = error.localizedDescription
        }
        if pendingPrompt?.id == prompt.id {
            pendingPrompt = nil
        }
    }

    func answerPlanSelection(_ result: SelectRowsResult, request: UIRequest) async {
        // The UI's copied source settings must change before Go receives a
        // rebuild reply, matching the engine/TUI cross-boundary invariant.
        if result.rebuild,
           let params = try? decode(SelectRowsParams.self, from: request.params) {
            setSourcePlanWindow(params.sourceID, result.planWindow)
        }
        guard let encoded = try? result.jsonValue(using: .agent) else {
            alertMessage = "Could not encode the plan selection response."
            return
        }
        await answerPrompt(result: encoded)
    }

    func initialPlanSelection(_ params: SelectRowsParams) -> Set<Int> {
        let overrides = planSelectionOverrides[params.sourceID] ?? [:]
        return Set(params.rows.compactMap { row in
            (overrides[row.id] ?? row.selectedByDefault) ? row.index : nil
        })
    }

    func rememberPlanSelection(
        _ params: SelectRowsParams,
        selectedIndices: Set<Int>,
        cursor: String?
    ) {
        var overrides = planSelectionOverrides[params.sourceID] ?? [:]
        for row in params.rows {
            overrides[row.id] = selectedIndices.contains(row.index)
        }
        planSelectionOverrides[params.sourceID] = overrides
        if let cursor {
            planCursorBySource[params.sourceID] = cursor
        }
    }

    func rememberedPlanCursor(sourceID: String, rows: [PlanRow]) -> String? {
        guard let cursor = planCursorBySource[sourceID],
              rows.contains(where: { $0.id == cursor }) else {
            return nil
        }
        return cursor
    }

    func cancelPrompt() async {
        guard let prompt = pendingPrompt, let connection = backend.connection else { return }
        let result: JSONValue
        switch prompt.request.kind {
        case .confirm:
            result = .object(["confirmed": .bool(false), "canceled": .bool(true)])
        case .input:
            result = .object(["value": .string(""), "canceled": .bool(true)])
        case .selectRows:
            result = .object([
                "selected_indices": .array([]),
                "download_order": .string("newest_first"),
                "canceled": .bool(true),
                "rebuild": .bool(false),
                "plan_window": .string("first"),
            ])
        }
        try? await connection.respond(to: prompt.request, result: result)
        if pendingPrompt?.id == prompt.id {
            pendingPrompt = nil
        }
    }

    func cancelRun(_ runID: String) async {
        await performOrderedCancellation(
            answerPending: { await self.cancelPrompt() },
            cancelRun: { _ = try? await self.client?.cancelRun(runID) }
        )
    }

    private func observe(connection: JSONRPCConnection) {
        notificationTask?.cancel()
        promptTask?.cancel()
        notificationTask = Task {
            for await notification in connection.notifications {
                if notification.method == "freedl.planEvent",
                   let event = try? decode(FreeDLPlanEventNotification.self, from: notification.params),
                   event.runID == freeDLRunID || (freeDLRunID == nil && freeDLOperation == .planning) {
                    freeDLRunID = event.runID
                    applyFreeDLPlanEvent(event.event)
                } else if notification.method == "sync.event",
                   let event = try? decode(SyncEventNotification.self, from: notification.params),
                   event.runID == syncRun.runID || (syncRun.runID == nil && syncRun.phase == .starting) {
                    syncRun.runID = event.runID
                    let sourceID = event.event.sourceID ?? event.source.rows.first?.sourceID ?? ""
                    if !sourceID.isEmpty {
                        syncRun.sources[sourceID] = event.source
                    }
                    syncRun.progress = event.progress
                    syncRun.activity.append(event.event)
                    if syncRun.activity.count > 200 {
                        syncRun.activity.removeFirst(syncRun.activity.count - 200)
                    }
                } else if notification.method == "sync.event",
                          let event = try? decode(SyncEventNotification.self, from: notification.params),
                          event.runID == freeDLRunID,
                          freeDLOperation == .capture {
                    let sourceID = event.event.sourceID ?? event.source.rows.first?.sourceID ?? ""
                    if !sourceID.isEmpty {
                        freeDLCaptureSources[sourceID] = event.source
                    }
                    freeDLCaptureActivity.append(event.event)
                    if freeDLCaptureActivity.count > 200 {
                        freeDLCaptureActivity.removeFirst(freeDLCaptureActivity.count - 200)
                    }
                } else if notification.method == "run.finished",
                          let finished = try? decode(RunFinishedNotification.self, from: notification.params) {
                    routeFinished(finished)
                }
            }
            // Intentional shutdown/restart cancels this observer before closing
            // the connection. An uncancelled end is therefore always an
            // unexpected disconnect, even if the process termination handler
            // has already advanced backend.state to `.exited`.
            if !Task.isCancelled {
                alertMessage = "Backend connection ended. Restart to reconnect; no write operation was replayed."
                pendingPrompt = nil
            }
        }
        promptTask = Task {
            for await request in connection.uiRequests {
                guard !Task.isCancelled else { return }
                pendingPrompt = PendingPrompt(request: request)
            }
            pendingPrompt = nil
        }
    }

    private func clearSessionState() {
        client = nil
        initialization = nil
        doctor = nil
        credentials = []
        syncSources = []
        syncSourceOptions = [:]
        syncRun = SyncRunState()
        playlists = []
        playlistConfig = nil
        playlistActiveRunID = nil
        providerPlaylists = []
        freeDLConfig = nil
        freeDLRunID = nil
        freeDLOperation = nil
        freeDLRows = []
        freeDLSelectionOverrides = [:]
        freeDLCapturePlan = nil
        freeDLCaptureRunID = nil
        freeDLPromotionPlan = nil
        freeDLStage = nil
        freeDLStatusMessage = nil
        freeDLCaptureSources = [:]
        freeDLCaptureActivity = []
        rekordboxConfig = nil
        rekordboxRuntime = nil
        rekordboxInspect = nil
        rekordboxPlan = nil
        rekordboxRunID = nil
        rekordboxOperation = nil
        rekordboxStatusMessage = nil
        rekordboxApplyBlockers = []
        onboarding = nil
        startupAttention = nil
        configFile = nil
        configProblems = []
        configStatusMessage = nil
        playlistOperations = [:]
        bufferedFinished = [:]
        planSelectionOverrides = [:]
        planCursorBySource = [:]
        pendingPrompt = nil
    }

    private func applySyncFinished(_ finished: RunFinishedNotification) {
        syncRun.exitCode = finished.exitCode
        syncRun.terminalMessage = finished.error
        switch finished.exitCode {
        case 0: syncRun.phase = .succeeded
        case 4: syncRun.phase = .dependencyFailure
        case 5: syncRun.phase = .partialFailure
        case 130: syncRun.phase = .canceled
        default: syncRun.phase = .failed
        }
        if syncRun.terminalMessage == nil {
            syncRun.terminalMessage = syncRun.phase == .succeeded
                ? "Sync completed successfully."
                : "Sync finished with exit code \(finished.exitCode)."
        }
        planSelectionOverrides = [:]
        planCursorBySource = [:]
    }

    private func routeFinished(_ finished: RunFinishedNotification) {
        if finished.runID == syncRun.runID || (syncRun.runID == nil && syncRun.phase == .starting) {
            syncRun.runID = finished.runID
            applySyncFinished(finished)
            pendingPrompt = nil
            return
        }
        if finished.runID == freeDLRunID || (freeDLRunID == nil && freeDLOperation != nil) {
            freeDLRunID = finished.runID
            applyFreeDLFinished(finished)
            return
        }
        if finished.runID == rekordboxRunID || (rekordboxRunID == nil && rekordboxOperation != nil) {
            rekordboxRunID = finished.runID
            applyRekordboxFinished(finished)
            return
        }
        guard let operation = playlistOperations.removeValue(forKey: finished.runID) else {
            // Long-running methods may finish before their start response is
            // delivered. Hold the terminal frame until its operation registers.
            bufferedFinished[finished.runID] = finished
            return
        }
        applyPlaylistFinished(finished, operation: operation)
    }

    private func consumeBufferedFinished(_ runID: String) {
        guard let finished = bufferedFinished.removeValue(forKey: runID) else { return }
        if let operation = playlistOperations.removeValue(forKey: runID) {
            applyPlaylistFinished(finished, operation: operation)
        } else if freeDLOperation != nil {
            applyFreeDLFinished(finished)
        } else if rekordboxOperation != nil {
            applyRekordboxFinished(finished)
        } else {
            bufferedFinished[runID] = finished
        }
    }

    private func applyPlaylistFinished(_ finished: RunFinishedNotification, operation: PlaylistOperation) {
        playlistActiveRunID = nil
        switch operation {
        case .providerList:
            if finished.exitCode == 0,
               let result = finished.result,
               let listing = try? decode(ProviderPlaylistListResult.self, from: result) {
                providerPlaylists = listing.playlists
                playlistStatusMessage = "Loaded \(listing.playlists.count) playlists from Music."
            } else {
                playlistStatusMessage = "Provider listing ended without changing any snapshot."
            }
        case .refresh:
            if finished.exitCode == 0,
               let result = finished.result,
               let refresh = try? decode(PlaylistRefreshResult.self, from: result) {
                playlistStatusMessage =
                    "Snapshot refreshed: +\(refresh.changes.added), −\(refresh.changes.removed), \(refresh.changes.kept) unchanged."
                Task { await loadPlaylists() }
            } else {
                playlistStatusMessage = finished.exitCode == 130
                    ? "Refresh canceled. The previous valid snapshot was preserved."
                    : "Refresh failed. The previous valid snapshot was preserved."
            }
        }
    }

    private func applyFreeDLPlanEvent(_ event: FreeDLPlanEvent) {
        if let stage = event.stage {
            let progress = if let current = event.current, let total = event.total, total > 0 {
                " \(current)/\(total)"
            } else {
                ""
            }
            freeDLStage = [stage, event.status, event.detail].compactMap { $0 }.joined(separator: " · ") + progress
        }
        if var row = event.row {
            if let selected = freeDLSelectionOverrides[row.remoteID] {
                row.selected = selected && row.selectable
            }
            if let index = freeDLRows.firstIndex(where: { $0.remoteID == row.remoteID }) {
                freeDLRows[index] = row
            } else {
                freeDLRows.append(row)
            }
        }
        if var plan = event.plan {
            reapplyFreeDLOverrides(to: &plan)
            freeDLCapturePlan = plan
            freeDLRows = plan.rows
        }
        if let error = event.error {
            freeDLStatusMessage = error
        }
    }

    private func reapplyFreeDLOverrides(to plan: inout FreeDLCapturePlan) {
        for index in plan.rows.indices {
            if let selected = freeDLSelectionOverrides[plan.rows[index].remoteID] {
                plan.rows[index].selected = selected && plan.rows[index].selectable
            }
        }
    }

    private func applyFreeDLFinished(_ finished: RunFinishedNotification) {
        let operation = freeDLOperation
        freeDLRunID = nil
        freeDLOperation = nil
        freeDLStage = nil
        if finished.exitCode != 0 {
            freeDLStatusMessage = finished.exitCode == 130
                ? "Operation canceled; existing buffer and plans were left intact."
                : (finished.error ?? "Free DL operation failed with exit code \(finished.exitCode).")
            return
        }
        guard let result = finished.result else {
            freeDLStatusMessage = "Operation completed."
            return
        }
        switch operation {
        case .planning:
            if var plan = try? decode(FreeDLCapturePlan.self, from: result) {
                reapplyFreeDLOverrides(to: &plan)
                freeDLCapturePlan = plan
                freeDLRows = plan.rows
                freeDLStatusMessage = "Capture plan ready."
            }
        case .capture:
            freeDLCaptureRunID = freeDLCapturePlan?.runID
            freeDLStatusMessage = "Capture completed. Build a promotion plan when ready."
        case .promotionBuild:
            freeDLPromotionPlan = try? decode(FreeDLPromotionPlan.self, from: result)
            freeDLStatusMessage = freeDLPromotionPlan == nil
                ? "Promotion plan could not be decoded."
                : "Promotion plan ready for review."
        case .promotionApply:
            freeDLStatusMessage = "Promotion apply completed."
        case nil:
            break
        }
    }

    private func applyRekordboxFinished(_ finished: RunFinishedNotification) {
        let operation = rekordboxOperation
        rekordboxRunID = nil
        rekordboxOperation = nil
        if finished.exitCode != 0 {
            rekordboxStatusMessage = finished.exitCode == 130
                ? "Operation canceled. No apply was resumed or replayed."
                : (finished.error ?? "Rekordbox operation failed with exit code \(finished.exitCode).")
            return
        }
        switch operation {
        case .ensure:
            rekordboxStatusMessage = "Managed runtime is ready."
            Task { await loadRekordbox() }
        case .reset:
            rekordboxStatusMessage = "Managed runtime reset. Ensure it before inspection or planning."
            Task { await loadRekordbox() }
        case .inspect:
            if let result = finished.result {
                rekordboxInspect = try? decode(RekordboxInspectResult.self, from: result)
            }
            rekordboxStatusMessage = rekordboxInspect.map {
                "Read-only inspection found \($0.playlists.count) playlists and \($0.contents.count) tracks."
            } ?? "Inspection result could not be decoded."
        case .plan:
            if let result = finished.result,
               let decoded = try? decode(RekordboxPlanResult.self, from: result) {
                rekordboxPlan = RekordboxPlanPresentation(decoded.plan)
                rekordboxApplyBlockers = rekordboxPlan?.blockers ?? []
                rekordboxStatusMessage = rekordboxPlan == nil
                    ? "Plan result could not be decoded."
                    : "Checksummed plan ready at \(decoded.planPath)."
            }
        case .apply(let dryRun):
            rekordboxStatusMessage = dryRun
                ? "Dry run completed; no database write was requested."
                : "Apply completed after backup and post-write verification."
        case nil:
            break
        }
    }

    private func parseRekordboxBlockers(_ data: JSONValue?) -> [RekordboxPlanRowView] {
        guard let rows = data?.objectValue?["blockers"]?.arrayValue else { return [] }
        return rows.compactMap(RekordboxPlanRowView.init)
    }

    private func validationProblems(from error: Error) -> [String] {
        guard case JSONRPCConnectionError.remote(_, _, let data) = error else { return [] }
        return validationProblems(from: data)
    }

    private func validationProblems(from data: JSONValue?) -> [String] {
        guard let values = data?.objectValue?["problems"]?.arrayValue else { return [] }
        return values.compactMap(\.stringValue)
    }

    private func decode<T: Decodable>(_ type: T.Type, from value: JSONValue) throws -> T {
        try JSONDecoder.agent.decode(type, from: JSONEncoder.agent.encode(value))
    }
}

@MainActor
func performOrderedCancellation(
    answerPending: () async -> Void,
    cancelRun: () async -> Void
) async {
    // Preserve the Go/TUI ordering: release any pending wire request first.
    await answerPending()
    await cancelRun()
}
