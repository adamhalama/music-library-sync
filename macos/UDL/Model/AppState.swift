import AppKit
import Combine
import Foundation

@MainActor
final class AppState: ObservableObject {
    enum Destination: String, CaseIterable, Identifiable, Hashable {
        case onboarding = "Welcome"
        case home = "Home"
        case sync = "Run Sync"
        case freeDL = "SoundCloud Free DL"
        case rekordbox = "Rekordbox Sync"
        case playlists = "Playlists"
        case phoneLibrary = "Phone Library"
        case doctor = "Check System"
        case credentials = "Credentials"
        case config = "Advanced Config"
        var id: String { rawValue }
    }

    /// One derived attention value, read by the sidebar badges, Home, and
    /// Doctor alike, so the same condition is never counted two ways.
    struct Attention: Equatable {
        var doctorErrors = 0
        var doctorWarnings = 0
        var doctorPassed = 0
        var unhealthyCredentials = 0
        var rekordboxBlockers = 0
        var freeDLSelectable = 0
        var playlistsWithoutSnapshot = 0
        var phoneLibraryRemainingSteps = 0
        var configProblems = 0
        var syncNeedsYou = false
        var syncActive = false
        var syncRemaining = 0

        var hasBlockingWork: Bool { doctorErrors > 0 || unhealthyCredentials > 0 || rekordboxBlockers > 0 }
    }

    /// C15 — the backend can exit or EOF at any time, and a restart never
    /// replays mutating requests.
    struct BackendRecovery: Equatable {
        var message: String
        var interrupted: [Destination]
    }

    /// C14 — `config.writeFile` is guarded by the SHA of the content that was
    /// read. A mismatch is not a generic save error: nothing was written, and
    /// the user has two real choices. Keeping it as its own value rather than a
    /// string means the screen can offer both instead of printing a sentence.
    struct ConfigConflict: Equatable {
        var path: String
        var expectedSHA256: String
        var actualSHA256: String
    }

    /// C11 — a refresh that fails or is canceled keeps the previous snapshot,
    /// and that has to read differently from a refresh that succeeded. The
    /// message alone cannot carry it; the severity does.
    struct PlaylistStatus: Equatable {
        var message: String
        var severity: Severity
        /// True while the message describes an outcome that preserved the
        /// previous snapshot, so the screen can say so without re-deriving it.
        var preservedPreviousSnapshot = false
    }

    /// What a workflow last said about itself. A failure caused by an action on
    /// a screen belongs on that screen, so every workflow reports through one of
    /// these rather than through `alertMessage`, which `UDLApp` renders as a
    /// blocking modal and which is reserved for session-level failures no screen
    /// owns — launch, restart, and a protocol frame the app cannot answer.
    ///
    /// `ExpressibleByStringLiteral` keeps the informational assignments reading
    /// as plain strings; only a failure has to name its severity.
    struct WorkflowStatus: Equatable, ExpressibleByStringLiteral {
        var message: String
        var severity: Severity = .info

        init(message: String, severity: Severity = .info) {
            self.message = message
            self.severity = severity
        }

        init(stringLiteral value: String) {
            self.init(message: value)
        }

        static func failure(_ message: String) -> Self {
            Self(message: message, severity: .error)
        }

        /// A cancellation is not a failure — nothing went wrong — but it is not
        /// a success either, and the two must not read alike.
        static func canceled(_ message: String) -> Self {
            Self(message: message, severity: .warn)
        }
    }

    struct PendingPrompt: Identifiable {
        let id = UUID()
        let request: UIRequest
        var input = ""
    }

    let backend = AgentProcess()
    @Published var destination: Destination? = .home
    @Published private(set) var backendRecovery: BackendRecovery?
    /// Workflows that were in flight when the backend went away. They are not
    /// resumed; the screen says so until the user re-runs them.
    @Published private(set) var notResumed: Set<Destination> = []
    @Published private(set) var initialization: InitializeResult?
    @Published private(set) var doctor: DoctorResult?
    @Published private(set) var credentials: [CredentialStatus] = []
    @Published private(set) var isLoadingDoctor = false
    @Published private(set) var isLoadingCredentials = false
    /// Doctor and Credentials had no sink of their own, so every failure they
    /// caused was reported by a modal that named no screen.
    @Published private(set) var doctorStatus: WorkflowStatus?
    @Published private(set) var credentialStatus: WorkflowStatus?
    /// C1/C2 — a decoded `ui.selectRows` request. It is derived once here
    /// rather than in the view, because the sidebar, the header counter, and
    /// the status bar all need to know which source udl is blocked on.
    struct PlanPrompt: Identifiable, Equatable {
        let id: UUID
        let request: UIRequest
        let params: SelectRowsParams

        static func == (lhs: PlanPrompt, rhs: PlanPrompt) -> Bool { lhs.id == rhs.id }
    }

    @Published var pendingPrompt: PendingPrompt? {
        didSet { refreshPlanPrompt() }
    }
    /// Non-nil only while udl is blocked on a plan selection. The plan surface
    /// is docked in the Sync workspace, so this never becomes a sheet.
    @Published private(set) var planPrompt: PlanPrompt?
    /// The blocking modal `UDLApp` renders, reserved for session-level failures
    /// no screen owns: the backend failing to launch or restart, a protocol
    /// frame the app cannot decode, and a reply the app cannot deliver while the
    /// backend is blocked waiting for it. Everything a screen's own action can
    /// cause reports through that screen's `WorkflowStatus` instead — a modal
    /// that names no screen is how a failed first-run write once looked like
    /// nothing at all having happened.
    @Published var alertMessage: String?
    @Published var projectDirectory: URL
    @Published private(set) var syncSources: [SourceCapability] = []
    @Published var syncSourceOptions: [String: SyncSourceOptions] = [:]
    // Every default below comes from `SyncDefaults`, which is the only place
    // they are written. `startDryRunPlan()` forces dry run on; reaching Run Sync
    // from the sidebar has to agree with it, and reading one constant is how
    // these two routes are kept from disagreeing.
    @Published var syncDryRun = SyncDefaults.dryRun
    @Published var syncUnlimited = SyncDefaults.unlimited
    @Published var syncPlanLimit = SyncDefaults.planLimit
    @Published var syncTimeoutSeconds = SyncDefaults.timeoutSeconds
    @Published var syncPlanWindow = SyncDefaults.planWindow
    @Published var syncAskOnExisting = SyncDefaults.askOnExisting
    @Published var syncScanGaps = SyncDefaults.scanGaps
    @Published var syncNoPreflight = SyncDefaults.noPreflight
    @Published var syncTrackStatus = SyncDefaults.trackStatus
    @Published private(set) var syncValidationMessage: String?
    @Published private(set) var syncRun = SyncRunState()
    @Published private(set) var selectedSyncSourceID: String?
    @Published private(set) var playlists: [PlaylistListRow] = []
    @Published private(set) var playlistConfig: PlaylistConfigResult?
    @Published private(set) var playlistActiveRunID: String?
    @Published private(set) var playlistStatus: PlaylistStatus?
    @Published private(set) var providerPlaylists: [ProviderPlaylist] = []
    /// One-shot cross-workflow navigation intents. Keeping the selected
    /// snapshot in AppState lets a handoff survive the destination view being
    /// destroyed and recreated without making it a sticky global selection.
    @Published private(set) var requestedPlaylistID: String?
    @Published private(set) var requestedRekordboxPlaylistID: String?
    @Published private(set) var freeDLConfig: FreeDLConfigResult?
    @Published private(set) var freeDLRunID: String?
    @Published private(set) var freeDLOperation: FreeDLOperation?
    @Published private(set) var freeDLRows: [FreeDLPlanRow] = []
    @Published var freeDLSelectionOverrides: [String: Bool] = [:]
    @Published private(set) var freeDLCapturePlan: FreeDLCapturePlan?
    @Published private(set) var freeDLCaptureRunID: String?
    @Published private(set) var freeDLPromotionPlan: FreeDLPromotionPlan?
    /// Promotion rows carry their own `selected` flag and `ApplyPromotionPlan`
    /// honours it, so the promotion table needs its own override map rather
    /// than reusing the capture one keyed by remote ID.
    @Published var freeDLPromotionOverrides: [String: Bool] = [:]
    /// C7 — the fourth run. `freedl.promote.apply` returns a result but leaves
    /// no state a later call can re-read, so the phase stepper needs this to
    /// tell "applied" from "never run" without inventing either.
    @Published private(set) var freeDLPromotionApplied = false
    @Published private(set) var freeDLStage: String?
    @Published private(set) var freeDLStatus: WorkflowStatus?
    @Published private(set) var freeDLCaptureSources: [String: SourceSnapshot] = [:]
    @Published private(set) var freeDLCaptureActivity: [OutputEvent] = []
    @Published private(set) var rekordboxConfig: RekordboxConfigResult?
    @Published private(set) var rekordboxRuntime: RekordboxDependencyResult?
    @Published private(set) var rekordboxInspect: RekordboxInspectResult?
    @Published private(set) var rekordboxPlan: RekordboxPlanPresentation?
    @Published private(set) var rekordboxRunID: String?
    @Published private(set) var rekordboxOperation: RekordboxOperation?
    @Published private(set) var rekordboxStatus: WorkflowStatus?
    @Published private(set) var rekordboxApplyBlockers: [RekordboxPlanRowView] = []
    /// C10 — the last precondition the backend refused on. The protocol
    /// reports no process state and no checksum state before an attempt, so
    /// this is only ever set from a real backend refusal, never guessed.
    @Published private(set) var rekordboxObstacle: RekordboxObstacle?
    // The Phone Library workflow lives in AppState+PhoneLibrary.swift, so its
    // state cannot be `private(set)`: Swift scopes that to the declaring file.
    // Nothing outside that extension writes them.
    @Published var navidromeConfig: NavidromeConfigResult?
    @Published var navidromeStatus: NavidromeStatus?
    @Published var navidromeSetupPlan: NavidromeSetupPlanPresentation?
    @Published var navidromeFavoritePlan: NavidromeFavoritePlanPresentation?
    @Published var navidromeFavoriteResult: NavidromeFavoriteApplyResult?
    /// What the server has starred right now. Read on demand — the return path
    /// is an explicit action, never a side effect of loading a screen.
    @Published var navidromeStarred: NavidromeStarredListResult?
    @Published var navidromeGenreDerivation: NavidromeGenreDerivation?
    /// Per-genre approval for the derived HARD BOUNCE allowlist. The derivation
    /// is evidence; the user removes rather than re-adds.
    @Published var navidromeGenreApproval: [String: Bool] = [:]
    @Published var navidromePlaylistRefresh: NavidromePlaylistRefreshResult?
    @Published var phoneLibraryOperation: PhoneLibraryOperation?
    @Published var phoneLibraryRunID: String?
    @Published var phoneLibraryStatus: WorkflowStatus?
    @Published private(set) var onboarding: OnboardingResult?
    @Published private(set) var startupAttention: StartupAttentionResult?
    @Published private(set) var configFile: ConfigFileResult?
    @Published private(set) var configProblems: [String] = []
    @Published private(set) var configStatusMessage: String?
    /// C14 — set only by a real `-32003` refusal from `config.writeFile`.
    @Published private(set) var configConflict: ConfigConflict?

    /// Internal rather than private because the Phone Library workflow is an
    /// extension in another file.
    private(set) var client: UDLClient?
    private var notificationTask: Task<Void, Never>?
    private var progressNotificationTask: Task<Void, Never>?
    private var promptTask: Task<Void, Never>?
    private var syncCancellationWatchdog: Task<Void, Never>?
    private var playlistOperations: [String: PlaylistOperation] = [:]
    private var bufferedFinished: [String: RunFinishedNotification] = [:]
    private var planSelectionOverrides: [String: [String: Bool]] = [:]
    private var planCursorBySource: [String: String] = [:]
    private var syncSidebarSelectionIsExplicit = false

    init() {
        projectDirectory = FileManager.default.homeDirectoryForCurrentUser
    }

    var backendVersion: String {
        initialization?.build.version ?? "—"
    }

    /// Derived from protocol state only. Nothing here is stored app-side or
    /// estimated; every field names the response it came from.
    var attention: Attention {
        var value = Attention()
        for check in doctor?.checks ?? [] {
            switch Severity.forCheck(check) {
            case .error: value.doctorErrors += 1
            case .warn: value.doctorWarnings += 1
            case .ok: value.doctorPassed += 1
            default: break
            }
        }
        value.unhealthyCredentials = credentials.filter { credentialSeverity($0) == .error }.count
        value.rekordboxBlockers = rekordboxApplyBlockers.count
        value.freeDLSelectable = freeDLRows.filter(\.selectable).count
        value.playlistsWithoutSnapshot = playlists.filter { $0.snapshot == nil || $0.snapshotError != nil }.count
        value.configProblems = configProblems.count
        // Only badge Phone Library once its state has actually been read;
        // before that the count would be an invented number of steps.
        value.phoneLibraryRemainingSteps = navidromeStatus == nil ? 0 : phoneLibraryProgress.remaining
        value.syncActive = syncRun.phase.isActive
        value.syncNeedsYou = pendingPrompt != nil
        if let progress = syncRun.progress?.progress.global {
            value.syncRemaining = max(progress.total - progress.completed, 0)
        }
        return value
    }

    /// C1 — only confirm and masked-input prompts stay modal. The plan prompt
    /// is a workspace, not a sheet, so it must never reach `.sheet(item:)`.
    var modalPrompt: PendingPrompt? {
        get { pendingPrompt?.request.kind == .selectRows ? nil : pendingPrompt }
        set {
            guard newValue == nil, pendingPrompt?.request.kind != .selectRows else { return }
            pendingPrompt = nil
        }
    }

    private func refreshPlanPrompt() {
        guard let prompt = pendingPrompt, prompt.request.kind == .selectRows else {
            planPrompt = nil
            return
        }
        if planPrompt?.id == prompt.id { return }
        guard let params = try? decode(SelectRowsParams.self, from: prompt.request.params) else {
            planPrompt = nil
            alertMessage = "The backend sent an invalid plan-selection request."
            return
        }
        planPrompt = PlanPrompt(id: prompt.id, request: prompt.request, params: params)
        syncRun.installPlan(params, selectedIndices: initialPlanSelection(params))
        selectedSyncSourceID = SyncSourceFocus.target(
            current: selectedSyncSourceID,
            selectionIsExplicit: syncSidebarSelectionIsExplicit,
            pendingInput: params.sourceID,
            active: activeSyncSourceID
        )
        syncSidebarSelectionIsExplicit = false
        // The plan is docked, so the workspace holding it has to be on screen
        // for the "Needs you" state to mean anything.
        destination = .sync
    }

    /// The capability record for a source, used to decide whether the plan
    /// window control is supported (C5) rather than inferring from the adapter.
    func syncCapability(forSourceID sourceID: String) -> SourceCapability? {
        syncSources.first { $0.sourceID == sourceID }
    }

    /// C2 — the source udl is currently working on, if any.
    var activeSyncSourceID: String? {
        if let planPrompt { return planPrompt.params.sourceID }
        let progressSource = syncRun.progress?.progress.source.id ?? ""
        return progressSource.isEmpty ? nil : progressSource
    }

    func selectSyncSource(_ sourceID: String) {
        guard syncRun.requestedSourceIDs.contains(sourceID) || syncRun.sourceTables[sourceID] != nil else { return }
        selectedSyncSourceID = sourceID
        syncSidebarSelectionIsExplicit = true
    }

    func setSyncTableSelection(sourceID: String, selectedIndices: Set<Int>) {
        syncRun.updateDraft(sourceID: sourceID, selectedIndices: selectedIndices)
    }

    func setSyncTableDownloadOrder(sourceID: String, order: DownloadOrder) {
        syncRun.updateDraft(sourceID: sourceID, downloadOrder: order)
    }

    func setSyncTablePlanWindow(sourceID: String, window: PlanWindow) {
        syncRun.updateDraft(sourceID: sourceID, planWindow: window)
    }

    /// C2 — "Source 2 of 4". Nil when the run has not reached a source yet, so
    /// the header says "Planning…" instead of inventing a position.
    var syncSourcePosition: (index: Int, total: Int)? {
        activeSyncSourceID.flatMap(syncSourcePosition(for:))
    }

    func syncSourcePosition(for sourceID: String) -> (index: Int, total: Int)? {
        let ids = syncRun.requestedSourceIDs
        guard !ids.isEmpty, let index = ids.firstIndex(of: sourceID) else { return nil }
        return (index + 1, ids.count)
    }

    /// C2 — exactly one source can read `Needs you`; sources udl has not
    /// reached yet emit no events, so they read `Queued`, never `Done`.
    func syncSourceLifecycle(_ sourceID: String) -> Lifecycle {
        if planPrompt?.params.sourceID == sourceID { return .needsYou }
        if let table = syncRun.sourceTables[sourceID] { return Lifecycle(wire: table.lifecycle) }
        if let snapshot = syncRun.sources[sourceID] { return Lifecycle(wire: snapshot.lifecycle) }
        if syncRun.progress?.progress.source.id == sourceID { return .planning }
        // "Queued" is only true while the run is still going. Once it has
        // ended, a source that never reported was never reached, and saying
        // "Queued" would imply work that is still coming.
        guard syncRun.phase.isActive else { return .notRun }
        return .queued
    }

    /// C13 — `external_override` is a working credential, but not the one this
    /// app manages, so it is a warning rather than a pass. Only `.error` feeds
    /// `unhealthyCredentials`, so this does not inflate the sidebar badge.
    func credentialSeverity(_ credential: CredentialStatus) -> Severity {
        switch credential.health {
        case "available": .ok
        case "external_override": .warn
        case "needs_refresh", "missing", "unavailable": .error
        default: .warn
        }
    }

    func dismissBackendRecovery() {
        backendRecovery = nil
    }

    /// C15 — the message a workflow screen shows after the backend came back.
    func notResumedMessage(for destination: Destination) -> String? {
        guard notResumed.contains(destination) else { return nil }
        return "Not resumed — the backend restarted and nothing was replayed. Re-run to continue."
    }

    func clearNotResumed(_ destination: Destination) {
        notResumed.remove(destination)
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
            // The connection is healthy again, but nothing was replayed:
            // `notResumed` deliberately survives so each workflow still says so.
            backendRecovery = nil
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
        progressNotificationTask?.cancel()
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
            doctorStatus = .failure("Doctor could not run: \(error.localizedDescription)")
        }
    }

    func refreshCredentials() async {
        guard let client else { return }
        isLoadingCredentials = true
        defer { isLoadingCredentials = false }
        do {
            credentials = try await client.listCredentials().credentials
        } catch {
            credentialStatus = .failure("Credentials could not load: \(error.localizedDescription)")
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
            credentialStatus = .failure("Keychain save failed: \(error.localizedDescription)")
            return false
        }
    }

    func clearCredential(_ kind: CredentialKind) async {
        guard let client else { return }
        do {
            _ = try await client.clearCredential(kind)
            await refreshCredentials()
        } catch {
            credentialStatus = .failure("Keychain clear failed: \(error.localizedDescription)")
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
            syncValidationMessage = "Sources could not load: \(error.localizedDescription)"
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
        clearNotResumed(.sync)
        let windows = Dictionary(uniqueKeysWithValues: selected.compactMap { source in
            syncSourceOptions[source.id].map { (source.id, $0.planWindow) }
        })
        let orders = Dictionary(uniqueKeysWithValues: selected.compactMap { source in
            syncSourceOptions[source.id].map { (source.id, $0.downloadOrder) }
        })
        syncCancellationWatchdog?.cancel()
        syncRun = SyncRunState(phase: .starting, requestedSourceIDs: selected.map(\.id))
        selectedSyncSourceID = selected.first?.id
        syncSidebarSelectionIsExplicit = false
        do {
            let started = try await client.startSync(SyncStartParams(
                sourceIDs: selected.map(\.id),
                dryRun: syncDryRun,
                timeoutSeconds: syncTimeoutSeconds,
                plan: true,
                planLimit: syncUnlimited ? 0 : syncPlanLimit,
                planWindow: syncPlanWindow,
                planWindowBySource: windows,
                downloadOrderBySource: orders,
                askOnExisting: syncAskOnExisting.value,
                askOnExistingSet: syncAskOnExisting.isSet,
                scanGaps: syncScanGaps,
                noPreflight: syncNoPreflight,
                trackStatus: syncTrackStatus
            ))
            syncRun.registerRunID(started.runID)
            await sendPendingSyncCancellationIfReady()
        } catch {
            syncRun.phase = .failed
            syncRun.terminalMessage = error.localizedDescription
        }
    }

    /// C1 — there is no plan-preview method. The plan only exists inside a
    /// run, so the shell's primary action starts a reversible dry run whose
    /// per-source `ui.selectRows` prompt *is* the plan surface.
    func startDryRunPlan() async {
        syncDryRun = true
        destination = .sync
        await startSync()
    }

    var isAnythingRunning: Bool {
        syncRun.phase.isActive || freeDLRunID != nil || rekordboxRunID != nil || playlistActiveRunID != nil
    }

    /// The one phrase every control disabled by a live run states, naming the
    /// run that holds it. Derived from exactly the state `isAnythingRunning`
    /// reads, so the control and its reason cannot disagree; `nil` means
    /// nothing is running and nothing needs explaining.
    var busyReason: String? {
        if syncRun.phase.isActive { return "A sync run is in progress. Stop it to start other work." }
        if freeDLRunID != nil { return "A Free DL step is running. Stop it to start other work." }
        if rekordboxRunID != nil { return "A Rekordbox operation is running. Stop it to start other work." }
        if playlistActiveRunID != nil { return "A playlist operation is running. Stop it to start other work." }
        return nil
    }

    /// ⌘. — cancels whichever run is live, preserving the ordered-cancellation
    /// invariant in each workflow's own cancel path.
    func cancelActiveWork() async {
        if syncRun.phase.isActive {
            await cancelActiveSync()
        } else if freeDLRunID != nil {
            await cancelFreeDLOperation()
        } else if rekordboxRunID != nil {
            await cancelRekordboxOperation()
        } else if playlistActiveRunID != nil {
            await cancelPlaylistOperation()
        }
    }

    func cancelActiveSync() async {
        guard syncRun.requestCancellation() else { return }
        scheduleSyncCancellationWatchdog()
        await sendPendingSyncCancellationIfReady()
    }

    func resetSyncRun() {
        guard !syncRun.phase.isActive else { return }
        syncCancellationWatchdog?.cancel()
        syncRun = SyncRunState()
        selectedSyncSourceID = nil
        syncSidebarSelectionIsExplicit = false
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
            playlistStatus = PlaylistStatus(
                message: "Playlist cache could not load: \(error.localizedDescription)",
                severity: .error,
                preservedPreviousSnapshot: true
            )
        }
    }

    func savePlaylistDefinition(_ definition: PlaylistDefinition) async -> Bool {
        guard let client else { return false }
        do {
            _ = try await client.savePlaylistDefinition(definition)
            await loadPlaylists()
            return true
        } catch {
            playlistStatus = PlaylistStatus(
                message: "Playlist definition could not save: \(error.localizedDescription)",
                severity: .error,
                preservedPreviousSnapshot: true
            )
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
            playlistStatus = PlaylistStatus(
                message: "Playlist config could not save: \(error.localizedDescription)",
                severity: .error,
                preservedPreviousSnapshot: true
            )
            return false
        }
    }

    func refreshPlaylist(_ playlistID: String) async {
        guard let client, playlistActiveRunID == nil else { return }
        clearNotResumed(.playlists)
        let provider = playlists.first { $0.id == playlistID }?.definition.providerDisplayName ?? "provider"
        playlistStatus = PlaylistStatus(
            message: "Refreshing from \(provider)… the existing snapshot remains active until success.",
            severity: .info
        )
        do {
            let started = try await client.refreshPlaylist(playlistID)
            playlistActiveRunID = started.runID
            playlistOperations[started.runID] = .refresh(playlistID)
            consumeBufferedFinished(started.runID)
        } catch {
            playlistStatus = PlaylistStatus(
                message: "Refresh did not start. The saved snapshot was not changed.",
                severity: .error,
                preservedPreviousSnapshot: true
            )
        }
    }

    func openPlaylist(_ playlistID: String) {
        requestedPlaylistID = playlistID
        destination = .playlists
    }

    func consumeRequestedPlaylistID() -> String? {
        defer { requestedPlaylistID = nil }
        return requestedPlaylistID
    }

    func openRekordboxPlaylist(_ playlistID: String) {
        requestedRekordboxPlaylistID = playlistID
        destination = .rekordbox
    }

    func consumeRequestedRekordboxPlaylistID() -> String? {
        defer { requestedRekordboxPlaylistID = nil }
        return requestedRekordboxPlaylistID
    }

    func discoverProviderPlaylists() async {
        guard let client, playlistActiveRunID == nil else { return }
        playlistStatus = PlaylistStatus(message: "Requesting playlists from Music…", severity: .info)
        do {
            let started = try await client.listProviderPlaylists(
                ProviderPlaylistListParams(provider: "apple_music")
            )
            playlistActiveRunID = started.runID
            playlistOperations[started.runID] = .providerList
            consumeBufferedFinished(started.runID)
        } catch {
            playlistStatus = PlaylistStatus(
                message: "Music playlist discovery did not start. No snapshot was touched.",
                severity: .error,
                preservedPreviousSnapshot: true
            )
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
            freeDLStatus = .failure("Free DL config could not load: \(error.localizedDescription)")
        }
    }

    func saveFreeDLConfig(_ config: FreeDLConfig) async -> Bool {
        guard let client else { return false }
        do {
            freeDLConfig = try await client.writeFreeDLConfig(config)
            return true
        } catch {
            freeDLStatus = .failure("Free DL config could not save: \(error.localizedDescription)")
            return false
        }
    }

    func startFreeDLPlan(jobID: String, playlistID: String? = nil) async {
        guard let client, freeDLRunID == nil else { return }
        freeDLOperation = .planning
        clearNotResumed(.freeDL)
        freeDLRows = []
        freeDLCapturePlan = nil
        freeDLPromotionPlan = nil
        freeDLPromotionOverrides = [:]
        freeDLPromotionApplied = false
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
            freeDLStatus = .failure("Free DL planning did not start: \(error.localizedDescription)")
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

    /// The promotion plan's own selection. `Service.ApplyPromotionPlan` skips
    /// any row whose `selected` is false or whose action is `skip`, so this
    /// override has to reach the plan that is sent, not just the table.
    func setFreeDLPromotionSelection(_ id: String, _ selected: Bool) {
        freeDLPromotionOverrides[id] = selected
        guard let index = freeDLPromotionPlan?.rows.firstIndex(where: { $0.id == id }) else { return }
        let applicable = freeDLPromotionPlan?.rows[index].isApplicable ?? false
        freeDLPromotionPlan?.rows[index].selected = selected && applicable
    }

    func startFreeDLCapture() async {
        guard let client, var plan = freeDLCapturePlan, freeDLRunID == nil else { return }
        reapplyFreeDLOverrides(to: &plan)
        let selected = plan.rows.filter { $0.selectable && $0.selected }.map(\.remoteID)
        guard !selected.isEmpty else {
            freeDLStatus = .failure("Select at least one Free DL row before capture.")
            return
        }
        freeDLOperation = .capture
        freeDLCaptureSources = [:]
        freeDLCaptureActivity = []
        freeDLStatus = "Capturing selected downloads…"
        do {
            let started = try await client.startFreeDLCapture(
                FreeDLCaptureStartParams(plan: plan, selectedRemoteIDs: selected)
            )
            freeDLRunID = started.runID
            consumeBufferedFinished(started.runID)
        } catch {
            freeDLOperation = nil
            freeDLStatus = .failure("Capture did not start: \(error.localizedDescription)")
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
            freeDLStatus = .failure("Promotion planning did not start: \(error.localizedDescription)")
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
            freeDLStatus = .failure("Promotion apply did not start: \(error.localizedDescription)")
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
            rekordboxStatus = .failure("Rekordbox setup could not load: \(error.localizedDescription)")
        }
    }

    func saveRekordboxConfig(_ config: RekordboxConfig) async -> Bool {
        guard let client else { return false }
        do {
            rekordboxConfig = try await client.writeRekordboxConfig(config)
            return true
        } catch {
            rekordboxStatus = .failure("Rekordbox config could not save: \(error.localizedDescription)")
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
        // A fresh plan re-reads both inputs, so any drift the previous apply
        // refused on is answered by definition.
        rekordboxObstacle = nil
        await startRekordboxOperation(.plan) { client in
            try await client.planRekordbox(params)
        }
    }

    func applyRekordbox(dryRun: Bool) async {
        guard let client, let plan = rekordboxPlan else { return }
        guard !plan.checksum.isEmpty else {
            rekordboxStatus = .failure("The plan has no checksum and cannot be applied.")
            return
        }
        guard plan.blockers.isEmpty else {
            rekordboxApplyBlockers = plan.blockers
            rekordboxStatus = .failure("Resolve every missing or ambiguous track before apply.")
            return
        }
        rekordboxOperation = .apply(dryRun: dryRun)
        rekordboxStatus = dryRun ? "Validating dry run…" : "Applying after backend integrity, process, and backup checks…"
        do {
            let started = try await client.applyRekordbox(
                RekordboxApplyParams(plan: plan.value, dryRun: dryRun)
            )
            rekordboxRunID = started.runID
            consumeBufferedFinished(started.runID)
        } catch JSONRPCConnectionError.remote(_, let message, let data) {
            rekordboxOperation = nil
            rekordboxApplyBlockers = parseRekordboxBlockers(data)
            // C9/C10 — `rekordbox.apply` validates the plan synchronously, so
            // this is where checksum drift and a refused partial mirror land.
            rekordboxObstacle = RekordboxObstacle(backendMessage: message)
            rekordboxStatus = .failure(
                message.localizedCaseInsensitiveContains("checksum")
                    ? "Plan integrity check failed: \(message)"
                    : message
            )
        } catch {
            rekordboxOperation = nil
            rekordboxObstacle = RekordboxObstacle(backendMessage: error.localizedDescription)
            rekordboxStatus = .failure(error.localizedDescription)
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
        clearNotResumed(.rekordbox)
        rekordboxOperation = operation
        rekordboxStatus = "Starting…"
        do {
            let started = try await start(client)
            rekordboxRunID = started.runID
            consumeBufferedFinished(started.runID)
        } catch {
            rekordboxOperation = nil
            rekordboxStatus = .failure(error.localizedDescription)
        }
    }

    func loadConfigEditor() async {
        guard let client else { return }
        do {
            configFile = try await client.readConfigFile()
            configProblems = []
            configStatusMessage = nil
            // Re-reading is one of C14's two ways out of a conflict: the SHA
            // the next write sends is now the one on disk.
            configConflict = nil
        } catch {
            configProblems = validationProblems(from: error)
            configStatusMessage = "Configuration could not load: \(error.localizedDescription)"
        }
    }

    /// - Parameter overwritingExternalChanges: C14's second way out. Sending no
    ///   expected SHA is the only thing that makes `config.writeFile` accept a
    ///   file that changed on disk, so it is a deliberate, separately named
    ///   call rather than a retry of the same one.
    func saveMainConfig(
        _ config: MainConfig,
        path: String? = nil,
        creating: Bool = false,
        overwritingExternalChanges: Bool = false
    ) async -> Bool {
        guard let client else { return false }
        configProblems = []
        do {
            _ = try await client.validateConfig(config)
            let saved = try await client.writeConfigFile(ConfigFileParams(
                path: path ?? configFile?.path ?? "",
                config: config,
                expectedContentSHA256: (creating || overwritingExternalChanges)
                    ? nil
                    : configFile?.contentSHA256
            ))
            configFile = saved
            configConflict = nil
            configStatusMessage = overwritingExternalChanges
                ? "Saved canonical configuration, replacing the version that had changed on disk."
                : "Saved canonical configuration."
            return true
        } catch JSONRPCConnectionError.remote(let code, let message, let data) {
            configProblems = validationProblems(from: data)
            if code == -32003 {
                // C14 — nothing was written. This is not a save error; it is a
                // choice between reloading and deliberately overwriting.
                configConflict = ConfigConflict(
                    path: conflictField(in: data, "path") ?? path ?? configFile?.path ?? "",
                    expectedSHA256: conflictField(in: data, "expected_content_sha256")
                        ?? configFile?.contentSHA256 ?? "",
                    actualSHA256: conflictField(in: data, "actual_content_sha256") ?? ""
                )
                configStatusMessage = nil
            } else {
                configStatusMessage = message
            }
            return false
        } catch {
            configStatusMessage = error.localizedDescription
            return false
        }
    }

    private func conflictField(in data: JSONValue?, _ key: String) -> String? {
        guard let value = data?.objectValue?[key]?.stringValue, !value.isEmpty else { return nil }
        return value
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
            destination = .home
            return true
        }
        return false
    }

    func useProjectDirectory(_ url: URL) async {
        guard url.isFileURL else { return }
        projectDirectory = url.standardizedFileURL
        await restart()
    }

    @discardableResult
    func answerPrompt(result: JSONValue) async -> Bool {
        guard let prompt = pendingPrompt, let connection = backend.connection else { return false }
        do {
            try await connection.respond(to: prompt.request, result: result)
        } catch {
            alertMessage = error.localizedDescription
            return false
        }
        if pendingPrompt?.id == prompt.id {
            pendingPrompt = nil
        }
        return true
    }

    @discardableResult
    func answerPlanSelection(_ result: SelectRowsResult, request: UIRequest) async -> Bool {
        // The UI's copied source settings must change before Go receives a
        // rebuild reply, matching the engine/TUI cross-boundary invariant.
        if result.rebuild,
           let params = try? decode(SelectRowsParams.self, from: request.params) {
            setSourcePlanWindow(params.sourceID, result.planWindow)
        }
        guard let encoded = try? result.jsonValue(using: .agent) else {
            syncRun.terminalMessage = "Plan response could not be encoded. Review the selection and try again."
            return false
        }
        guard await answerPrompt(result: encoded) else {
            syncRun.terminalMessage = "Plan response could not be delivered. The table remains editable; try Continue again."
            return false
        }
        if !result.rebuild,
           let params = try? decode(SelectRowsParams.self, from: request.params) {
            syncRun.acceptPlan(sourceID: params.sourceID)
        }
        return true
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

    /// "Reset to defaults" on the plan surface: drop this session's remembered
    /// overrides so the next read of `initialPlanSelection` falls back to the
    /// backend's own `selected_by_default` for every row.
    func resetPlanSelection(sourceID: String) {
        planSelectionOverrides[sourceID] = nil
        planCursorBySource[sourceID] = nil
    }

    /// C17 — restores the values the GUI used to hardcode, so "back to how it
    /// was" is one click rather than five.
    func resetSyncAdvanced() {
        syncPlanWindow = SyncDefaults.planWindow
        syncAskOnExisting = SyncDefaults.askOnExisting
        syncScanGaps = SyncDefaults.scanGaps
        syncNoPreflight = SyncDefaults.noPreflight
        syncTrackStatus = SyncDefaults.trackStatus
    }

    func rememberedPlanCursor(sourceID: String, rows: [PlanRow]) -> String? {
        guard let cursor = planCursorBySource[sourceID],
              rows.contains(where: { $0.id == cursor }) else {
            return nil
        }
        return cursor
    }

    @discardableResult
    func cancelPrompt() async -> Bool {
        guard let prompt = pendingPrompt else { return true }
        guard let connection = backend.connection else { return false }
        do {
            try await connection.respond(to: prompt.request, result: prompt.request.kind.canceledResult)
            if pendingPrompt?.id == prompt.id {
                pendingPrompt = nil
            }
            return true
        } catch {
            // The backend is blocked waiting for this reply, so failure to
            // deliver it is one of the few session-level errors that owns a
            // modal. Keep the prompt visible so the state is not fabricated.
            alertMessage = error.localizedDescription
            return false
        }
    }

    private func sendPendingSyncCancellationIfReady() async {
        guard syncRun.beginCancellationRequest(), let runID = syncRun.runID else { return }
        let promptReplyDelivered = await cancelPrompt()
        guard let client else {
            syncRun.failCancellation("The backend client is unavailable.")
            return
        }
        do {
            let result = try await client.cancelRun(runID)
            guard result.canceled else {
                syncRun.failCancellation("The backend did not acknowledge the request.")
                return
            }
            syncRun.acknowledgeCancellation()
        } catch JSONRPCConnectionError.remote(let code, _, _) where code == -32002 && promptReplyDelivered {
            // If our pending UI reply reached Go, `run not found` means its
            // terminal path won the race and removed the registry entry.
            syncRun.acknowledgeCancellation()
        } catch {
            syncRun.failCancellation(error.localizedDescription)
        }
    }

    private func scheduleSyncCancellationWatchdog() {
        syncCancellationWatchdog?.cancel()
        syncCancellationWatchdog = Task { [weak self] in
            try? await Task.sleep(for: .seconds(2))
            guard !Task.isCancelled else { return }
            self?.syncRun.markStillStopping()
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
        progressNotificationTask?.cancel()
        promptTask?.cancel()
        notificationTask = Task.detached { [weak self] in
            for await notification in connection.notifications {
                guard !Task.isCancelled,
                      let data = try? JSONEncoder.agent.encode(notification.params) else { continue }
                switch notification.method {
                case "freedl.planEvent":
                    if let event = try? JSONDecoder.agent.decode(FreeDLPlanEventNotification.self, from: data) {
                        await self?.applyFreeDLPlanNotification(event)
                    }
                case "sync.event":
                    if let event = try? JSONDecoder.agent.decode(SyncEventNotification.self, from: data) {
                        await self?.applyLosslessSyncEvent(event)
                    }
                case "run.finished":
                    if let event = try? JSONDecoder.agent.decode(RunFinishedNotification.self, from: data) {
                        await self?.routeFinished(event)
                    }
                default:
                    break
                }
            }
            // Intentional shutdown/restart cancels this observer before closing
            // the connection. An uncancelled end is therefore always an
            // unexpected disconnect, even if the process termination handler
            // has already advanced backend.state to `.exited`.
            if !Task.isCancelled {
                await self?.handleUnexpectedDisconnect()
            }
        }
        progressNotificationTask = Task.detached { [weak self] in
            for await notification in connection.progressNotifications {
                guard !Task.isCancelled,
                      notification.method == "sync.progress",
                      let data = try? JSONEncoder.agent.encode(notification.params),
                      let progress = try? JSONDecoder.agent.decode(SyncProgressNotification.self, from: data) else {
                    continue
                }
                await self?.applySyncProgress(progress)
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

    private func applyFreeDLPlanNotification(_ notification: FreeDLPlanEventNotification) {
        guard notification.runID == freeDLRunID || (freeDLRunID == nil && freeDLOperation == .planning) else { return }
        freeDLRunID = notification.runID
        applyFreeDLPlanEvent(notification.event)
    }

    private func applyLosslessSyncEvent(_ notification: SyncEventNotification) async {
        if notification.runID == syncRun.runID || (syncRun.runID == nil && syncRun.phase.isActive) {
            syncRun.registerRunID(notification.runID)
            let sourceID = notification.event.sourceID ?? notification.source.rows.first?.sourceID ?? ""
            if !sourceID.isEmpty {
                syncRun.mergeSnapshot(sourceID: sourceID, snapshot: notification.source)
                selectedSyncSourceID = SyncSourceFocus.target(
                    current: selectedSyncSourceID,
                    selectionIsExplicit: syncSidebarSelectionIsExplicit,
                    pendingInput: planPrompt?.params.sourceID,
                    active: sourceID
                )
            }
            syncRun.activity.append(notification.event)
            if syncRun.activity.count > 200 {
                syncRun.activity.removeFirst(syncRun.activity.count - 200)
            }
            await sendPendingSyncCancellationIfReady()
            return
        }
        guard notification.runID == freeDLRunID, freeDLOperation == .capture else { return }
        let sourceID = notification.event.sourceID ?? notification.source.rows.first?.sourceID ?? ""
        if !sourceID.isEmpty {
            freeDLCaptureSources[sourceID] = notification.source
        }
        freeDLCaptureActivity.append(notification.event)
        if freeDLCaptureActivity.count > 200 {
            freeDLCaptureActivity.removeFirst(freeDLCaptureActivity.count - 200)
        }
    }

    private func applySyncProgress(_ notification: SyncProgressNotification) {
        if notification.runID == syncRun.runID || (syncRun.runID == nil && syncRun.phase.isActive) {
            syncRun.registerRunID(notification.runID)
            guard syncRun.applyProgress(notification) else { return }
            selectedSyncSourceID = SyncSourceFocus.target(
                current: selectedSyncSourceID,
                selectionIsExplicit: syncSidebarSelectionIsExplicit,
                pendingInput: planPrompt?.params.sourceID,
                active: notification.sourceID
            )
            return
        }
        guard notification.runID == freeDLRunID, freeDLOperation == .capture,
              let row = notification.row else { return }
        let existing = freeDLCaptureSources[notification.sourceID] ?? SourceSnapshot(
            lifecycle: "running",
            confirmed: true,
            rows: [],
            activity: []
        )
        freeDLCaptureSources[notification.sourceID] = existing.merging(row)
    }

    /// C15 — an unexpected EOF or backend exit. Nothing is replayed, so every
    /// in-flight workflow is marked interrupted and its screen says so instead
    /// of spinning forever on a run that no longer exists.
    private func handleUnexpectedDisconnect() {
        var interrupted: [Destination] = []
        if syncRun.phase.isActive {
            interrupted.append(.sync)
            syncRun.phase = .failed
            syncRun.terminalMessage = "Backend connection ended. The run was not resumed."
        }
        if freeDLOperation != nil || freeDLRunID != nil {
            interrupted.append(.freeDL)
            freeDLOperation = nil
            freeDLRunID = nil
            freeDLStage = nil
            freeDLStatus = "Backend connection ended. No Free DL step was resumed or replayed."
        }
        if interruptPhoneLibrary() {
            interrupted.append(.phoneLibrary)
        }
        if rekordboxOperation != nil || rekordboxRunID != nil {
            interrupted.append(.rekordbox)
            rekordboxOperation = nil
            rekordboxRunID = nil
            rekordboxStatus = "Backend connection ended. No apply was resumed or replayed."
        }
        if playlistActiveRunID != nil {
            interrupted.append(.playlists)
            playlistActiveRunID = nil
            playlistStatus = PlaylistStatus(
                message: "Backend connection ended. The previous valid snapshot was preserved.",
                severity: .error,
                preservedPreviousSnapshot: true
            )
        }
        pendingPrompt = nil
        notResumed.formUnion(interrupted)
        backendRecovery = BackendRecovery(
            message: "The backend connection ended. Restart to reconnect; no write operation was replayed.",
            interrupted: interrupted
        )
    }

    private func clearSessionState() {
        syncCancellationWatchdog?.cancel()
        client = nil
        initialization = nil
        doctor = nil
        credentials = []
        syncSources = []
        syncSourceOptions = [:]
        syncRun = SyncRunState()
        selectedSyncSourceID = nil
        syncSidebarSelectionIsExplicit = false
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
        freeDLPromotionOverrides = [:]
        freeDLPromotionApplied = false
        freeDLStage = nil
        freeDLStatus = nil
        freeDLCaptureSources = [:]
        freeDLCaptureActivity = []
        rekordboxConfig = nil
        rekordboxRuntime = nil
        rekordboxInspect = nil
        rekordboxPlan = nil
        rekordboxRunID = nil
        rekordboxOperation = nil
        rekordboxStatus = nil
        rekordboxApplyBlockers = []
        rekordboxObstacle = nil
        clearPhoneLibrarySession()
        onboarding = nil
        startupAttention = nil
        configFile = nil
        configProblems = []
        configStatusMessage = nil
        configConflict = nil
        playlistOperations = [:]
        bufferedFinished = [:]
        planSelectionOverrides = [:]
        planCursorBySource = [:]
        pendingPrompt = nil
    }

    private func applySyncFinished(_ finished: RunFinishedNotification) {
        syncCancellationWatchdog?.cancel()
        syncRun.finish(exitCode: finished.exitCode, error: finished.error)
        planSelectionOverrides = [:]
        planCursorBySource = [:]
    }

    private func routeFinished(_ finished: RunFinishedNotification) {
        if finished.runID == syncRun.runID || (syncRun.runID == nil && syncRun.phase.isActive) {
            syncRun.registerRunID(finished.runID)
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
        if finished.runID == phoneLibraryRunID || (phoneLibraryRunID == nil && phoneLibraryOperation != nil) {
            phoneLibraryRunID = finished.runID
            applyPhoneLibraryFinished(finished)
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

    /// Internal because the Phone Library workflow lives in another file.
    func consumeBufferedFinished(_ runID: String) {
        guard let finished = bufferedFinished.removeValue(forKey: runID) else { return }
        if let operation = playlistOperations.removeValue(forKey: runID) {
            applyPlaylistFinished(finished, operation: operation)
        } else if freeDLOperation != nil {
            applyFreeDLFinished(finished)
        } else if rekordboxOperation != nil {
            applyRekordboxFinished(finished)
        } else if phoneLibraryOperation != nil {
            applyPhoneLibraryFinished(finished)
        } else {
            bufferedFinished[runID] = finished
        }
    }

    /// Internal so the failure-surfacing behaviour can be tested directly. It is
    /// the one handler whose whole job is what it says when things go wrong.
    func applyPlaylistFinished(_ finished: RunFinishedNotification, operation: PlaylistOperation) {
        playlistActiveRunID = nil
        switch operation {
        case .providerList:
            if finished.exitCode == 0,
               let result = finished.result,
               let listing = try? decode(ProviderPlaylistListResult.self, from: result) {
                providerPlaylists = listing.playlists
                playlistStatus = PlaylistStatus(
                    message: "Loaded \(listing.playlists.count) playlists from Music.",
                    severity: .ok
                )
            } else if finished.exitCode == 130 {
                playlistStatus = PlaylistStatus(
                    message: "Provider listing canceled.",
                    severity: .warn,
                    preservedPreviousSnapshot: true
                )
            } else {
                // The backend already produces an actionable message — a revoked
                // Automation grant names System Settings. Discarding it made that
                // whole class of failure invisible.
                playlistStatus = PlaylistStatus(
                    message: Self.playlistFailureDetail(
                        finished,
                        fallback: "Provider listing failed with exit code \(finished.exitCode).",
                        undecodable: "Provider listing returned a result the app could not read."
                    ),
                    severity: .error,
                    preservedPreviousSnapshot: true
                )
            }
        case .refresh:
            if finished.exitCode == 0,
               let result = finished.result,
               let refresh = try? decode(PlaylistRefreshResult.self, from: result) {
                playlistStatus = PlaylistStatus(
                    message: "Snapshot refreshed: +\(refresh.changes.added), −\(refresh.changes.removed), \(refresh.changes.kept) unchanged.",
                    severity: .ok
                )
                Task { await loadPlaylists() }
            } else {
                // C11 — a failed or canceled refresh never discards the last
                // valid snapshot, and the message says so rather than reading
                // as a generic failure. The backend's own reason leads, so the
                // preservation guarantee reads as reassurance rather than as
                // the entire explanation.
                let detail = finished.exitCode == 130
                    ? "Refresh canceled."
                    : Self.playlistFailureDetail(
                        finished,
                        fallback: "Refresh failed with exit code \(finished.exitCode).",
                        undecodable: "Refresh returned a result the app could not read."
                    )
                playlistStatus = PlaylistStatus(
                    message: "\(detail) The previous valid snapshot was preserved.",
                    severity: finished.exitCode == 130 ? .warn : .error,
                    preservedPreviousSnapshot: true
                )
            }
        }
    }

    /// The backend message wins whenever there is one, matching how the
    /// Rekordbox handler reports. A zero exit code that still failed to decode
    /// is not a backend failure, so it gets its own wording instead of a
    /// nonsensical "failed with exit code 0".
    static func playlistFailureDetail(
        _ finished: RunFinishedNotification,
        fallback: String,
        undecodable: String
    ) -> String {
        if let error = finished.error?.trimmingCharacters(in: .whitespacesAndNewlines), !error.isEmpty {
            return error.hasSuffix(".") || error.hasSuffix("!") || error.hasSuffix("?") ? error : error + "."
        }
        return finished.exitCode == 0 ? undecodable : fallback
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
            freeDLStatus = .failure(error)
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
            freeDLStatus = finished.exitCode == 130
                ? .canceled("Operation canceled; existing buffer and plans were left intact.")
                : .failure(finished.error ?? "Free DL operation failed with exit code \(finished.exitCode).")
            return
        }
        guard let result = finished.result else {
            freeDLStatus = "Operation completed."
            return
        }
        switch operation {
        case .planning:
            if var plan = try? decode(FreeDLCapturePlan.self, from: result) {
                reapplyFreeDLOverrides(to: &plan)
                freeDLCapturePlan = plan
                freeDLRows = plan.rows
                freeDLStatus = "Capture plan ready."
            }
        case .capture:
            freeDLCaptureRunID = freeDLCapturePlan?.runID
            freeDLStatus = "Capture completed. Build a promotion plan when ready."
        case .promotionBuild:
            if var plan = try? decode(FreeDLPromotionPlan.self, from: result) {
                for index in plan.rows.indices {
                    if let selected = freeDLPromotionOverrides[plan.rows[index].id] {
                        plan.rows[index].selected = selected && plan.rows[index].isApplicable
                    }
                }
                freeDLPromotionPlan = plan
                freeDLPromotionApplied = false
                freeDLStatus = "Promotion plan ready for review."
            } else {
                freeDLPromotionPlan = nil
                freeDLStatus = "Promotion plan could not be decoded."
            }
        case .promotionApply:
            freeDLPromotionApplied = true
            freeDLStatus = "Promotion apply completed."
        case nil:
            break
        }
    }

    private func applyRekordboxFinished(_ finished: RunFinishedNotification) {
        let operation = rekordboxOperation
        rekordboxRunID = nil
        rekordboxOperation = nil
        if finished.exitCode != 0 {
            rekordboxStatus = finished.exitCode == 130
                ? .canceled("Operation canceled. No apply was resumed or replayed.")
                : .failure(finished.error ?? "Rekordbox operation failed with exit code \(finished.exitCode).")
            // C10 — "Rekordbox is running" is only ever reported by a run that
            // reached `CheckRekordboxClosed`; the protocol exposes no process
            // state before then. Inspect and apply are the two that reach it,
            // so their failures are the only honest source for the gate.
            if finished.exitCode != 130, let message = finished.error {
                switch operation {
                case .inspect, .apply:
                    rekordboxObstacle = RekordboxObstacle(backendMessage: message)
                default:
                    break
                }
            }
            return
        }
        // A step that reached the backend and succeeded proves the process and
        // integrity gates it passed through, so a stale obstacle is cleared.
        switch operation {
        case .inspect, .apply:
            rekordboxObstacle = nil
        default:
            break
        }
        switch operation {
        case .ensure:
            rekordboxStatus = "Managed runtime is ready."
            Task { await loadRekordbox() }
        case .reset:
            rekordboxStatus = "Managed runtime reset. Ensure it before inspection or planning."
            Task { await loadRekordbox() }
        case .inspect:
            if let result = finished.result {
                rekordboxInspect = try? decode(RekordboxInspectResult.self, from: result)
            }
            rekordboxStatus = rekordboxInspect.map {
                WorkflowStatus(
                    message: "Read-only inspection found \($0.playlists.count) playlists and \($0.contents.count) tracks."
                )
            } ?? .failure("Inspection result could not be decoded.")
        case .plan:
            if let result = finished.result,
               let decoded = try? decode(RekordboxPlanResult.self, from: result) {
                rekordboxPlan = RekordboxPlanPresentation(decoded.plan)
                rekordboxApplyBlockers = rekordboxPlan?.blockers ?? []
                rekordboxStatus = rekordboxPlan == nil
                    ? .failure("Plan result could not be decoded.")
                    : WorkflowStatus(message: "Checksummed plan ready at \(decoded.planPath).")
            }
        case .apply(let dryRun):
            rekordboxStatus = dryRun
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
        try decodeWire(type, from: value)
    }

    /// Shared with the Phone Library extension, which lives in another file and
    /// so cannot reach a private helper.
    func decodeWire<T: Decodable>(_ type: T.Type, from value: JSONValue) throws -> T {
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
